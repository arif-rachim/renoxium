package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // daily_at time zones work without the host's tz database

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// Jobs run on live apps only. Their last run is stored, so a restart neither
// skips nor repeats a run, and a newly published job first runs after its
// interval or at its next daily time, not at once.

// FreePlanMinInterval is the shortest "every" on the Free plan; longer
// schedules are kept, shorter ones run this often.
const FreePlanMinInterval = 5 * time.Minute

// DefaultJobTimezone is used when a job has no timezone.
const DefaultJobTimezone = "Asia/Dubai"

// JobState is a job's schedule and last run, for the owner's app page.
type JobState struct {
	Name     string
	Schedule string
	LastRun  time.Time
	Status   string // "" before the first run, then ok or error
	Message  string
	NextRun  time.Time
}

type liveJobs struct {
	version int
	jobs    []appdef.Job
}

type jobCache struct {
	mu      sync.Mutex
	apps    map[string]liveJobs
	running sync.Map // owner -> true while that account's due jobs run
}

func (p *Platform) ensureJobTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS job_runs (app TEXT NOT NULL, job TEXT NOT NULL, last_run INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT '', PRIMARY KEY (app, job))`)
	return err
}

// RunJobs checks for due jobs every interval (30 seconds when zero) until
// ctx ends.
func (p *Platform) RunJobs(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 30 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		p.runDueJobs(ctx, time.Now(), false)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// RunDueJobs runs every job that is due at now and waits for them.
func (p *Platform) RunDueJobs(ctx context.Context, now time.Time) int {
	return p.runDueJobs(ctx, now, true)
}

// runDueJobs starts every due job. The scheduler does not wait: a slow
// job of one account must not delay the next check for all the others.
func (p *Platform) runDueJobs(ctx context.Context, now time.Time, wait bool) int {
	rows, err := p.db.Query(`SELECT name, live_version FROM apps WHERE live_version > 0 AND suspended = ''`)
	if err != nil {
		slog.Error("jobs: list apps", "err", err)
		return 0
	}
	type appVersion struct {
		app     string
		version int
	}
	var live []appVersion
	for rows.Next() {
		var a appVersion
		if rows.Scan(&a.app, &a.version) == nil {
			live = append(live, a)
		}
	}
	rows.Close()

	// Each account's due jobs run one after another on their own queue, and
	// the queues share a few slots, so an account with many slow jobs waits
	// for itself, not ahead of everyone else. An account whose queue from an
	// earlier check is still running is skipped this time.
	type dueJob struct {
		app string
		job appdef.Job
	}
	queues := map[string][]dueJob{}
	started := 0
	for _, a := range live {
		for _, j := range p.jobsOf(a.app, a.version) {
			if p.jobDue(a.app, j, now) {
				owner := p.ownerOf(a.app)
				queues[owner] = append(queues[owner], dueJob{a.app, j})
			}
		}
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(1, p.cfg.ScriptsPerOwner))
	for owner, queue := range queues {
		if _, busy := p.jobs.running.LoadOrStore(owner, true); busy {
			continue
		}
		started += len(queue)
		wg.Add(1)
		go func(owner string, queue []dueJob) {
			defer wg.Done()
			defer p.jobs.running.Delete(owner)
			for _, d := range queue {
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				p.runLiveJob(ctx, d.app, d.job, now)
				<-slots
			}
		}(owner, queue)
	}
	if wait {
		wg.Wait()
	}
	return started
}

// jobsOf returns the jobs of an app's live version, cached per version so
// apps are not loaded just to find they have none.
func (p *Platform) jobsOf(app string, version int) []appdef.Job {
	p.jobs.mu.Lock()
	defer p.jobs.mu.Unlock()
	if p.jobs.apps == nil {
		p.jobs.apps = map[string]liveJobs{}
	}
	if c, ok := p.jobs.apps[app]; ok && c.version == version {
		return c.jobs
	}
	def, _, err := p.Definition(app, version)
	if err != nil {
		return nil
	}
	p.jobs.apps[app] = liveJobs{version: version, jobs: def.Jobs}
	return def.Jobs
}

func (p *Platform) jobInterval(app string, j appdef.Job) time.Duration {
	d, _ := j.JobInterval()
	if p.cfg.OwnerPlan != nil && p.appQuotaPlan(app) == planFree && d < FreePlanMinInterval {
		d = FreePlanMinInterval
	}
	return d
}

func jobLocation(j appdef.Job) *time.Location {
	name := j.Timezone
	if name == "" {
		name = DefaultJobTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

// lastSlot is the most recent daily_at time at or before now.
func lastSlot(j appdef.Job, now time.Time) time.Time {
	at, _ := time.Parse("15:04", j.DailyAt)
	local := now.In(jobLocation(j))
	slot := time.Date(local.Year(), local.Month(), local.Day(), at.Hour(), at.Minute(), 0, 0, local.Location())
	if slot.After(local) {
		slot = slot.AddDate(0, 0, -1)
	}
	return slot
}

func (p *Platform) nextRun(app string, j appdef.Job, last time.Time) time.Time {
	if j.Every != "" {
		return last.Add(p.jobInterval(app, j))
	}
	return lastSlot(j, last).AddDate(0, 0, 1)
}

func (p *Platform) jobDue(app string, j appdef.Job, now time.Time) bool {
	var last int64
	err := p.db.QueryRow(`SELECT last_run FROM job_runs WHERE app = ? AND job = ?`, app, j.Name).Scan(&last)
	if err != nil {
		// First seen: start counting from now.
		p.db.Exec(`INSERT OR IGNORE INTO job_runs (app, job, last_run) VALUES (?, ?, ?)`, app, j.Name, now.Unix())
		return false
	}
	return !now.Before(p.nextRun(app, j, time.Unix(last, 0)))
}

func (p *Platform) runLiveJob(ctx context.Context, app string, j appdef.Job, now time.Time) {
	st, err := p.loadStage(app, Live)
	if err != nil || st == nil {
		return
	}
	text, err := p.runJob(ctx, st, j, now, newRequestID())
	status, msg := "ok", text
	if err != nil {
		status, msg = "error", err.Error()
	}
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	p.db.Exec(`UPDATE job_runs SET last_run = ?, status = ?, message = ? WHERE app = ? AND job = ?`, now.Unix(), status, msg, app, j.Name)
}

// runJob runs a job's script once on a stage and logs it.
func (p *Platform) runJob(ctx context.Context, st *stage, j appdef.Job, scheduled time.Time, rid string) (string, error) {
	start := time.Now()
	host := &scriptHost{st: st, p: p, rid: rid}
	defer host.runDeferred(ctx) // on_payment runs from money calls, after the slot is released
	release, err := p.acquireSlot(ctx, st)
	if err != nil {
		err = fmt.Errorf("%v; the job runs again at its next time", err)
		p.event(st, "server", "warn", rid, "job "+j.Name+" skipped: "+err.Error())
		return "", err
	}
	defer release()
	req := sandbox.Request{Method: "JOB", Path: "/_rx/jobs/" + j.Name, Origin: p.URL(st.app, st.name),
		Body:   map[string]any{"job": j.Name, "scheduled_at": scheduled.UTC().Format("2006-01-02 15:04:05")},
		Params: map[string]string{}, Query: map[string]string{}, Form: map[string]string{}, FormAll: map[string][]string{}}
	resp, err := p.runScript(ctx, st, "job "+j.Name, st.def.Scripts[j.Script], req, host)
	took := time.Since(start).Round(time.Microsecond)
	jc := p.meter.counters(st.app, st.name)
	jc.add(mJobs, 1)
	jc.add(mJobUS, time.Since(start).Microseconds())
	if err != nil {
		var se *sandbox.ScriptError
		if errors.As(err, &se) {
			err = fmt.Errorf("script %s: %s", j.Script, se.Message)
		}
		p.event(st, "server", "error", rid, fmt.Sprintf("job %s -> failed in %s: %v", j.Name, took, err))
		return "", err
	}
	text := resp.Text
	if resp.HasJSON {
		text = fmt.Sprint(resp.JSON)
	}
	msg := fmt.Sprintf("job %s -> ok in %s", j.Name, took)
	if t := strings.TrimSpace(text); t != "" {
		msg += ": " + t
	}
	p.event(st, "server", "info", rid, msg)
	return text, nil
}

// JobStates lists the live app's jobs with their last run.
func (p *Platform) JobStates(app string) []JobState {
	var version int
	if p.db.QueryRow(`SELECT live_version FROM apps WHERE name = ?`, app).Scan(&version) != nil || version == 0 {
		return nil
	}
	var out []JobState
	for _, j := range p.jobsOf(app, version) {
		s := JobState{Name: j.Name}
		if j.Every != "" {
			s.Schedule = "every " + p.jobInterval(app, j).String()
		} else {
			tz := j.Timezone
			if tz == "" {
				tz = DefaultJobTimezone
			}
			s.Schedule = "daily at " + j.DailyAt + " (" + tz + ")"
		}
		var last int64
		if p.db.QueryRow(`SELECT last_run, status, message FROM job_runs WHERE app = ? AND job = ?`, app, j.Name).Scan(&last, &s.Status, &s.Message) == nil {
			s.LastRun = time.Unix(last, 0)
			s.NextRun = p.nextRun(app, j, s.LastRun)
		}
		if s.Status == "" {
			s.LastRun = time.Time{}
		}
		out = append(out, s)
	}
	return out
}

package platform

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// The resource meter: what every app stage uses of the server, so the
// operator can see which apps slow the others down and which cost more
// than they bring in (/operator/resources).
//
// Counting happens in memory, per app stage and hour, in atomic counters:
// a request adds two numbers and takes no lock and no database write. Every
// meterFlushEvery the counts are added to app_usage_hourly, one row per app
// stage and hour; rows older than meterKeepDays are deleted. Reports merge
// what is still in memory, so the current hour is always complete.

// Meter fields. The order is the column order of app_usage_hourly.
const (
	mRequests         = iota // requests answered on the app's hosts
	mBytesOut                // response bytes written
	mRuns                    // script runs (requests, jobs, hooks, tools)
	mScriptUS                // time runs held a worker (µs)
	mCPUUS                   // worker CPU time (µs); wall time where it is not measured
	mDBUS                    // time the server spent on the run's db.query / db.exec (µs)
	mHeapPeak                // largest worker heap seen in a run (bytes), max
	mTimeouts                // runs stopped at the time limit
	mMemKills                // runs stopped at a memory limit
	mRestarts                // workers killed by the server and started again
	mScriptErrors            // runs that failed for another reason
	mSlotWaitUS              // time waiting for the app's or account's own script slots (µs)
	mWorkerWaitUS            // time waiting for a free worker, held by other runs (µs)
	mWaits                   // runs that waited more than 1 ms
	mRefusedApp              // runs refused after 3 s: the app's own limit
	mRefusedOwner            // runs refused after 3 s: the account's limit
	mRefusedThrottle         // runs refused by an operator throttle
	mCausedWaitUS            // worker wait of other apps' runs while this app held workers (µs)
	mHeldWhileWaiting        // how often this app held a worker another app was waiting for
	mPeakScripts             // most runs at the same time, max ("*" row: busy workers)
	mRTPeak                  // most open realtime connections, max
	mRTConnSec               // realtime connection time (s)
	mRTMessages              // realtime.publish calls
	mRTDelivered             // realtime messages delivered to browsers
	mFetches                 // http.fetch calls made
	mEmails                  // emails sent (visitor, app and owner emails)
	mWhatsApp                // WhatsApp messages sent
	mJobs                    // scheduled job runs
	mJobUS                   // time in job runs (µs)
	nMeter
)

var meterColumns = [nMeter]string{"requests", "bytes_out", "runs", "script_us", "cpu_us", "db_us", "heap_peak", "timeouts", "mem_kills",
	"restarts", "script_errors", "slot_wait_us", "worker_wait_us", "waits", "refused_app", "refused_owner", "refused_throttle",
	"caused_wait_us", "held_while_waiting", "peak_scripts", "rt_peak", "rt_conn_sec", "rt_messages", "rt_delivered", "fetches",
	"emails", "whatsapp", "jobs", "job_us"}

func meterIsMax(f int) bool { return f == mHeapPeak || f == mPeakScripts || f == mRTPeak }

// PlatformRow is the app name of the server-wide row: busy workers.
const PlatformRow = "*"

const (
	meterKeepDays  = 90
	meterNamesCap  = 50 // routes, jobs and tools per app stage and hour; more count as "(other)"
	meterRecentMin = 16 // minutes of contention kept for alerts
)

var meterFlushEvery = 5 * time.Minute

type meterKey struct{ app, stage string }

type meterCounters struct {
	v     [nMeter]atomic.Int64
	nmu   sync.Mutex
	names map[string]*NameUsage
}

func (c *meterCounters) add(f int, n int64) { c.v[f].Add(n) }

func (c *meterCounters) max(f int, n int64) {
	for {
		old := c.v[f].Load()
		if n <= old || c.v[f].CompareAndSwap(old, n) {
			return
		}
	}
}

type meterBucket struct {
	hour int64    // Unix time of the hour's start
	apps sync.Map // meterKey -> *meterCounters
}

func (b *meterBucket) counters(k meterKey) *meterCounters {
	if c, ok := b.apps.Load(k); ok {
		return c.(*meterCounters)
	}
	c, _ := b.apps.LoadOrStore(k, &meterCounters{})
	return c.(*meterCounters)
}

type meter struct {
	cur     atomic.Pointer[meterBucket]
	mu      sync.Mutex     // retires buckets and writes them; readers take it too
	retired []*meterBucket // retired buckets not yet written
	now     func() time.Time

	hmu     sync.Mutex
	holders map[meterKey]int // runs holding a worker, per app stage
	holding int
	recent  [meterRecentMin]contentionMinute
}

// contentionMinute is one minute of worker waits and memory kills, kept in
// memory for the monitor's alerts.
type contentionMinute struct {
	minute int64
	wait   int64            // µs of worker wait over all apps
	caused map[string]int64 // app -> µs of others' wait it caused
	kills  map[string]int   // app -> memory kills
}

func (m *meter) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func hourOf(t time.Time) int64 { return t.Unix() - t.Unix()%3600 }

// counters returns the current hour's counters of an app stage.
func (m *meter) counters(app, stage string) *meterCounters {
	h := hourOf(m.clock())
	b := m.cur.Load()
	if b == nil || b.hour != h {
		b = m.rotate(h)
	}
	return b.counters(meterKey{app, stage})
}

// rotate starts a new bucket for hour h, unless another call already did.
func (m *meter) rotate(h int64) *meterBucket {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.cur.Load()
	if b != nil && b.hour == h {
		return b
	}
	nb := &meterBucket{hour: h}
	m.cur.Store(nb)
	if b != nil {
		m.retired = append(m.retired, b)
	}
	return nb
}

func (p *Platform) ensureMeterTables() error {
	cols := make([]string, nMeter)
	for i, c := range meterColumns {
		cols[i] = c + " INTEGER NOT NULL DEFAULT 0"
	}
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS app_usage_hourly (app TEXT NOT NULL, stage TEXT NOT NULL, hour INTEGER NOT NULL, ` +
			strings.Join(cols, ", ") + `, PRIMARY KEY (app, stage, hour))`,
		`CREATE INDEX IF NOT EXISTS app_usage_hourly_hour ON app_usage_hourly (hour)`,
		`CREATE TABLE IF NOT EXISTS app_usage_names (app TEXT NOT NULL, stage TEXT NOT NULL, hour INTEGER NOT NULL, name TEXT NOT NULL,
			runs INTEGER NOT NULL, wall_us INTEGER NOT NULL, cpu_us INTEGER NOT NULL, PRIMARY KEY (app, stage, hour, name))`,
		`CREATE TABLE IF NOT EXISTS operator_audit (id INTEGER PRIMARY KEY, at TEXT NOT NULL, operator TEXT NOT NULL, app TEXT NOT NULL,
			action TEXT NOT NULL, detail TEXT NOT NULL)`,
	} {
		if _, err := p.db.Exec(stmt); err != nil {
			return err
		}
	}
	p.meter.holders = map[meterKey]int{}
	return p.ensureThrottleTable()
}

// flushMeter writes the buckets retired at the previous flush and retires
// the current one, so a counter still being added to when its bucket was
// retired is not lost. With all set (shutdown, tests) everything is
// written now.
func (p *Platform) flushMeter(all bool) {
	m := &p.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	write := m.retired
	m.retired = nil
	if cur := m.cur.Swap(nil); cur != nil {
		if all {
			write = append(write, cur)
		} else {
			m.retired = append(m.retired, cur)
		}
	}
	if len(write) == 0 {
		return
	}
	set := make([]string, nMeter)
	for i, c := range meterColumns {
		if meterIsMax(i) {
			set[i] = c + " = max(" + c + ", excluded." + c + ")"
		} else {
			set[i] = c + " = " + c + " + excluded." + c
		}
	}
	upsert := `INSERT INTO app_usage_hourly (app, stage, hour, ` + strings.Join(meterColumns[:], ", ") + `) VALUES (?, ?, ?` +
		strings.Repeat(", ?", nMeter) + `) ON CONFLICT (app, stage, hour) DO UPDATE SET ` + strings.Join(set, ", ")
	tx, err := p.db.Begin()
	if err != nil {
		m.retired = append(write, m.retired...) // try again next time
		return
	}
	defer tx.Rollback()
	args := make([]any, 3+nMeter)
	for _, b := range write {
		b.apps.Range(func(k, v any) bool {
			key, c := k.(meterKey), v.(*meterCounters)
			args[0], args[1], args[2] = key.app, key.stage, b.hour
			for i := range nMeter {
				args[3+i] = c.v[i].Load()
			}
			tx.Exec(upsert, args...)
			c.nmu.Lock()
			for name, n := range c.names {
				tx.Exec(`INSERT INTO app_usage_names (app, stage, hour, name, runs, wall_us, cpu_us) VALUES (?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (app, stage, hour, name) DO UPDATE SET runs = runs + excluded.runs, wall_us = wall_us + excluded.wall_us,
					cpu_us = cpu_us + excluded.cpu_us`, key.app, key.stage, b.hour, name, n.Runs, n.WallUS, n.CPUUS)
			}
			c.nmu.Unlock()
			return true
		})
	}
	if tx.Commit() != nil {
		m.retired = append(write, m.retired...)
	}
}

// RunUsage writes the resource meter every meterFlushEvery until ctx ends.
func (p *Platform) RunUsage(ctx context.Context) {
	t := time.NewTicker(meterFlushEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			p.flushMeter(true)
			return
		case <-t.C:
			p.flushMeter(false)
		}
	}
}

func (p *Platform) cleanMeter() {
	cut := hourOf(p.meter.clock().AddDate(0, 0, -meterKeepDays))
	p.db.Exec(`DELETE FROM app_usage_hourly WHERE hour < ?`, cut)
	p.db.Exec(`DELETE FROM app_usage_names WHERE hour < ?`, cut)
	p.db.Exec(`DELETE FROM operator_audit WHERE at < ?`, p.meter.clock().AddDate(0, 0, -365).UTC().Format(time.RFC3339))
	p.cleanThrottles()
}

// --- recording ---

func (p *Platform) meterAdd(app, stage string, f int, n int64) {
	p.meter.counters(app, stage).add(f, n)
}

func (p *Platform) meterMax(app, stage string, f int, n int64) {
	p.meter.counters(app, stage).max(f, n)
}

// meterWriter counts a request and the bytes written for it.
type meterWriter struct {
	http.ResponseWriter
	n int64
}

func (w *meterWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}

func (w *meterWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush keeps streamed answers (realtime, MCP) streaming through the wrapper.
func (w *meterWriter) Flush() { http.NewResponseController(w.ResponseWriter).Flush() }

func (p *Platform) meterRequest(app, stage string, bytes int64) {
	c := p.meter.counters(app, stage)
	c.add(mRequests, 1)
	c.add(mBytesOut, bytes)
}

// hold notes that a run of an app stage got a worker.
func (m *meter) hold(k meterKey) (app, all int) {
	m.hmu.Lock()
	defer m.hmu.Unlock()
	m.holders[k]++
	m.holding++
	return m.holders[k], m.holding
}

func (m *meter) unhold(k meterKey) {
	m.hmu.Lock()
	defer m.hmu.Unlock()
	if m.holders[k]--; m.holders[k] <= 0 {
		delete(m.holders, k)
	}
	m.holding--
}

// holdersExcept lists the runs holding workers now, one entry per run,
// leaving out the app that is about to wait.
func (m *meter) holdersExcept(app string) []meterKey {
	m.hmu.Lock()
	defer m.hmu.Unlock()
	var out []meterKey
	for k, n := range m.holders {
		if k.app == app {
			continue
		}
		for range n {
			out = append(out, k)
		}
	}
	return out
}

// Busy is how many script runs hold a worker now.
func (p *Platform) Busy() int {
	p.meter.hmu.Lock()
	defer p.meter.hmu.Unlock()
	return p.meter.holding
}

// Workers reports busy and total script workers when the sandbox is a
// worker pool; total is 0 otherwise.
func (p *Platform) Workers() (busy, total int) {
	if pool, ok := p.sb.(interface{ Busy() (int, int) }); ok {
		return pool.Busy()
	}
	return p.Busy(), 0
}

// runScript runs one of an app's scripts with the meter attached. what
// names it for the "top routes, jobs and tools" list, e.g. "GET /order".
func (p *Platform) runScript(ctx context.Context, st *stage, what, src string, req sandbox.Request, host sandbox.Host) (sandbox.Response, error) {
	k := meterKey{st.app, st.name}
	stats := &sandbox.RunStats{}
	var blocked []meterKey
	held := false
	stats.Queued = func() { blocked = p.meter.holdersExcept(st.app) }
	stats.Started = func() {
		if held {
			return
		}
		held = true
		mine, all := p.meter.hold(k)
		p.meterMax(st.app, st.name, mPeakScripts, int64(mine))
		p.meterMax(PlatformRow, "", mPeakScripts, int64(all))
	}
	resp, err := p.sb.Run(sandbox.WithStats(ctx, stats), src, req, host)
	if held {
		p.meter.unhold(k)
	}
	p.recordRun(k, what, stats, err, blocked)
	return resp, err
}

func (p *Platform) recordRun(k meterKey, what string, s *sandbox.RunStats, err error, blocked []meterKey) {
	c := p.meter.counters(k.app, k.stage)
	wall := s.Wall.Microseconds()
	cpu := wall
	if s.CPUKnown {
		cpu = s.CPU.Microseconds()
	}
	c.add(mRuns, 1)
	c.add(mScriptUS, wall)
	c.add(mCPUUS, cpu)
	c.add(mDBUS, s.HostDB.Microseconds())
	if s.HeapPeak > 0 {
		c.max(mHeapPeak, s.HeapPeak)
	}
	switch {
	case s.Stopped == "time":
		c.add(mTimeouts, 1)
	case s.Stopped == "memory":
		c.add(mMemKills, 1)
	case err != nil:
		c.add(mScriptErrors, 1)
	}
	if s.Restarted {
		c.add(mRestarts, 1)
	}
	wait := s.QueueWait.Microseconds()
	if wait > 0 {
		c.add(mWorkerWaitUS, wait)
		if wait > 1000 {
			c.add(mWaits, 1)
		}
	}
	p.meterAdd(PlatformRow, "", mScriptUS, wall)
	p.meterAdd(PlatformRow, "", mCPUUS, cpu)
	if what != "" {
		c.nmu.Lock()
		if c.names == nil {
			c.names = map[string]*NameUsage{}
		}
		n := c.names[what]
		if n == nil {
			if len(c.names) >= meterNamesCap {
				what = "(other)"
				n = c.names[what]
			}
			if n == nil {
				n = &NameUsage{Name: what}
				c.names[what] = n
			}
		}
		n.Runs++
		n.WallUS += wall
		n.CPUUS += cpu
		c.nmu.Unlock()
	}
	// The wait is shared out over the runs that held the workers.
	var caused map[string]int64
	if wait > 0 && len(blocked) > 0 {
		share := wait / int64(len(blocked))
		caused = map[string]int64{}
		for _, b := range blocked {
			hc := p.meter.counters(b.app, b.stage)
			hc.add(mCausedWaitUS, share)
			hc.add(mHeldWhileWaiting, 1)
			caused[b.app] += share
		}
	}
	if wait > 0 || s.Stopped == "memory" {
		p.meter.noteRecent(k.app, wait, caused, s.Stopped == "memory")
	}
}

func (m *meter) noteRecent(app string, wait int64, caused map[string]int64, kill bool) {
	minute := m.clock().Unix() / 60
	m.hmu.Lock()
	defer m.hmu.Unlock()
	r := &m.recent[minute%meterRecentMin]
	if r.minute != minute {
		*r = contentionMinute{minute: minute, caused: map[string]int64{}, kills: map[string]int{}}
	}
	r.wait += wait
	for a, n := range caused {
		r.caused[a] += n
	}
	if kill {
		r.kills[app]++
	}
}

// Contention is the last minutes of worker waits, for the monitor.
type Contention struct {
	Minutes  int              // minutes covered
	Wait     time.Duration    // worker wait of all runs
	Caused   map[string]int64 // app -> µs of that wait it caused
	Active   map[string]int   // app -> minutes in which it caused wait
	MemKills map[string]int   // app -> runs stopped at a memory limit
}

// RecentContention sums the last minutes (at most 15) of worker waits and
// memory kills.
func (p *Platform) RecentContention(minutes int) Contention {
	m := &p.meter
	minutes = min(max(minutes, 1), meterRecentMin-1)
	nowMin := m.clock().Unix() / 60
	out := Contention{Minutes: minutes, Caused: map[string]int64{}, Active: map[string]int{}, MemKills: map[string]int{}}
	m.hmu.Lock()
	defer m.hmu.Unlock()
	for _, r := range m.recent {
		if r.minute <= nowMin-int64(minutes) || r.minute > nowMin {
			continue
		}
		out.Wait += time.Duration(r.wait) * time.Microsecond
		for a, n := range r.caused {
			out.Caused[a] += n
			if n > 0 {
				out.Active[a]++
			}
		}
		for a, n := range r.kills {
			out.MemKills[a] += n
		}
	}
	return out
}

// --- reading ---

// Usage is what an app stage (or all of them) used over a period. Times
// are in microseconds; see the m* constants for each field.
type Usage struct {
	Requests, BytesOut, Runs, ScriptUS, CPUUS, DBUS, HeapPeak, Timeouts, MemKills, Restarts, ScriptErrors      int64
	SlotWaitUS, WorkerWaitUS, Waits, RefusedApp, RefusedOwner, RefusedThrottle, CausedWaitUS, HeldWhileWaiting int64
	PeakScripts, RTPeak, RTConnSec, RTMessages, RTDelivered, Fetches, Emails, WhatsApp, Jobs, JobUS            int64
}

func (u *Usage) fields() [nMeter]*int64 {
	return [nMeter]*int64{&u.Requests, &u.BytesOut, &u.Runs, &u.ScriptUS, &u.CPUUS, &u.DBUS, &u.HeapPeak, &u.Timeouts, &u.MemKills,
		&u.Restarts, &u.ScriptErrors, &u.SlotWaitUS, &u.WorkerWaitUS, &u.Waits, &u.RefusedApp, &u.RefusedOwner, &u.RefusedThrottle,
		&u.CausedWaitUS, &u.HeldWhileWaiting, &u.PeakScripts, &u.RTPeak, &u.RTConnSec, &u.RTMessages, &u.RTDelivered, &u.Fetches,
		&u.Emails, &u.WhatsApp, &u.Jobs, &u.JobUS}
}

// Add adds o to u (peaks take the larger value).
func (u *Usage) Add(o Usage) {
	a, b := u.fields(), o.fields()
	for i := range nMeter {
		if meterIsMax(i) {
			*a[i] = max(*a[i], *b[i])
		} else {
			*a[i] += *b[i]
		}
	}
}

func (u *Usage) addCounters(c *meterCounters) {
	a := u.fields()
	for i := range nMeter {
		v := c.v[i].Load()
		if meterIsMax(i) {
			*a[i] = max(*a[i], v)
		} else {
			*a[i] += v
		}
	}
}

// Refused is every run refused for a concurrency limit or a throttle.
func (u Usage) Refused() int64 { return u.RefusedApp + u.RefusedOwner + u.RefusedThrottle }

// StageUsage is one app stage's usage.
type StageUsage struct {
	App, Stage string
	Usage
}

// HourUsage is one hour of usage.
type HourUsage struct {
	Hour time.Time
	Usage
}

// NameUsage is one route, job, hook or tool of an app.
type NameUsage struct {
	Name                string
	Runs, WallUS, CPUUS int64
}

// memBuckets is every bucket not yet in the database. Call with m.mu held.
func (m *meter) memBuckets() []*meterBucket {
	out := append([]*meterBucket(nil), m.retired...)
	if b := m.cur.Load(); b != nil {
		out = append(out, b)
	}
	return out
}

func scanUsage(rows *sql.Rows, extra ...any) (Usage, error) {
	var u Usage
	f := u.fields()
	dest := append([]any(nil), extra...)
	for i := range nMeter {
		dest = append(dest, f[i])
	}
	return u, rows.Scan(dest...)
}

// UsageByStage sums usage per app stage over hours starting in [from, to).
// The server-wide row (app PlatformRow) is included.
func (p *Platform) UsageByStage(from, to time.Time) ([]StageUsage, error) {
	m := &p.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	sums := make([]string, nMeter)
	for i, c := range meterColumns {
		if meterIsMax(i) {
			sums[i] = "max(" + c + ")"
		} else {
			sums[i] = "sum(" + c + ")"
		}
	}
	rows, err := p.db.Query(`SELECT app, stage, `+strings.Join(sums, ", ")+` FROM app_usage_hourly WHERE hour >= ? AND hour < ? GROUP BY app, stage`,
		hourOf(from), to.Unix())
	if err != nil {
		return nil, err
	}
	byKey := map[meterKey]*StageUsage{}
	for rows.Next() {
		var k meterKey
		u, err := scanUsage(rows, &k.app, &k.stage)
		if err != nil {
			rows.Close()
			return nil, err
		}
		byKey[k] = &StageUsage{App: k.app, Stage: k.stage, Usage: u}
	}
	rows.Close()
	for _, b := range m.memBuckets() {
		if b.hour < hourOf(from) || b.hour >= to.Unix() {
			continue
		}
		b.apps.Range(func(k, v any) bool {
			key := k.(meterKey)
			s := byKey[key]
			if s == nil {
				s = &StageUsage{App: key.app, Stage: key.stage}
				byKey[key] = s
			}
			s.addCounters(v.(*meterCounters))
			return true
		})
	}
	out := make([]StageUsage, 0, len(byKey))
	for _, s := range byKey {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].App != out[j].App {
			return out[i].App < out[j].App
		}
		return out[i].Stage < out[j].Stage
	})
	return out, nil
}

// UsageHours is an app's usage (both stages; PlatformRow for the server)
// per hour from from to to, oldest first, with empty hours included.
func (p *Platform) UsageHours(app string, from, to time.Time) ([]HourUsage, error) {
	m := &p.meter
	first, last := hourOf(from), hourOf(to)
	if last < first {
		return nil, errors.New("to is before from")
	}
	out := make([]HourUsage, (last-first)/3600+1)
	for i := range out {
		out[i].Hour = time.Unix(first+int64(i)*3600, 0).UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := p.db.Query(`SELECT hour, `+strings.Join(meterColumns[:], ", ")+` FROM app_usage_hourly WHERE app = ? AND hour >= ? AND hour <= ?`, app, first, last)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h int64
		u, err := scanUsage(rows, &h)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out[(h-first)/3600].Add(u)
	}
	rows.Close()
	for _, b := range m.memBuckets() {
		if b.hour < first || b.hour > last {
			continue
		}
		b.apps.Range(func(k, v any) bool {
			if k.(meterKey).app == app {
				out[(b.hour-first)/3600].addCounters(v.(*meterCounters))
			}
			return true
		})
	}
	return out, nil
}

// UsageNames lists an app's routes, jobs, hooks and tools by time held
// since from, both stages together, most first.
func (p *Platform) UsageNames(app string, from time.Time, limit int) ([]NameUsage, error) {
	m := &p.meter
	m.mu.Lock()
	defer m.mu.Unlock()
	byName := map[string]*NameUsage{}
	rows, err := p.db.Query(`SELECT name, sum(runs), sum(wall_us), sum(cpu_us) FROM app_usage_names WHERE app = ? AND hour >= ? GROUP BY name`, app, hourOf(from))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n NameUsage
		if rows.Scan(&n.Name, &n.Runs, &n.WallUS, &n.CPUUS) == nil {
			byName[n.Name] = &n
		}
	}
	rows.Close()
	for _, b := range m.memBuckets() {
		if b.hour < hourOf(from) {
			continue
		}
		b.apps.Range(func(k, v any) bool {
			if k.(meterKey).app != app {
				return true
			}
			c := v.(*meterCounters)
			c.nmu.Lock()
			for name, n := range c.names {
				t := byName[name]
				if t == nil {
					t = &NameUsage{Name: name}
					byName[name] = t
				}
				t.Runs += n.Runs
				t.WallUS += n.WallUS
				t.CPUUS += n.CPUUS
			}
			c.nmu.Unlock()
			return true
		})
	}
	out := make([]NameUsage, 0, len(byName))
	for _, n := range byName {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].WallUS != out[j].WallUS {
			return out[i].WallUS > out[j].WallUS
		}
		return out[i].Name < out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Storage is what an app keeps on disk.
type Storage struct {
	LiveDB, PreviewDB int64 // database files, write-ahead log included
	Files             int64 // images and other uploads of the owner (shared ones counted in full)
	Private           int64 // visitor uploads, both stages
}

// Total is every byte of the app.
func (s Storage) Total() int64 { return s.LiveDB + s.PreviewDB + s.Files + s.Private }

// StorageByApp measures every app's storage now.
func (p *Platform) StorageByApp() (map[string]Storage, error) {
	out := map[string]Storage{}
	rows, err := p.db.Query(`SELECT name FROM apps`)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	rows.Close()
	size := func(path string) int64 {
		var n int64
		for _, suffix := range []string{"", "-wal"} {
			if fi, err := os.Stat(path + suffix); err == nil {
				n += fi.Size()
			}
		}
		return n
	}
	for _, n := range names {
		out[n] = Storage{LiveDB: size(p.dbPath(n, Live)), PreviewDB: size(p.dbPath(n, Preview))}
	}
	if rows, err := p.db.Query(`SELECT f.app, sum(b.size) FROM app_files f JOIN blobs b ON b.hash = f.hash GROUP BY f.app`); err == nil {
		for rows.Next() {
			var app string
			var n int64
			if rows.Scan(&app, &n) == nil {
				s := out[app]
				s.Files = n
				out[app] = s
			}
		}
		rows.Close()
	}
	if rows, err := p.db.Query(`SELECT app, sum(size) FROM private_files GROUP BY app`); err == nil {
		for rows.Next() {
			var app string
			var n int64
			if rows.Scan(&app, &n) == nil {
				s := out[app]
				s.Private = n
				out[app] = s
			}
		}
		rows.Close()
	}
	return out, nil
}

// --- operator audit ---

// AuditEntry is one operator action.
type AuditEntry struct {
	At, Operator, App, Action, Detail string
}

// Audit records an operator action on an app, in the operator audit and in
// the app's own log.
func (p *Platform) Audit(operator, app, action, detail string) {
	p.db.Exec(`INSERT INTO operator_audit (at, operator, app, action, detail) VALUES (?, ?, ?, ?, ?)`, now(), operator, app, action, detail)
	l, err := p.log(app)
	if err == nil {
		msg := "Renoxium operator: " + action
		if detail != "" {
			msg += ": " + detail
		}
		l.Add(applog.Event{Stage: Live, Source: "operator", Level: "warn", Message: msg})
	}
}

// AuditLog lists operator actions, newest first; app "" lists all apps.
func (p *Platform) AuditLog(app string, limit int) ([]AuditEntry, error) {
	q := `SELECT at, operator, app, action, detail FROM operator_audit`
	args := []any{}
	if app != "" {
		q += ` WHERE app = ?`
		args = append(args, app)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := p.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.At, &a.Operator, &a.App, &a.Action, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// OperatorSettings returns the stored operator settings whose key starts
// with prefix.
func (p *Platform) OperatorSettings(prefix string) (map[string]string, error) {
	rows, err := p.db.Query(`SELECT key, value FROM operator_settings WHERE key LIKE ? ESCAPE '\'`, likeEscape(prefix)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetResourceSetting stores one of the resource page's unit prices or
// thresholds (keys "resources:...").
func (p *Platform) SetResourceSetting(key, value string) error {
	if !strings.HasPrefix(key, "resources:") || len(key) > 80 || len(value) > 40 {
		return errors.New("unknown setting")
	}
	return p.setSetting(key, value)
}

// ScriptSeries is every app's script time in µs (both stages) in n equal
// slices of the hours from from to to, for sparklines.
func (p *Platform) ScriptSeries(from, to time.Time, n int) (map[string][]int64, error) {
	m := &p.meter
	first, end := hourOf(from), to.Unix()
	span := end - first
	if n <= 0 || span <= 0 {
		return nil, errors.New("empty series")
	}
	out := map[string][]int64{}
	put := func(app string, hour, v int64) {
		if app == PlatformRow || v == 0 {
			return
		}
		i := int((hour - first) * int64(n) / span)
		if i < 0 || i >= n {
			return
		}
		s := out[app]
		if s == nil {
			s = make([]int64, n)
			out[app] = s
		}
		s[i] += v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rows, err := p.db.Query(`SELECT app, hour, sum(script_us) FROM app_usage_hourly WHERE hour >= ? AND hour < ? GROUP BY app, hour`, first, end)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var app string
		var h, v int64
		if rows.Scan(&app, &h, &v) == nil {
			put(app, h, v)
		}
	}
	rows.Close()
	for _, b := range m.memBuckets() {
		b.apps.Range(func(k, v any) bool {
			put(k.(meterKey).app, b.hour, v.(*meterCounters).v[mScriptUS].Load())
			return true
		})
	}
	return out, nil
}

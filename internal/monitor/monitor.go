// Package monitor watches the server and tells people when something needs
// attention: the operators about disk space, backups and error spikes, and
// app owners about errors in their live apps.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// Thresholds; variables so tests can change them.
var (
	DiskLowPercent      = 10.0    // alert operators below this much free space
	DiskLowBytes        = 2 << 30 // or below 2 GB
	DiskCriticalPercent = 3.0     // /healthz fails below this
	BackupMaxAge        = 26 * time.Hour
	ErrorSpike          = 200 // live server errors across all apps in one check
	ownerEvery          = 24 * time.Hour
	operatorEvery       = 6 * time.Hour
)

// Monitor checks the server every Interval.
type Monitor struct {
	Platform  *platform.Platform
	Mailer    email.Sender // nil: alerts only go to the log
	Operators []string     // email addresses for operator alerts
	DataDir   string
	// Backups is true when backup.sh runs on this server: it writes
	// DataDir/.last-backup (and .last-offsite after an off-site copy).
	Backups  bool
	SiteURL  string
	Interval time.Duration

	mu         sync.Mutex
	started    time.Time
	status     Status
	sentOp     map[string]time.Time // operator alert kind -> last sent
	sentOwner  map[string]time.Time // app -> last owner email
	ownerQueue map[string]platform.ErrorCount
}

// Status is the latest check, for the operator panel.
type Status struct {
	Checked       time.Time
	DiskFree      uint64
	DiskTotal     uint64
	DiskPercent   float64 // free
	NewestBackup  time.Time
	NewestOffsite time.Time
	Problems      []string
	Alerts        []string // recent alerts, newest first
}

// DiskFreeGB and the others format Status for templates.
func (s Status) DiskFreeGB() string  { return fmt.Sprintf("%.1f", float64(s.DiskFree)/(1<<30)) }
func (s Status) DiskTotalGB() string { return fmt.Sprintf("%.1f", float64(s.DiskTotal)/(1<<30)) }
func (s Status) BackupAge() string   { return age(s.NewestBackup) }
func (s Status) OffsiteAge() string  { return age(s.NewestOffsite) }

func age(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return time.Since(t).Round(time.Minute).String() + " ago"
}

// Run checks until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	if m.Interval == 0 {
		m.Interval = 5 * time.Minute
	}
	m.mu.Lock()
	m.started = time.Now()
	m.mu.Unlock()
	t := time.NewTicker(m.Interval)
	defer t.Stop()
	for {
		m.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Status returns the latest check.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	s.Problems = append([]string(nil), s.Problems...)
	s.Alerts = append([]string(nil), s.Alerts...)
	return s
}

// Healthy is false when the server cannot work properly (the disk is almost
// full), so external uptime checks on /healthz raise an alarm too.
func (m *Monitor) Healthy() error {
	s := m.Status()
	if !s.Checked.IsZero() && s.DiskTotal > 0 && s.DiskPercent < DiskCriticalPercent {
		return fmt.Errorf("disk almost full: %.1f%% free", s.DiskPercent)
	}
	return nil
}

// Check runs every check once.
func (m *Monitor) Check(ctx context.Context) {
	m.mu.Lock()
	if m.started.IsZero() {
		m.started = time.Now()
	}
	if m.sentOp == nil {
		m.sentOp, m.sentOwner, m.ownerQueue = map[string]time.Time{}, map[string]time.Time{}, map[string]platform.ErrorCount{}
	}
	started := m.started
	m.mu.Unlock()

	s := Status{Checked: time.Now()}
	var problems []string

	if free, total, err := diskSpace(m.DataDir); err == nil && total > 0 {
		s.DiskFree, s.DiskTotal, s.DiskPercent = free, total, float64(free)*100/float64(total)
		if s.DiskPercent < DiskLowPercent || free < uint64(DiskLowBytes) {
			problems = append(problems, fmt.Sprintf("Disk space low: %s GB free of %s GB (%.1f%%) on %s", s.DiskFreeGB(), s.DiskTotalGB(), s.DiskPercent, m.DataDir))
			m.operatorAlert("disk", problems[len(problems)-1],
				"Delete old files or grow the disk. Renoxium stops working when the disk is full. Old backups are in /var/backups/renoxium.")
		}
	}

	if m.Backups {
		s.NewestBackup = stampTime(filepath.Join(m.DataDir, ".last-backup"))
		s.NewestOffsite = stampTime(filepath.Join(m.DataDir, ".last-offsite"))
		since := s.NewestBackup
		if since.IsZero() {
			since = started
		}
		if time.Since(since) > BackupMaxAge {
			msg := "Last backup: " + s.BackupAge()
			problems = append(problems, msg)
			m.operatorAlert("backup", msg, "Check /etc/cron.d/renoxium-backup and run the backup once by hand to see the error: sudo /opt/renoxium/backup.sh")
		} else if !s.NewestOffsite.IsZero() && time.Since(s.NewestOffsite) > BackupMaxAge {
			msg := "Last off-site backup copy: " + s.OffsiteAge()
			problems = append(problems, msg)
			m.operatorAlert("offsite", msg, "The local backup works but the rclone copy does not. Run sudo /opt/renoxium/backup.sh to see the error.")
		}
	}

	errs := m.Platform.TakeErrors()
	total := 0
	var apps []string
	for app, e := range errs {
		total += e.N
		apps = append(apps, app)
	}
	if total >= ErrorSpike {
		sort.Slice(apps, func(i, j int) bool { return errs[apps[i]].N > errs[apps[j]].N })
		var lines []string
		for i, app := range apps {
			if i == 10 {
				break
			}
			lines = append(lines, fmt.Sprintf("%s: %d (latest: %s)", app, errs[app].N, short(errs[app].Last)))
		}
		msg := fmt.Sprintf("%d server errors in live apps in the last %s", total, m.Interval)
		problems = append(problems, msg)
		m.operatorAlert("errors", msg, strings.Join(lines, "\n"))
	}
	m.ownerAlerts(errs)

	m.mu.Lock()
	s.Alerts = m.status.Alerts
	s.Problems = problems
	m.status = s
	m.mu.Unlock()
}

// ownerAlerts emails an owner on their app's first error, then at most once
// a day with what happened since.
func (m *Monitor) ownerAlerts(errs map[string]platform.ErrorCount) {
	m.mu.Lock()
	for app, e := range errs {
		q := m.ownerQueue[app]
		q.N += e.N
		q.Last = e.Last
		m.ownerQueue[app] = q
	}
	var due []string
	for app := range m.ownerQueue {
		if time.Since(m.sentOwner[app]) >= ownerEvery {
			due = append(due, app)
		}
	}
	sends := map[string]platform.ErrorCount{}
	for _, app := range due {
		sends[app] = m.ownerQueue[app]
		delete(m.ownerQueue, app)
		m.sentOwner[app] = time.Now()
	}
	m.mu.Unlock()
	for app, e := range sends {
		m.Platform.MailOwner(app, fmt.Sprintf("Your live app had %d error%s", e.N, plural(e.N)),
			fmt.Sprintf("Visitors of %s ran into %d server error%s. The latest was:\n\n%s\n\n"+
				"Ask the agent that built the app (ChatGPT or Claude) to read the logs and fix it, or see the latest errors at %s/apps/%s.\n"+
				"You get at most one of these emails a day per app.", app, e.N, plural(e.N), short(e.Last), m.SiteURL, app))
	}
}

func (m *Monitor) operatorAlert(kind, subject, text string) {
	m.mu.Lock()
	if time.Since(m.sentOp[kind]) < operatorEvery {
		m.mu.Unlock()
		return
	}
	m.sentOp[kind] = time.Now()
	m.status.Alerts = append([]string{time.Now().UTC().Format("2 Jan 15:04") + " UTC: " + subject}, m.status.Alerts...)
	if len(m.status.Alerts) > 20 {
		m.status.Alerts = m.status.Alerts[:20]
	}
	m.mu.Unlock()
	slog.Error("monitor alert", "kind", kind, "subject", subject)
	if m.Mailer == nil {
		return
	}
	for _, to := range m.Operators {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := m.Mailer.Send(ctx, email.Message{To: to, Subject: "[Renoxium alert] " + subject, Text: text + "\n\nOperator panel: " + m.SiteURL + "/operator\n",
			Tag: "operator_alert", FromName: "Renoxium monitor"})
		cancel()
		if err != nil {
			slog.Error("monitor alert not emailed", "to", to, "err", err)
		}
	}
}

// stampTime reads a Unix time written by backup.sh.
func stampTime(path string) time.Time {
	raw, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func short(s string) string {
	if r := []rune(s); len(r) > 500 {
		return string(r[:500]) + "…"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func jobsApp() *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "runs", Columns: []appdef.Column{{Name: "job", Type: "text"}, {Name: "at", Type: "text"}}}},
		Scripts: map[string]string{
			"tick": `function handle(req) {
  if (req.method !== 'JOB' || req.user !== null) throw new Error('not a job');
  db.exec('INSERT INTO runs (job, at) VALUES (?, ?)', req.body.job, req.body.scheduled_at);
  return {text: 'counted ' + db.query('SELECT count(*) AS n FROM runs')[0].n};
}`,
			"broken": `function handle(req) { throw new Error('report failed') }`,
		},
		Templates: map[string]string{"t": "x"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
		Jobs: []appdef.Job{{Name: "every_ten", Script: "tick", Every: "10m"}, {Name: "morning", Script: "tick", DailyAt: "08:00"},
			{Name: "fast", Script: "broken", Every: "1m"}},
	}
}

func TestScheduledJobs(t *testing.T) {
	plans := map[string]string{"usr_free": "free"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_free", "shop")
	if res, err := p.ApplyChange(ctx, "shop", jobsApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	runs := func() int {
		_, n, _ := p.AdminList(ctx, "shop", "runs", "", 1)
		return n
	}

	// Preview never runs jobs by itself, but an agent can run one.
	res, err := p.TestRequest(ctx, "shop", platform.TestRequestInput{Job: "every_ten"})
	if err != nil || res.Status != 200 || res.Body != "counted 1" || len(res.Logs) == 0 {
		t.Fatalf("test job: %v %+v", err, res)
	}
	p.SetAppKind("shop", platform.KindShop) // a shop without Stripe keeps the free limits
	if _, err := p.Publish("shop", 0); err != nil {
		t.Fatal(err)
	}

	// 06:00 in Dubai (02:00 UTC). The first check only starts the clocks.
	start := time.Date(2026, 11, 18, 2, 0, 0, 0, time.UTC)
	if n := p.RunDueJobs(ctx, start); n != 0 {
		t.Fatalf("ran %d jobs on first sight", n)
	}
	// An app on the free limits stretches "every 1m" to 5 minutes.
	if n := p.RunDueJobs(ctx, start.Add(2*time.Minute)); n != 0 {
		t.Fatalf("ran %d jobs after 2 minutes", n)
	}
	if n := p.RunDueJobs(ctx, start.Add(5*time.Minute)); n != 1 {
		t.Fatalf("after 5 minutes ran %d, want only the fast one", n)
	}
	if n := p.RunDueJobs(ctx, start.Add(10*time.Minute)); n != 2 {
		t.Fatalf("after 10 minutes ran %d, want every_ten and fast", n)
	}
	if runs() != 1 {
		t.Fatalf("live runs = %d", runs())
	}
	// 08:00 Dubai = 04:00 UTC: the daily job runs once.
	at8 := time.Date(2026, 11, 18, 4, 0, 30, 0, time.UTC)
	p.RunDueJobs(ctx, at8)
	p.RunDueJobs(ctx, at8.Add(time.Minute))
	if runs() != 3 { // every_ten again at 08:00, and morning once
		t.Fatalf("live runs after 08:00 = %d", runs())
	}

	states := map[string]platform.JobState{}
	for _, s := range p.JobStates("shop") {
		states[s.Name] = s
	}
	if s := states["fast"]; s.Status != "error" || !strings.Contains(s.Message, "report failed") || s.Schedule != "every 5m0s" {
		t.Errorf("fast = %+v", s)
	}
	if s := states["morning"]; s.Status != "ok" || s.Schedule != "daily at 08:00 (Asia/Dubai)" || !s.NextRun.Equal(time.Date(2026, 11, 19, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("morning = %+v", s)
	}
	if errs := p.TakeErrors(); errs["shop"].N == 0 {
		t.Error("job failure not counted for the owner alert")
	}
}

func TestJobValidation(t *testing.T) {
	def := jobsApp()
	def.Jobs = append(def.Jobs, appdef.Job{Name: "both", Script: "tick", Every: "1h", DailyAt: "08:00"},
		appdef.Job{Name: "quick", Script: "tick", Every: "10s"}, appdef.Job{Name: "late", Script: "tick", DailyAt: "25:00"},
		appdef.Job{Name: "where", Script: "nope", DailyAt: "08:00", Timezone: "Mars/Base"})
	raw, _ := jsonMarshal(def)
	_, problems := appdef.Parse(raw)
	text := strings.Join(problems, "\n")
	for _, want := range []string{`job "both": set either`, `job "quick": every must be`, `job "late": daily_at`, `script "nope"`, `unknown timezone "Mars/Base"`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

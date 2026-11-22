package platform_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// A deactivated account's live apps show the paused page and run no jobs;
// the preview, the data and the versions stay.
func TestDeactivatedOwnerPausesLiveApps(t *testing.T) {
	var paused atomic.Bool
	var active atomic.Int32
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerDeactivated: func(id string) bool { return id == "usr_idle" && paused.Load() },
		OwnerActive:      func(id string) { active.Add(1) }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_idle", "shop")
	if res, err := p.ApplyChange(ctx, "shop", jobsApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := p.Publish("shop", 0); err != nil {
		t.Fatal(err)
	}
	if active.Load() != 1 {
		t.Fatalf("publish recorded %d activities", active.Load())
	}
	serve := func(host string) (int, string) {
		w := httptest.NewRecorder()
		p.ServeApp(w, httptest.NewRequest("GET", "http://"+host+"/", nil))
		return w.Code, w.Body.String()
	}
	start := time.Date(2026, 11, 18, 2, 0, 0, 0, time.UTC)
	p.RunDueJobs(ctx, start) // starts the clocks

	paused.Store(true)
	p.ForgetPaused("usr_idle")
	if code, body := serve("shop.renoxium.test"); code != 503 || !strings.Contains(body, "This site is paused") || !strings.Contains(body, "signing in to Renoxium") {
		t.Fatalf("live: %d %s", code, body)
	}
	if code, _ := serve("shop--preview.renoxium.test"); code == 503 {
		t.Fatal("the preview is paused too")
	}
	if n := p.RunDueJobs(ctx, start.Add(time.Hour)); n != 0 {
		t.Fatalf("ran %d jobs of a paused account", n)
	}
	if _, err := p.Publish("shop", 0); err == nil {
		t.Fatal("published while deactivated")
	}
	if !p.OwnerPaused("usr_idle") || p.HasPaidWorkApp("usr_idle") {
		t.Fatal("state")
	}

	paused.Store(false)
	p.ForgetPaused("usr_idle")
	if code, _ := serve("shop.renoxium.test"); code != 200 {
		t.Fatalf("live after reactivation: %d", code)
	}
	if n := p.RunDueJobs(ctx, start.Add(2*time.Hour)); n == 0 {
		t.Fatal("jobs did not come back")
	}
}

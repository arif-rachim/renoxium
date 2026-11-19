package monitor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func TestContentionAndMemoryKillAlerts(t *testing.T) {
	pool, err := sandbox.NewPool(sandbox.PoolConfig{Workers: 1, Timeout: time.Second, HeapLimit: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mail := &outbox{}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", ScriptsPerApp: 2, ScriptsPerOwner: 2}, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	def := &appdef.App{Scripts: map[string]string{
		"busy": `function handle(req) { const end = Date.now() + 150; while (Date.now() < end) {} return {text: "ok"} }`,
		"mem":  `function handle(req) { const a = []; for (let i = 0; ; i++) a.push({i: i, s: "abcdefghij" + i}) }`,
		"page": `function handle(req) { return {text: "ok"} }`,
	}, Routes: []appdef.Route{{Method: "GET", Path: "/busy/:n", Script: "busy"}, {Method: "GET", Path: "/mem/:n", Script: "mem"}, {Method: "GET", Path: "/page/:n", Script: "page"}}}
	for _, app := range []string{"hog-shop", "calm-shop"} {
		p.CreateApp("usr_"+app, app)
		p.ApplyChange(ctx, app, def)
		p.Publish(app, 0)
	}
	old := []any{ContentionMinWait, ContentionMinutes, MemKillSpike}
	ContentionMinWait, ContentionMinutes, MemKillSpike = 100*time.Millisecond, 1, 2
	defer func() {
		ContentionMinWait, ContentionMinutes, MemKillSpike = old[0].(time.Duration), old[1].(int), old[2].(int)
	}()
	m := &Monitor{Platform: p, Mailer: mail, Operators: []string{"ops@renoxium.test"}, SiteURL: "https://renoxium.test"}

	// Quiet: no alert.
	m.Check(ctx)
	if len(mail.subjects()) != 0 {
		t.Fatalf("alerts without load: %v", mail.subjects())
	}
	// The hog keeps the only worker busy while the calm app's requests wait.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); serve(p, "hog-shop", "/busy/1") }()
		go func() { defer wg.Done(); time.Sleep(20 * time.Millisecond); serve(p, "calm-shop", "/page/1") }()
		wg.Wait()
	}
	serve(p, "hog-shop", "/mem/1")
	serve(p, "hog-shop", "/mem/2")
	m.Check(ctx)
	got := strings.Join(mail.subjects(), "\n")
	for _, want := range []string{"[Renoxium alert] hog-shop caused", "of script worker waits in the last 15 minutes",
		"[Renoxium alert] hog-shop: 2 scripts stopped at the memory limit"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "calm-shop") {
		t.Errorf("the waiting app was named: %s", got)
	}
	mail.mu.Lock()
	body := mail.got[0].Text
	mail.mu.Unlock()
	if !strings.Contains(body, "https://renoxium.test/operator/resources/hog-shop") {
		t.Errorf("no link to the app's resource page: %s", body)
	}
	// Rate-limited like the other alerts.
	n := len(mail.subjects())
	serve(p, "hog-shop", "/mem/3")
	m.Check(ctx)
	if len(mail.subjects()) != n {
		t.Errorf("sent again: %v", mail.subjects()[n:])
	}
}

func serve(p *platform.Platform, app, path string) {
	p.ServeApp(newRecorder(), newRequest("http://"+app+".renoxium.test"+path))
}

package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/email"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func TestMain(m *testing.M) {
	sandbox.ServeWorkerIfRequested()
	os.Exit(m.Run())
}

type outbox struct {
	mu  sync.Mutex
	got []email.Message
}

func (o *outbox) Send(_ context.Context, m email.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.got = append(o.got, m)
	return nil
}

func (o *outbox) subjects() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var s []string
	for _, m := range o.got {
		s = append(s, m.To+": "+m.Subject)
	}
	return s
}

func TestMonitor(t *testing.T) {
	dir := t.TempDir()
	mail := &outbox{}
	p, err := platform.New(platform.Config{DataDir: dir, AppsDomain: "renoxium.test", PublicScheme: "https", Mailer: mail, SiteURL: "https://renoxium.test",
		OwnerEmail: func(string) (string, bool) { return "owner@shop.ae", true }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_1", "shop")
	p.ApplyChange(ctx, "shop", &appdef.App{Scripts: map[string]string{"boom": `function handle(req) { throw new Error('price missing') }`},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Script: "boom"}}})
	p.Publish("shop", 0)
	os.WriteFile(filepath.Join(dir, ".last-backup"), []byte(fmt.Sprint(time.Now().Add(-48*time.Hour).Unix())), 0o644)

	oldDisk, oldSpike := DiskLowPercent, ErrorSpike
	DiskLowPercent, ErrorSpike = 101, 2 // every disk is "low"; two errors are a spike
	defer func() { DiskLowPercent, ErrorSpike = oldDisk, oldSpike }()

	m := &Monitor{Platform: p, Mailer: mail, Operators: []string{"ops@renoxium.test"}, DataDir: dir, Backups: true, SiteURL: "https://renoxium.test"}
	p.TestRequest(ctx, "shop", platform.TestRequestInput{Path: "/"}) // errors on preview are not counted
	serveLive(t, p, 3)
	m.Check(ctx)

	got := strings.Join(mail.subjects(), "\n")
	for _, want := range []string{"ops@renoxium.test: [Renoxium alert] Disk space low", "ops@renoxium.test: [Renoxium alert] Last backup: 48h",
		"ops@renoxium.test: [Renoxium alert] 3 server errors", "owner@shop.ae: [shop] Your live app had 3 errors"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	st := m.Status()
	if len(st.Problems) != 3 || len(st.Alerts) != 3 || st.DiskTotal == 0 {
		t.Errorf("status = %+v", st)
	}

	// Within the quiet periods nothing is sent again; new errors wait for the daily owner email.
	n := len(mail.subjects())
	serveLive(t, p, 1)
	m.Check(ctx)
	if len(mail.subjects()) != n {
		t.Errorf("sent again: %v", mail.subjects()[n:])
	}
	if m.Healthy() != nil {
		t.Error("unhealthy with free disk space")
	}
}

// serveLive makes n failing requests to the live app.
func serveLive(t *testing.T, p *platform.Platform, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		w := newRecorder()
		r := newRequest("http://shop.renoxium.test/")
		if !p.ServeApp(w, r) {
			t.Fatal("not served")
		}
	}
}

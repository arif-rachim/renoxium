package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/licence"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// The owner's path on their own server, as the guide (docs/self-host.md)
// walks it: import the export, serve the app, sign in as admin, change the
// definition, roll it back, and let a job run.
func TestSelfHostCommands(t *testing.T) {
	seed, public, err := licence.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	old := licence.PublicKeys
	licence.PublicKeys = []string{public}
	t.Cleanup(func() { licence.PublicKeys = old })
	key, err := licence.KeyFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}

	// On Renoxium: a Pro account's shop, exported from the console.
	hosted, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https", LicenceKey: key,
		OwnerPlan:  func(string) string { return accounts.PlanPro },
		OwnerEmail: func(string) (string, bool) { return "owner@shop.ae", true }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, res, err := hosted.CreateFromExample(ctx, "usr_owner", "noor-shop", "store-uae"); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	if _, err := hosted.Publish("noor-shop", 0); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(t.TempDir(), "noor-shop.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if err := hosted.ExportApp(ctx, "noor-shop", zw); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	f.Close()
	hosted.Close()

	// On the owner's server.
	dir := filepath.Join(t.TempDir(), "data")
	if code := importCommand(dir, "noorperfumes.ae", []string{zipPath, "admin@noorperfumes.ae", "www.noorperfumes.ae"}); code != 0 {
		t.Fatalf("import: exit %d", code)
	}
	if code := importCommand(dir, "noorperfumes.ae", []string{zipPath, "admin@noorperfumes.ae"}); code == 0 {
		t.Error("a second import into the same data directory was accepted")
	}

	// The admin signs in with a password from admin-password.
	if code := adminPasswordCommand(dir, []string{"admin@noorperfumes.ae"}); code != 0 {
		t.Fatalf("admin-password: exit %d", code)
	}
	acc, err := accounts.Open(filepath.Join(dir, "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	u, err := acc.UserByEmail("admin@noorperfumes.ae")
	if err != nil || !u.Verified || acc.Plan(u.ID) != accounts.PlanPro {
		t.Errorf("admin account: %+v %v plan %s", u, err, acc.Plan(u.ID))
	}
	acc.Close()

	p, app, err := openSelfHosted(dir, "noorperfumes.ae")
	if err != nil {
		t.Fatal(err)
	}
	if app != "noor-shop" {
		t.Fatalf("app %q", app)
	}
	info, _ := p.Info(app)
	if info.LiveURL != "https://www.noorperfumes.ae" || info.LiveVersion == 0 {
		t.Errorf("info %+v", info)
	}
	if r, err := p.TestRequest(ctx, app, platform.TestRequestInput{Path: "/", Stage: "live"}); err != nil || r.Status != 200 {
		t.Fatalf("live home: %v %d", err, r.Status)
	}
	p.RunDueJobs(ctx, time.Now()) // the first check only starts counting
	if n := p.RunDueJobs(ctx, time.Now().Add(48*time.Hour)); n == 0 {
		t.Error("the daily job did not run")
	}
	def, _, err := p.Definition(app, 0)
	if err != nil {
		t.Fatal(err)
	}
	first := info.LiveVersion
	p.Close()

	// A changed definition goes live with apply, and rollback undoes it.
	def.Templates["home"] += "<!-- changed on the owner's server -->"
	raw, _ := json.Marshal(def)
	defPath := filepath.Join(t.TempDir(), "definition.json")
	os.WriteFile(defPath, raw, 0o600)
	if code := applyCommand(dir, "noorperfumes.ae", []string{defPath}); code != 0 {
		t.Fatalf("apply: exit %d", code)
	}
	os.WriteFile(defPath, []byte(`{"routes":[{"method":"GET","path":"/","template":"missing"}]}`), 0o600)
	if code := applyCommand(dir, "noorperfumes.ae", []string{defPath}); code == 0 {
		t.Error("a broken definition was applied")
	}
	p, _, _ = openSelfHosted(dir, "noorperfumes.ae")
	if info, _ := p.Info(app); info.LiveVersion <= first {
		t.Errorf("after apply live is %d, was %d", info.LiveVersion, first)
	}
	p.Close()
	if code := rollbackCommand(dir, "noorperfumes.ae", []string{}); code != 0 {
		t.Fatalf("rollback: exit %d", code)
	}
	p, _, _ = openSelfHosted(dir, "noorperfumes.ae")
	defer p.Close()
	if info, _ := p.Info(app); info.LiveVersion != first {
		t.Errorf("after rollback live is %d, want %d", info.LiveVersion, first)
	}

	// Without its licence the data directory does not start.
	os.Remove(filepath.Join(dir, licenceFile))
	if _, err := selfHostedApp(dir, p); err == nil {
		t.Error("started without a licence")
	}
}

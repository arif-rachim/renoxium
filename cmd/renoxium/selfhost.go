package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/licence"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// Running one app on its owner's own server (docs/self-host.md): import an
// export from a paid account into an empty data directory, then serve it
// with -self-hosted. The licence the export carries is kept in the data
// directory and checked offline at every start.

const licenceFile = "licence.json"

// importCommand creates a self-hosted data directory from an export.
func importCommand(dataDir, appsDomain string, args []string) int {
	if len(args) < 2 || len(args) > 4 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR -apps-domain DOMAIN import EXPORT.zip ADMIN_EMAIL [YOUR_DOMAIN] [APP]\n"+
			"  imports an app exported as a paid work app or from a Pro or Business account into an empty data directory;\n"+
			"  the app is served at APP.DOMAIN, and at YOUR_DOMAIN (e.g. www.yourshop.com) when given")
		return 2
	}
	zipPath, email, ownDomain, want := args[0], args[1], "", ""
	if len(args) > 2 {
		ownDomain = args[2]
	}
	if len(args) > 3 {
		want = args[3]
	}
	if entries, err := os.ReadDir(dataDir); err == nil && len(entries) > 0 {
		fmt.Fprintf(os.Stderr, "%s is not empty: import into a new, empty data directory\n", dataDir)
		return 1
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open the export:", err)
		return 1
	}
	defer zr.Close()
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app, lic, password, err := importInto(dataDir, appsDomain, &zr.Reader, email, ownDomain, want)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		// The directory was empty: leave it so, to try again.
		entries, _ := os.ReadDir(dataDir)
		for _, e := range entries {
			os.RemoveAll(filepath.Join(dataDir, e.Name()))
		}
		return 1
	}
	fmt.Printf("Imported %s (exported from %s on the %s plan).\n\n", app, lic.Email, lic.Plan)
	fmt.Printf("Site:        https://%s.%s\n", app, appsDomain)
	if ownDomain != "" {
		fmt.Printf("             https://%s\n", strings.ToLower(ownDomain))
	}
	fmt.Printf("Admin email: %s\nPassword:    %s   (shown once; change it under Account settings)\n\n", email, password)
	fmt.Printf("Start it with: renoxium -data %s -apps-domain %s -base-url https://admin.%s -self-hosted\n", dataDir, appsDomain, appsDomain)
	return 0
}

// importInto does the import; it returns the app's name, its licence and
// the admin account's new password.
func importInto(dataDir, appsDomain string, zr *zip.Reader, email, ownDomain, want string) (string, licence.Licence, string, error) {
	acc, err := accounts.Open(filepath.Join(dataDir, "accounts.db"))
	if err != nil {
		return "", licence.Licence{}, "", err
	}
	defer acc.Close()
	password := randomPassword()
	u, err := acc.CreateUser(email, password)
	if err != nil {
		return "", licence.Licence{}, "", fmt.Errorf("admin account: %w", err)
	}
	if tok, err := acc.CreateEmailToken(u.ID, accounts.PurposeVerify); err == nil {
		acc.VerifyEmail(tok)
	}
	p, err := platform.New(platform.Config{DataDir: dataDir, AppsDomain: appsDomain, PublicScheme: "https", SelfHosted: true}, sandbox.NewGoja())
	if err != nil {
		return "", licence.Licence{}, "", err
	}
	defer p.Close()
	app, lic, err := p.ImportApp(context.Background(), u.ID, zr, want)
	if err != nil {
		return "", lic, "", err
	}
	if err := acc.GrantPlan(u.ID, lic.Plan); err != nil {
		return "", lic, "", err
	}
	if ownDomain != "" {
		if err := p.UseOwnDomain(app, ownDomain); err != nil {
			return "", lic, "", err
		}
	}
	for _, f := range zr.File {
		if f.Name == app+"/"+licenceFile {
			rc, err := f.Open()
			if err != nil {
				return "", lic, "", err
			}
			raw, err := io.ReadAll(rc)
			rc.Close()
			if err == nil {
				err = os.WriteFile(filepath.Join(dataDir, licenceFile), raw, 0o600)
			}
			if err != nil {
				return "", lic, "", err
			}
		}
	}
	return app, lic, password, nil
}

// selfHostedApp checks the data directory's licence and returns the app it
// is for, which must be the only app there.
func selfHostedApp(dataDir string, p *platform.Platform) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, licenceFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	lic, err := licence.Verify(raw)
	if err != nil {
		return "", err
	}
	apps, err := p.AllApps("", 2)
	if err != nil {
		return "", err
	}
	if len(apps) != 1 || apps[0].Name != lic.App {
		return "", fmt.Errorf("a self-hosted data directory holds one app, %s; run renoxium import into an empty directory", lic.App)
	}
	return lic.App, nil
}

// openSelfHosted opens the platform of a self-hosted data directory for a
// command, and its one app.
func openSelfHosted(dataDir, appsDomain string) (*platform.Platform, string, error) {
	p, err := platform.New(platform.Config{DataDir: dataDir, AppsDomain: appsDomain, PublicScheme: "https", SelfHosted: true}, sandbox.NewGoja())
	if err != nil {
		return nil, "", err
	}
	app, err := selfHostedApp(dataDir, p)
	if err != nil {
		p.Close()
		return nil, "", err
	}
	return p, app, nil
}

// applyCommand checks a changed definition and makes it live, keeping the
// previous version for rollback.
func applyCommand(dataDir, appsDomain string, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR -apps-domain DOMAIN apply DEFINITION.json\n"+
			"  checks the definition (every page is rendered), then makes it live; the live data is backed up first")
		return 2
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	def, problems := appdef.Parse(raw)
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "the definition is not valid:\n  "+strings.Join(problems, "\n  "))
		return 1
	}
	p, app, err := openSelfHosted(dataDir, appsDomain)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer p.Close()
	res, err := p.ApplyChange(context.Background(), app, def)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !res.OK {
		fmt.Fprintln(os.Stderr, "the app does not pass its checks; nothing changed:\n  "+strings.Join(res.Problems, "\n  "))
		return 1
	}
	pub, err := p.Publish(app, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("version %d is live at %s\n", pub.LiveVersion, pub.LiveURL)
	for _, m := range pub.Migrations {
		fmt.Println("  " + m)
	}
	return 0
}

// rollbackCommand makes an earlier version live again: the one before the
// live version, or the version given.
func rollbackCommand(dataDir, appsDomain string, args []string) int {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR -apps-domain DOMAIN rollback [VERSION]")
		return 2
	}
	p, app, err := openSelfHosted(dataDir, appsDomain)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer p.Close()
	info, err := p.Info(app)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	version := info.LiveVersion - 1
	if len(args) == 1 {
		if version, err = strconv.Atoi(args[0]); err != nil || version < 1 {
			fmt.Fprintln(os.Stderr, "VERSION is a version number, e.g. 3")
			return 2
		}
	}
	if version < 1 {
		fmt.Fprintln(os.Stderr, "there is no earlier version")
		return 1
	}
	pub, err := p.Publish(app, version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("version %d is live at %s\n", pub.LiveVersion, pub.LiveURL)
	return 0
}

// adminPasswordCommand gives the admin account a new password.
func adminPasswordCommand(dataDir string, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR admin-password EMAIL")
		return 2
	}
	acc, err := accounts.Open(filepath.Join(dataDir, "accounts.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer acc.Close()
	u, err := acc.UserByEmail(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "no account with that email")
		return 1
	}
	password := randomPassword()
	tok, err := acc.CreateEmailToken(u.ID, accounts.PurposeReset)
	if err == nil {
		_, err = acc.ResetPassword(tok, password)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("new password for %s: %s\n", u.Email, password)
	return 0
}

// licenceKeyCommand makes a key pair for signing licences: the seed for
// RENOXIUM_LICENCE_KEY on the Renoxium server, the public key for
// internal/licence.PublicKeys.
func licenceKeyCommand() int {
	seed, public, err := licence.NewKey()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("RENOXIUM_LICENCE_KEY=%s\npublic key for internal/licence.PublicKeys: %s\n", seed, public)
	return 0
}

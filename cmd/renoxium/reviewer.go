package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

// reviewerCommand prepares an account for a directory's reviewers (ChatGPT,
// Claude): confirmed, a member by grant so every tool works, with a new
// password printed once, and a published example store to try the read
// tools on. Running it again gives a new password and keeps the rest.
func reviewerCommand(dataDir, appsDomain string, args []string) int {
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR -apps-domain DOMAIN reviewer EMAIL [APP]\n"+
			"  creates or resets a reviewer account (member by grant, confirmed) and, unless it has apps, publishes the store-uae example as APP (default reviewer-shop)")
		return 2
	}
	email, app := strings.ToLower(strings.TrimSpace(args[0])), "reviewer-shop"
	if len(args) == 2 {
		app = args[1]
	}
	acc, err := accounts.Open(filepath.Join(dataDir, "accounts.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer acc.Close()
	password := randomPassword()
	u, err := acc.UserByEmail(email)
	switch {
	case err == nil:
		tok, terr := acc.CreateEmailToken(u.ID, "reset")
		if terr == nil {
			_, terr = acc.ResetPassword(tok, password)
		}
		if terr != nil {
			fmt.Fprintln(os.Stderr, "reset password:", terr)
			return 1
		}
	default:
		if u, err = acc.CreateUser(email, password); err != nil {
			fmt.Fprintln(os.Stderr, "create account:", err)
			return 1
		}
	}
	if !u.Verified {
		if tok, err := acc.CreateEmailToken(u.ID, "verify"); err == nil {
			acc.VerifyEmail(tok)
		}
	}
	if err := acc.GrantPlan(u.ID, accounts.PlanMember); err != nil {
		fmt.Fprintln(os.Stderr, "plan:", err)
		return 1
	}

	p, err := platform.New(platform.Config{DataDir: dataDir, AppsDomain: appsDomain, PublicScheme: "https"}, sandbox.NewGoja())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer p.Close()
	apps, err := p.Apps(u.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(apps) == 0 {
		if err := publishExample(p, u.ID, app, "store-uae"); err != nil {
			fmt.Fprintln(os.Stderr, "example store:", err)
			return 1
		}
		apps, _ = p.Apps(u.ID)
	}
	fmt.Printf("Reviewer account ready (member by grant, email confirmed)\n  email:    %s\n  password: %s\n", u.Email, password)
	for _, a := range apps {
		fmt.Printf("  app:      %s  live %s\n", a.App, a.LiveURL)
	}
	fmt.Println("The password is shown only now; run the command again for a new one.")
	return 0
}

func publishExample(p *platform.Platform, owner, app, example string) error {
	ex, ok := examples.Get(example)
	if !ok {
		return fmt.Errorf("no example %q", example)
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	if _, err := p.CreateApp(owner, app); err != nil {
		return err
	}
	res, err := p.ApplyChange(context.Background(), app, def)
	if err != nil {
		return err
	}
	if !res.OK {
		return errors.New(strings.Join(res.Problems, "; "))
	}
	_, err = p.Publish(app, 0)
	return err
}

func randomPassword() string {
	b := make([]byte, 15)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

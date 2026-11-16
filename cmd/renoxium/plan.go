package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arif-rachim/renoxium/internal/accounts"
)

// planCommand gives an account a plan without a Stripe subscription, for the
// operator's own account, partners or trials. A running server notices
// within 30 seconds.
func planCommand(dataDir string, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR plan EMAIL free|pro|business")
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
		fmt.Fprintf(os.Stderr, "no account with email %s\n", args[0])
		return 1
	}
	if err := acc.GrantPlan(u.ID, args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s is now on the %s plan\n", u.Email, args[1])
	return 0
}

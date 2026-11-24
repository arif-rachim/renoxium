package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/arif-rachim/renoxium/internal/accounts"
)

// planCommand makes an account a member by agreement with Renoxium (no shop
// fee, no work app subscription), for the operator's own account, partners
// or trials; "free" removes the grant. A running server notices
// within 30 seconds.
func planCommand(dataDir string, args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: renoxium -data DIR plan EMAIL member|free")
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
	if args[1] == accounts.PlanFree {
		fmt.Printf("%s has no grant now: a member only with a card saved\n", u.Email)
	} else {
		fmt.Printf("%s is now a member by agreement\n", u.Email)
	}
	return 0
}

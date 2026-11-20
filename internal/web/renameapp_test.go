package web_test

import (
	"net/url"
	"strings"
	"testing"
)

// The agent asks to rename or delete an app and gets a link; nothing
// changes until the owner confirms there with their password.
func TestRenameAndDeleteNeedTheOwner(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "lina@read.ae")
	s := e.mcpSession(e.connect(c, e.registerClient()).AccessToken)
	if _, errText := call(t, s, "create_app", map[string]any{"app": "little-reader", "from_example": "catalog"}); errText != "" {
		t.Fatal(errText)
	}

	if _, errText := call(t, s, "rename_app", map[string]any{"app": "little-reader", "new_name": "Bad Name"}); !strings.Contains(errText, "not allowed") {
		t.Fatalf("bad name: %s", errText)
	}
	out, errText := call(t, s, "rename_app", map[string]any{"app": "little-reader", "new_name": "lina-books"})
	link, _ := out["confirm_url"].(string)
	if errText != "" || out["needs_confirmation"] != true || link != e.base+"/apps/little-reader/rename?to=lina-books" {
		t.Fatalf("rename_app: %s %v", errText, out)
	}
	if _, err := e.p.Info("little-reader"); err != nil {
		t.Fatal("the tool renamed the app by itself")
	}
	del, _ := call(t, s, "delete_app", map[string]any{"app": "little-reader"})
	if del["confirm_url"] != e.base+"/apps/little-reader/delete" {
		t.Fatalf("delete_app: %v", del)
	}

	// The owner opens the link: the page shows the new address.
	r, _ := c.Get(link)
	page := read(t, r)
	if r.StatusCode != 200 || !strings.Contains(page, "lina-books.renoxium.test") || !strings.Contains(page, "Rename little-reader to lina-books") {
		t.Fatalf("rename page: %d %s", r.StatusCode, page)
	}
	path := strings.TrimPrefix(link, e.base)
	post := func(password string) (int, string) {
		r, err := c.PostForm(e.base+"/apps/little-reader/rename", url.Values{"csrf": {e.csrf(c, path)}, "to": {"lina-books"}, "password": {password}})
		if err != nil {
			t.Fatal(err)
		}
		return r.StatusCode, read(t, r)
	}
	if code, _ := post("wrong password!!"); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	if code, body := post("correct horse battery"); code != 200 || !strings.Contains(body, "lina-books") {
		t.Fatalf("rename: %d %s", code, body)
	}
	if _, err := e.p.Info("lina-books"); err != nil {
		t.Fatal("not renamed", err)
	}
	// The agent sees the new name; another account cannot open the rename page.
	apps, _ := call(t, s, "list_apps", map[string]any{})
	if !strings.Contains(strings.Join(appNames(apps), ","), "lina-books") {
		t.Fatalf("list_apps: %v", apps)
	}
	other := e.browser()
	e.signup(other, "eve@x.ae")
	if r, _ := other.Get(e.base + "/apps/lina-books/rename?to=eve-books"); r.StatusCode != 404 {
		t.Fatalf("someone else's app: %d", r.StatusCode)
	}
}

func appNames(out map[string]any) []string {
	var names []string
	list, _ := out["apps"].([]any)
	for _, a := range list {
		if m, ok := a.(map[string]any); ok {
			names = append(names, m["app"].(string))
		}
	}
	return names
}

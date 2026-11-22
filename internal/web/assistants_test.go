package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The consent screen says which Renoxium account an assistant is being
// connected to and what it can and cannot do; the account page lists
// connected assistants and disconnects one at once.
func TestConsentNamesTheAccountAndAssistantsCanBeDisconnected(t *testing.T) {
	e := newEnv(t)
	c := e.browser()
	e.signup(c, "lina@read.ae")
	clientID := e.registerClient()
	_, challenge := pkce()
	r, _ := c.Get(e.base + e.authorizeURL(clientID, challenge))
	page := read(t, r)
	for _, want := range []string{"Connect Test Agent to your Renoxium account", `<strong>lina@read.ae</strong>`, "Renoxium account · Free plan · 0 apps",
		"Not you? Switch account", "Test Agent cannot", "it only sends you a link, and you confirm", "you go back to <strong>localhost:9999</strong>",
		"Connect Test Agent to this account", "Don't connect", "disconnect Test Agent at any time"} {
		if !strings.Contains(page, want) {
			t.Errorf("consent page lacks %q", want)
		}
	}
	if strings.Contains(page, `href="/signup"`) || strings.Contains(page, `href="/login"`) {
		t.Error("the consent page's header offers to sign in, though the owner is signed in")
	}
	if strings.Contains(page, "<code>") {
		t.Error("the return address is in a code block again (it breaks the sentence on the sign-in layout)")
	}

	tok := e.connect(c, clientID)
	mcpStatus := func() int {
		req, _ := http.NewRequest("POST", e.base+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := mcpStatus(); code != 200 {
		t.Fatalf("connected agent: %d", code)
	}
	r, _ = c.Get(e.base + "/account")
	page = read(t, r)
	if !strings.Contains(page, "Connected AI assistants") || !strings.Contains(page, "<strong>Test Agent</strong>") || !strings.Contains(page, "localhost:9999") ||
		!strings.Contains(page, "Connected ") {
		t.Fatalf("account page: %s", page)
	}

	// Another account cannot disconnect it.
	other := e.browser()
	e.signup(other, "eve@x.ae")
	other.PostForm(e.base+"/account/assistants/disconnect", url.Values{"csrf": {e.csrf(other, "/account")}, "client": {clientID}})
	if code := mcpStatus(); code != 200 {
		t.Fatal("another account disconnected the agent")
	}
	// The owner disconnects it: its token stops working at once.
	r, _ = c.PostForm(e.base+"/account/assistants/disconnect", url.Values{"csrf": {e.csrf(c, "/account")}, "client": {clientID}})
	if page := read(t, r); r.StatusCode != 200 || !strings.Contains(page, "Test Agent was disconnected") || strings.Contains(page, "<strong>Test Agent</strong>") {
		t.Fatalf("disconnect: %d %s", r.StatusCode, page)
	}
	if code := mcpStatus(); code != http.StatusUnauthorized {
		t.Fatalf("disconnected agent: %d", code)
	}
	if r, _ := c.PostForm(e.base+"/account/assistants/disconnect", url.Values{"client": {clientID}}); r.StatusCode != http.StatusForbidden {
		t.Fatal("disconnect without the form token")
	}
}

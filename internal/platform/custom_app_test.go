package platform_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// An app written from scratch, without a finished example: a clinic queue
// (testdata/clinic-queue.json). Visitors take a number and follow their turn
// on a private ticket page; the reception calls a number in the admin panel,
// on_admin_change stamps it and tells every screen; a TV page, a JSON API and
// a public MCP tool show the queue.
func TestCustomAppFromScratch(t *testing.T) {
	raw, err := os.ReadFile("testdata/clinic-queue.json")
	if err != nil {
		t.Fatal(err)
	}
	def, problems := appdef.Parse(raw)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "clinic")
	if res, err := p.ApplyChange(ctx, "clinic", def); err != nil || !res.OK {
		t.Fatalf("apply: %v %+v", err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "clinic", in)
		if err != nil || r.Status >= 500 {
			t.Fatalf("%+v: %v %d\n%s", in, err, r.Status, logText(r))
		}
		return r
	}
	has := func(what, body string, wants ...string) {
		t.Helper()
		for _, w := range wants {
			if !strings.Contains(body, w) {
				t.Errorf("%s lacks %q", what, w)
			}
		}
	}
	has("home", req(platform.TestRequestInput{Path: "/"}).Body, "Al Shifa Family Clinic", "General doctor", "Take a number", "--brand:#0f766e")

	var tickets []string
	for _, name := range []string{"Aisha", "Omar", "Sara"} {
		r := req(platform.TestRequestInput{Method: "POST", Path: "/take", Form: map[string]string{"service": "1", "name": name}})
		if r.Status != 303 || !strings.HasPrefix(r.Location, "/t/") {
			t.Fatalf("take a number: %+v", r)
		}
		tickets = append(tickets, r.Location)
	}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/take", Form: map[string]string{"service": "1"}}); r.Location != "/?error=full" {
		t.Fatalf("without a name: %+v", r)
	}
	has("third ticket", req(platform.TestRequestInput{Path: tickets[2]}).Body, "A003", "<strong>2</strong> people ahead")

	// The reception calls the first number in the admin panel.
	a, err := p.TestRequest(ctx, "clinic", platform.TestRequestInput{Admin: &platform.TestAdminInput{Action: "update", Table: "tickets", ID: 1, Values: map[string]any{"status": "called"}}})
	if err != nil || a.Body != "ok" || a.Admin.Row["called_at"] == nil {
		t.Fatalf("call a number: %v %+v", err, a)
	}
	has("called ticket", req(platform.TestRequestInput{Path: tickets[0]}).Body, "It is your turn")
	has("third ticket after the call", req(platform.TestRequestInput{Path: tickets[2]}).Body, "<strong>1</strong> person ahead", "Now serving: A001")
	has("TV screen", req(platform.TestRequestInput{Path: "/screen"}).Body, "A001", "2 waiting")
	has("API", req(platform.TestRequestInput{Path: "/api/state"}).Body, `"serving":"A001","waiting":2`)
	if r := req(platform.TestRequestInput{Path: "/t/not-a-ticket"}); r.Status != 404 {
		t.Fatalf("unknown ticket: %d", r.Status)
	}
	mcp := req(platform.TestRequestInput{Method: "POST", Path: "/mcp", JSON: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "queue_status", "arguments": map[string]any{}}}})
	has("MCP tool", mcp.Body, "General doctor", "A001")
}

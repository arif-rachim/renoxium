package platform_test

import (
	"encoding/json"

	"context"
	"github.com/arif-rachim/renoxium/internal/applog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func toolsApp(answer string) *appdef.App {
	return &appdef.App{
		Tables: []appdef.Table{{Name: "products", Columns: []appdef.Column{{Name: "name", Type: "text"}, {Name: "price", Type: "real"}}}},
		Seed:   map[string][]map[string]any{"products": {{"name": "Royal Oud", "price": 250}, {"name": "White Musk", "price": 95}}},
		Scripts: map[string]string{
			"search": `function handle(req) {
  if (req.method !== 'TOOL' || req.user !== null) throw new Error('not a tool call');
  const q = String(req.body.q || '');
  if (q === 'secret') return {text: 'no such products', status: 404};
  if (q === 'boom') throw new Error('database on fire');
  return {json: {` + answer + `: db.query("SELECT name, price FROM products WHERE name LIKE ?", '%' + q + '%'), url: req.origin}};
}`},
		Templates: map[string]string{"t": "shop"},
		Routes:    []appdef.Route{{Method: "GET", Path: "/", Template: "t"}},
		Tools: []appdef.Tool{{Name: "search_products", Description: "Find products by name.", Script: "search", ReadOnly: true,
			Params: []appdef.ToolParam{{Name: "q", Type: "string", Description: "part of the name", Required: true}}}},
	}
}

func TestAppMCPTools(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	p.CreateApp("usr_test", "oud")
	if res, err := p.ApplyChange(ctx, "oud", toolsApp("products")); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.Publish("oud", 0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "oud.renoxium.test"
		if !p.ServeApp(w, r) {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "shopper", Version: "1"}, nil)
	cs, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) != 1 || tools.Tools[0].Name != "search_products" || !tools.Tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	call := func(q string) (string, bool) {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_products", Arguments: map[string]any{"q": q}})
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(*mcp.TextContent).Text, res.IsError
	}
	if text, isErr := call("Oud"); isErr || text != `{"products":[{"name":"Royal Oud","price":250}],"url":"https://oud.renoxium.test"}` {
		t.Fatalf("search: %s %v", text, isErr)
	}
	if text, isErr := call("secret"); !isErr || text != "no such products" {
		t.Fatalf("4xx answer: %s %v", text, isErr)
	}
	if text, isErr := call("boom"); !isErr || strings.Contains(text, "fire") {
		t.Fatalf("script error leaked or not an error: %s %v", text, isErr)
	}
	logs, _ := p.Logs("oud", logFilterAll())
	if !anyLog(logs, "MCP tool search_products -> failed: script search: Error: database on fire") || !anyLog(logs, "MCP tool search_products -> ok") {
		t.Fatalf("logs: %+v", logs)
	}

	// The builder agent tests tools on preview with test_request; a new version is served at once.
	p.ApplyChange(ctx, "oud", toolsApp("items"))
	res, err := p.TestRequest(ctx, "oud", platform.TestRequestInput{Method: "POST", Path: "/mcp",
		JSON: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "search_products", "arguments": map[string]any{"q": "Musk"}}}})
	if err != nil || res.Status != 200 || !strings.Contains(res.Body, `\"items\":[{\"name\":\"White Musk\"`) {
		t.Fatalf("test_request: %v %+v", err, res)
	}

	// Apps without tools have no /mcp.
	p.CreateApp("usr_test", "plain")
	p.ApplyChange(ctx, "plain", &appdef.App{Templates: map[string]string{"t": "x"}, Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}}})
	if r, _ := p.TestRequest(ctx, "plain", platform.TestRequestInput{Method: "POST", Path: "/mcp", JSON: map[string]any{}}); r.Status != 404 {
		t.Fatalf("plain app /mcp: %d", r.Status)
	}
}

func TestToolValidation(t *testing.T) {
	def := toolsApp("x")
	def.Tools = append(def.Tools, appdef.Tool{Name: "Bad-Name", Description: "", Script: "nope", Params: []appdef.ToolParam{{Name: "a", Type: "date"}}})
	raw, _ := jsonMarshal(def)
	_, problems := appdef.Parse(raw)
	text := strings.Join(problems, "\n")
	for _, want := range []string{`tool "Bad-Name": name`, "description is required", `script "nope"`, "type must be string"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}

func anyLog(events []applog.Event, text string) bool {
	for _, e := range events {
		if strings.Contains(e.Message, text) {
			return true
		}
	}
	return false
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

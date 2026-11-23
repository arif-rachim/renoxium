package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The builder MCP server (renoxium.com/mcp) is under review for the ChatGPT
// app directory: everything an MCP client can read must stay exactly as it
// was reviewed (commit d10b7c5) until the review ends. This test compares
// what a signed-in agent receives (the server instructions; every tool's
// name, title, description, annotations and schemas; the get_example
// reference; the get_example list with its descriptions) with the snapshot
// in testdata/mcp-snapshot.json, which was generated from d10b7c5.
//
// To change the MCP text deliberately after the review, regenerate the
// snapshot and commit it with the change:
//
//	RENOXIUM_MCP_SNAPSHOT=update go test ./internal/mcpserver -run TestMCPServerIsFrozenForReview
const snapshotFile = "testdata/mcp-snapshot.json"

// mcpSnapshot is what an MCP client of the builder server can see without
// any app of its own.
type mcpSnapshot struct {
	Instructions string            `json:"instructions"`
	Tools        []json.RawMessage `json:"tools"`
	Reference    string            `json:"reference"`
	Examples     json.RawMessage   `json:"examples"`
}

func takeMCPSnapshot(t *testing.T) mcpSnapshot {
	t.Helper()
	s := New(nil, Options{ConfirmPublish: func(string, int) string { return "" },
		ConfirmRename: func(a, n string) string { return "" }, ConfirmDelete: func(a string) string { return "" }})
	verify := func(ctx context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
		return &auth.TokenInfo{UserID: "usr_snapshot", Expiration: time.Now().Add(time.Hour)}, nil
	}
	srv := httptest.NewServer(Handler(s, verify, "http://example.test/.well-known/oauth-protected-resource/mcp"))
	defer srv.Close()
	client := &http.Client{Transport: bearer{http.DefaultTransport}}
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "snapshot"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: client}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	var snap mcpSnapshot
	snap.Instructions = cs.InitializeResult().Instructions
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(tool)
		if err != nil {
			t.Fatal(err)
		}
		snap.Tools = append(snap.Tools, raw)
	}
	call := func(args map[string]any) map[string]json.RawMessage {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_example", Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("get_example %v: %v %+v", args, err, res)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		out := map[string]json.RawMessage{}
		json.Unmarshal(raw, &out)
		return out
	}
	json.Unmarshal(call(map[string]any{"reference_only": true})["reference"], &snap.Reference)
	snap.Examples = call(map[string]any{})["examples"]
	return snap
}

type bearer struct{ rt http.RoundTripper }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer snapshot")
	return b.rt.RoundTrip(r)
}

func marshalSnapshot(t *testing.T, snap mcpSnapshot) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		t.Fatal(err)
	}
	// Indent the raw parts too, so a difference shows on its own line.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, buf.Bytes(), "", "  "); err != nil {
		t.Fatal(err)
	}
	return pretty.Bytes()
}

func TestMCPServerIsFrozenForReview(t *testing.T) {
	got := marshalSnapshot(t, takeMCPSnapshot(t))
	if os.Getenv("RENOXIUM_MCP_SNAPSHOT") == "update" {
		if err := os.MkdirAll(filepath.Dir(snapshotFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(snapshotFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", snapshotFile)
		return
	}
	want, err := os.ReadFile(snapshotFile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		return
	}
	var g, w mcpSnapshot
	json.Unmarshal(got, &g)
	json.Unmarshal(want, &w)
	what := []string{}
	if g.Instructions != w.Instructions {
		what = append(what, "the server instructions")
	}
	if g.Reference != w.Reference {
		what = append(what, "the get_example reference")
	}
	if !jsonEqual(g.Examples, w.Examples) {
		what = append(what, "the get_example list")
	}
	if len(g.Tools) != len(w.Tools) {
		what = append(what, "the number of tools")
	}
	for i := range min(len(g.Tools), len(w.Tools)) {
		if !jsonEqual(g.Tools[i], w.Tools[i]) {
			var named struct{ Name string }
			json.Unmarshal(w.Tools[i], &named)
			what = append(what, "tool "+named.Name)
		}
	}
	t.Errorf("the builder MCP server is frozen for the ChatGPT app directory review: what MCP clients receive must stay as it was at "+
		"commit d10b7c5, but these differ: %v.\nUndo the change, or after the review update the snapshot deliberately with\n"+
		"  RENOXIUM_MCP_SNAPSHOT=update go test ./internal/mcpserver -run TestMCPServerIsFrozenForReview\nand commit %s with it.", what, snapshotFile)
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	json.Unmarshal(a, &x)
	json.Unmarshal(b, &y)
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return bytes.Equal(ax, by)
}

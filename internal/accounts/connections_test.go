package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"
)

// An owner sees each connected agent and can end one agent's access at
// once, without signing the others out.
func TestConnectionsAndDisconnect(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	u, _ := s.CreateUser("owner@shop.ae", "correct horse battery")
	other, _ := s.CreateUser("other@shop.ae", "correct horse battery")
	connect := func(userID, name, redirect string) (Client, Tokens) {
		t.Helper()
		c, err := s.RegisterClient(name, []string{redirect})
		if err != nil {
			t.Fatal(err)
		}
		verifier := "0123456789012345678901234567890123456789abcdef"
		sum := sha256.Sum256([]byte(verifier))
		code, _ := s.CreateCode(c.ID, userID, redirect, base64.RawURLEncoding.EncodeToString(sum[:]), "apps")
		tok, err := s.ExchangeCode(code, c.ID, redirect, verifier)
		if err != nil {
			t.Fatal(err)
		}
		return c, tok
	}
	start := time.Now()
	s.now = func() time.Time { return start }
	claude, claudeTok := connect(u.ID, "Claude", "https://claude.ai/api/mcp/auth_callback")
	s.now = func() time.Time { return start.Add(time.Minute) }
	chatgpt, _ := connect(u.ID, "ChatGPT", "https://chatgpt.com/connector_platform_oauth_redirect")
	connect(other.ID, "Claude", "https://claude.ai/cb")

	list, err := s.Connections(u.ID)
	if err != nil || len(list) != 2 || list[0].Name != "ChatGPT" || list[0].Host != "chatgpt.com" || list[1].Host != "claude.ai" ||
		!list[1].ConnectedAt.Equal(time.Unix(start.Unix(), 0)) {
		t.Fatalf("connections: %v %+v", err, list)
	}
	// Renewing tokens marks the agent as used.
	s.now = func() time.Time { return start.Add(time.Hour) }
	if _, err := s.Refresh(claudeTok.RefreshToken, claude.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.Connections(u.ID); list[0].Name != "Claude" || !list[0].LastUsedAt.Equal(time.Unix(start.Add(time.Hour).Unix(), 0)) {
		t.Fatalf("after refresh: %+v", list)
	}

	// Disconnecting ChatGPT ends its access only.
	if ok, err := s.Disconnect(u.ID, chatgpt.ID); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if list, _ := s.Connections(u.ID); len(list) != 1 || list[0].Name != "Claude" {
		t.Fatalf("after disconnect: %+v", list)
	}
	// Another account's agent is not someone else's to disconnect.
	if ok, _ := s.Disconnect(other.ID, chatgpt.ID); ok {
		t.Fatal("disconnected an agent of another account")
	}
	if list, _ := s.Connections(other.ID); len(list) != 1 {
		t.Fatalf("other account: %+v", list)
	}
}

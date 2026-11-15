package whatsapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudAPIRequest(t *testing.T) {
	var path, auth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"messages":[{"id":"wamid.x"}]}`))
	}))
	defer srv.Close()
	c := &CloudAPI{Token: "tok", PhoneNumberID: "123", Template: "login_code", BaseURL: srv.URL}
	if err := c.SendCode(context.Background(), "+971501234567", "482913"); err != nil {
		t.Fatal(err)
	}
	if path != "/v23.0/123/messages" || auth != "Bearer tok" || got["to"] != "971501234567" || got["messaging_product"] != "whatsapp" {
		t.Fatalf("request: %s %s %v", path, auth, got)
	}
	tmpl := got["template"].(map[string]any)
	raw, _ := json.Marshal(tmpl)
	if tmpl["name"] != "login_code" || strings.Count(string(raw), "482913") != 2 || !strings.Contains(string(raw), `"sub_type":"url"`) {
		t.Fatalf("template: %s", raw)
	}
}

func TestCloudAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"Template name does not exist"}}`))
	}))
	defer srv.Close()
	err := (&CloudAPI{Token: "t", PhoneNumberID: "1", Template: "x", BaseURL: srv.URL}).SendCode(context.Background(), "+971501234567", "1")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"+971 50 123 4567":  "+971501234567",
		"050-123-4567":      "+971501234567",
		"00971501234567":    "+971501234567",
		"+62 812 3456 7890": "+6281234567890",
		"(+44) 7700 900123": "",
		"12345":             "",
		"+971 50 abc":       "",
		"+0123456789":       "",
	} {
		got, ok := Normalize(in)
		if (want == "" && ok) || (want != "" && (!ok || got != want)) {
			t.Errorf("%q: got %q %v, want %q", in, got, ok, want)
		}
	}
}

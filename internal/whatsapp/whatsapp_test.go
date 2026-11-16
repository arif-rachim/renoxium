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

func TestCloudAPIErrors(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []string
	}{
		{`{"error":{"message":"(#132001) Template name does not exist in the translation","type":"OAuthException","code":132001,
			"error_data":{"messaging_product":"whatsapp","details":"template name (login_code) does not exist in en_US"},"fbtrace_id":"Abc123"}}`,
			[]string{"whatsapp error 132001", "does not exist in en_US", "WHATSAPP_OTP_LANGUAGE", `language "en"`, "fbtrace_id Abc123"}},
		{`{"error":{"message":"(#131042) Business eligibility payment issue","code":131042,"error_data":{"details":"Message failed to send because there were one or more errors related to your payment method."}}}`,
			[]string{"whatsapp error 131042", "payment method in WhatsApp Manager"}},
		{`{"error":{"message":"(#131030) Recipient phone number not in allowed list","code":131030}}`,
			[]string{"131030", "allowed list", "added as recipients"}},
		{`{"error":{"message":"Error validating access token","code":190,"error_subcode":463}}`,
			[]string{"190", "token is invalid or expired"}},
		{`<html>bad gateway</html>`, []string{"HTTP 400", "bad gateway"}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(400)
			w.Write([]byte(tc.body))
		}))
		err := (&CloudAPI{Token: "t", PhoneNumberID: "1", Template: "login_code", BaseURL: srv.URL}).SendCode(context.Background(), "+971501234567", "1")
		srv.Close()
		for _, w := range tc.want {
			if err == nil || !strings.Contains(err.Error(), w) {
				t.Errorf("%s: missing %q in %v", tc.body[:30], w, err)
			}
		}
	}
}

func TestDefaultLanguageIsEn(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	(&CloudAPI{Token: "t", PhoneNumberID: "1", Template: "login_code", BaseURL: srv.URL}).SendCode(context.Background(), "+971501234567", "123456")
	if lang := got["template"].(map[string]any)["language"].(map[string]any)["code"]; lang != "en" {
		t.Fatalf("language = %v", lang)
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

package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResendRequest(t *testing.T) {
	var got map[string]any
	var auth, idem string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, idem = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()
	s := &Resend{Key: "re_test", From: "Renoxium <no-reply@renoxium.com>", Endpoint: srv.URL}
	err := s.Send(context.Background(), Message{To: "a@b.ae", Subject: "Hi", Text: "hello", HTML: "<p>hello</p>", ReplyTo: "shop@b.ae", Tag: "password_reset"})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer re_test" || len(idem) != 32 || got["from"] != "Renoxium <no-reply@renoxium.com>" ||
		got["to"].([]any)[0] != "a@b.ae" || got["subject"] != "Hi" || got["reply_to"] != "shop@b.ae" ||
		got["tags"].([]any)[0].(map[string]any)["value"] != "password_reset" {
		t.Fatalf("request: %v %s %s", got, auth, idem)
	}
}

func TestResendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"name":"validation_error","message":"The renoxium.com domain is not verified."}`))
	}))
	defer srv.Close()
	err := (&Resend{Key: "k", From: "x@renoxium.com", Endpoint: srv.URL}).Send(context.Background(), Message{To: "a@b.ae", Subject: "s", Text: "t"})
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "not verified") {
		t.Fatalf("err = %v", err)
	}
}

package whatsapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account"}`)
	good := Sign("app-secret", body)
	cases := []struct {
		secret, header string
		body           []byte
		want           bool
	}{
		{"app-secret", good, body, true},
		{"other-secret", good, body, false},
		{"app-secret", good, []byte(`{"object":"x"}`), false},
		{"app-secret", "", body, false},
		{"app-secret", "sha1=" + good[7:], body, false},
		{"app-secret", "sha256=zz", body, false},
		{"", Sign("", body), body, false},
	}
	for i, c := range cases {
		if got := VerifySignature(c.secret, c.body, c.header); got != c.want {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
}

func TestEvents(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba","changes":[{"field":"messages","value":{
		"metadata":{"phone_number_id":"100"},
		"contacts":[{"wa_id":"971501234567","user_id":"AE.1","profile":{"name":"Sara"}}],
		"messages":[{"from":"971501234567","id":"wamid.in","type":"button","button":{"payload":"rx-stop:abc","text":"Stop messages"}},
			{"from_user_id":"AE.2","id":"wamid.user","type":"text","text":{"body":"hi"}}],
		"statuses":[{"id":"wamid.out","status":"delivered","pricing":{"category":"utility","type":"regular"}}]}},
		{"field":"account_update","value":{}}]}]}`)
	in, st, err := Events(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(in) != 2 || in[0].Phone() != "+971501234567" || in[0].Button.Payload != "rx-stop:abc" || in[0].PhoneNumberID != "100" {
		t.Fatalf("inbound = %+v", in)
	}
	if in[1].Phone() != "" {
		t.Error("a username-only sender has no phone")
	}
	if len(st) != 1 || st[0].Status != "delivered" || st[0].Pricing.Category != "utility" || st[0].PhoneNumberID != "100" {
		t.Fatalf("statuses = %+v", st)
	}
	if _, _, err := Events([]byte("not json")); err == nil {
		t.Error("bad body parsed")
	}
}

func TestSendTemplateAndText(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(raw, &m)
		got = append(got, m)
		w.Write([]byte(`{"messaging_product":"whatsapp","messages":[{"id":"wamid.42"}]}`))
	}))
	defer srv.Close()
	c := &CloudAPI{Token: "t", PhoneNumberID: "100", BaseURL: srv.URL}
	id, err := c.SendTemplate(context.Background(), "+971501234567", Message{Template: "rx_update", Body: []string{"Oud", "Order #1", "out\nfor delivery"},
		URLParam: "abc", StopPayload: "rx-stop:abc"})
	if err != nil || id != "wamid.42" {
		t.Fatalf("send: %q %v", id, err)
	}
	if _, err := c.SendText(context.Background(), "+971501234567", "hello"); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	want := `[{"messaging_product":"whatsapp","recipient_type":"individual","template":{"components":[` +
		`{"parameters":[{"text":"Oud","type":"text"},{"text":"Order #1","type":"text"},{"text":"out for delivery","type":"text"}],"type":"body"},` +
		`{"index":"0","parameters":[{"text":"abc","type":"text"}],"sub_type":"url","type":"button"},` +
		`{"index":"1","parameters":[{"payload":"rx-stop:abc","type":"payload"}],"sub_type":"quick_reply","type":"button"}],` +
		`"language":{"code":"en"},"name":"rx_update"},"to":"971501234567","type":"template"},` +
		`{"messaging_product":"whatsapp","recipient_type":"individual","text":{"body":"hello","preview_url":false},"to":"971501234567","type":"text"}]`
	if string(b) != want {
		t.Fatalf("requests:\n%s\nwant\n%s", b, want)
	}
}

func TestInteractiveAndButtons(t *testing.T) {
	body := []byte(`{"entry":[{"changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"100"},
		"contacts":[{"wa_id":"971501234567","profile":{"name":"Sara"}}],
		"messages":[{"from":"971501234567","id":"w1","type":"interactive","interactive":{"type":"button_reply","button_reply":{"id":"rx-btn:m1:book","title":"Book"}}}]}}]}]}`)
	in, _, err := Events(body)
	if err != nil || len(in) != 1 || in[0].Interactive.ButtonReply.ID != "rx-btn:m1:book" || in[0].Name != "Sara" {
		t.Fatalf("interactive = %+v %v", in, err)
	}
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"messages":[{"id":"wamid.b"}]}`))
	}))
	defer srv.Close()
	c := &CloudAPI{Token: "t", PhoneNumberID: "100", BaseURL: srv.URL}
	if id, err := c.SendButtons(context.Background(), "+971501234567", "Pick one", []Button{{ID: "a", Title: "Yes"}, {ID: "b", Title: "No"}}); err != nil || id != "wamid.b" {
		t.Fatal(id, err)
	}
	b, _ := json.Marshal(got["interactive"])
	if string(b) != `{"action":{"buttons":[{"reply":{"id":"a","title":"Yes"},"type":"reply"},{"reply":{"id":"b","title":"No"},"type":"reply"}]},"body":{"text":"Pick one"},"type":"button"}` {
		t.Fatalf("interactive request = %s", b)
	}
}

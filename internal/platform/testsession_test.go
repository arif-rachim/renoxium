package platform_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// An agent tests signed-in flows on preview: a ready-made visitor books and
// cancels, and a named session signs up through the real sign-in pages.
// Neither reaches live.
func TestVisitorsAndSessionsOnPreview(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	if _, res, err := p.CreateFromExample(ctx, "usr_test", "bayti", "home-service-uae"); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	v, err := p.CreateTestVisitor(ctx, "bayti", platform.TestVisitorInput{Name: "Fatima"})
	if err != nil || !strings.HasSuffix(v.Email, "@example.com") || !v.Verified || v.ID == 0 {
		t.Fatalf("visitor: %v %+v", err, v)
	}
	if again, _ := p.CreateTestVisitor(ctx, "bayti", platform.TestVisitorInput{Email: strings.ToUpper(v.Email)}); !again.Existed || again.ID != v.ID {
		t.Fatalf("same email again: %+v", again)
	}
	if w, err := p.CreateTestVisitor(ctx, "bayti", platform.TestVisitorInput{Phone: "+971 50 123 4567", Name: "Omar"}); err != nil || w.Phone != "+971501234567" {
		t.Fatalf("phone visitor: %v %+v", err, w)
	}
	if _, err := p.CreateTestVisitor(ctx, "bayti", platform.TestVisitorInput{Email: "not an address"}); err == nil {
		t.Fatal("bad email accepted")
	}

	// Book with cash as the visitor, follow the redirect, read the booking.
	day := bookableDay()
	form := map[string]string{"service": "1", "qty": "2", "zone": "1", "date": day, "slot": "10:00", "repeat": "once", "payment": "cash",
		"name": "Fatima", "phone": "+971501234567", "address": "Villa 12", "flat": "12"}
	r, err := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Method: "POST", Path: "/book", Form: form, UserEmail: v.Email, Follow: true})
	if err != nil || r.Status != 200 || len(r.Redirects) != 1 || !strings.HasPrefix(r.Redirects[0], "303 /bookings/1") || !strings.Contains(r.Body, "Confirmed") {
		t.Fatalf("book and follow: %v %d %v\n%s\n%s", err, r.Status, r.Redirects, r.Body, logText(r))
	}
	r, _ = p.TestRequest(ctx, "bayti", platform.TestRequestInput{Method: "POST", Path: "/bookings/1/cancel", UserEmail: v.Email, Follow: true})
	if r.Status != 200 || !strings.Contains(r.Body, "Cancelled") {
		t.Fatalf("cancel: %d %v\n%s", r.Status, r.Redirects, r.Body)
	}

	// A real sign-up through the sign-in pages, in a session.
	r, err = p.TestRequest(ctx, "bayti", platform.TestRequestInput{Path: "/_rx/auth/signup?next=/bookings", Session: "s1", Follow: true})
	if err != nil || r.Status != 200 || len(r.Redirects) != 1 || !strings.Contains(r.Redirects[0], "--id.") || !strings.Contains(r.Body, "password") {
		t.Fatalf("sign-up page: %v %d %v", err, r.Status, r.Redirects)
	}
	r, err = p.TestRequest(ctx, "bayti", platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup?next=/bookings", Session: "s1", Follow: true,
		Form: map[string]string{"name": "Sara", "email": "sara@mail.ae", "password": "sara password 1", "next": "/bookings"}})
	if err != nil || r.Status != 200 || !strings.Contains(strings.Join(r.Redirects, " "), "/_rx/auth/callback") {
		t.Fatalf("sign-up: %v %d %v\n%s", err, r.Status, r.Redirects, r.Body)
	}
	// The session is signed in on later calls; another session is not.
	if r, _ := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Path: "/bookings", Session: "s1"}); r.Status != 200 {
		t.Fatalf("signed-in session: %d %s", r.Status, r.Location)
	}
	if r, _ := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Path: "/bookings", Session: "s2"}); r.Status != 303 {
		t.Fatalf("other session: %d", r.Status)
	}
	// Posting the form straight away, without opening the page, works too.
	r, _ = p.TestRequest(ctx, "bayti", platform.TestRequestInput{Method: "POST", Path: "/_rx/auth/signup?next=/bookings", Session: "s3", Follow: true,
		Form: map[string]string{"name": "Ali", "email": "ali@mail.ae", "password": "ali password 12", "next": "/bookings"}})
	if chain := strings.Join(r.Redirects, " "); r.Status != 200 || !strings.Contains(chain, "/_rx/auth/callback") || strings.Contains(chain, "login") {
		t.Fatalf("direct sign-up: %d %v", r.Status, r.Redirects)
	}

	// Full URLs on the preview's own hosts are accepted; other sites are not, and nothing goes to live.
	info, _ := p.Info("bayti")
	if r, err := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Path: info.PreviewURL + "/s/1"}); err != nil || r.Status != 200 {
		t.Fatalf("full url: %v %d", err, r.Status)
	}
	for _, in := range []platform.TestRequestInput{
		{Path: "https://example.com/"},
		{Path: info.LiveURL + "/"},
		{Stage: "live", Path: "/", Session: "s1"},
		{Stage: "live", Path: "/", Follow: true},
		{Path: "/", Session: "bad name!"},
	} {
		if _, err := p.TestRequest(ctx, "bayti", in); err == nil {
			t.Errorf("accepted %+v", in)
		}
	}

	// The next change starts the preview afresh: visitors and sessions are gone.
	def, n, _ := p.Definition("bayti", 0)
	if res, _ := p.ApplyChange(ctx, "bayti", def); !res.OK || res.Version != n+1 {
		t.Fatal("reapply")
	}
	if _, err := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Path: "/bookings", UserEmail: v.Email}); err == nil {
		t.Fatal("test visitor survived a change")
	}
	if r, _ := p.TestRequest(ctx, "bayti", platform.TestRequestInput{Path: "/bookings", Session: "s1"}); r.Status != 303 {
		t.Fatalf("session survived a change: %d", r.Status)
	}
}

// bookableDay is three days ahead in Dubai, skipping the example's closed Sunday.
func bookableDay() string {
	d := time.Now().UTC().Add(4*time.Hour).AddDate(0, 0, 3)
	if d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, 1)
	}
	return d.Format("2006-01-02")
}

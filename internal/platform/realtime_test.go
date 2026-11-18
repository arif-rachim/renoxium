package platform_test

import (
	"bufio"
	"context"
	"github.com/arif-rachim/renoxium/examples"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/platform"
	"github.com/arif-rachim/renoxium/internal/sandbox"
)

func liveApp() *appdef.App {
	return &appdef.App{
		Scripts: map[string]string{
			"pos": `function handle(req) {
  const r = realtime.publish({channel: 'order-' + req.params.id, data: {lat: Number(req.form.lat), lng: Number(req.form.lng)}});
  realtime.publish({channel: 'public:queue', data: {waiting: 3}});
  return {json: r};
}`,
			"token": `function handle(req) { return {text: realtime.token('order-' + req.params.id)} }`,
			"bad":   `function handle(req) { realtime.publish({channel: 'Bad Channel!', data: 1}); return {text: 'no'} }`,
		},
		Templates: map[string]string{"t": "x"},
		Routes: []appdef.Route{{Method: "GET", Path: "/", Template: "t"}, {Method: "POST", Path: "/orders/:id/position", Script: "pos"},
			{Method: "GET", Path: "/orders/:id/token", Script: "token"}, {Method: "GET", Path: "/bad", Script: "bad"}},
	}
}

type sse struct {
	resp  *http.Response
	lines chan string
}

func subscribe(t *testing.T, base, path string) (*sse, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", base+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil, resp.StatusCode
	}
	s := &sse{resp: resp, lines: make(chan string, 100)}
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				s.lines <- strings.TrimPrefix(sc.Text(), "data: ")
			}
		}
	}()
	return s, 200
}

func (s *sse) next(t *testing.T) string {
	t.Helper()
	select {
	case l := <-s.lines:
		return l
	case <-time.After(3 * time.Second):
		t.Fatal("no event")
		return ""
	}
}

func TestRealtime(t *testing.T) {
	plans := map[string]string{"usr_free": "free"}
	p, err := platform.New(platform.Config{DataDir: t.TempDir(), AppsDomain: "renoxium.test", PublicScheme: "https",
		OwnerPlan: func(id string) string { return plans[id] }}, sandbox.NewGoja())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	p.CreateApp("usr_free", "deliv")
	if res, err := p.ApplyChange(ctx, "deliv", liveApp()); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	p.Publish("deliv", 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "deliv.renoxium.test"
		if !p.ServeApp(w, r) {
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer srv.CloseClientConnections() // open streams would keep Close waiting
	post := func(id, lat string) string {
		r, err := http.PostForm(srv.URL+"/orders/"+id+"/position", url.Values{"lat": {lat}, "lng": {"55.27"}})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return string(b)
	}
	get := func(path string) string {
		r, _ := http.Get(srv.URL + path)
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return string(b)
	}

	// A private channel needs the token a script hands out, for that channel only.
	if _, code := subscribe(t, srv.URL, "/_rx/live/order-1"); code != 403 {
		t.Fatalf("no token: %d", code)
	}
	other := get("/orders/2/token")
	if _, code := subscribe(t, srv.URL, "/_rx/live/order-1?token="+other); code != 403 {
		t.Fatalf("token of another channel: %d", code)
	}
	// Last message first, then new ones as they are published.
	post("1", "25.20")
	tok := get("/orders/1/token")
	buyer, code := subscribe(t, srv.URL, "/_rx/live/order-1?token="+tok)
	if code != 200 {
		t.Fatalf("with token: %d", code)
	}
	if got := buyer.next(t); got != `{"lat":25.2,"lng":55.27}` {
		t.Fatalf("last message = %s", got)
	}
	queue, code := subscribe(t, srv.URL, "/_rx/live/public:queue")
	if code != 200 {
		t.Fatalf("public channel: %d", code)
	}
	queue.next(t) // last message from the first post
	if out := post("1", "25.21"); out != `{"delivered":1}` {
		t.Fatalf("publish answer = %s", out)
	}
	if got := buyer.next(t); got != `{"lat":25.21,"lng":55.27}` {
		t.Fatalf("live message = %s", got)
	}
	if got := queue.next(t); got != `{"waiting":3}` {
		t.Fatalf("queue = %s", got)
	}
	if n := p.LiveSubscribers("deliv", platform.Live); n != 2 {
		t.Fatalf("subscribers = %d", n)
	}

	// Plan limit on open connections.
	old := platform.LiveSubscribersPerPlan["free"]
	platform.LiveSubscribersPerPlan["free"] = 2
	defer func() { platform.LiveSubscribersPerPlan["free"] = old }()
	if _, code := subscribe(t, srv.URL, "/_rx/live/public:queue"); code != 503 {
		t.Fatalf("over the limit: %d", code)
	}
	buyer.resp.Body.Close()
	deadline := time.Now().Add(2 * time.Second)
	for p.LiveSubscribers("deliv", platform.Live) != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := p.LiveSubscribers("deliv", platform.Live); n != 1 {
		t.Fatalf("subscribers after a browser left = %d", n)
	}

	if r, _ := http.Get(srv.URL + "/bad"); r.StatusCode != 500 {
		t.Fatalf("bad channel: %d", r.StatusCode)
	}
}

func TestDeliveryTrackingExample(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	ex, _ := examples.Get("delivery-tracking")
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "flowers")
	if res, err := p.ApplyChange(ctx, "flowers", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "flowers", in)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := req(platform.TestRequestInput{Path: "/track?id=1&code=abc123"}); r.Status != 303 || r.Location != "/track/1?code=ABC123" {
		t.Fatalf("track form: %+v", r)
	}
	if r := req(platform.TestRequestInput{Path: "/track/1?code=ABC123"}); r.Status != 200 || !strings.Contains(r.Body, "/_rx/live/delivery-1?token=") {
		t.Fatalf("track page: %d", r.Status)
	}
	for _, path := range []string{"/track/1?code=WRONG", "/courier/1?key=wrong"} {
		if r := req(platform.TestRequestInput{Path: path}); r.Status != 404 {
			t.Fatalf("%s: %d", path, r.Status)
		}
	}
	if r := req(platform.TestRequestInput{Path: "/courier/1?key=courier-demo-key-change-me"}); r.Status != 200 || !strings.Contains(r.Body, "Mark as delivered") {
		t.Fatalf("courier page: %d", r.Status)
	}
	key := map[string]string{"key": "courier-demo-key-change-me", "lat": "25.21", "lng": "55.28"}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/courier/1/position", Form: key}); r.Status != 200 {
		t.Fatalf("position: %+v", r)
	}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/courier/1/position", Form: map[string]string{"key": "x", "lat": "1", "lng": "1"}}); r.Status != 403 {
		t.Fatalf("position with a wrong key: %d", r.Status)
	}
	if r := req(platform.TestRequestInput{Method: "POST", Path: "/courier/1/delivered", Form: key}); r.Status != 200 {
		t.Fatalf("delivered: %+v", r)
	}
	if r := req(platform.TestRequestInput{Path: "/track/1?code=ABC123"}); !strings.Contains(r.Body, "Delivered") {
		t.Fatal("status not delivered")
	}
}

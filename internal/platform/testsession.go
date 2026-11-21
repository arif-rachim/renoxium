package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/internal/whatsapp"
)

// Test visitors and test sessions let an agent try signed-in flows on its
// app's preview. Both exist only on preview: visitors are rows in the
// preview database (dropped with it on the next change), and a session's
// cookies are kept in memory for the preview's own hosts only.

// TestVisitor is a visitor account made on preview for testing.
type TestVisitor struct {
	ID       int64  `json:"id"`
	Email    string `json:"email,omitempty" jsonschema:"pass it as as_visitor to act as this visitor"`
	Phone    string `json:"phone,omitempty"`
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
	Existed  bool   `json:"existed,omitempty" jsonschema:"true when a preview visitor with this email or phone was already there; it is returned unchanged"`
	Note     string `json:"note"`
}

// TestVisitorInput describes the visitor to make; every field is optional.
type TestVisitorInput struct {
	Email string `json:"email,omitempty" jsonschema:"email of the visitor; omit for a made-up address on example.com"`
	Phone string `json:"phone,omitempty" jsonschema:"WhatsApp number, e.g. +971501234567, for apps that sign in with WhatsApp"`
	Name  string `json:"name,omitempty" jsonschema:"display name; default Test Visitor"`
}

// MaxTestVisitors is how many visitors create_test_visitor may add to one
// preview, so a looping agent cannot fill the preview database.
const MaxTestVisitors = 50

// CreateTestVisitor adds a confirmed visitor without a password to the
// app's preview database. Live is never touched.
func (p *Platform) CreateTestVisitor(ctx context.Context, app string, in TestVisitorInput) (TestVisitor, error) {
	if _, err := p.Info(app); err != nil {
		return TestVisitor{}, err
	}
	st, err := p.loadStage(app, Preview)
	if err != nil || st == nil {
		return TestVisitor{}, errors.New("preview has no version yet: call apply_change or create_app with from_example first")
	}
	email, phone := "", ""
	if in.Email != "" {
		var ok bool
		if email, ok = normalizeEmail(in.Email); !ok {
			return TestVisitor{}, fmt.Errorf("email %q is not a valid address", in.Email)
		}
	}
	if in.Phone != "" {
		var ok bool
		if phone, ok = whatsapp.Normalize(in.Phone); !ok {
			return TestVisitor{}, fmt.Errorf("phone %q is not a valid international number, e.g. +971501234567", in.Phone)
		}
	}
	if email == "" && phone == "" {
		email = "test-" + strings.ToLower(secretToken()[:8]) + "@example.com"
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Test Visitor"
	}
	if len(name) > 100 {
		return TestVisitor{}, errors.New("name: at most 100 characters")
	}
	note := "Preview only: this visitor is dropped with the preview data on the next change. Call view_page or submit_form with as_visitor set to act as them."
	var v TestVisitor
	var e, ph sql.NullString
	var verified sql.NullString
	err = st.admin.QueryRowContext(ctx, `SELECT id, email, phone, name, verified_at FROM rx_users WHERE (email = ? AND email != '') OR (phone = ? AND phone != '')`,
		email, phone).Scan(&v.ID, &e, &ph, &v.Name, &verified)
	if err == nil {
		v.Email, v.Phone, v.Verified, v.Existed, v.Note = e.String, ph.String, verified.Valid, true, note
		return v, nil
	}
	// Test visitors are listed in a table of the preview database only; it
	// goes with the preview data and never exists on live.
	if _, err := st.admin.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS rx_test_visitors (user_id INTEGER PRIMARY KEY)`); err != nil {
		return TestVisitor{}, err
	}
	var total int
	st.admin.QueryRowContext(ctx, `SELECT count(*) FROM rx_test_visitors`).Scan(&total)
	if total >= MaxTestVisitors {
		return TestVisitor{}, fmt.Errorf("at most %d test visitors per preview; reuse one (the next change starts the preview afresh)", MaxTestVisitors)
	}
	res, err := st.admin.ExecContext(ctx, `INSERT INTO rx_users (email, phone, name, verified_at) VALUES (?, ?, ?, ?)`,
		nullIfEmpty(email), nullIfEmpty(phone), name, now())
	if err != nil {
		return TestVisitor{}, err
	}
	id, _ := res.LastInsertId()
	st.admin.ExecContext(ctx, `INSERT INTO rx_test_visitors (user_id) VALUES (?)`, id)
	return TestVisitor{ID: id, Email: email, Phone: phone, Name: name, Verified: true, Note: note}, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// testJar is the cookies of one named test session, per host.
type testJar struct {
	mu      sync.Mutex
	cookies map[string]map[string]string // host -> name -> value
	used    time.Time
}

const (
	testSessionTTL    = time.Hour
	maxTestSessions   = 10 // per app
	maxTestRedirects  = 8
	maxSessionCookies = 30 // per host
)

var sessionNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// testSession returns the jar of a named session of an app's preview,
// making it on first use.
func (p *Platform) testSession(app, name string) (*testJar, error) {
	if !sessionNameRE.MatchString(name) {
		return nil, errors.New("session: 1-32 letters, digits, - or _, e.g. s1")
	}
	key := app + "\x00" + name
	if j, ok := p.testJars.Load(key); ok {
		jar := j.(*testJar)
		jar.mu.Lock()
		fresh := time.Since(jar.used) < testSessionTTL
		jar.mu.Unlock()
		if fresh {
			return jar, nil
		}
		p.testJars.Delete(key)
	}
	n := 0
	p.testJars.Range(func(k, v any) bool {
		j := v.(*testJar)
		j.mu.Lock()
		stale := time.Since(j.used) >= testSessionTTL
		j.mu.Unlock()
		if stale {
			p.testJars.Delete(k)
		} else if strings.HasPrefix(k.(string), app+"\x00") {
			n++
		}
		return true
	})
	if n >= maxTestSessions {
		return nil, fmt.Errorf("at most %d test sessions per app at once; reuse a name", maxTestSessions)
	}
	jar := &testJar{cookies: map[string]map[string]string{}, used: time.Now()}
	actual, _ := p.testJars.LoadOrStore(key, jar)
	return actual.(*testJar), nil
}

// dropTestSessions forgets an app's test sessions; their visitor sessions
// went with the preview data.
func (p *Platform) dropTestSessions(app string) {
	p.testJars.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), app+"\x00") {
			p.testJars.Delete(k)
		}
		return true
	})
}

func (j *testJar) get(host, name string) string {
	if j == nil {
		return ""
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cookies[host][name]
}

func (j *testJar) set(host, name, value string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cookies[host] == nil {
		j.cookies[host] = map[string]string{}
	}
	j.cookies[host][name] = value
}

// add puts the jar's cookies for host on req, except names already set.
func (j *testJar) add(req *http.Request, host string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.used = time.Now()
	for name, value := range j.cookies[host] {
		if _, err := req.Cookie(name); err != nil {
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
	}
}

// keep stores the cookies a response set for host, and removes expired ones.
func (j *testJar) keep(host string, set []*http.Cookie) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	m := j.cookies[host]
	if m == nil {
		m = map[string]string{}
		j.cookies[host] = m
	}
	for _, c := range set {
		if c.MaxAge < 0 || c.Value == "" {
			delete(m, c.Name)
		} else if len(m) < maxSessionCookies || m[c.Name] != "" {
			m[c.Name] = c.Value
		}
	}
}

// testTarget splits a test_request path into the host it goes to and the
// path. A full URL is accepted for the preview's own two hosts, so an agent
// can follow a redirect to the sign-in host by hand.
func (p *Platform) testTarget(app, stageName, raw string) (host, path string, err error) {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		return "", raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", errors.New("path must start with /")
	}
	h := strings.ToLower(u.Host)
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	strip := func(s string) string {
		if hh, _, err := net.SplitHostPort(s); err == nil {
			return hh
		}
		return s
	}
	for _, own := range []string{p.host(app, stageName), p.idHost(app, stageName)} {
		if strip(own) == h {
			path := u.EscapedPath()
			if path == "" {
				path = "/"
			}
			if u.RawQuery != "" {
				path += "?" + u.RawQuery
			}
			return own, path, nil
		}
	}
	return "", "", fmt.Errorf("%s is not this app's %s address: give a path such as /bookings, or a full URL on %s or %s", u.Host, stageName, p.host(app, stageName), p.idHost(app, stageName))
}

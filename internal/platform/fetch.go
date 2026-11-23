package platform

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// http.fetch lets scripts call external HTTPS APIs, on the owner's terms:
//   - only hosts listed in the definition and allowed by the owner, HTTPS on
//     port 443, never private or local addresses (checked after DNS);
//   - API keys live in the platform, encrypted, each bound to the host it was
//     entered for; scripts name a secret and never see its value;
//   - redirects are not followed, so a key never travels to another host;
//   - per-run, per-minute and monthly limits by plan.
// The request runs in this process: script workers have no network.

// FetchCallsPerPlan is how many http.fetch calls an account's live apps may
// make per calendar month.
var FetchCallsPerPlan = map[string]int{planFree: 1000, planPro: 20000, planBusiness: 100000}

// Per-run limits; variables so tests can change them.
var (
	fetchPerRun     = 5
	fetchWaitPerRun = 8 * time.Second // a script worker is held while a call waits
	// fetchWaitPerOwner is how long, per minute, the scripts of one account
	// may hold workers waiting for external answers; it keeps an account
	// that calls slow hosts from occupying the workers.
	fetchWaitPerOwner = 30 * time.Second
	// fetchUnconfirmedPerDay is the daily calls of an account whose email
	// is not confirmed yet.
	fetchUnconfirmedPerDay = 100
	// PreviewFetchWindow is how long preview makes real calls after the
	// owner turns that on.
	PreviewFetchWindow = 2 * time.Hour
	fetchDefaultTimout = 5 * time.Second
	fetchMaxTimeout    = 10 * time.Second
	fetchMaxBody       = 256 << 10
	fetchMaxResponse   = 1 << 20
	fetchPerMinute     = 60
)

// HostState is a host an app asks for, and whether the owner allowed it.
type HostState struct {
	Host, Purpose string
	Allowed       bool
	AllowedBy     string
	AllowedAt     string
}

// SecretState is a secret an app asks for and whether the owner entered it.
type SecretState struct {
	appdef.Secret
	Set       bool
	Hint      string // last characters of the value
	SavedHost string // host the value was entered for
	Mismatch  bool   // the definition now sends it to another host: it is not used until entered again
}

func (p *Platform) ensureFetchTables() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS app_hosts (app TEXT NOT NULL, host TEXT NOT NULL, allowed_by TEXT NOT NULL,
		allowed_at TEXT NOT NULL DEFAULT (datetime('now')), PRIMARY KEY (app, host))`)
	if err == nil {
		_, err = p.db.Exec(`CREATE TABLE IF NOT EXISTS app_secrets (app TEXT NOT NULL, name TEXT NOT NULL, host TEXT NOT NULL,
			value BLOB NOT NULL, hint TEXT NOT NULL, updated_at TEXT NOT NULL DEFAULT (datetime('now')), PRIMARY KEY (app, name))`)
	}
	return err
}

func (p *Platform) secretKey() []byte {
	mac := hmac.New(sha256.New, p.live.key)
	mac.Write([]byte("renoxium app secrets v1"))
	return mac.Sum(nil)
}

func (p *Platform) seal(app, name string, plain []byte) ([]byte, error) {
	block, _ := aes.NewCipher(p.secretKey())
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return gcm.Seal(nonce, nonce, plain, []byte(app+"/"+name)), nil
}

func (p *Platform) open(app, name string, sealed []byte) ([]byte, error) {
	block, _ := aes.NewCipher(p.secretKey())
	gcm, _ := cipher.NewGCM(block)
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("secret is damaged")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(app+"/"+name))
}

// Connections lists the hosts and secrets of the app's newest definition,
// with the owner's decisions, for the owner's page.
func (p *Platform) Connections(app string) ([]HostState, []SecretState, error) {
	def, _, err := p.Definition(app, 0)
	if err != nil {
		return nil, nil, err
	}
	var hosts []HostState
	for _, h := range def.Hosts {
		s := HostState{Host: h.Host, Purpose: h.Purpose}
		if p.db.QueryRow(`SELECT allowed_by, allowed_at FROM app_hosts WHERE app = ? AND host = ?`, app, h.Host).Scan(&s.AllowedBy, &s.AllowedAt) == nil {
			s.Allowed = true
		}
		hosts = append(hosts, s)
	}
	var secrets []SecretState
	for _, sec := range def.Secrets {
		s := SecretState{Secret: sec}
		if p.db.QueryRow(`SELECT hint, host FROM app_secrets WHERE app = ? AND name = ?`, app, sec.Name).Scan(&s.Hint, &s.SavedHost) == nil {
			s.Set = true
			s.Mismatch = s.SavedHost != sec.Host
		}
		secrets = append(secrets, s)
	}
	return hosts, secrets, nil
}

// AllowHost lets the app's scripts call a host listed in its definition.
func (p *Platform) AllowHost(app, host, by string) error {
	def, _, err := p.Definition(app, 0)
	if err != nil {
		return err
	}
	for _, h := range def.Hosts {
		if h.Host == host {
			_, err := p.db.Exec(`INSERT OR REPLACE INTO app_hosts (app, host, allowed_by) VALUES (?, ?, ?)`, app, host, by)
			return err
		}
	}
	return fmt.Errorf("%s is not requested by this app", host)
}

// RevokeHost stops calls to a host at once.
func (p *Platform) RevokeHost(app, host string) error {
	_, err := p.db.Exec(`DELETE FROM app_hosts WHERE app = ? AND host = ?`, app, host)
	return err
}

// SetSecret stores a secret's value for the host the definition names now.
func (p *Platform) SetSecret(app, name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n") {
		return errors.New("enter the key on one line (at most 4096 characters)")
	}
	def, _, err := p.Definition(app, 0)
	if err != nil {
		return err
	}
	for _, s := range def.Secrets {
		if s.Name != name {
			continue
		}
		if strings.EqualFold(s.Prefix, "Basic") && !strings.Contains(value, ":") {
			return errors.New("enter it as user:password")
		}
		sealed, err := p.seal(app, name, []byte(value))
		if err != nil {
			return err
		}
		hint := ""
		if len(value) > 8 {
			hint = value[len(value)-4:]
		}
		_, err = p.db.Exec(`INSERT OR REPLACE INTO app_secrets (app, name, host, value, hint) VALUES (?, ?, ?, ?, ?)`, app, name, s.Host, sealed, hint)
		return err
	}
	return fmt.Errorf("%s is not requested by this app", name)
}

// DeleteSecret removes a secret's value.
func (p *Platform) DeleteSecret(app, name string) error {
	_, err := p.db.Exec(`DELETE FROM app_secrets WHERE app = ? AND name = ?`, app, name)
	return err
}

// FetchUsage is an account's live http.fetch calls this month and its quota.
func (p *Platform) FetchUsage(owner string) (used, quota int) {
	return p.usage(owner, "fetch"), FetchCallsPerPlan[p.quotaPlan(owner)]
}

type fetchInput struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	Body      any               `json:"body"`
	Secret    string            `json:"secret"`
	TimeoutMS int               `json:"timeout_ms"`
}

// fetchError is shown to the script.
func fetchError(format string, args ...any) error { return fmt.Errorf("http.fetch: "+format, args...) }

// httpFetch answers http.fetch({url, method, headers, body, secret, timeout_ms}).
func (h *scriptHost) httpFetch(ctx context.Context, arg any) (any, error) {
	p, st := h.p, h.st
	var in fetchInput
	if err := decodeArg(arg, &in); err != nil || in.URL == "" {
		return nil, fetchError("expects {url, method, headers, body, secret, timeout_ms}")
	}
	u, err := url.Parse(in.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || u.Hostname() == "" {
		return nil, fetchError("url must be https://host/path on port 443")
	}
	host := strings.ToLower(u.Hostname())
	listed := false
	for _, hst := range st.def.Hosts {
		listed = listed || hst.Host == host
	}
	if !listed {
		return nil, fetchError("%s is not in the app's hosts; add it to hosts with a purpose, then the owner allows it", host)
	}
	var allowed int
	p.db.QueryRow(`SELECT count(*) FROM app_hosts WHERE app = ? AND host = ?`, st.app, host).Scan(&allowed)
	if allowed == 0 {
		return nil, fetchError("the owner has not allowed %s yet; they allow it at %s/apps/%s/connections", host, p.cfg.SiteURL, st.app)
	}
	method := strings.ToUpper(in.Method)
	if method == "" {
		method = http.MethodGet
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return nil, fetchError("method must be GET, POST, PUT, PATCH or DELETE")
	}

	// Limits: per run, per minute and, on live, per month by plan.
	h.mu.Lock()
	h.fetches++
	n, waited := h.fetches, h.fetchWait
	h.mu.Unlock()
	if n > fetchPerRun {
		return nil, fetchError("at most %d calls per request or job", fetchPerRun)
	}
	if waited >= fetchWaitPerRun {
		return nil, fetchError("external calls in this request already took %s", fetchWaitPerRun)
	}
	// Preview runs on a copy of live data, and an agent can change preview
	// scripts without the owner seeing them. So preview only makes real
	// calls while the owner has turned that on.
	if st.name == Preview && !p.PreviewFetchOn(st.app) {
		return nil, fetchError("preview does not call other services until the owner turns on test calls (for %s) at %s/apps/%s/connections",
			PreviewFetchWindow, p.cfg.SiteURL, st.app)
	}
	if !p.allow("fetch:"+st.app+":"+st.name, fetchPerMinute, time.Minute) {
		return nil, fetchError("more than %d calls a minute for this app", fetchPerMinute)
	}
	// The monthly quota counts live and preview alike: both send real
	// requests from Renoxium's address.
	owner := p.ownerOf(st.app)
	if p.cfg.OwnerPlan != nil {
		if used, quota := p.FetchUsage(owner); used >= quota {
			return nil, fetchError("the account used all %d external calls of this month; for more, %s", quota, p.moreHint(p.quotaPlan(owner), st.app))
		}
	}
	if p.cfg.OwnerEmail != nil {
		if _, confirmed := p.cfg.OwnerEmail(owner); !confirmed && !p.allowDurable("fetch-unconfirmed:"+owner, fetchUnconfirmedPerDay, 24*time.Hour) {
			return nil, fetchError("confirm your Renoxium email address to make more than %d external calls a day", fetchUnconfirmedPerDay)
		}
	}
	if p.usedBudget("fetch-wait:"+owner, time.Minute) >= int(fetchWaitPerOwner.Milliseconds()) {
		return nil, fetchError("this account's external calls waited more than %s in the last minute; call faster services or fewer of them", fetchWaitPerOwner)
	}

	var body io.Reader
	contentType := ""
	switch b := in.Body.(type) {
	case nil:
	case string:
		body = strings.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			return nil, fetchError("body cannot be encoded as JSON")
		}
		body, contentType = bytes.NewReader(raw), "application/json"
	}
	if body != nil {
		raw, _ := io.ReadAll(io.LimitReader(body, int64(fetchMaxBody)+1))
		if len(raw) > fetchMaxBody {
			return nil, fetchError("body is larger than %d KB", fetchMaxBody>>10)
		}
		body = bytes.NewReader(raw)
	}

	// The secret: entered by the owner for exactly this host.
	var secretValue string
	var spec appdef.Secret
	if in.Secret != "" {
		found := false
		for _, s := range st.def.Secrets {
			if s.Name == in.Secret {
				spec, found = s, true
			}
		}
		if !found {
			return nil, fetchError("secret %s is not in the app's secrets", in.Secret)
		}
		if spec.Host != host {
			return nil, fetchError("secret %s is only sent to %s", in.Secret, spec.Host)
		}
		var sealed []byte
		var savedHost string
		if err := p.db.QueryRow(`SELECT value, host FROM app_secrets WHERE app = ? AND name = ?`, st.app, in.Secret).Scan(&sealed, &savedHost); err != nil {
			return nil, fetchError("the owner has not entered %s yet; they enter it at %s/apps/%s/connections", in.Secret, p.cfg.SiteURL, st.app)
		}
		if savedHost != host {
			return nil, fetchError("%s was entered for %s; the owner must enter it again for %s", in.Secret, savedHost, host)
		}
		plain, err := p.open(st.app, in.Secret, sealed)
		if err != nil {
			return nil, fetchError("secret %s cannot be read; the owner must enter it again", in.Secret)
		}
		secretValue = string(plain)
		if spec.Query != "" {
			q := u.Query()
			q.Set(spec.Query, secretValue)
			u.RawQuery = q.Encode()
		}
	}

	timeout := fetchDefaultTimout
	if in.TimeoutMS > 0 {
		timeout = min(time.Duration(in.TimeoutMS)*time.Millisecond, fetchMaxTimeout)
	}
	timeout = min(timeout, fetchWaitPerRun-waited) // never past the run's waiting time
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fetchError("%v", err)
	}
	for k, v := range in.Headers {
		switch strings.ToLower(k) {
		case "host", "content-length", "transfer-encoding", "connection", "upgrade", "te", "trailer", "proxy-authorization":
			continue
		}
		if !headerName(k) || strings.ContainsAny(v, "\r\n") {
			return nil, fetchError("header %q is not valid", k)
		}
		req.Header.Set(k, v)
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Renoxium-App/1 (+https://www.renoxium.com)")
	}
	if spec.Header != "" {
		v := secretValue
		switch {
		case strings.EqualFold(spec.Prefix, "Basic"):
			v = "Basic " + base64.StdEncoding.EncodeToString([]byte(secretValue))
		case spec.Prefix != "":
			v = spec.Prefix + " " + secretValue
		}
		req.Header.Set(spec.Header, v)
	}

	start := time.Now()
	resp, err := p.fetchClient().Do(req)
	took := time.Since(start)
	h.mu.Lock()
	h.fetchWait += took
	h.mu.Unlock()
	p.spend("fetch-wait:"+owner, int(took.Milliseconds()), 1<<30, time.Minute)
	p.meterAdd(st.app, st.name, mFetches, 1)
	if p.cfg.OwnerPlan != nil {
		p.addUsage(owner, "fetch")
	}
	logURL := "https://" + host + u.EscapedPath()
	if err != nil {
		msg := err.Error()
		if secretValue != "" {
			msg = strings.ReplaceAll(msg, secretValue, "[secret]")
			msg = strings.ReplaceAll(msg, url.QueryEscape(secretValue), "[secret]")
		}
		p.event(st, "server", "warn", h.rid, fmt.Sprintf("fetch %s %s failed after %s: %s", method, logURL, took.Round(time.Millisecond), msg))
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fetchError("%s did not answer within %s", host, timeout)
		}
		return nil, fetchError("%s could not be reached", host)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, int64(fetchMaxResponse)+1))
	truncated := len(raw) > fetchMaxResponse
	if truncated {
		raw = raw[:fetchMaxResponse]
	}
	text := string(raw)
	if secretValue != "" && len(secretValue) >= 6 { // an API that echoes the request does not hand the key to the script
		text = strings.ReplaceAll(text, secretValue, "[secret]")
	}
	p.event(st, "server", "info", h.rid, fmt.Sprintf("fetch %s %s -> %d in %s", method, logURL, resp.StatusCode, took.Round(time.Millisecond)))
	headers := map[string]any{}
	for k, v := range resp.Header {
		if lk := strings.ToLower(k); lk != "set-cookie" {
			headers[lk] = v[0]
		}
	}
	out := map[string]any{"status": resp.StatusCode, "ok": resp.StatusCode >= 200 && resp.StatusCode < 300, "headers": headers, "body": text, "truncated": truncated}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); (mt == "application/json" || strings.HasSuffix(mt, "+json")) && !truncated {
		var v any
		if json.Unmarshal([]byte(text), &v) == nil {
			out["json"] = v
		}
	}
	return out, nil
}

func headerName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

// fetchClient refuses private, local and special addresses after DNS
// resolution, so a host name cannot point the request inside the server's
// network, and never follows redirects.
func (p *Platform) fetchClient() *http.Client {
	if p.cfg.FetchTransport != nil {
		return &http.Client{Transport: p.cfg.FetchTransport, CheckRedirect: noRedirects}
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: func(network, address string, _ syscallRawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		if ip := net.ParseIP(host); ip == nil || !publicIP(ip) {
			return fmt.Errorf("address %s is not a public internet address", host)
		}
		return nil
	}}
	return &http.Client{CheckRedirect: noRedirects, Transport: &http.Transport{
		Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true, MaxIdleConnsPerHost: 4, IdleConnTimeout: 60 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: fetchMaxTimeout, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}}
}

func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

var blockedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, c := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
		"192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "100::/64", "2001:db8::/32", "fc00::/7", "fe80::/10", "ff00::/8"} {
		_, n, _ := net.ParseCIDR(c)
		out = append(out, n)
	}
	return out
}()

func publicIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

// PreviewFetchOn reports whether preview may make real http.fetch calls now.
func (p *Platform) PreviewFetchOn(app string) bool {
	return time.Now().Before(p.PreviewFetchUntil(app))
}

// PreviewFetchUntil is when preview's real calls end; zero when off.
func (p *Platform) PreviewFetchUntil(app string) time.Time {
	var until int64
	p.db.QueryRow(`SELECT preview_fetch_until FROM apps WHERE name = ?`, app).Scan(&until)
	if until == 0 {
		return time.Time{}
	}
	return time.Unix(until, 0)
}

// SetPreviewFetch turns preview's real calls on for PreviewFetchWindow, or off.
func (p *Platform) SetPreviewFetch(app string, on bool) error {
	var until int64
	if on {
		until = time.Now().Add(PreviewFetchWindow).Unix()
	}
	_, err := p.db.Exec(`UPDATE apps SET preview_fetch_until = ? WHERE name = ?`, until, app)
	return err
}

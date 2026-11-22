package platform

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Realtime: scripts publish small JSON messages to named channels with
// realtime.publish(channel, data); browsers receive them with
// EventSource("/_rx/live/<channel>"). The hub runs in this process, so a
// waiting browser costs a goroutine, never a script worker. Channels named
// "public:..." are open to anyone; any other channel needs a token from
// realtime.token(channel), which a script hands only to the right visitor
// (for example the buyer of an order). Each channel keeps its last message,
// so a new subscriber sees the latest state (such as a courier's position)
// at once.

// LivePrefix is where browsers subscribe.
const LivePrefix = "/_rx/live/"

// Limits; variables so tests can change them.
var (
	LiveSubscribersPerPlan = map[string]int{planFree: 50, planPro: 500, planBusiness: 2000}
	livePerVisitor         = 20               // open subscriptions per visitor address and app
	livePublishPerMinute   = 600              // publishes per app
	liveMaxMessage         = 8 << 10          // bytes of JSON per message
	liveKeepAlive          = 25 * time.Second // comment lines keep proxies from closing the stream
	liveMaxConnection      = time.Hour        // then the browser reconnects by itself
	liveTokenTTL           = 24 * time.Hour   // realtime.token lifetime
	liveLastTTL            = 6 * time.Hour    // how long a channel's last message is kept
	// liveLastChannels is how many channels of one app stage keep their last
	// message. Channel names are free text, so without a cap an app could
	// fill the server's memory by publishing to ever new names. Past the cap
	// the channel published to longest ago forgets its message.
	liveLastChannels = 200
	// liveMaxTotal caps open streams over all apps: each holds a connection
	// and a goroutine on a small server.
	liveMaxTotal = 5000
)

var channelRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,99}$`)

type liveMessage struct {
	data []byte
	at   time.Time
}

type liveHub struct {
	mu     sync.Mutex
	subs   map[string]map[chan []byte]bool   // "app/stage/channel" -> subscribers
	counts map[string]int                    // "app/stage" -> subscribers
	total  int                               // all subscribers
	last   map[string]map[string]liveMessage // "app/stage" -> "app/stage/channel" -> last message
	key    []byte
	stop   chan struct{}
	once   sync.Once
}

// StopLive ends every open stream, so a server shutdown does not wait for
// them; browsers reconnect by themselves.
func (p *Platform) StopLive() { p.live.once.Do(func() { close(p.live.stop) }) }

func (h *liveHub) init(key []byte) {
	h.subs, h.counts, h.last = map[string]map[chan []byte]bool{}, map[string]int{}, map[string]map[string]liveMessage{}
	h.stop = make(chan struct{})
	h.key = key
	if len(h.key) == 0 {
		h.key = make([]byte, 32)
		rand.Read(h.key)
	}
}

func (h *liveHub) publish(appStage, key string, data []byte) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	last := h.last[appStage]
	if last == nil {
		last = map[string]liveMessage{}
		h.last[appStage] = last
	}
	if _, known := last[key]; !known && len(last) >= liveLastChannels {
		oldest, at := "", time.Now()
		for k, m := range last {
			if time.Since(m.at) > liveLastTTL {
				delete(last, k)
			} else if m.at.Before(at) {
				oldest, at = k, m.at
			}
		}
		if len(last) >= liveLastChannels {
			delete(last, oldest)
		}
	}
	last[key] = liveMessage{data: data, at: time.Now()}
	n := 0
	for c := range h.subs[key] {
		select {
		case c <- data:
			n++
		default: // a slow browser misses a message rather than slowing everyone
		}
	}
	return n
}

// subscribe adds a subscriber unless the app is at its limit, and returns
// the channel's last message.
func (h *liveHub) subscribe(appStage, key string, limit int) (chan []byte, []byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.counts[appStage] >= limit || h.total >= liveMaxTotal {
		return nil, nil, false
	}
	h.total++
	c := make(chan []byte, 16)
	if h.subs[key] == nil {
		h.subs[key] = map[chan []byte]bool{}
	}
	h.subs[key][c] = true
	h.counts[appStage]++
	var last []byte
	if m, ok := h.last[appStage][key]; ok && time.Since(m.at) < liveLastTTL {
		last = m.data
	}
	return c, last, true
}

func (h *liveHub) unsubscribe(appStage, key string, c chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs[key], c)
	if len(h.subs[key]) == 0 {
		delete(h.subs, key)
	}
	h.counts[appStage]--
	h.total--
}

// LiveSubscribers is how many browsers are subscribed to an app's stage.
func (p *Platform) LiveSubscribers(app, stageName string) int {
	p.live.mu.Lock()
	defer p.live.mu.Unlock()
	return p.live.counts[app+"/"+stageName]
}

func (p *Platform) liveToken(st *stage, channel string, expires time.Time) string {
	mac := hmac.New(sha256.New, p.live.key)
	fmt.Fprintf(mac, "live|%s|%s|%s|%d", st.app, st.name, channel, expires.Unix())
	return strconv.FormatInt(expires.Unix(), 36) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (p *Platform) liveTokenValid(st *stage, channel, token string) bool {
	exp, _, ok := strings.Cut(token, ".")
	n, err := strconv.ParseInt(exp, 36, 64)
	if !ok || err != nil || time.Now().Unix() > n {
		return false
	}
	return hmac.Equal([]byte(token), []byte(p.liveToken(st, channel, time.Unix(n, 0))))
}

type publishInput struct {
	Channel string `json:"channel"`
	Data    any    `json:"data"`
}

// realtimePublish answers realtime.publish({channel, data}).
func (p *Platform) realtimePublish(st *stage, arg any) (any, error) {
	var in publishInput
	if err := decodeArg(arg, &in); err != nil || in.Channel == "" {
		return nil, errors.New("realtime.publish expects {channel, data}")
	}
	if !channelRE.MatchString(in.Channel) {
		return nil, fmt.Errorf("channel %q: use lowercase letters, digits and _ . : - (at most 100), e.g. order-42 or public:queue", in.Channel)
	}
	data, err := json.Marshal(in.Data)
	if err != nil {
		return nil, errors.New("realtime.publish: data cannot be encoded as JSON")
	}
	if len(data) > liveMaxMessage {
		return nil, fmt.Errorf("realtime.publish: data is %d bytes; at most %d", len(data), liveMaxMessage)
	}
	if !p.allow("live-pub:"+st.app+":"+st.name, livePublishPerMinute, time.Minute) {
		return nil, fmt.Errorf("realtime.publish: more than %d messages a minute for this app", livePublishPerMinute)
	}
	n := p.live.publish(st.app+"/"+st.name, st.app+"/"+st.name+"/"+in.Channel, data)
	c := p.meter.counters(st.app, st.name)
	c.add(mRTMessages, 1)
	c.add(mRTDelivered, int64(n))
	return map[string]any{"delivered": n}, nil
}

// realtimeToken answers realtime.token(channel) or realtime.token({channel}).
func (p *Platform) realtimeToken(st *stage, arg any) (any, error) {
	channel, _ := arg.(string)
	if m, ok := arg.(map[string]any); ok {
		channel, _ = m["channel"].(string)
	}
	if !channelRE.MatchString(channel) {
		return nil, fmt.Errorf("realtime.token: channel %q is not valid", channel)
	}
	return p.liveToken(st, channel, time.Now().Add(liveTokenTTL)), nil
}

// serveLive streams a channel to a browser as server-sent events.
func (p *Platform) serveLive(w http.ResponseWriter, r *http.Request, st *stage) {
	channel := strings.TrimPrefix(r.URL.Path, LivePrefix)
	if r.Method != http.MethodGet || !channelRE.MatchString(channel) {
		http.NotFound(w, r)
		return
	}
	if !strings.HasPrefix(channel, "public:") && !p.liveTokenValid(st, channel, r.URL.Query().Get("token")) {
		http.Error(w, "this channel needs a valid token from realtime.token", http.StatusForbidden)
		return
	}
	ip := p.clientIP(r)
	if !p.allow("live-ip:"+st.app+":"+ip, livePerVisitor*3, time.Minute) {
		http.Error(w, "too many connections; try again in a minute", http.StatusTooManyRequests)
		return
	}
	limit := LiveSubscribersPerPlan[planBusiness]
	if p.cfg.OwnerPlan != nil {
		limit = LiveSubscribersPerPlan[p.appQuotaPlan(st.app)]
	}
	appStage, key := st.app+"/"+st.name, st.app+"/"+st.name+"/"+channel
	if !p.liveVisitorOpen(appStage+"/"+ip, 1) {
		http.Error(w, "too many open connections from this address", http.StatusTooManyRequests)
		return
	}
	defer p.liveVisitorOpen(appStage+"/"+ip, -1)
	c, last, ok := p.live.subscribe(appStage, key, limit)
	if !ok {
		p.event(st, "server", "warn", "", fmt.Sprintf("realtime: subscriber limit of %d reached; upgrade the plan for more", limit))
		http.Error(w, "this app has too many live connections right now", http.StatusServiceUnavailable)
		return
	}
	defer p.live.unsubscribe(appStage, key, c)
	p.meterMax(st.app, st.name, mRTPeak, int64(p.LiveSubscribers(st.app, st.name)))
	opened := time.Now()
	defer func() { p.meterAdd(st.app, st.name, mRTConnSec, int64(time.Since(opened).Seconds())) }()

	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{}) // the server's write timeout would end the stream
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n\n")
	if last != nil {
		fmt.Fprintf(w, "data: %s\n\n", last)
	}
	rc.Flush()

	ctx, cancel := context.WithTimeout(r.Context(), liveMaxConnection)
	defer cancel()
	tick := time.NewTicker(liveKeepAlive)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.live.stop:
			return
		case data := <-c:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
		}
		if rc.Flush() != nil {
			return
		}
	}
}

// liveVisitorOpen counts open streams per visitor address; it refuses past
// the per-visitor limit.
func (p *Platform) liveVisitorOpen(key string, delta int) bool {
	p.live.mu.Lock()
	defer p.live.mu.Unlock()
	k := "ip:" + key
	if delta > 0 && p.live.counts[k] >= livePerVisitor {
		return false
	}
	p.live.counts[k] += delta
	if p.live.counts[k] <= 0 {
		delete(p.live.counts, k)
	}
	return true
}

// forgetLive drops the stored last messages of an app (both stages) and of
// stages whose messages have all expired. Called when an app is deleted
// and from Cleanup.
func (p *Platform) forgetLive(app string) {
	p.live.mu.Lock()
	defer p.live.mu.Unlock()
	for appStage, last := range p.live.last {
		if app != "" && (appStage == app+"/"+Live || appStage == app+"/"+Preview) {
			delete(p.live.last, appStage)
			continue
		}
		for k, m := range last {
			if time.Since(m.at) > liveLastTTL {
				delete(last, k)
			}
		}
		if len(last) == 0 {
			delete(p.live.last, appStage)
		}
	}
}

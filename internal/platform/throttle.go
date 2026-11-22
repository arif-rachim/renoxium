package platform

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Operator throttles: an app that slows other apps down can be held to
// fewer concurrent scripts and a per-minute script budget until a chosen
// time, instead of being suspended. A throttle applies to that app alone
// (its own slot channel and its own budget key), and the owner sees why in
// their admin panel.

// Throttle is an operator limit on one app.
type Throttle struct {
	App       string
	Slots     int // scripts of the app at the same time; 0 leaves the normal limit
	PerMinute int // script runs a minute; 0 means no budget
	Until     time.Time
	Reason    string
	By        string // operator email
	CreatedAt string
}

// ScriptsPerApp is how many scripts of one app may run at once.
func (p *Platform) ScriptsPerApp() int { return p.cfg.ScriptsPerApp }

// MaxThrottle is the longest throttle an operator can set at once.
const MaxThrottle = 30 * 24 * time.Hour

type throttleSet struct {
	at time.Time
	m  map[string]Throttle
}

const throttleTTL = 30 * time.Second

func (p *Platform) ensureThrottleTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS app_throttles (app TEXT PRIMARY KEY, slots INTEGER NOT NULL, per_minute INTEGER NOT NULL,
		until INTEGER NOT NULL, reason TEXT NOT NULL, by TEXT NOT NULL, created_at TEXT NOT NULL)`)
	return err
}

func (p *Platform) loadThrottles() *throttleSet {
	set := &throttleSet{at: time.Now(), m: map[string]Throttle{}}
	rows, err := p.db.Query(`SELECT app, slots, per_minute, until, reason, by, created_at FROM app_throttles WHERE until > ?`, time.Now().Unix())
	if err == nil {
		for rows.Next() {
			var t Throttle
			var until int64
			if rows.Scan(&t.App, &t.Slots, &t.PerMinute, &until, &t.Reason, &t.By, &t.CreatedAt) == nil {
				t.Until = time.Unix(until, 0)
				set.m[t.App] = t
			}
		}
		rows.Close()
	}
	p.throttles.Store(set)
	return set
}

// ThrottleOf returns an app's throttle while it lasts.
func (p *Platform) ThrottleOf(app string) (Throttle, bool) {
	set := p.throttles.Load()
	if set == nil {
		set = p.loadThrottles()
	} else if time.Since(set.at) > throttleTTL && p.throttleLoading.CompareAndSwap(false, true) {
		// One request reads the table again; the others use the last copy.
		set = p.loadThrottles()
		p.throttleLoading.Store(false)
	}
	t, ok := set.m[app]
	if !ok || !time.Now().Before(t.Until) {
		return Throttle{}, false
	}
	return t, true
}

// SetThrottle limits an app until t.Until.
func (p *Platform) SetThrottle(t Throttle) error {
	switch {
	case t.Slots < 0 || t.Slots > p.cfg.ScriptsPerApp:
		return fmt.Errorf("concurrent scripts must be between 1 and %d (0 keeps the normal limit)", p.cfg.ScriptsPerApp)
	case t.PerMinute < 0 || t.PerMinute > 100000:
		return errors.New("the per-minute budget must be a positive number of script runs (0 for none)")
	case t.Slots == 0 && t.PerMinute == 0:
		return errors.New("choose fewer concurrent scripts, a per-minute budget, or both")
	case !t.Until.After(time.Now()):
		return errors.New("the throttle must end in the future")
	case time.Until(t.Until) > MaxThrottle:
		return fmt.Errorf("a throttle lasts at most %d days", int(MaxThrottle.Hours()/24))
	}
	if _, err := p.Info(t.App); err != nil {
		return err
	}
	if _, err := p.db.Exec(`INSERT INTO app_throttles (app, slots, per_minute, until, reason, by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (app) DO UPDATE SET slots = excluded.slots, per_minute = excluded.per_minute, until = excluded.until,
		reason = excluded.reason, by = excluded.by, created_at = excluded.created_at`,
		t.App, t.Slots, t.PerMinute, t.Until.Unix(), t.Reason, t.By, now()); err != nil {
		return err
	}
	p.loadThrottles()
	return nil
}

// Unthrottle ends an app's throttle now.
func (p *Platform) Unthrottle(app string) error {
	if _, err := p.db.Exec(`DELETE FROM app_throttles WHERE app = ?`, app); err != nil {
		return err
	}
	p.loadThrottles()
	return nil
}

func (p *Platform) cleanThrottles() {
	p.db.Exec(`DELETE FROM app_throttles WHERE until <= ?`, time.Now().Unix())
}

// Notice is what the owner reads about a throttle in the admin panel; site
// is Renoxium's address.
func (t Throttle) Notice(site string) string {
	var what string
	switch {
	case t.Slots > 0 && t.PerMinute > 0:
		what = fmt.Sprintf("%d script%s at a time and %d script runs a minute", t.Slots, plural(t.Slots), t.PerMinute)
	case t.Slots > 0:
		what = fmt.Sprintf("%d script%s at a time", t.Slots, plural(t.Slots))
	default:
		what = fmt.Sprintf("%d script runs a minute", t.PerMinute)
	}
	msg := "Renoxium has limited this app to " + what + " until " + t.Until.UTC().Format("2 Jan 2006 15:04") + " UTC, because it used much more of the shared server than other apps"
	if t.Reason != "" {
		msg += " (" + t.Reason + ")"
	}
	return msg + ". Visitors may see slower pages or an error while it is busy. Ask the agent that built it to make its scripts lighter. If the business needs this much, contact Renoxium about a larger plan at " + site + "/support."
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// slotError is why a script did not get a slot.
type slotError struct{ msg string }

func (e *slotError) Error() string { return e.msg }

var (
	errBusyApp   = &slotError{"too many requests running at once for this app; the request waited 3s and gave up"}
	errBusyOwner = &slotError{"too many requests running at once for this account's apps; the request waited 3s and gave up"}
)

// acquireSlot waits up to 3 seconds for a script slot of the app's owner
// and one of the app itself (and of its throttle, if the operator set one).
// The owner cap stops one account from filling every worker by spreading
// slow scripts over many apps. The waits and refusals are metered.
func (p *Platform) acquireSlot(ctx context.Context, st *stage) (release func(), err error) {
	app := st.app
	start := time.Now()
	c := p.meter.counters(app, st.name)
	refuse := func(f int, err error) (func(), error) {
		c.add(f, 1)
		c.add(mSlotWaitUS, time.Since(start).Microseconds())
		return nil, err
	}
	var throttleSlots chan struct{}
	if t, ok := p.ThrottleOf(app); ok {
		if t.PerMinute > 0 && !p.allow("throttle-runs:"+app, t.PerMinute, time.Minute) {
			return refuse(mRefusedThrottle, &slotError{fmt.Sprintf("this app is throttled by Renoxium until %s UTC to %d script runs a minute, and the budget for this minute is used up",
				t.Until.UTC().Format("2 Jan 15:04"), t.PerMinute)})
		}
		if t.Slots > 0 {
			throttleSlots = p.slotsFor(fmt.Sprintf("throttle:%s:%d", app, t.Slots), t.Slots)
		}
	}
	ownerSlots := p.slotsFor("owner:"+p.ownerOf(app), p.cfg.ScriptsPerOwner)
	appSlots := p.slotsFor("app:"+app, p.cfg.ScriptsPerApp)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if throttleSlots != nil {
		select {
		case throttleSlots <- struct{}{}:
		case <-ctx.Done():
			return refuse(mRefusedThrottle, &slotError{"this app is throttled by Renoxium to fewer scripts at a time; the request waited 3s and gave up"})
		}
	}
	freeThrottle := func() {
		if throttleSlots != nil {
			<-throttleSlots
		}
	}
	select {
	case ownerSlots <- struct{}{}:
	case <-ctx.Done():
		freeThrottle()
		return refuse(mRefusedOwner, errBusyOwner)
	}
	select {
	case appSlots <- struct{}{}:
	case <-ctx.Done():
		<-ownerSlots
		freeThrottle()
		return refuse(mRefusedApp, errBusyApp)
	}
	if wait := time.Since(start).Microseconds(); wait > 0 {
		c.add(mSlotWaitUS, wait)
		if wait > 1000 {
			c.add(mWaits, 1)
		}
	}
	return func() { <-appSlots; <-ownerSlots; freeThrottle() }, nil
}

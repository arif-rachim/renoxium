package platform

import (
	"time"
)

// Durable limits. The limits in allow live in memory and start again when
// the server restarts, which is fine for request rates but not for limits
// that cap money, reputation or guesses: daily emails and WhatsApp
// messages, sign-in code guesses per number. Those are stored here.

func (p *Platform) ensureLimitTable() error {
	_, err := p.db.Exec(`CREATE TABLE IF NOT EXISTS rate_windows (key TEXT PRIMARY KEY, start INTEGER NOT NULL, count INTEGER NOT NULL)`)
	return err
}

// allowDurable counts one use of key in a window of period that starts with
// its first use, and reports whether it is within max. Like allow, a
// refused use still counts.
func (p *Platform) allowDurable(key string, max int, period time.Duration) bool {
	return p.addDurable(key, 1, period) <= max
}

// addDurable adds n to key's window and returns the window's new total. On
// a database error it returns a huge total: a limit that cannot be checked
// refuses.
func (p *Platform) addDurable(key string, n int, period time.Duration) int {
	now := time.Now().Unix()
	var count int
	err := p.db.QueryRow(`INSERT INTO rate_windows (key, start, count) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET
			count = CASE WHEN start <= ? THEN excluded.count ELSE count + excluded.count END,
			start = CASE WHEN start <= ? THEN excluded.start ELSE start END
		RETURNING count`, key, now, n, now-int64(period.Seconds()), now-int64(period.Seconds())).Scan(&count)
	if err != nil {
		return 1 << 30
	}
	return count
}

// usedDurable is key's current total without counting a use.
func (p *Platform) usedDurable(key string, period time.Duration) int {
	var start int64
	var count int
	if p.db.QueryRow(`SELECT start, count FROM rate_windows WHERE key = ?`, key).Scan(&start, &count) != nil ||
		start <= time.Now().Add(-period).Unix() {
		return 0
	}
	return count
}

// cleanDurable deletes windows that ended more than a day ago (no window is
// longer than a day).
func (p *Platform) cleanDurable() {
	p.db.Exec(`DELETE FROM rate_windows WHERE start < ?`, time.Now().Add(-48*time.Hour).Unix())
}

// spend adds amount to an in-memory budget of max per period and reports
// whether the budget had room before this use. It is for budgets measured
// in something other than calls, such as milliseconds spent waiting.
func (p *Platform) spend(key string, amount, max int, period time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	w := p.limits[key]
	if w == nil || time.Since(w.start) > period {
		w = &window{start: time.Now(), period: period}
		p.limits[key] = w
	}
	ok := w.count < max
	w.count += amount
	return ok
}

// usedBudget is what spend has added to key in the current period.
func (p *Platform) usedBudget(key string, period time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if w := p.limits[key]; w != nil && time.Since(w.start) <= period {
		return w.count
	}
	return 0
}

package platform

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Funnel events: the first time an owner reached a step that existing
// tables cannot tell afterwards, one row per (owner, kind), written when it
// happens. Sign-up, email confirmation, agent connection and paid plans are
// in the accounts database.
const (
	FunnelApp      = "app"      // first app created; app = its name
	FunnelPublish  = "publish"  // first publish to live; detail = example it was built from, if recognised
	FunnelActivity = "activity" // first customer activity on live; detail = "notice" (notify.owner) or "card" (paid card payment)
)

// FunnelEvent is an owner's first event of a kind.
type FunnelEvent struct {
	App, Detail string
	At          time.Time
}

func (p *Platform) ensureGrowthTables() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS funnel_events (owner TEXT NOT NULL, kind TEXT NOT NULL, app TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
			at TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS funnel_events_first ON funnel_events (owner, kind)`,
		// Figures the operator enters on /operator (marketing spend per month, gross margin).
		`CREATE TABLE IF NOT EXISTS operator_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)`,
	} {
		if _, err := p.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

// funnel records an owner's first event of a kind. It never fails the
// caller: after the first time it costs a map lookup, and the first time
// one indexed insert.
func (p *Platform) funnel(owner, kind, app, detail string) {
	if owner == "" || owner == DemoOwner {
		return
	}
	key := owner + "\x00" + kind
	if _, seen := p.funnelSeen.Load(key); seen {
		return
	}
	if _, err := p.db.Exec(`INSERT OR IGNORE INTO funnel_events (owner, kind, app, detail, at) VALUES (?, ?, ?, ?, ?)`,
		owner, kind, app, detail, now()); err != nil {
		slog.Warn("funnel event not recorded", "owner", owner, "kind", kind, "err", err)
		return
	}
	p.funnelSeen.Store(key, true)
}

// backfillEvent records an event found in existing data, keeping the
// earliest time when one is already recorded.
func backfillEvent(tx *sql.Tx, owner, kind, app, detail string, at time.Time) error {
	if owner == "" || owner == DemoOwner || at.IsZero() {
		return nil
	}
	_, err := tx.Exec(`INSERT INTO funnel_events (owner, kind, app, detail, at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (owner, kind) DO UPDATE SET app = excluded.app, detail = excluded.detail, at = excluded.at WHERE excluded.at < funnel_events.at`,
		owner, kind, app, detail, at.UTC().Format(time.RFC3339))
	return err
}

// BackfillFunnel derives funnel events from what is already stored, for
// owners who reached a step before events were recorded: the first app
// (apps that still exist), the first publish (time of the earliest stored
// version up to the live one, so it may be early) and the first paid card
// payment in a live app that takes cards. Owner notices sent before are not
// stored anywhere and are not backfilled. Call it at startup; it is safe
// to repeat.
func (p *Platform) BackfillFunnel(ctx context.Context) error {
	type appRow struct {
		name, owner, created string
		live                 int
		cards                bool
	}
	rows, err := p.db.QueryContext(ctx, `SELECT a.name, a.owner, a.created_at, a.live_version, coalesce(pa.charges_enabled, 0)
		FROM apps a LEFT JOIN payment_accounts pa ON pa.app = a.name WHERE a.owner NOT IN ('', ?)`, DemoOwner)
	if err != nil {
		return err
	}
	var apps []appRow
	for rows.Next() {
		var a appRow
		if err := rows.Scan(&a.name, &a.owner, &a.created, &a.live, &a.cards); err != nil {
			rows.Close()
			return err
		}
		apps = append(apps, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	known := map[string]bool{} // owner + kind already recorded
	if rows, err := p.db.QueryContext(ctx, `SELECT owner, kind FROM funnel_events`); err == nil {
		for rows.Next() {
			var o, k string
			if rows.Scan(&o, &k) == nil {
				known[o+"\x00"+k] = true
			}
		}
		rows.Close()
	}

	type found struct {
		app, detail string
		at          time.Time
	}
	first := map[string]map[string]found{FunnelApp: {}, FunnelPublish: {}, FunnelActivity: {}}
	keep := func(kind, owner, app, detail string, at time.Time) {
		if at.IsZero() {
			return
		}
		if f, ok := first[kind][owner]; !ok || at.Before(f.at) {
			first[kind][owner] = found{app, detail, at}
		}
	}
	for _, a := range apps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		keep(FunnelApp, a.owner, a.name, "", parseStamp(a.created))
		if a.live == 0 {
			continue
		}
		if !known[a.owner+"\x00"+FunnelPublish] {
			var at string
			p.db.QueryRowContext(ctx, `SELECT coalesce(min(created_at), '') FROM versions WHERE app = ? AND n <= ?`, a.name, a.live).Scan(&at)
			detail := ""
			if def, _, err := p.Definition(a.name, a.live); err == nil {
				detail = guessTemplate(def)
			}
			keep(FunnelPublish, a.owner, a.name, detail, parseStamp(at))
		}
		if a.cards && !known[a.owner+"\x00"+FunnelActivity] {
			st, err := p.loadStage(a.name, Live)
			if err != nil || st == nil {
				continue
			}
			var at string
			st.admin.QueryRowContext(ctx, `SELECT coalesce(min(coalesce(updated_at, created_at)), '') FROM rx_payments
				WHERE status IN ('paid', 'partially_refunded', 'refunded') AND amount > 0`).Scan(&at)
			keep(FunnelActivity, a.owner, a.name, "card", parseStamp(at))
		}
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for kind, owners := range first {
		for owner, f := range owners {
			if err := backfillEvent(tx, owner, kind, f.app, f.detail, f.at); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// parseStamp reads the platform's RFC 3339 times and SQLite's datetime().
func parseStamp(v string) time.Time {
	for _, layout := range []string{time.RFC3339, time.DateTime} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// FunnelEvents returns every owner's first events, by owner and kind.
func (p *Platform) FunnelEvents(ctx context.Context) (map[string]map[string]FunnelEvent, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT owner, kind, app, detail, at FROM funnel_events`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]FunnelEvent{}
	for rows.Next() {
		var owner, kind, at string
		var e FunnelEvent
		if err := rows.Scan(&owner, &kind, &e.App, &e.Detail, &at); err != nil {
			return nil, err
		}
		e.At = parseStamp(at)
		if out[owner] == nil {
			out[owner] = map[string]FunnelEvent{}
		}
		out[owner][kind] = e
	}
	return out, rows.Err()
}

// SuspendedOwners are the owners with an app taken offline.
func (p *Platform) SuspendedOwners(ctx context.Context) (map[string]bool, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT DISTINCT owner FROM apps WHERE suspended != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var o string
		if err := rows.Scan(&o); err != nil {
			return nil, err
		}
		out[o] = true
	}
	return out, rows.Err()
}

// CardFees sums Renoxium's application fee on live card payments completed
// since a time, in minor units per currency. It reads the live database of
// every app that takes cards, so callers should cache the answer.
func (p *Platform) CardFees(ctx context.Context, since time.Time) (map[string]int64, error) {
	byApp, err := p.CardFeesByApp(ctx, since)
	out := map[string]int64{}
	for _, fees := range byApp {
		for cur, n := range fees {
			out[cur] += n
		}
	}
	return out, err
}

// CardFeesByApp is CardFees per app: app -> currency -> minor units.
func (p *Platform) CardFeesByApp(ctx context.Context, since time.Time) (map[string]map[string]int64, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT a.name FROM apps a JOIN payment_accounts pa ON pa.app = a.name
		WHERE pa.charges_enabled = 1 AND a.live_version > 0 AND a.owner != ?`, DemoOwner)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	rows.Close()
	out := map[string]map[string]int64{}
	for _, name := range names {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		st, err := p.loadStage(name, Live)
		if err != nil || st == nil {
			continue
		}
		list, err := st.admin.QueryContext(ctx, `SELECT currency, sum(application_fee) FROM rx_payments
			WHERE status IN ('paid', 'partially_refunded') AND application_fee > 0 AND coalesce(updated_at, created_at) >= ? GROUP BY currency`,
			since.UTC().Format(time.DateTime))
		if err != nil {
			continue
		}
		for list.Next() {
			var cur string
			var fee int64
			if list.Scan(&cur, &fee) == nil {
				if out[name] == nil {
					out[name] = map[string]int64{}
				}
				out[name][cur] += fee
			}
		}
		list.Close()
	}
	return out, nil
}

// Operator settings for unit economics.
const (
	DefaultGrossMarginPct = 80
	spendPrefix           = "marketing_spend:"
	marginKey             = "gross_margin_pct"
)

func (p *Platform) setSetting(key, value string) error {
	_, err := p.db.Exec(`INSERT INTO operator_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now())
	return err
}

// MarketingSpend returns the AED amounts the operator entered, by month
// ("2006-01").
func (p *Platform) MarketingSpend() (map[string]int64, error) {
	rows, err := p.db.Query(`SELECT substr(key, ?), value FROM operator_settings WHERE key LIKE ? ESCAPE '\'`,
		len(spendPrefix)+1, likeEscape(spendPrefix)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var month, v string
		if err := rows.Scan(&month, &v); err != nil {
			return nil, err
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			out[month] = n
		}
	}
	return out, rows.Err()
}

// SetMarketingSpend stores a month's marketing spend in AED.
func (p *Platform) SetMarketingSpend(month string, aed int64) error {
	if _, err := time.Parse("2006-01", month); err != nil || len(month) != 7 {
		return errors.New("month must look like 2026-01")
	}
	if aed < 0 || aed > 1e9 {
		return errors.New("spend must be between 0 and 1,000,000,000 AED")
	}
	return p.setSetting(spendPrefix+month, strconv.FormatInt(aed, 10))
}

// GrossMargin is the operator's gross margin in percent (default 80).
func (p *Platform) GrossMargin() int {
	var v string
	if p.db.QueryRow(`SELECT value FROM operator_settings WHERE key = ?`, marginKey).Scan(&v) == nil {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			return n
		}
	}
	return DefaultGrossMarginPct
}

// SetGrossMargin stores the gross margin in percent, 1 to 100.
func (p *Platform) SetGrossMargin(pct int) error {
	if pct < 1 || pct > 100 {
		return errors.New("gross margin must be between 1 and 100 percent")
	}
	return p.setSetting(marginKey, strconv.Itoa(pct))
}

var exampleTables = sync.OnceValue(func() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, ex := range examples.All() {
		def, problems := appdef.Parse(ex.JSON)
		if len(problems) > 0 {
			continue
		}
		set := map[string]bool{}
		for _, t := range def.Tables {
			set[t.Name] = true
		}
		out[ex.Name] = set
	}
	return out
})

// guessTemplate names the example an app was built from: the one whose
// tables it shares most (at least half of the two sets together), or ""
// when none is that close. Agents copy an example and change its seed rows,
// so the table names survive.
func guessTemplate(def *appdef.App) string {
	if def == nil || len(def.Tables) == 0 {
		return ""
	}
	tables := map[string]bool{}
	for _, t := range def.Tables {
		tables[t.Name] = true
	}
	best, bestScore := "", 0.0
	for name, set := range exampleTables() {
		common := 0
		for t := range tables {
			if set[t] {
				common++
			}
		}
		union := len(tables) + len(set) - common
		if union == 0 {
			continue
		}
		if score := float64(common) / float64(union); score > bestScore || (score == bestScore && name < best) {
			best, bestScore = name, score
		}
	}
	if bestScore < 0.5 {
		return ""
	}
	return best
}

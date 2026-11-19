package platform

import (
	"context"
	"strings"
	"time"
)

// Figures for the owner's dashboard, read from the stage the admin panel
// shows (live, or a demo's public preview). Days are UAE dates; stored
// times are UTC, so they are shifted by four hours before grouping.

// SalesDaily is the paid card payments of each of the last days days, in
// the app's main currency, with that currency.
func (p *Platform) SalesDaily(ctx context.Context, app string, days int) (string, []DayCount, error) {
	out := emptyDays(time.Now(), days)
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return "AED", out, err
	}
	currency := "aed"
	st.admin.QueryRowContext(ctx, `SELECT currency FROM rx_payments WHERE status = 'paid' GROUP BY currency ORDER BY count(*) DESC LIMIT 1`).Scan(&currency)
	idx := dayIndex(out)
	// Partly refunded payments stay "paid"; what was refunded is not a sale.
	rows, err := st.admin.QueryContext(ctx, `SELECT date(coalesce(updated_at, created_at), '+4 hours') AS d, count(*), coalesce(sum(amount - refunded), 0)
		FROM rx_payments WHERE status = 'paid' AND currency = ? AND date(coalesce(updated_at, created_at), '+4 hours') >= ? GROUP BY d`, currency, out[0].Day)
	if err != nil {
		return strings.ToUpper(currency), out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var n int
		var minor int64
		if rows.Scan(&d, &n, &minor) == nil {
			if i, ok := idx[d]; ok {
				out[i].Count, out[i].Value = n, float64(minor)/100
			}
		}
	}
	return strings.ToUpper(currency), out, rows.Err()
}

// SignupsDaily is how many visitor accounts were created on each day.
func (p *Platform) SignupsDaily(ctx context.Context, app string, days int) ([]DayCount, error) {
	out := emptyDays(time.Now(), days)
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return out, err
	}
	idx := dayIndex(out)
	rows, err := st.admin.QueryContext(ctx, `SELECT date(created_at, '+4 hours') AS d, count(*) FROM rx_users WHERE date(created_at, '+4 hours') >= ? GROUP BY d`, out[0].Day)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var n int
		if rows.Scan(&d, &n) == nil {
			if i, ok := idx[d]; ok {
				out[i].Count = n
			}
		}
	}
	return out, rows.Err()
}

func dayIndex(days []DayCount) map[string]int {
	m := make(map[string]int, len(days))
	for i, d := range days {
		m[d.Day] = i
	}
	return m
}

// TableStat is a table's size and how many rows were added in the last 7 days.
type TableStat struct {
	Name       string
	Rows, Week int
}

// TableStats counts the rows of every table the admin panel shows.
func (p *Platform) TableStats(ctx context.Context, app string) ([]TableStat, error) {
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return nil, err
	}
	week := time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02 15:04:05")
	var out []TableStat
	for _, t := range st.def.Tables {
		res, err := st.db.Query(ctx, "SELECT count(*) AS n, coalesce(sum(created_at >= ?), 0) AS w FROM "+quote(t.Name), []any{week})
		if err != nil {
			return nil, err
		}
		ts := TableStat{Name: t.Name}
		if len(res) == 1 {
			ts.Rows, ts.Week = toInt(res[0]["n"]), toInt(res[0]["w"])
		}
		out = append(out, ts)
	}
	return out, nil
}

func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// PaymentsPage is one page of an app's payments, newest first, with the
// total. status narrows to one status ("" for all).
func (p *Platform) PaymentsPage(ctx context.Context, app, status string, page int) ([]Payment, int, error) {
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	where, args := "", []any{}
	if status != "" {
		where, args = "WHERE status = ? ", append(args, status)
	}
	var total int
	if err := st.admin.QueryRowContext(ctx, `SELECT count(*) FROM rx_payments `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	list, err := queryPayments(ctx, st, where+`ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?`, append(args, AdminPageSize, (page-1)*AdminPageSize)...)
	return list, total, err
}

// SubscriptionsPage is one page of an app's subscriptions, newest first.
func (p *Platform) SubscriptionsPage(ctx context.Context, app string, page int) ([]Subscription, int, error) {
	st, err := p.loadStage(app, p.AdminStage(app))
	if err != nil || st == nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	const where = `WHERE status != 'pending' AND status != 'expired' `
	var total int
	if err := st.admin.QueryRowContext(ctx, `SELECT count(*) FROM rx_subscriptions `+where).Scan(&total); err != nil {
		return nil, 0, err
	}
	list, err := querySubscriptions(ctx, st, where+`ORDER BY created_at DESC, rowid DESC LIMIT ? OFFSET ?`, AdminPageSize, (page-1)*AdminPageSize)
	return list, total, err
}

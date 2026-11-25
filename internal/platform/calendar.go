package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/arif-rachim/renoxium/internal/appdef"
)

// CalendarLimit is the most rows one calendar view shows.
const CalendarLimit = 500

// CalendarRows is what a calendar view of a live table shows.
type CalendarRows struct {
	Table appdef.Table
	// Rows are the rows whose day (or day range) touches the view, ordered
	// by day, start time and id.
	Rows []map[string]any
	// Capped is true when more than CalendarLimit rows matched; Rows then
	// holds the first CalendarLimit.
	Capped bool
	// Lanes names the group ids found in Rows from the lane table
	// (staff_id → staff.name), when the calendar has one.
	Lanes map[int64]string
}

// AdminCalendar returns the rows of a live table with calendar metadata
// whose day falls between from and to (YYYY-MM-DD, inclusive), or, with an
// end_date column, whose range overlaps them. Rows with a status in
// hide_status are left out unless all is true.
func (p *Platform) AdminCalendar(ctx context.Context, app, table, from, to string, all bool) (CalendarRows, error) {
	st, t, err := p.liveTable(ctx, app, table)
	if err != nil {
		return CalendarRows{}, err
	}
	c := t.Calendar
	if c == nil {
		return CalendarRows{}, fmt.Errorf("table %q has no calendar: %w", table, ErrNotFound)
	}
	for _, d := range []string{from, to} {
		if _, err := time.Parse(time.DateOnly, d); err != nil {
			return CalendarRows{}, fmt.Errorf("date %q: use YYYY-MM-DD", d)
		}
	}
	if from > to {
		return CalendarRows{}, errors.New("the first day is after the last day")
	}
	day := "substr(" + quote(c.Date) + ", 1, 10)"
	var where string
	args := []any{}
	if c.EndDate != "" {
		last := "coalesce(nullif(substr(" + quote(c.EndDate) + ", 1, 10), ''), " + day + ")"
		where = day + " <= ? AND " + last + " >= ? AND " + day + " <> ''"
		args = append(args, to, from)
	} else {
		where = day + " BETWEEN ? AND ?"
		args = append(args, from, to)
	}
	if !all && c.Status != "" && len(c.HideStatus) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(c.HideStatus)), ",")
		where += " AND (" + quote(c.Status) + " IS NULL OR " + quote(c.Status) + " NOT IN (" + marks + "))"
		for _, s := range c.HideStatus {
			args = append(args, s)
		}
	}
	order := day
	if c.Start != "" {
		order += ", " + quote(c.Start)
	} else if t.ColumnType(c.Date) == "datetime" {
		order += ", " + quote(c.Date)
	}
	args = append(args, CalendarLimit+1)
	rows, err := st.db.Query(ctx, "SELECT * FROM "+quote(t.Name)+" WHERE "+where+" ORDER BY "+order+", id LIMIT ?", args)
	if err != nil {
		return CalendarRows{}, err
	}
	out := CalendarRows{Table: t, Rows: rows}
	if len(rows) > CalendarLimit {
		out.Rows, out.Capped = rows[:CalendarLimit], true
	}
	if lt, ok := st.def.CalendarGroupTable(t); ok && lt.LaneColumn() != "" && t.ColumnType(c.Group) == "integer" {
		var ids []any
		for _, r := range out.Rows {
			if id, ok := r[c.Group].(int64); ok && !slices.Contains(ids, any(id)) {
				ids = append(ids, id)
			}
		}
		out.Lanes = map[int64]string{}
		if len(ids) > 0 {
			marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
			names, err := st.db.Query(ctx, "SELECT id, "+quote(lt.LaneColumn())+" AS name FROM "+quote(lt.Name)+" WHERE id IN ("+marks+")", ids)
			if err != nil {
				return CalendarRows{}, err
			}
			for _, n := range names {
				if s, ok := n["name"].(string); ok && s != "" {
					out.Lanes[n["id"].(int64)] = s
				}
			}
		}
	}
	return out, nil
}

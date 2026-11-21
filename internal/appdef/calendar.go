package appdef

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Calendar tells the admin panel to show a table as a day and week
// schedule as well as a list. It never changes the database.
type Calendar struct {
	Date       string   `json:"date" jsonschema:"required: text column holding the day as YYYY-MM-DD (or a datetime column; its date part is used)"`
	Start      string   `json:"start,omitempty" jsonschema:"text column with the start time, HH:MM"`
	End        string   `json:"end,omitempty" jsonschema:"text column with the end time, HH:MM"`
	Duration   string   `json:"duration,omitempty" jsonschema:"integer or real column with the length in minutes, used when there is no end"`
	EndDate    string   `json:"end_date,omitempty" jsonschema:"text or datetime column with the last day of a multi-day booking (rentals), YYYY-MM-DD"`
	Group      string   `json:"group,omitempty" jsonschema:"column that splits a day into lanes, e.g. staff_id or crew_id; an integer column named <stem>_id shows the name from the table <stem> or <stem>s"`
	GroupTable string   `json:"group_table,omitempty" jsonschema:"table whose name column titles the lanes when it is not found from the group column's name, e.g. crews"`
	Title      Columns  `json:"title,omitempty" jsonschema:"column names shown on each item, joined with ' · ', e.g. [\"customer_name\", \"item\"]"`
	Status     string   `json:"status,omitempty" jsonschema:"text column that colours items, e.g. status"`
	HideStatus []string `json:"hide_status,omitempty" jsonschema:"status values hidden unless the owner chooses to show them, e.g. [\"cancelled\", \"failed\", \"expired\"]"`
}

// Columns is a list of column names. Definitions saved before it was a
// list may hold one name as plain text, which reads as a list of one.
type Columns []string

func (c *Columns) UnmarshalJSON(data []byte) error {
	var one string
	if json.Unmarshal(data, &one) == nil {
		*c = nil
		if one != "" {
			*c = Columns{one}
		}
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return errors.New("must be a list of column names")
	}
	*c = list
	return nil
}

// TitleColumns returns the title column names.
func (c *Calendar) TitleColumns() []string { return c.Title }

// ColumnType returns the type of a table column, including the automatic
// id (integer) and created_at (datetime) columns, or "" if there is none.
func (t Table) ColumnType(name string) string {
	switch name {
	case "id":
		return "integer"
	case "created_at":
		return "datetime"
	}
	for _, c := range t.Columns {
		if c.Name == name {
			return c.Type
		}
	}
	return ""
}

// CalendarGroupTable returns the table whose rows title the lanes of t's
// calendar: group_table if set, otherwise for an integer group column
// named <stem>_id the table <stem>, <stem>s, <stem>es or <stem minus y>ies.
func (a *App) CalendarGroupTable(t Table) (Table, bool) {
	c := t.Calendar
	if c == nil || c.Group == "" {
		return Table{}, false
	}
	var names []string
	if c.GroupTable != "" {
		names = []string{c.GroupTable}
	} else if stem, ok := strings.CutSuffix(c.Group, "_id"); ok && stem != "" && t.ColumnType(c.Group) == "integer" {
		names = []string{stem, stem + "s", stem + "es"}
		if s, ok := strings.CutSuffix(stem, "y"); ok {
			names = append(names, s+"ies")
		}
	}
	for _, n := range names {
		for _, other := range a.Tables {
			if other.Name == n {
				return other, true
			}
		}
	}
	return Table{}, false
}

// LaneColumn is the column that names a row of a lane table: name, label
// or title if present, else the first text column, else "".
func (t Table) LaneColumn() string {
	for _, n := range []string{"name", "label", "title"} {
		if t.ColumnType(n) == "text" {
			return n
		}
	}
	for _, c := range t.Columns {
		if c.Type == "text" {
			return c.Name
		}
	}
	return ""
}

func (a *App) validateCalendars() []string {
	var p []string
	for _, t := range a.Tables {
		c := t.Calendar
		if c == nil {
			continue
		}
		add := func(format string, args ...any) {
			p = append(p, fmt.Sprintf("table %q: calendar: ", t.Name)+fmt.Sprintf(format, args...))
		}
		check := func(field, col string, types ...string) {
			if col == "" {
				return
			}
			typ := t.ColumnType(col)
			switch {
			case typ == "":
				add("%s: column %q does not exist", field, col)
			case !slices.Contains(types, typ):
				add("%s: column %q is %s; use a %s column", field, col, typ, strings.Join(types, " or "))
			}
		}
		if c.Date == "" {
			add("date is required: the column holding the day, YYYY-MM-DD")
		}
		check("date", c.Date, "text", "datetime")
		check("start", c.Start, "text")
		check("end", c.End, "text")
		check("duration", c.Duration, "integer", "real")
		check("end_date", c.EndDate, "text", "datetime")
		check("group", c.Group, "text", "integer")
		check("status", c.Status, "text")
		if (c.End != "" || c.Duration != "") && c.Start == "" {
			add("end and duration need start")
		}
		if c.GroupTable != "" {
			switch {
			case c.Group == "":
				add("group_table needs group")
			case !slices.ContainsFunc(a.Tables, func(o Table) bool { return o.Name == c.GroupTable }):
				add("group_table: table %q is not defined", c.GroupTable)
			case t.ColumnType(c.Group) != "" && t.ColumnType(c.Group) != "integer":
				add("group_table needs an integer group column holding the row id, not %q", c.Group)
			}
		}
		titles := c.TitleColumns()
		if len(titles) > 5 {
			add("title: at most 5 columns")
		}
		for _, col := range titles {
			check("title", col, "text", "integer", "real", "boolean", "datetime")
		}
		if len(c.HideStatus) > 0 && c.Status == "" {
			add("hide_status needs status")
		}
		var options []string
		for _, col := range t.Columns {
			if col.Name == c.Status {
				options = col.Options
			}
		}
		for _, v := range c.HideStatus {
			if len(options) > 0 && !slices.Contains(options, v) {
				add("hide_status: %q is not one of the status options (%s)", v, strings.Join(options, ", "))
			}
		}
	}
	return p
}

package appdef

import (
	"encoding/json"
	"strings"
	"testing"
)

func calendarApp(c *Calendar) *App {
	return &App{
		Tables: []Table{
			{Name: "crews", Columns: []Column{{Name: "name", Type: "text"}}},
			{Name: "staff", Columns: []Column{{Name: "phone", Type: "text"}, {Name: "name", Type: "text"}}},
			{Name: "bookings", Calendar: c, Columns: []Column{
				{Name: "date", Type: "text"}, {Name: "time", Type: "text"}, {Name: "slot_end", Type: "text"},
				{Name: "duration_min", Type: "integer"}, {Name: "end_date", Type: "text"}, {Name: "staff_id", Type: "integer"},
				{Name: "crew_id", Type: "integer"}, {Name: "team", Type: "integer"}, {Name: "customer_name", Type: "text"},
				{Name: "price", Type: "real"}, {Name: "lines", Type: "json"},
				{Name: "status", Type: "text", Options: []string{"pending", "confirmed", "cancelled"}},
			}},
		},
		Templates: map[string]string{"t": "x"},
		Routes:    []Route{{Method: "GET", Path: "/", Template: "t"}},
	}
}

func TestCalendarValid(t *testing.T) {
	for _, title := range []Columns{nil, {"customer_name"}, {"customer_name", "id"}, {"customer_name", "price"}} {
		a := calendarApp(&Calendar{Date: "date", Start: "time", End: "slot_end", Duration: "duration_min", EndDate: "end_date",
			Group: "staff_id", Title: title, Status: "status", HideStatus: []string{"cancelled"}})
		if p := a.Validate(); len(p) > 0 {
			t.Errorf("title %v: %v", title, p)
		}
	}
	// A datetime column such as created_at is a valid date.
	if p := calendarApp(&Calendar{Date: "created_at"}).Validate(); len(p) > 0 {
		t.Error(p)
	}
	// Definitions decoded from JSON accept one title or a list.
	for _, raw := range []string{`"customer_name"`, `["customer_name","price"]`} {
		def, problems := Parse([]byte(`{"tables":[{"name":"b","calendar":{"date":"d","title":` + raw + `},"columns":[{"name":"d","type":"text"},{"name":"customer_name","type":"text"},{"name":"price","type":"real"}]}],
			"templates":{"t":"x"},"routes":[{"method":"GET","path":"/","template":"t"}]}`))
		if len(problems) > 0 || def.Tables[0].Calendar.TitleColumns()[0] != "customer_name" {
			t.Errorf("%s: %v", raw, problems)
		}
	}
}

func TestCalendarProblems(t *testing.T) {
	cases := []struct {
		cal  Calendar
		want string
	}{
		{Calendar{Start: "time"}, "date is required"},
		{Calendar{Date: "day"}, `date: column "day" does not exist`},
		{Calendar{Date: "price"}, `date: column "price" is real; use a text or datetime column`},
		{Calendar{Date: "date", Start: "duration_min"}, `start: column "duration_min" is integer; use a text column`},
		{Calendar{Date: "date", Start: "time", Duration: "customer_name"}, `duration: column "customer_name" is text`},
		{Calendar{Date: "date", End: "slot_end"}, "end and duration need start"},
		{Calendar{Date: "date", Group: "lines"}, `group: column "lines" is json`},
		{Calendar{Date: "date", Group: "team", GroupTable: "teams"}, `group_table: table "teams" is not defined`},
		{Calendar{Date: "date", GroupTable: "crews"}, "group_table needs group"},
		{Calendar{Date: "date", Group: "customer_name", GroupTable: "crews"}, "group_table needs an integer group column"},
		{Calendar{Date: "date", Title: Columns{"customer_name", "nope"}}, `title: column "nope" does not exist`},
		{Calendar{Date: "date", Title: Columns{"lines"}}, `title: column "lines" is json`},
		{Calendar{Date: "date", Status: "price"}, `status: column "price" is real`},
		{Calendar{Date: "date", HideStatus: []string{"cancelled"}}, "hide_status needs status"},
		{Calendar{Date: "date", Status: "status", HideStatus: []string{"canceled"}}, `hide_status: "canceled" is not one of the status options (pending, confirmed, cancelled)`},
	}
	for _, c := range cases {
		cal := c.cal
		got := strings.Join(calendarApp(&cal).Validate(), "\n")
		if !strings.Contains(got, `table "bookings": calendar: `+c.want) {
			t.Errorf("%+v: want %q in:\n%s", c.cal, c.want, got)
		}
	}
}

func TestCalendarGroupTable(t *testing.T) {
	cases := map[string]string{"staff_id": "staff", "crew_id": "crews", "team": "", "customer_name": ""}
	for group, want := range cases {
		a := calendarApp(&Calendar{Date: "date", Group: group})
		lt, ok := a.CalendarGroupTable(a.Tables[2])
		if ok != (want != "") || lt.Name != want {
			t.Errorf("%s: got %q %v, want %q", group, lt.Name, ok, want)
		}
	}
	a := calendarApp(&Calendar{Date: "date", Group: "team", GroupTable: "crews"})
	if lt, ok := a.CalendarGroupTable(a.Tables[2]); !ok || lt.Name != "crews" || lt.LaneColumn() != "name" {
		t.Errorf("group_table: %q %v", lt.Name, ok)
	}
	if got := (Table{Columns: []Column{{Name: "n", Type: "integer"}, {Name: "full_name", Type: "text"}}}).LaneColumn(); got != "full_name" {
		t.Errorf("lane column %q", got)
	}
}

// A definition saved when title could be one name still reads; anything
// that is not a name or a list of names is refused with a clear message.
func TestCalendarTitleForms(t *testing.T) {
	for in, want := range map[string]int{`"customer_name"`: 1, `["customer_name","id"]`: 2, `""`: 0} {
		var c Calendar
		if err := json.Unmarshal([]byte(`{"date":"date","title":`+in+`}`), &c); err != nil || len(c.Title) != want {
			t.Errorf("title %s: %v %v", in, c.Title, err)
		}
	}
	var c Calendar
	if err := json.Unmarshal([]byte(`{"date":"date","title":["customer_name",3]}`), &c); err == nil || !strings.Contains(err.Error(), "list of column names") {
		t.Errorf("a number in title: %v", err)
	}
}

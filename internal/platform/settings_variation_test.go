package platform_test

import (
	"fmt"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

// Owners and their AI assistants fill in the settings row of a finished
// example: a number they do not use is often 0, or left empty when the
// column allows it. Every numeric setting of every finished example is set
// to 0 and to empty in turn, and the visitor pages must still work: no
// server error and no NaN, undefined or [object Object] on the page.
func TestExampleSettingsAtZeroAndEmpty(t *testing.T) {
	examplesToCheck := []struct {
		example, table string
		pages          []string // besides the pages without parameters, which applying already renders
	}{
		{"store-uae", "store", []string{"/", "/p/1", "/p/2", "/cart", "/api/products?ids=1.1,2", "/orders"}},
		{"booking-uae", "business", []string{"/", "/s/1", "/c/1", "/c/2", "/api/slots?service=1&date=" + dubaiDate(2), "/bookings"}},
		{"home-service-uae", "business", []string{"/", "/s/1", "/s/2", "/api/slots?date=" + dubaiDate(2), "/bookings"}},
		{"reservation-uae", "business", []string{"/", "/item/1", "/item/4", "/item/6",
			"/api/availability?item=4&start=" + dubaiDate(3) + "&end=" + dubaiDate(5) + "&qty=1", "/reservations"}},
		{"quote-portal-uae", "business", []string{"/", "/enquiry", "/portal"}},
		{"listings-uae", "agency", []string{"/", "/l/1", "/search", "/agents/1"}},
	}
	for _, x := range examplesToCheck {
		ex, _ := examples.Get(x.example)
		def, _ := appdef.Parse(ex.JSON)
		var cols []appdef.Column
		for _, tb := range def.Tables {
			if tb.Name == x.table {
				for _, c := range tb.Columns {
					if c.Type == "integer" || c.Type == "real" || c.Type == "boolean" {
						cols = append(cols, c)
					}
				}
			}
		}
		if len(cols) == 0 && x.example != "listings-uae" {
			t.Fatalf("%s: no numeric settings in %s", x.example, x.table)
		}
		for _, col := range cols {
			values := []any{0}
			if !col.Required {
				values = append(values, nil)
			}
			for _, v := range values {
				t.Run(fmt.Sprintf("%s/%s=%v", x.example, col.Name, v), func(t *testing.T) {
					e := newExampleEnv(t, x.example, func(def *appdef.App) { def.Seed[x.table][0][col.Name] = v })
					const who = "fatima@mail.ae"
					e.signup(who)
					for _, path := range x.pages {
						e.get(path, "")
						e.get(path, who)
					}
				})
			}
		}
	}
}

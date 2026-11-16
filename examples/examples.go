// Package examples holds complete app definitions that agents can read with
// the get_example tool and that tests apply end to end.
package examples

import (
	_ "embed"
	"sort"
)

var (
	//go:embed catalog.json
	catalog []byte
	//go:embed tasks.json
	tasks []byte
	//go:embed store-uae.json
	storeUAE []byte
)

// Example is one complete app definition.
type Example struct {
	Name        string
	Description string
	JSON        []byte
}

var all = map[string]Example{
	"catalog": {Name: "catalog", Description: "A small shop: product list, product page with an order form, and a JSON API.", JSON: catalog},
	"store-uae": {Name: "store-uae", Description: "A complete online store for the UAE, ready to customise: catalog with photos and categories, cart, " +
		"AED prices including 5% VAT, delivery fee with a free-delivery threshold, checkout with card payment (Stripe) or cash on delivery, " +
		"orders for signed-in shoppers (email or WhatsApp sign-in), English and Arabic (right to left). Change the store, products and texts tables' seed rows.", JSON: storeUAE},
	"tasks": {Name: "tasks", Description: "Create, edit, toggle and delete records: forms, path parameters, boolean columns, filters and counts.", JSON: tasks},
}

// Get returns an example by name.
func Get(name string) (Example, bool) {
	e, ok := all[name]
	return e, ok
}

// All returns every example, sorted by name.
func All() []Example {
	out := make([]Example, 0, len(all))
	for _, e := range all {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

package platform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/internal/platform"
)

// One call makes a store with the business's own settings and products.
// Mistakes in the data are reported before an app is made.
func TestCreateCustomized(t *testing.T) {
	p := newPlatform(t)
	ctx := context.Background()
	if _, _, err := p.CreateCustomized(ctx, "usr_test", "bad-shop", "store-uae", platform.Customize{
		Settings: map[string]any{"whatsap": "+971501234567"}}); err == nil || !strings.Contains(err.Error(), "whatsapp") {
		t.Fatalf("unknown settings column: %v", err)
	}
	if _, _, err := p.CreateCustomized(ctx, "usr_test", "bad-shop", "store-uae", platform.Customize{
		Rows: map[string][]map[string]any{"items": {{"name": "x"}}}}); err == nil || !strings.Contains(err.Error(), "products") {
		t.Fatalf("unknown table: %v", err)
	}
	if apps, _ := p.Apps("usr_test"); len(apps) != 0 {
		t.Fatalf("a refused call made an app: %v", apps)
	}

	_, res, err := p.CreateCustomized(ctx, "usr_test", "oud-house", "store-uae", platform.Customize{
		Business: "Oud House",
		Settings: map[string]any{"whatsapp": "+971501234567", "delivery_fee": 20},
		Rows: map[string][]map[string]any{"products": {
			{"name": "Royal Oud", "price": 250, "active": 1, "stock": 5},
			{"name": "Amber Musk", "price": 120, "active": 1, "stock": 9},
		}},
	})
	if err != nil || !res.OK {
		t.Fatalf("create: %v %+v", err, res)
	}
	home, err := p.TestRequest(ctx, "oud-house", platform.TestRequestInput{Path: "/"})
	if err != nil || home.Status != 200 {
		t.Fatalf("home: %v %d", err, home.Status)
	}
	for _, want := range []string{"Oud House", "Royal Oud", "Amber Musk"} {
		if !strings.Contains(home.Body, want) {
			t.Errorf("home page lacks %q", want)
		}
	}
}

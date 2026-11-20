package platform_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
)

// The motion kit (templates motion and motion_js) is one kit: every example
// that carries it carries the same copy, so a fix lands everywhere. A
// template's own motion goes in its own css, never in a changed copy.
func TestMotionKitIsTheSameEverywhere(t *testing.T) {
	var first string
	var kit map[string]string
	n := 0
	for _, ex := range examples.All() {
		var def struct{ Templates map[string]string }
		json.Unmarshal(ex.JSON, &def)
		if def.Templates["motion"] == "" && def.Templates["motion_js"] == "" {
			continue
		}
		n++
		for _, name := range []string{"motion", "motion_js"} {
			if !strings.Contains(def.Templates["head"]+def.Templates["bottom"], `{{template "`+name+`" .}}`) {
				t.Errorf("%s does not include %s from head or bottom", ex.Name, name)
			}
		}
		if kit == nil {
			first, kit = ex.Name, def.Templates
			continue
		}
		for _, name := range []string{"motion", "motion_js"} {
			if def.Templates[name] != kit[name] {
				t.Errorf("%s's %s differs from %s's: copy the kit, do not change it in one template", ex.Name, name, first)
			}
		}
	}
	if n < 2 {
		t.Fatalf("the motion kit is in %d examples, want store-uae and booking-uae at least", n)
	}
}

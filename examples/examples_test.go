package examples

import (
	"strings"
	"testing"
)

// Every finished UAE template belongs in the public gallery: adding one
// without gallery words would leave it out of the gallery unnoticed.
func TestFinishedTemplatesAreInTheGallery(t *testing.T) {
	for _, e := range All() {
		if !strings.HasSuffix(e.Name, "-uae") {
			continue
		}
		g := e.Gallery
		if g == nil {
			t.Errorf("%s has no gallery entry: add it to the gallery map in examples.go", e.Name)
			continue
		}
		if g.Title == "" || g.Summary == "" || len(g.For) == 0 || g.Order == 0 {
			t.Errorf("%s: gallery entry needs a title, a summary, who it is for and an order: %+v", e.Name, *g)
		}
	}
}

func TestGalleryOrder(t *testing.T) {
	list := Gallery()
	if len(list) == 0 || list[0].Name != "store-uae" {
		t.Fatalf("gallery starts with %v", list)
	}
	seen := map[int]string{}
	for _, e := range list {
		if other, dup := seen[e.Gallery.Order]; dup {
			t.Errorf("%s and %s share gallery position %d", e.Name, other, e.Gallery.Order)
		}
		seen[e.Gallery.Order] = e.Name
	}
	for _, name := range []string{"tasks", "catalog"} {
		if e, _ := Get(name); e.Gallery != nil {
			t.Errorf("%s is a building example, not a gallery template", name)
		}
	}
}

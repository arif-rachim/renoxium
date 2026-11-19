package examples

import (
	"encoding/json"
	"testing"
)

// Demo photos point at seed rows and image columns that exist, every file
// is used, and the templates themselves keep empty image seeds.
func TestDemoImagesMatchTheTemplates(t *testing.T) {
	used := map[string]bool{}
	for name, sets := range demoImageSets {
		e, ok := Get(name)
		if !ok || e.Gallery == nil {
			t.Errorf("demo images for %s, which is no gallery template", name)
			continue
		}
		var def struct {
			Tables []struct {
				Name    string
				Columns []struct{ Name, Type string }
			}
			Seed map[string][]map[string]any
		}
		if err := json.Unmarshal(e.JSON, &def); err != nil {
			t.Fatal(err)
		}
		types := map[string]string{}
		for _, tb := range def.Tables {
			for _, c := range tb.Columns {
				types[tb.Name+"."+c.Name] = c.Type
			}
		}
		for _, set := range sets {
			keys := map[string]map[string]any{}
			for _, row := range def.Seed[set.Table] {
				if k, ok := row[set.Key].(string); ok {
					keys[k] = row
				}
			}
			for key, cols := range set.Rows {
				row, ok := keys[key]
				if !ok {
					t.Errorf("%s: no %s row with %s %q", name, set.Table, set.Key, key)
					continue
				}
				for col, file := range cols {
					if types[set.Table+"."+col] != "image" {
						t.Errorf("%s: %s.%s is not an image column", name, set.Table, col)
					}
					if v, _ := row[col].(string); v != "" {
						t.Errorf("%s: the template's own seed has an image in %s.%s for %q; users' templates keep it empty", name, set.Table, col, key)
					}
					if b, err := DemoImage(file); err != nil || len(b) == 0 || len(b) > 200<<10 {
						t.Errorf("%s: demo image %s: %d bytes, %v", name, file, len(b), err)
					}
					used[file] = true
				}
			}
		}
	}
	for _, f := range DemoImageFiles() {
		if !used[f] {
			t.Errorf("demo-images/%s is not used by any demo", f)
		}
	}
}

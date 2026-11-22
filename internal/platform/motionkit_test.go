package platform_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
)

// The business templates that carry the motion kit.
var motionKitExamples = []string{"store-uae", "booking-uae", "home-service-uae", "reservation-uae", "listings-uae", "quote-portal-uae", "clinic-queue-uae"}

// The motion kit (templates motion and motion_js) is one kit: every example
// that carries it carries the same copy, so a fix lands everywhere. A
// template's own motion goes in its own css, never in a changed copy.
func TestMotionKitIsTheSameEverywhere(t *testing.T) {
	var first string
	var kit map[string]string
	have := map[string]bool{}
	for _, ex := range examples.All() {
		var def struct{ Templates map[string]string }
		json.Unmarshal(ex.JSON, &def)
		if def.Templates["motion"] == "" && def.Templates["motion_js"] == "" {
			continue
		}
		have[ex.Name] = true
		// The stylesheet comes from head; the script closes every branch of bottom (boutique and the usual footer);
		// top marks <html> with "js" before anything paints, so what waits for an animation only waits with JavaScript.
		if !strings.Contains(def.Templates["head"], `{{template "motion" .}}`) {
			t.Errorf("%s does not include motion from head", ex.Name)
		}
		ends := strings.Count(def.Templates["bottom"], `</body></html>`)
		if n := strings.Count(def.Templates["bottom"], `{{template "motion_js" .}}</body></html>`); n == 0 || n != ends {
			t.Errorf("%s: motion_js closes %d of the %d endings of bottom", ex.Name, n, ends)
		}
		if !strings.Contains(def.Templates["top"], `document.documentElement.classList.add('js')`) {
			t.Errorf("%s: top does not add the js class", ex.Name)
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
	for _, name := range motionKitExamples {
		if !have[name] {
			t.Errorf("%s does not carry the motion kit", name)
		}
	}
	if len(have) != len(motionKitExamples) {
		t.Errorf("the motion kit is in %d examples, want %v", len(have), motionKitExamples)
	}
	// The list to detail transition: snapshots crop instead of stretching, and reduced motion switches it off.
	for _, want := range []string{"@view-transition{navigation:auto}", "::view-transition-old(*),::view-transition-new(*){width:100%;height:100%;object-fit:cover}",
		"@media (prefers-reduced-motion:reduce){@view-transition{navigation:none}",
		// Titles: a box that hugs the text, snapshots sized as text (never cropped), and no title transition at all in a
		// browser that cannot tell them apart from photos.
		".vtt{display:inline-block;max-width:100%;text-decoration:inherit;view-transition-class:vtt}",
		"::view-transition-old(.vtt),::view-transition-new(.vtt){height:auto;object-fit:fill}",
		"@supports not (view-transition-class:vtt){.vtt{view-transition-name:none!important}}"} {
		if !strings.Contains(kit["motion"], want) {
			t.Errorf("the kit's stylesheet lacks %q", want)
		}
	}
	// Every text the kit's script reads exists in English and Arabic in every template that carries it.
	keys := regexp.MustCompile(`\{\{\.t\.([a-z_]+)\}\}`).FindAllStringSubmatch(kit["motion_js"], -1)
	for _, name := range motionKitExamples {
		ex, _ := examples.Get(name)
		var def struct{ Seed map[string][]map[string]any }
		json.Unmarshal(ex.JSON, &def)
		texts := map[string]map[string]any{}
		for _, r := range def.Seed["texts"] {
			texts[fmt.Sprint(r["key"])] = r
		}
		for _, k := range keys {
			if r := texts[k[1]]; r == nil || fmt.Sprint(r["en"]) == "" || fmt.Sprint(r["ar"]) == "" || r["ar"] == nil {
				t.Errorf("%s: text %q, which the motion kit reads, is missing in English or Arabic", name, k[1])
			}
		}
	}
}

var transitionName = regexp.MustCompile(`<([a-z0-9]+)[^<>]* style="view-transition-name:([a-z]+-[0-9]+)"`)

// transitionNames returns the view-transition-names of a page with the tag
// that carries each, and fails when a name is used twice: the browser skips
// the whole transition then.
func transitionNames(t *testing.T, where, body string) map[string]string {
	t.Helper()
	names := map[string]string{}
	for _, m := range transitionName.FindAllStringSubmatch(body, -1) {
		if _, twice := names[m[2]]; twice {
			t.Errorf("%s: view-transition-name %s is used twice", where, m[2])
		}
		names[m[2]] = m[1]
	}
	return names
}

type transitionNav struct {
	list   string // the page with the cards
	photo  string // prefix of the name a card with a photo gives its photo frame
	title  string // prefix of the name a card without a photo gives its title ("" where the focal object needs no photo)
	detail string // the page a card leads to; %s is the row's id
}

// Every list to detail navigation of the templates with the kit.
var transitionCases = []struct {
	example, settings string
	photos            map[string]string // table -> image column
	navs              []transitionNav
	photoCarriesMask  bool // quote-portal's rows: the photo itself has the border-radius, there is no frame around it
}{
	{"store-uae", "store", map[string]string{"products": "image"}, []transitionNav{{"/", "pimg-", "pttl-", "/p/%s"}}, false},
	{"booking-uae", "business", map[string]string{"services": "image", "classes": "image"},
		[]transitionNav{{"/", "simg-", "sttl-", "/s/%s"}, {"/", "cimg-", "cttl-", "/c/%s"}}, false},
	{"home-service-uae", "business", map[string]string{"services": "image"}, []transitionNav{{"/", "simg-", "sttl-", "/s/%s"}}, false},
	{"reservation-uae", "business", map[string]string{"items": "image"}, []transitionNav{{"/", "iimg-", "ittl-", "/item/%s"}}, false},
	{"listings-uae", "agency", map[string]string{"listings": "image1", "agents": "photo"},
		[]transitionNav{{"/", "limg-", "lttl-", "/l/%s"}, {"/search", "limg-", "lttl-", "/l/%s"}, {"/agents/1", "limg-", "lttl-", "/l/%s"},
			{"/", "aimg-", "", "/agents/%s"}, {"/l/1", "aimg-", "", "/agents/%s"}}, false},
	{"quote-portal-uae", "business", map[string]string{"packages": "image"}, []transitionNav{{"/", "kimg-", "kttl-", "/packages/%s"}}, true},
	{"clinic-queue-uae", "clinic", map[string]string{"services": "image"}, []transitionNav{{"/", "simg-", "sttl-", "/s/%s"}}, false},
}

// A card and the page it leads to share a view-transition-name, in every
// layout of every template with the kit (boutique, the standard layout and
// the luxury one use different card markup). With a photo the name sits on
// the element that carries the mask, never on a photo inside a frame: a
// snapshot of the bare photo is a rectangle, and the shape would vanish while
// it moves. Without a photo the card's title carries a name instead and moves
// into the page's h1, in a box of its own (class vtt) so the text is sized as
// text. A row never names both.
func TestListAndDetailShareATransitionName(t *testing.T) {
	for _, c := range transitionCases {
		for _, font := range []string{"boutique", "modern", "atelier"} {
			for _, withPhotos := range []bool{true, false} {
				e := newExampleEnv(t, c.example, func(def *appdef.App) {
					def.Seed[c.settings][0]["font"] = font
					for table, col := range c.photos {
						for _, row := range def.Seed[table] {
							row[col] = ""
							if withPhotos {
								row[col] = "/_rx/files/photo.webp"
							}
						}
					}
				})
				kind := " without photos"
				if withPhotos {
					kind = " with photos"
				}
				for _, n := range c.navs {
					where := c.example + " (" + font + kind + ") " + n.list
					want, never := n.photo, n.title
					if !withPhotos && n.title != "" {
						want, never = n.title, n.photo
					}
					// check tells whether name sits on the right kind of element of a page.
					check := func(page, name, tag, body string) {
						t.Helper()
						if want == n.title && n.title != "" {
							if !strings.Contains(body, `<span class="vtt" style="view-transition-name:`+name+`">`) {
								t.Errorf("%s: on %s, %s is on <%s>: a title's name belongs on a span of class vtt around the text", where, page, name, tag)
							}
						} else if tag == "img" && !c.photoCarriesMask {
							t.Errorf("%s: on %s, %s is on <%s>: the name belongs on the element that carries the mask", where, page, name, tag)
						}
					}
					list := e.get(n.list, "")
					names := transitionNames(t, where, list.Body)
					found := 0
					for name, tag := range names {
						if never != "" && strings.HasPrefix(name, never) {
							t.Errorf("%s: %s is named although the row has %s", where, name, map[bool]string{true: "a photo", false: "no photo"}[withPhotos])
						}
						if !strings.HasPrefix(name, want) {
							continue
						}
						found++
						check(n.list, name, tag, list.Body)
						path := fmt.Sprintf(n.detail, strings.TrimPrefix(name, want))
						detail := e.get(path, "")
						if detail.Status != 200 {
							t.Errorf("%s: %s answers %d", where, path, detail.Status)
							continue
						}
						there := transitionNames(t, c.example+" ("+font+kind+") "+path, detail.Body)
						if tag, ok := there[name]; !ok {
							t.Errorf("%s: %s has no element named %s, so the card's focal object cannot move to it", where, path, name)
						} else {
							check(path, name, tag, detail.Body)
						}
						if other := never + strings.TrimPrefix(name, want); never != "" && there[other] != "" {
							t.Errorf("%s: %s names both %s and %s", where, path, name, other)
						}
					}
					if found == 0 {
						t.Errorf("%s: no card carries a view-transition-name %s…", where, want)
					}
				}
				// Arabic pages carry the same names.
				first := c.navs[0].photo
				if !withPhotos {
					first = c.navs[0].title
				}
				if ar := e.get(c.navs[0].list+"?lang=ar", ""); !strings.Contains(ar.Body, `style="view-transition-name:`+first) {
					t.Errorf("%s (%s%s): the Arabic list page has no transition names", c.example, font, kind)
				}
			}
		}
	}
}

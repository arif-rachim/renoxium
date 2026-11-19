package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// Photos of the public site pages, embedded in the binary. Each photo is
// stored as <name>-400.webp, -800.webp and -1600.webp (3:2); see
// static/img/CREDITS.md for where each one comes from.
//
//go:embed static/img/*.webp
var staticFiles embed.FS

// staticVersion holds a short content hash per file, added to its URL so
// the files can be cached as immutable and still change on a deploy.
var staticVersion = func() map[string]string {
	m := map[string]string{}
	entries, _ := fs.ReadDir(staticFiles, "static/img")
	for _, e := range entries {
		b, _ := staticFiles.ReadFile("static/img/" + e.Name())
		sum := sha256.Sum256(b)
		m[e.Name()] = hex.EncodeToString(sum[:5])
	}
	return m
}()

func staticURL(file string) string {
	return "/static/img/" + file + "?v=" + staticVersion[file]
}

func (s *Site) registerStatic() {
	s.mux.HandleFunc("GET /static/img/{file}", serveStatic)
}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if _, ok := staticVersion[name]; !ok || !strings.HasSuffix(name, ".webp") {
		http.NotFound(w, r)
		return
	}
	b, err := staticFiles.ReadFile("static/img/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
}

// photo is one picture of the site pages, used through Src and SrcSet so
// every page gets the same sizes.
type photo struct {
	Name, Alt string
	Right     bool // the subject is on the right: crop from there
}

func (p photo) Src() string { return staticURL(p.Name + "-800.webp") }
func (p photo) Small() string {
	return staticURL(p.Name + "-400.webp")
}
func (p photo) SrcSet() string {
	return staticURL(p.Name+"-400.webp") + " 400w, " + staticURL(p.Name+"-800.webp") + " 800w, " + staticURL(p.Name+"-1600.webp") + " 1600w"
}

var photos = map[string]photo{
	"shop-boutique":   {Name: "shop-boutique", Alt: "Dresses, shirts and jeans on rails and shelves in a small clothing boutique"},
	"shop-cake":       {Name: "shop-cake", Alt: "A chocolate cake with piped cream on a white cake stand"},
	"shop-flowers":    {Name: "shop-flowers", Alt: "Bouquets of roses and potted flowers outside a flower shop"},
	"book-salon":      {Name: "book-salon", Alt: "A bright beauty salon with round mirrors and styling chairs"},
	"book-cleaning":   {Name: "book-cleaning", Alt: "A gloved hand holding a cleaning spray bottle", Right: true},
	"book-safari":     {Name: "book-safari", Alt: "A camel caravan crossing golden desert dunes"},
	"book-yacht":      {Name: "book-yacht", Alt: "A white motor yacht on clear blue water"},
	"quote-villa":     {Name: "quote-villa", Alt: "A modern white villa with a pool"},
	"quote-skyline":   {Name: "quote-skyline", Alt: "The Dubai skyline with Burj Khalifa at sunrise"},
	"quote-meeting":   {Name: "quote-meeting", Alt: "Two consultants giving each other a high five at a desk with papers and a laptop"},
	"deliver-courier": {Name: "deliver-courier", Alt: "A courier on a red scooter with a delivery box, riding through the city at night"},
}

// examplePhoto is the photo on each template's card.
var examplePhoto = map[string]string{
	"store-uae":         "shop-boutique",
	"booking-uae":       "book-salon",
	"home-service-uae":  "book-cleaning",
	"reservation-uae":   "book-safari",
	"quote-portal-uae":  "quote-meeting",
	"listings-uae":      "quote-villa",
	"delivery-tracking": "deliver-courier",
}

// exampleJob is the job of the business each template does, as the
// landing page groups them.
var exampleJob = map[string]string{
	"store-uae":         "Sell products",
	"booking-uae":       "Take bookings",
	"home-service-uae":  "Take bookings",
	"reservation-uae":   "Take bookings",
	"quote-portal-uae":  "Send quotes",
	"listings-uae":      "Send quotes",
	"delivery-tracking": "Deliver",
}

// sitePath is one of the three jobs on the landing page: what the owner
// wants to do, and the templates that do it.
type sitePath struct {
	ID, N, Title, Promise string
	Suits, Includes       []string
	Photo                 photo
	Extra                 []photo
	Demos                 []galleryCard
}

func (s *Site) sitePaths(cards []galleryCard) []sitePath {
	byName := map[string]galleryCard{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	pick := func(names ...string) []galleryCard {
		var out []galleryCard
		for _, n := range names {
			if c, ok := byName[n]; ok {
				out = append(out, c)
			}
		}
		return out
	}
	return []sitePath{
		{ID: "sell", N: "01", Title: "Sell products", Promise: "A shop that takes cards and cash on delivery, so orders stop living in your DMs.",
			Suits:    []string{"Perfume and fashion", "Electronics", "Cakes and flowers to order"},
			Includes: []string{"Cart and checkout", "Sizes and colours", "Discount codes", "Order-ahead dates"},
			Photo:    photos["shop-boutique"], Extra: []photo{photos["shop-cake"], photos["shop-flowers"]}, Demos: pick("store-uae")},
		{ID: "book", N: "02", Title: "Take bookings", Promise: "Customers pick a free time and pay a deposit; your calendar fills itself.",
			Suits:    []string{"Salons and spas", "Cleaning and AC", "Safaris, yachts and rentals"},
			Includes: []string{"Deposits", "Reminders", "Staff calendar"},
			Photo:    photos["book-salon"], Extra: []photo{photos["book-safari"], photos["book-yacht"]}, Demos: pick("booking-uae", "home-service-uae", "reservation-uae")},
		{ID: "quote", N: "03", Title: "Send quotes", Promise: "Quote online, get accepted and paid a deposit, then share documents in a private portal.",
			Suits:    []string{"Business setup and PRO", "Accountants and planners", "Real-estate brokers"},
			Includes: []string{"Online deposits", "Client portal", "Viewing requests"},
			Photo:    photos["quote-meeting"], Extra: []photo{photos["quote-villa"], photos["quote-skyline"]}, Demos: pick("quote-portal-uae", "listings-uae")},
	}
}

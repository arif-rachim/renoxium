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
	"unicode"
)

// Screenshots of the template demos shown on the public site pages,
// embedded in the binary; see static/img/CREDITS.md.
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

// screen is a screenshot of a template demo: a phone screen (600x1298)
// unless W and H say otherwise.
type screen struct {
	File, Alt string
	W, H      int
}

func (s screen) Src() string { return staticURL(s.File) }

func phone(file, alt string) screen { return screen{File: file, Alt: alt, W: 600, H: 1298} }

var screens = map[string]screen{
	"store-uae":         phone("screen-store-product.webp", "The Royal Oud product page of the Al Noor Perfumes demo shop: AED 250.00 including VAT, a 50 ml option, quantity, Add to cart and a Chat on WhatsApp button"),
	"store-ar":          phone("screen-store-home-ar.webp", "The Al Noor Perfumes demo shop in Arabic: the shop name, category filters and four perfumes with AED prices, laid out right to left"),
	"booking-uae":       phone("screen-booking-slots.webp", "The Haircut and Blow-dry page of the Lamsa Beauty Lounge demo: AED 150 including VAT, a choice of stylist, a date and a grid of free times from 11:00 to 20:00"),
	"home-service-uae":  phone("screen-home-service.webp", "The Home cleaning page of the Bayti Home Services demo: AED 40 per hour, number of hours, area, date and arrival windows, one of them already full"),
	"reservation-uae":   phone("screen-reservation.webp", "The Evening Desert Safari with BBQ Dinner page of the Sahara Bay demo: what is included, adult and child prices, and a list of departures with seats left"),
	"quote-portal-uae":  phone("screen-quote-portal.webp", "The home page of the Bayan Business Setup demo: an Ask for a quote button and company formation packages from AED 12,900 and AED 14,500 with typical durations"),
	"listings-uae":      phone("screen-listing.webp", "An off-plan three-bedroom townhouse in JVC on the Marsa Homes demo: AED 2,150,000, property type, bedrooms, bathrooms, area, developer and handover date"),
	"delivery-tracking": phone("screen-delivery.webp", "Delivery #1 of the Al Noor Flowers demo for Mariam, marked On the way, with the courier's position on a map of Dubai"),
	"admin-calendar": {File: "screen-admin-calendar.webp", W: 1400, H: 875,
		Alt: "The owner's admin panel of the Lamsa Beauty Lounge demo: the bookings calendar for Thursday 19 November, one lane each for Layla, Maryam and Priya, with appointments from 10:00 to 18:00 marked confirmed or pending"},
}

// exampleJob is what the owner does with each template, as the landing
// page and the gallery group them, in their words.
var exampleJob = map[string]string{
	"store-uae":         "I sell products",
	"booking-uae":       "I take bookings",
	"home-service-uae":  "I take bookings",
	"reservation-uae":   "I take bookings",
	"quote-portal-uae":  "I send quotes",
	"listings-uae":      "I send quotes",
	"delivery-tracking": "I deliver",
}

// demoName names each demo by the business it runs, for links on the
// landing page.
var demoName = map[string]string{
	"store-uae":        "Perfume shop demo",
	"booking-uae":      "Salon demo",
	"home-service-uae": "Home cleaning demo",
	"reservation-uae":  "Desert safari demo",
	"quote-portal-uae": "Business setup demo",
	"listings-uae":     "Property broker demo",
}

// sitePath is one of the three ways in on the landing page: what the owner
// does, and the demos that show it.
type sitePath struct {
	ID, Title, Text string
	Screen          screen
	Demos           []demoLink
}

type demoLink struct{ Label, URL string }

func (s *Site) sitePaths(cards []galleryCard) []sitePath {
	byName := map[string]galleryCard{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	pick := func(names ...string) []demoLink {
		var out []demoLink
		for _, n := range names {
			if c, ok := byName[n]; ok {
				out = append(out, demoLink{Label: demoName[n], URL: c.DemoURL + "/"})
			}
		}
		return out
	}
	return []sitePath{
		{ID: "sell", Title: "I sell products", Screen: screens["store-ar"], Demos: pick("store-uae"),
			Text: "A shop with a cart and checkout for a perfume shop, a boutique or cakes ordered days ahead. Customers pay by card or cash on delivery."},
		{ID: "book", Title: "I take bookings", Screen: screens["booking-uae"], Demos: pick("booking-uae", "home-service-uae", "reservation-uae"),
			Text: "Customers pick a free time and pay a deposit, for a salon, a cleaning company or a desert safari."},
		{ID: "quote", Title: "I send quotes", Screen: screens["quote-portal-uae"], Demos: pick("quote-portal-uae", "listings-uae"),
			Text: "Clients ask for a quote, accept it and pay a deposit online, for a business setup firm or a property broker."},
	}
}

// suits joins the kinds of business a template suits into one sentence
// part: "beauty salons and barbers, spas and clinics".
func suits(kinds []string) string {
	out := ""
	for i, k := range kinds {
		r := []rune(k)
		if len(r) > 1 && unicode.IsUpper(r[0]) && unicode.IsLower(r[1]) {
			r[0] = unicode.ToLower(r[0])
		}
		if i > 0 {
			out += ", "
		}
		out += string(r)
	}
	return out
}

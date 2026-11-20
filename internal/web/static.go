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
	"store-uae":         phone("screen-store-home.webp", "The home page of the Al Noor Perfumes demo shop: Premium perfume collection, Oud, musk and amber from the UAE for every moment, a Discover the collection button and an arched photo of desert dunes at dusk"),
	"store-ar":          phone("screen-store-home-ar.webp", "The Al Noor Perfumes demo shop in Arabic: the perfume families with arched photos, a search field and perfumes in arched cards with Best seller and New labels and AED prices, laid out right to left"),
	"booking-uae":       phone("screen-booking.webp", "The home page of the Lamsa Beauty Lounge demo: Where calm meets care, Hair, nails and skincare in Jumeirah, every visit, a Book an appointment button and a photo of a calm spa bathroom with a turning stamp"),
	"home-service-uae":  phone("screen-home-service.webp", "The home page of the Bayti Home Services demo: Cleaning, AC, handyman, Home care, done right, Book a visit and How it works buttons and a photo of a tidy living room"),
	"reservation-uae":   phone("screen-reservation.webp", "The home page of the Sahara Bay Tours and Rentals demo: Desert safaris, cruises, car hire and party rentals in Dubai on a brown banner with a See tours and rentals button and a photo of a camel caravan crossing the dunes"),
	"quote-portal-uae":  phone("screen-quote-portal.webp", "The home page of the Bayan Business Setup demo: Company formation, visas and tax services in Dubai, with a clear quote before you pay, an Ask for a quote button and a photo of the Dubai skyline at night"),
	"listings-uae":      phone("screen-listing.webp", "The home page of the Marsa Homes demo: Homes in Dubai, known street by street, with Browse properties and List your property buttons, a photo of Downtown Dubai at sunset and a search bar for purpose, location, type and price"),
	"delivery-tracking": phone("screen-delivery.webp", "Delivery #1 of the Al Noor Flowers demo for Mariam: the courier's position on a map of Dubai and the progress Preparing, On the way, Delivered, with On the way reached"),
	"admin-calendar": {File: "screen-admin-calendar.webp", W: 1400, H: 920,
		Alt: "The owner's admin panel of the Lamsa Beauty Lounge demo: the bookings calendar for one day, one lane each for Layla, Maryam and Priya, with nine appointments from 10:00 to 19:00, each with the customer's name and service, marked confirmed or pending"},
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

// trade is one kind of business on the landing page: what its customers do
// on the site and what the owner gets, shown with the demo that fits it.
type trade struct {
	ID, Tab, Example  string
	Customers, Owner  []string
	Screen            screen
	DemoURL, AdminURL string
}

var trades = []trade{
	{ID: "salon", Tab: "Salons and spas", Example: "booking-uae",
		Customers: []string{"Pick a service, a stylist and a free time", "Pay a deposit by card, or pay at the salon", "Join a class with limited seats, billed per term or monthly", "Cancel in time and the deposit is refunded at once"},
		Owner:     []string{"A day calendar with one lane per stylist", "No double bookings: free times come from your hours and each stylist's bookings", "Tomorrow's schedule by email every evening", "Move or cancel a booking and the customer is emailed"}},
	{ID: "shop", Tab: "Shops", Example: "store-uae",
		Customers: []string{"Browse by category, search and filter", "Choose a size or option with its own price and stock", "Pay by card or cash on delivery, with discount codes", "Pick a delivery date for made-to-order cakes or flowers"},
		Owner:     []string{"Every order with its status in your panel", "A delivery fee, and free delivery above an amount you set", "Stock for each product option", "Tax invoices once you add your TRN"}},
	{ID: "home", Tab: "Home services", Example: "home-service-uae",
		Customers: []string{"Choose a service, an area and an arrival window", "Add a photo of the problem", "Book weekly or two-weekly visits, paid by card automatically", "Follow the crew on a map on the day"},
		Owner:     []string{"A set number of crews per window, so you are never overbooked", "A phone page for each crew: on the way, arrived, completed, cash collected", "Area surcharges added to the price", "Cancellations before your cut-off are refunded automatically"}},
	{ID: "tours", Tab: "Tours and rentals", Example: "reservation-uae",
		Customers: []string{"Book seats on a dated departure, with adult and child prices", "Rent a car or equipment by the day, with a weekly price", "Add extras such as a child seat or delivery", "Get a voucher to show on the day"},
		Owner:     []string{"Seats and stock counted from the reservations, never oversold", "A deposit online and the balance on the day", "A security deposit as a card hold you release or capture at the return", "Tomorrow's departures, handovers and returns by email"}},
	{ID: "property", Tab: "Property brokers", Example: "listings-uae",
		Customers: []string{"Search by sale or rent, community, bedrooms and price", "See the permit number and QR code on every listing", "Request a viewing or ask a question without signing in", "Call or WhatsApp the agent from the listing"},
		Owner:     []string{"Leads in your panel with a follow-up status", "Viewings in a calendar, one lane per agent", "A listing without a permit number stays hidden", "A daily summary by email"}},
	{ID: "services", Tab: "Business services", Example: "quote-portal-uae",
		Customers: []string{"Send an enquiry with a target date and a budget", "Read your quote and accept it online", "Pay the deposit and the balance by card or bank transfer", "Follow progress and share documents in a private portal"},
		Owner:     []string{"Write quotes in your panel, with 5% VAT added or included", "A payments ledger for each quote", "Clients are emailed when you update a quote", "Card payments refunded when you cancel a quote, if you choose"}},
}

// siteTrades fills the trades with their demos; a trade whose demo is not
// in the gallery is left out.
func (s *Site) siteTrades(cards []galleryCard) []trade {
	byName := map[string]galleryCard{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	var out []trade
	for _, t := range trades {
		c, ok := byName[t.Example]
		if !ok {
			continue
		}
		t.Screen, t.DemoURL, t.AdminURL = screens[t.Example], c.DemoURL+"/", c.AdminURL
		out = append(out, t)
	}
	return out
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

package examples

import (
	"embed"
	"sort"
)

// Photos for the template demos only. The templates themselves keep empty
// image seeds, so an agent copying one (get_example) uploads the owner's
// own photos; the platform uploads these to the demo-<example> apps after
// every reset instead. Sources and licences are in demo-images/CREDITS.md.

//go:embed demo-images/*.webp
var demoImages embed.FS

// DemoImageSet gives some rows of one table photos in a demo: Rows maps the
// value of Key in a seed row to that row's image columns and their files in
// demo-images/.
type DemoImageSet struct {
	Table string
	Key   string
	Rows  map[string]map[string]string
}

var demoImageSets = map[string][]DemoImageSet{
	"store-uae": {{Table: "store", Key: "name", Rows: map[string]map[string]string{
		"Al Noor Perfumes": {"hero_image": "store-hero.webp", "story_image": "store-story.webp"},
	}}, {Table: "products", Key: "name", Rows: map[string]map[string]string{
		"Royal Oud":    {"image": "store-royal-oud.webp"},
		"White Musk":   {"image": "store-white-musk.webp"},
		"Amber Night":  {"image": "store-amber-night.webp"},
		"Oud Gift Set": {"image": "store-oud-gift-set.webp"},
	}}},
	"booking-uae": {
		{Table: "services", Key: "name", Rows: map[string]map[string]string{
			"Haircut & Blow-dry":      {"image": "booking-haircut.webp"},
			"Root Colour":             {"image": "booking-hair-colour.webp"},
			"Classic Manicure":        {"image": "booking-manicure.webp"},
			"Gel Manicure & Pedicure": {"image": "booking-gel-nails.webp"},
			"Signature Facial":        {"image": "booking-facial.webp"},
			"Moroccan Bath":           {"image": "booking-moroccan-bath.webp"},
		}},
		{Table: "classes", Key: "name", Rows: map[string]map[string]string{
			"Everyday Makeup Workshop": {"image": "booking-makeup-class.webp"},
		}},
	},
	"home-service-uae": {{Table: "business", Key: "name", Rows: map[string]map[string]string{
		"Bayti Home Services": {"hero_image": "home-hero.webp", "story_image": "home-story.webp"},
	}}, {Table: "services", Key: "name", Rows: map[string]map[string]string{
		"Home cleaning":                       {"image": "home-cleaning.webp"},
		"Deep cleaning (1-bedroom apartment)": {"image": "home-deep-cleaning.webp"},
		"Sofa shampoo cleaning":               {"image": "home-sofa.webp"},
		"AC service":                          {"image": "home-ac.webp"},
		"Handyman":                            {"image": "home-handyman.webp"},
		"Pest control (apartment)":            {"image": "home-pest-control.webp"},
	}}},
	"reservation-uae": {{Table: "business", Key: "name", Rows: map[string]map[string]string{
		"Sahara Bay Tours & Rentals": {"hero_image": "tour-hero.webp", "story_image": "tour-story.webp"},
	}}, {Table: "items", Key: "name", Rows: map[string]map[string]string{
		"Evening Desert Safari with BBQ Dinner": {"image": "tour-safari.webp"},
		"Marina Dhow Dinner Cruise":             {"image": "tour-dhow.webp"},
		"Sunset Yacht Trip":                     {"image": "tour-yacht.webp"},
		"Nissan Sunny":                          {"image": "car-sedan.webp"},
		"Nissan Patrol":                         {"image": "car-suv.webp"},
		"Gold Chiavari Chair":                   {"image": "party-chairs.webp"},
		"Arabic Majlis Tent 5 x 5 m":            {"image": "party-majlis-tent.webp"},
		"Bouncy Castle 4 x 4 m":                 {"image": "party-bouncy-castle.webp"},
	}}},
	"quote-portal-uae": {{Table: "business", Key: "name", Rows: map[string]map[string]string{
		"Bayan Business Setup": {"hero_image": "quote-hero.webp", "story_image": "quote-story.webp"},
	}}, {Table: "packages", Key: "name", Rows: map[string]map[string]string{
		"Mainland LLC company setup": {"image": "quote-mainland.webp"},
		"Free-zone company setup":    {"image": "quote-free-zone.webp"},
		"Golden Visa application":    {"image": "quote-golden-visa.webp"},
		"Trade licence renewal":      {"image": "quote-licence-renewal.webp"},
		"VAT registration":           {"image": "quote-vat.webp"},
		"Monthly bookkeeping":        {"image": "quote-bookkeeping.webp"},
	}}},
	"delivery-tracking": {{Table: "shop", Key: "name", Rows: map[string]map[string]string{
		"Al Noor Flowers": {"hero_image": "delivery-hero.webp", "story_image": "delivery-story.webp"},
	}}},
	"listings-uae": {{Table: "agency", Key: "name", Rows: map[string]map[string]string{
		"Marsa Homes": {"hero_image": "listings-hero.webp", "story_image": "listings-story.webp"},
	}}, {Table: "listings", Key: "reference", Rows: map[string]map[string]string{
		"MH-1001": {"image1": "listing-living-room.webp", "image2": "listing-kitchen.webp", "image3": "listing-bedroom.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1002": {"image1": "listing-apartment-1.webp", "image2": "listing-kitchen.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1003": {"image1": "listing-apartment-2.webp", "image2": "listing-bedroom.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1004": {"image1": "listing-villa-1.webp", "image2": "listing-living-room.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1005": {"image1": "listing-burj-view.webp", "image2": "listing-apartment-3.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1006": {"image1": "listing-townhouse.webp", "image2": "listing-kitchen.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1007": {"image1": "listing-villa-2.webp", "image2": "listing-bedroom.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1008": {"image1": "listing-office.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1009": {"image1": "listing-villa-3.webp"},
		"MH-1010": {"image1": "listing-apartment-3.webp", "image2": "listing-bedroom.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1011": {"image1": "listing-apartment-1.webp", "image2": "listing-living-room.webp", "permit_qr": "listings-permit-qr.webp"},
		"MH-1012": {"image1": "listing-retail.webp", "permit_qr": "listings-permit-qr.webp"},
	}}},
}

// DemoImages returns the photo sets of an example's demo, nil for none.
func DemoImages(example string) []DemoImageSet { return demoImageSets[example] }

// DemoImage returns a demo photo by its file name in demo-images/.
func DemoImage(file string) ([]byte, error) { return demoImages.ReadFile("demo-images/" + file) }

// DemoImageFiles lists every demo photo file, sorted.
func DemoImageFiles() []string {
	entries, _ := demoImages.ReadDir("demo-images")
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

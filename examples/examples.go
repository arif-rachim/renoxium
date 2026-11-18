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
	//go:embed delivery-tracking.json
	deliveryTracking []byte
	//go:embed store-uae.json
	storeUAE []byte
	//go:embed home-service-uae.json
	homeServiceUAE []byte
	//go:embed reservation-uae.json
	reservationUAE []byte
	//go:embed booking-uae.json
	bookingUAE []byte
	//go:embed listings-uae.json
	listingsUAE []byte
)

// Example is one complete app definition.
type Example struct {
	Name        string
	Description string
	JSON        []byte
}

var all = map[string]Example{
	"booking-uae": {Name: "booking-uae", Description: "A complete booking site for UAE businesses that sell time, ready to customise: appointments with a staff member " +
		"(beauty salons, barbershops, spas) and group classes with limited seats (academies, training centres, tutors). Free times are worked out from the " +
		"opening hours, each service's duration and each staff member's bookings, and checked again when booking; classes refuse enrolments when full. " +
		"AED prices including 5% VAT, an online deposit by card (Stripe; 0 to 100% of the price) or pay at the venue, bookings for signed-in customers " +
		"with cancellation until a set number of hours before, printable receipts (a UAE tax invoice once the TRN is set), confirmation and day-before " +
		"reminder emails, tomorrow's schedule emailed to the owner every evening, read-only tools for customers' AI assistants, English and Arabic " +
		"(right to left). Change the business, services, staff, classes and texts tables' seed rows and keep the scripts: " +
		"a salon fills services and staff and may leave classes empty; an academy fills classes and may leave services and staff empty.", JSON: bookingUAE},
	"catalog": {Name: "catalog", Description: "A small shop: product list, product page with an order form, and a JSON API.", JSON: catalog},
	"reservation-uae": {Name: "reservation-uae", Description: "A complete reservation site for UAE businesses that sell dated inventory, ready to customise. " +
		"Each item is sold in one of two ways (items.mode): departure = seats on dated departures with adult and child prices (desert safari, dhow cruise, " +
		"yacht trip, city tour); range = a quantity rented from a start date to an end date with a price per day, optional weekly price, minimum days and a " +
		"refundable security deposit shown to the customer and collected at handover (car rental, party and event equipment). Availability is never counted down: " +
		"it is added up from the reservations, so cancelled, failed, expired and refunded reservations free their seats or stock by themselves. " +
		"AED prices including 5% VAT, optional extras per booking, person or day, an online card deposit (business.deposit_percent: 100 = pay in full, 0 = nothing online) " +
		"through Stripe with the balance due on the day, or reserve now and pay later, reservations for signed-in customers with cancellation inside the " +
		"free-cancellation window, a printable voucher (a tax invoice once the TRN is set) showing the licence number, confirmation and day-before reminder emails, " +
		"a daily job that sends the owner tomorrow's departures, handovers and returns and expires unpaid reservations, read-only MCP tools, English and Arabic (right to left). " +
		"Dates: the seeded departures have no date; they repeat on weekdays (departures.weekdays: daily, or e.g. fri,sat) at departures.time, so the sample is always in the future. " +
		"For a one-off departure set departures.date to YYYY-MM-DD (UAE local date) and leave weekdays empty. " +
		"To customise, change the seed rows of business and texts, then: tour operator = items with mode departure plus departures and extras, and delete the range items; " +
		"car rental = items with mode range (stock = cars of that model, price_day, price_week, min_days, security_deposit) and extras such as child seat or delivery, " +
		"and leave departures empty; party rental = items with mode range (stock = pieces owned) and a delivery extra. Keep the scripts. " +
		"Not possible on the platform, so not in the example: card holds or later automatic charges, customer file uploads (licence numbers are typed as notes), WhatsApp messages to customers.", JSON: reservationUAE},
	"store-uae": {Name: "store-uae", Description: "A complete online store for the UAE, ready to customise: catalog with photos and categories, cart, " +
		"AED prices including 5% VAT, delivery fee with a free-delivery threshold, checkout with card payment (Stripe) or cash on delivery, " +
		"orders for signed-in shoppers (email or WhatsApp sign-in), printable invoices (a UAE tax invoice once the store's TRN is set), " +
		"discount codes, product options such as sizes with their own price and stock (variants table), English and Arabic (right to left). " +
		"For made-to-order goods (cakes, flowers, catering) set store.preorder_days: shoppers then choose a delivery or pickup date that many days ahead. " +
		"Change the store, products, variants, coupons and texts tables' seed rows.", JSON: storeUAE},
	"delivery-tracking": {Name: "delivery-tracking", Description: "Live delivery tracking: the courier opens a private link on their phone and shares " +
		"their location; the customer follows the courier on a map that moves in real time (realtime.publish and EventSource), " +
		"and the owner is told when it is delivered. Deliveries, tracking codes and courier keys are managed in the admin panel.", JSON: deliveryTracking},
	"home-service-uae": {Name: "home-service-uae", Description: "A complete booking and dispatch site for a UAE home-service company, ready to customise for cleaning, " +
		"AC servicing, pest control, handyman, movers or laundry pickup: services priced per hour, per unit or fixed, areas served with surcharges, " +
		"date and arrival-window picker with a limited number of crews per window, AED prices including 5% VAT, card payment (Stripe) or cash on completion, " +
		"weekly and 2-weekly repeat visits booked by a daily job, bookings for signed-in customers with live status and the crew on a map, " +
		"a private phone page for each crew (on the way, arrived, completed, cash collected), cancellation before a cut-off, printable receipts " +
		"(a UAE tax invoice once the TRN is set), email confirmations and reminders, owner notices, tools for customers' AI assistants, " +
		"English and Arabic (right to left). Change the business, services, zones, crews and texts tables' seed rows.", JSON: homeServiceUAE},
	"listings-uae": {Name: "listings-uae", Description: "A property listings and lead-capture site for a UAE real-estate brokerage or agent, ready to customise " +
		"(no online payments): search by sale or rent, type, community, bedrooms and price with sorting and pages, listing pages with gallery, " +
		"AED prices, agent card with call and WhatsApp buttons, and forms to request a viewing or ask a question; leads go to the owner by " +
		"email (and WhatsApp notices) and into the admin panel with a follow-up status, with spam limits for forms that need no sign-in. " +
		"Regulatory fields are first-class: each listing shows its advertising permit number and QR code (Trakheesi in Dubai, ADREC in Abu Dhabi), " +
		"the office number (ORN) and the agent's broker number (BRN), and a listing without a permit number is hidden from visitors. " +
		"English and Arabic (right to left), a daily summary for the owner, and read-only tools for buyers' AI assistants. " +
		"Change the agency, agents, listings and texts tables' seed rows; it also suits car dealers or other listing-and-lead businesses by changing the listing fields.", JSON: listingsUAE},
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

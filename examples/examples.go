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
	//go:embed quote-portal-uae.json
	quotePortalUAE []byte
	//go:embed home-service-uae.json
	homeServiceUAE []byte
	//go:embed reservation-uae.json
	reservationUAE []byte
	//go:embed booking-uae.json
	bookingUAE []byte
	//go:embed listings-uae.json
	listingsUAE []byte
	//go:embed clinic-queue-uae.json
	clinicQueueUAE []byte
)

// Example is one complete app definition.
type Example struct {
	Name        string
	Description string // for building agents: what the example does and how to adapt it
	JSON        []byte
	// Gallery puts the example in the public template gallery with a live
	// demo. Nil keeps it for agents only.
	Gallery *GalleryInfo
	// HiddenFromAgents keeps the example out of the builder MCP server:
	// get_example neither lists nor returns it and create_app refuses it as
	// an unknown example (ForAgents, GetForAgents, platform.CreateForAgent).
	// The gallery, the demos and "use this template" on the website still
	// offer it. The MCP server is frozen while the ChatGPT app directory
	// reviews it; to offer a hidden example to agents after the review,
	// remove HiddenFromAgents from its entry in all, add it to the list of
	// examples in mcpserver's instructions if it should be named there, and
	// regenerate internal/mcpserver/testdata/mcp-snapshot.json (see
	// TestMCPServerIsFrozenForReview).
	HiddenFromAgents bool
}

// GalleryInfo is how the template gallery presents an example to business
// owners: plain words, no technical terms.
type GalleryInfo struct {
	Title   string   // e.g. "Salon and class booking"
	Summary string   // one or two sentences
	For     []string // kinds of business it suits, e.g. "Beauty salons"
	Order   int      // position in the gallery, lowest first
}

var gallery = map[string]GalleryInfo{
	"store-uae": {Order: 1, Title: "Online store",
		Summary: "A shop with a catalog, cart and checkout: card payment or cash on delivery, AED prices with VAT, discount codes, sizes and colours, invoices, in English and Arabic.",
		For:     []string{"Perfume and fashion shops", "Electronics and accessories", "Cakes and flowers to order"}},
	"booking-uae": {Order: 2, Title: "Appointments and classes",
		Summary: "Customers pick a service, a staff member and a free time, or enrol in a class with limited seats; deposits by card, reminders and a daily schedule for you.",
		For:     []string{"Beauty salons and barbers", "Spas and clinics", "Academies and tutors"}},
	"home-service-uae": {Order: 3, Title: "Home services",
		Summary: "Bookings by area and arrival window, crews dispatched from their phones, live status for the customer, repeat visits and card or cash payment.",
		For:     []string{"Cleaning", "AC servicing and pest control", "Handyman and movers"}},
	"reservation-uae": {Order: 4, Title: "Tours and rentals",
		Summary: "Seats on dated departures or items rented by the day, with deposits, extras, vouchers, document uploads and automatic refunds on cancellation.",
		For:     []string{"Desert safaris and yacht trips", "Car rental", "Party and event rentals"}},
	"quote-portal-uae": {Order: 5, Title: "Quotes and client portal",
		Summary: "Clients send an enquiry, you send a quote, they accept and pay a deposit online, then follow progress and upload their documents in a private portal.",
		For:     []string{"Business setup and PRO services", "Accounting firms", "Event planners and photographers"}},
	"listings-uae": {Order: 6, Title: "Property listings",
		Summary: "Search homes to buy or rent, listing pages with permit numbers, and viewing requests that land in your calendar with one lane per agent.",
		For:     []string{"Real-estate brokers", "Property agents", "Car dealers"}},
	"clinic-queue-uae": {Order: 7, Title: "Walk-in queue",
		Summary: "Patients take a number on their phone or at reception, follow their place live and get a WhatsApp message when two are ahead; a TV screen calls numbers to rooms. No payments: a work app.",
		For:     []string{"GP, dental and physio clinics", "Barbers and walk-in salons", "Service counters and typing centres"}},
	"delivery-tracking": {Order: 8, Title: "Live delivery tracking",
		Summary: "Your courier shares their location from their phone and the customer follows the delivery on a live map.",
		For:     []string{"Restaurants and grocers", "Couriers", "Any business that delivers"}},
}

func init() {
	for name, g := range gallery {
		e, ok := all[name]
		if !ok {
			panic("examples: gallery entry for unknown example " + name)
		}
		g := g
		e.Gallery = &g
		all[name] = e
	}
}

var all = map[string]Example{
	"booking-uae": {Name: "booking-uae", Description: "A complete booking site for UAE businesses that sell time, ready to customise: appointments with a staff member " +
		"(beauty salons, barbershops, spas) and group classes with limited seats (academies, training centres, tutors). Free times are worked out from the " +
		"opening hours, each service's duration and each staff member's bookings, and checked again when booking; classes refuse enrolments when full. " +
		"AED prices including 5% VAT, an online deposit by card (Stripe; 0 to 100% of the price) or pay at the venue, bookings for signed-in customers " +
		"with cancellation until a set number of hours before (what was paid by card is refunded at once), classes billed per term or monthly by card " +
		"(a subscription the customer can stop), emails to the customer when the owner moves, confirms or cancels a booking in the admin panel " +
		"(cancelling refunds it; a moved booking is checked for double booking), printable receipts (a UAE tax invoice once the TRN is set), " +
		"confirmation and day-before reminder emails, tomorrow's schedule emailed to the owner every evening, read-only tools for customers' AI assistants, English and Arabic " +
		"(right to left). Change the business, services, staff, classes and texts tables' seed rows and keep the scripts: " +
		"a salon fills services and staff and may leave classes empty; an academy fills classes and may leave services and staff empty.", JSON: bookingUAE},
	"catalog": {Name: "catalog", Description: "A small shop: product list, product page with an order form, and a JSON API.", JSON: catalog},
	"quote-portal-uae": {Name: "quote-portal-uae", Description: "Quote-to-payment client portal for UAE service firms that sell by quotation " +
		"(business setup and PRO consultants, accounting and tax firms, event and wedding planners, photographers, small agencies): " +
		"packages with starting prices, enquiries from signed-in visitors, quotes the owner writes in the admin panel (lines as JSON, 5% VAT added on top " +
		"or included), online acceptance recorded with name and time, deposit and balance by card (payments.checkout, on_payment) or bank transfer " +
		"the owner marks paid, a payments ledger, progress updates and messages per quote, printable receipts (UAE tax invoices once the TRN is set), " +
		"emails to clients right after the owner changes a row in the admin panel (on_admin_change, with an hourly job retrying emails over the daily limit), shared helpers in lib, a daily summary for the owner, read-only MCP tools, " +
		"English and Arabic (right to left). To reuse it, change the seed rows of business, packages and texts and keep the scripts: " +
		"for an event planner or photographer, replace the packages (e.g. wedding planning, corporate event, half-day shoot) with their categories, " +
		"set prices_include_vat to 1 when selling to private clients, adjust deposit_percent, and reword the how_* and message_hint texts " +
		"(the enquiry form already has a target date and a budget). Signed-in clients attach documents to a quote (passport copy, Emirates ID, trade licence) " +
		"as private uploads (file column, /_rx/upload.js, ownership checked with files.get, at most 20 per quote; the owner opens them in the admin panel and is notified). " +
		"Setting a quote to cancelled refunds its card payments with payments.refund when the owner ticks refund_on_cancel (otherwise the money stays recorded); " +
		"refunds keep the amount paid and record refunded_amount in the ledger, are emailed to the client and shown in the portal.", JSON: quotePortalUAE},
	"reservation-uae": {Name: "reservation-uae", Description: "A complete reservation site for UAE businesses that sell dated inventory, ready to customise. " +
		"Each item is sold in one of two ways (items.mode): departure = seats on dated departures with adult and child prices (desert safari, dhow cruise, " +
		"yacht trip, city tour); range = a quantity rented from a start date to an end date with a price per day, optional weekly price, minimum days and a " +
		"refundable security deposit (car rental, party and event equipment). Availability is never counted down: " +
		"it is added up from the reservations, so cancelled, failed, expired and refunded reservations free their seats or stock by themselves. " +
		"AED prices including 5% VAT, optional extras per booking, person or day, an online card deposit (business.deposit_percent: 100 = pay in full, 0 = nothing online) " +
		"through Stripe with the balance due on the day, or reserve now and pay later, reservations for signed-in customers with cancellation inside the " +
		"free-cancellation window (what was paid is refunded automatically with payments.refund), a printable voucher (a tax invoice once the TRN is set) showing the licence number, " +
		"confirmation and day-before reminder emails, a daily job that sends the owner tomorrow's departures, handovers and returns and expires unpaid reservations, " +
		"read-only MCP tools, English and Arabic (right to left). " +
		"Security deposit: with business.deposit_hold_enabled it is a card hold the customer authorises on the reservation page from 6 days before the start " +
		"(holds last about 7 days; the daily job emails the link), and at the return the owner sets reservations.deposit_action to release, or to capture with a " +
		"damage_amount; with it off, the deposit is collected at handover. Rentals in the category Cars, or items with needs_documents, tell the customer to bring " +
		"the documents the rental needs: they are checked in person at handover and nothing is uploaded online. An on_admin_change hook emails " +
		"the customer when the owner confirms, changes dates, marks a no-show or cancels (refunding the payment and releasing the hold). Shared helpers are in lib. " +
		"Dates: the seeded departures have no date; they repeat on weekdays (departures.weekdays: daily, or e.g. fri,sat) at departures.time, so the sample is always in the future. " +
		"For a one-off departure set departures.date to YYYY-MM-DD (UAE local date) and leave weekdays empty. " +
		"To customise, change the seed rows of business and texts, then: tour operator = items with mode departure plus departures and extras, and delete the range items; " +
		"car rental = items with mode range (stock = cars of that model, price_day, price_week, min_days, security_deposit) and extras such as child seat or delivery, " +
		"and leave departures empty; party rental = items with mode range (stock = pieces owned) and a delivery extra. Keep the scripts. " +
		"Not possible on the platform, so not in the example: later automatic charges of a saved card, WhatsApp messages to customers.", JSON: reservationUAE},
	"store-uae": {Name: "store-uae", Description: "A complete online store for the UAE, ready to customise: catalog with photos and categories, cart, " +
		"AED prices including 5% VAT, a delivery fee (store.delivery_fee) with an optional free-delivery threshold (store.free_delivery_over; " +
		"set it to 0 or leave it empty for a flat fee on every order), checkout with card payment (Stripe) or cash on delivery, " +
		"orders for signed-in shoppers (email or WhatsApp sign-in), printable invoices (a UAE tax invoice once the store's TRN is set), " +
		"discount codes, product options such as sizes with their own price and stock (variants table), English and Arabic (right to left), " +
		"a modern feel (motion, a slide-in cart, search and filters without reloading, a swipeable photo gallery: products.image plus the optional image2 and image3). " +
		"For made-to-order goods (cakes, flowers, catering) set store.preorder_days: shoppers then choose a delivery or pickup date that many days ahead. " +
		"Change the store, products, variants, coupons and texts tables' seed rows.", JSON: storeUAE},
	"delivery-tracking": {Name: "delivery-tracking", Description: "Live delivery tracking: the courier opens a private link on their phone and shares " +
		"their location; the customer follows the courier on a map that moves in real time (realtime.publish and EventSource), " +
		"and the owner is told when it is delivered. Deliveries, tracking codes and courier keys are managed in the admin panel.", JSON: deliveryTracking},
	"home-service-uae": {Name: "home-service-uae", Description: "A complete booking and dispatch site for a UAE home-service company, ready to customise for cleaning, " +
		"AC servicing, pest control, handyman, movers or laundry pickup: services priced per hour, per unit or fixed, areas served with surcharges, " +
		"date and arrival-window picker with a limited number of crews per window, AED prices including 5% VAT, card payment (Stripe) or cash on completion, " +
		"weekly and 2-weekly repeat visits booked by a daily job, optionally paid automatically by card (payments.subscribe, each renewal pays the next visit), " +
		"an optional private photo of the problem (visitor upload), bookings for signed-in customers with live status and the crew on a map, " +
		"a private phone page for each crew (on the way, arrived, completed, cash collected), cancellation before a cut-off with automatic card refunds, " +
		"admin-panel hooks (setting the crew emails the customer, cancelling refunds and emails, status changes reach the live page), printable receipts " +
		"(a UAE tax invoice once the TRN is set), email confirmations and reminders, owner notices, tools for customers' AI assistants, shared script helpers in lib, " +
		"English and Arabic (right to left). Change the business, services, zones, crews and texts tables' seed rows.", JSON: homeServiceUAE},
	"listings-uae": {Name: "listings-uae", Description: "A property listings and lead-capture site for a UAE real-estate brokerage or agent, ready to customise " +
		"(no online payments): search by sale or rent, type, community, bedrooms and price with sorting and pages, listing pages with gallery, " +
		"AED prices, agent card with call and WhatsApp buttons, and forms to request a viewing or ask a question; leads go to the owner by " +
		"email (and WhatsApp notices) and into the admin panel with a follow-up status, with spam limits for forms that need no sign-in. " +
		"Regulatory fields are first-class: each listing shows its advertising permit number and QR code (Trakheesi in Dubai, ADREC in Abu Dhabi), " +
		"the office number (ORN) and the agent's broker number (BRN), and a listing without a permit number is hidden from visitors. " +
		"Viewing requests appear in an admin calendar by day, one lane per agent (spam and lost leads hidden); when the owner sets a lead to " +
		"viewing booked with a date and time, on_admin_change emails the visitor a confirmation with the listing and the agent's contact. " +
		"English and Arabic (right to left), a daily summary for the owner, read-only tools for buyers' AI assistants, and shared helpers in lib. " +
		"Change the agency, agents, listings and texts tables' seed rows; it also suits car dealers or other listing-and-lead businesses by changing the listing fields.", JSON: listingsUAE},
	"clinic-queue-uae": {Name: "clinic-queue-uae", HiddenFromAgents: true, Description: "A walk-in queue for a small UAE clinic, ready to customise (no payments, so it is a work app): " +
		"GP, dental and physiotherapy clinics, and also barbers or a service counter. Patients take a number without signing in, on their phone " +
		"(from a QR code at reception) or at a reception tablet (/kiosk): name, mobile number and the service or doctor. Their ticket page " +
		"(/ticket?k=<secret>) shows the number, the patients ahead and an estimated wait (minutes per patient divided by the doctors seeing " +
		"that service), updating live from one public realtime channel (public:queue, codes and counts only, never names). A waiting-room " +
		"screen for a TV (/screen) shows the numbers called and their rooms, with a chime, the next numbers and a QR code to take a number. " +
		"Staff call the next patient, call again, mark done or no-show on /staff (signed-in visitors whose email the owner lists in the staff " +
		"table) or in the admin panel (on_admin_change sets the times and room and updates the screens). When a patient has notify_ahead " +
		"(default 2) patients ahead they are told once with whatsapp.send (clinic.whatsapp_alerts; within the plan's WhatsApp messages, " +
		"falling back to email when it throws) and by email when they gave one. Every ticket belongs to a UAE day, so the queue starts empty " +
		"each morning; a job at 00:05 closes what was left, writes daily_stats (patients, seen, no-shows, average and longest wait, busiest " +
		"hour), wipes contact details older than keep_contact_days and emails the owner yesterday's numbers; /staff/report shows the last 14 days. " +
		"Opening hours, closed days, the last number before closing and a daily limit come from the clinic row. English and Arabic (right to left), " +
		"read-only tools for patients' AI assistants. To customise, change the seed rows of clinic, services (one ticket letter each, minutes " +
		"per patient, how many see them at once), rooms (the letters each room calls) and texts, and keep the scripts: a barber has one service " +
		"per barber and one room (chair) each; a typing centre has counters as rooms.", JSON: clinicQueueUAE},
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

// ForAgents returns the examples AI agents see through the builder MCP
// server (get_example, create_app), sorted by name: every example not
// HiddenFromAgents.
func ForAgents() []Example {
	var out []Example
	for _, e := range All() {
		if !e.HiddenFromAgents {
			out = append(out, e)
		}
	}
	return out
}

// GetForAgents returns an example by name as the builder MCP server offers
// it: a HiddenFromAgents example is not found.
func GetForAgents(name string) (Example, bool) {
	e, ok := all[name]
	if !ok || e.HiddenFromAgents {
		return Example{}, false
	}
	return e, true
}

// Gallery returns the examples shown in the public template gallery, in
// gallery order.
func Gallery() []Example {
	var out []Example
	for _, e := range all {
		if e.Gallery != nil {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Gallery.Order != out[j].Gallery.Order {
			return out[i].Gallery.Order < out[j].Gallery.Order
		}
		return out[i].Name < out[j].Name
	})
	return out
}

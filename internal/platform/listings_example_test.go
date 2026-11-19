package platform_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/arif-rachim/renoxium/examples"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/applog"
	"github.com/arif-rachim/renoxium/internal/platform"
)

// applyListings applies the listings-uae example to a new app "marsa".
func applyListings(t *testing.T) *platform.Platform {
	t.Helper()
	p := newPlatform(t)
	ex, ok := examples.Get("listings-uae")
	if !ok {
		t.Fatal("listings-uae is not registered")
	}
	def, problems := appdef.Parse(ex.JSON)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	p.CreateApp("usr_test", "marsa")
	if res, err := p.ApplyChange(context.Background(), "marsa", def); err != nil || !res.OK {
		t.Fatal(err, res)
	}
	return p
}

// The UAE listings example works end to end on preview: browsing and
// searching in two languages, the permit rule, leads from public forms with
// their limits, and the daily summary.
func TestListingsUAEExample(t *testing.T) {
	p := applyListings(t)
	ctx := context.Background()
	req := func(in platform.TestRequestInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "marsa", in)
		if err != nil {
			t.Fatalf("%+v: %v", in, err)
		}
		if r.Status >= 500 {
			t.Fatalf("%s %s: %d\n%s", in.Method, in.Path, r.Status, logText(r))
		}
		return r
	}
	get := func(path string) platform.TestResponse {
		t.Helper()
		r := req(platform.TestRequestInput{Path: path})
		if strings.Contains(r.Body, "no value") || strings.Contains(r.Body, "ZgotmplZ") {
			t.Fatalf("%s: a template value is missing or was refused:\n%s", path, r.Body)
		}
		return r
	}
	// has reports which listing references a page shows.
	check := func(what, body string, want, not []string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: %s is missing", what, w)
			}
		}
		for _, n := range not {
			if strings.Contains(body, n) {
				t.Errorf("%s: %s should not be there", what, n)
			}
		}
	}

	// Home in English and Arabic. MH-1009 is featured but has no permit number.
	home := get("/")
	check("home", home.Body, []string{`dir="ltr"`, "Marsa Homes", "MH-1001", "AED 2,350,000", "Two-bedroom apartment with full marina view",
		"Omar Al Hashimi", "SAMPLE-50101", `name="community"`, "Dubai Marina"}, []string{"MH-1009", "MH-1010", "MH-1012", "penthouse (waiting"})
	ar := get("/?lang=ar")
	check("arabic home", ar.Body, []string{`dir="rtl"`, "مرسى هومز", "شقة بغرفتي نوم بإطلالة كاملة على المارينا", "عمر الهاشمي", "/l/1?lang=ar"}, []string{"MH-1009"})

	// Search filters.
	all := get("/search")
	check("search", all.Body, []string{"9 properties found", "Page 1 / 2", "page=2"}, []string{"MH-1009", "MH-1010", "MH-1012"})
	check("rent", get("/search?purpose=rent").Body, []string{"MH-1002", "MH-1003", "MH-1007", "MH-1008", "MH-1011", "per year", `AED 6,500 <span class="per">per month`},
		[]string{"MH-1001", "MH-1004", "MH-1012"})
	check("two bedrooms", get("/search?beds=2").Body, []string{"MH-1001", "MH-1011", "2 properties found"}, []string{"MH-1010", "MH-1002"})
	check("studio", get("/search?beds=0").Body, []string{"MH-1003", "Studio"}, []string{"MH-1008", "MH-1001"})
	check("price range", get("/search?purpose=sale&min=1000000&max=2500000").Body, []string{"MH-1001", "MH-1005", "MH-1006", "Off-plan"}, []string{"MH-1004", "MH-1002"})
	check("community", get("/search?community=Dubai+Marina").Body, []string{"MH-1001", "MH-1011", "Reserved"}, []string{"MH-1009", "MH-1002"})
	check("type", get("/search?type=villa").Body, []string{"MH-1004", "MH-1007"}, []string{"MH-1001", "MH-1006"})
	check("nothing found", get("/search?community=Nowhere").Body, []string{"No properties match"}, []string{"MH-10"})
	check("arabic search", get("/search?purpose=rent&lang=ar").Body, []string{`dir="rtl"`, "سنوياً", "شقة مشرقة بغرفة نوم قرب سيركل مول", `name="lang" value="ar"`}, nil)

	// Sorting and pages: six per page, dearest first.
	first := get("/search?sort=price_desc")
	check("dearest first", first.Body, []string{"MH-1004", "MH-1001", "MH-1011", "sort=price_desc&amp;page=2"}, []string{"MH-1003", "MH-1002", "MH-1008"})
	if strings.Index(first.Body, "MH-1004") > strings.Index(first.Body, "MH-1001") {
		t.Error("price_desc does not put the dearest listing first")
	}
	check("second page", get("/search?sort=price_desc&page=2").Body, []string{"MH-1008", "MH-1002", "MH-1003", "Page 2 / 2"}, []string{"MH-1004", "page=3"})
	cheap := get("/search?sort=price_asc&purpose=sale")
	if strings.Index(cheap.Body, "MH-1005") > strings.Index(cheap.Body, "MH-1004") {
		t.Error("price_asc does not put the cheapest listing first")
	}
	newest := get("/search")
	if strings.Index(newest.Body, "MH-1006") > strings.Index(newest.Body, "MH-1005") {
		t.Error("newest first is not the default order")
	}

	// A listing page shows the permit number, the office number and the agent's broker number.
	l := get("/l/1")
	check("listing", l.Body, []string{"Two-bedroom apartment with full marina view", "AED 2,350,000", "Permit number", "SAMPLE-7100000001", "ORN", "SAMPLE-12345",
		"BRN", "SAMPLE-50101", "Omar Al Hashimi", `href="tel:&#43;971500000001"`, "https://wa.me/971500000001?text=Hello%2c%20I%20am%20interested%20in%20MH-1001",
		"Request a viewing", "Ask a question", `name="listing_id" value="1"`, `name="website"`, "Marina view", "1,310 sq ft", "View on map"}, nil)
	check("rental listing", get("/l/3").Body, []string{"AED 6,500", `<span class="per">per month`, "Studio", "Furnished", "SAMPLE-7100000003", "SAMPLE-50102"}, nil)
	check("off-plan listing", get("/l/6").Body, []string{"Off-plan", "Sample Developments", "Q4 2028"}, nil)
	check("arabic listing", get("/l/1?lang=ar").Body, []string{`dir="rtl"`, "رقم التصريح", "SAMPLE-7100000001", "عمر الهاشمي", "طلب معاينة"}, nil)
	// No permit number, sold, hidden and unknown listings have no page.
	for _, path := range []string{"/l/9", "/l/10", "/l/12", "/l/999", "/l/abc", "/agents/99"} {
		if r := get(path); r.Status != 404 {
			t.Errorf("%s: status %d, want 404", path, r.Status)
		}
	}
	check("agent", get("/agents/1").Body, []string{"Omar Al Hashimi", "SAMPLE-50101", "MH-1001", "MH-1004", "MH-1007"}, []string{"MH-1009", "MH-1002"})
	check("contact", get("/contact").Body, []string{"Marsa Homes Real Estate Brokers LLC", "SAMPLE-12345", "Call me back", `action="/lead"`}, nil)
	check("list your property", get("/list-your-property").Body, []string{"List your property", "Value my property", "free valuation"}, []string{"Call me back"})

	// Leads: a viewing request and a question, without signing in.
	lead := func(form map[string]string) platform.TestResponse {
		t.Helper()
		return req(platform.TestRequestInput{Method: "POST", Path: "/lead", Form: form})
	}
	viewing := map[string]string{"kind": "viewing", "listing_id": "1", "name": "Fatima Al Ali", "phone": "+971 50 123 4567", "email": "Fatima@mail.ae",
		"message": "Is the price negotiable?", "date": "2099-01-15", "time": "17:30", "source": "/l/1", "lang": "en"}
	if r := lead(viewing); r.Status != 303 || r.Location != "/thanks?kind=viewing" {
		t.Fatalf("viewing lead: %+v\n%s", r, logText(r))
	}
	check("thanks", get("/thanks?kind=viewing").Body, []string{"Thank you", "We received your viewing request", "https://wa.me/971500000000"}, nil)
	// The same number written the local way, in Arabic, about another listing.
	if r := lead(map[string]string{"kind": "enquiry", "listing_id": "2", "name": "Fatima Al Ali", "phone": "050 123 4567", "email": "fatima@mail.ae", "message": "Are pets allowed?",
		"source": "/l/2", "lang": "ar"}); r.Location != "/thanks?kind=enquiry&lang=ar" {
		t.Fatalf("enquiry lead: %+v", r)
	}
	if r := lead(map[string]string{"kind": "valuation", "name": "Khalid", "phone": "+971521112233", "message": "3 bed villa in Arabian Ranches", "source": "/list-your-property"}); r.Location != "/thanks?kind=valuation" {
		t.Fatalf("valuation lead: %+v", r)
	}
	// Preview never sends email: the owner notices and the visitor's confirmation are in the log.
	logs, _ := p.Logs("marsa", logFilterAll())
	for _, want := range []string{
		`to the owner, "New viewing request: MH-1001, Fatima Al Ali, +971501234567"`,
		"Preferred: 2099-01-15 17:30",
		"https://marsa--preview.renoxium.test/l/1",
		`to the owner, "New enquiry: MH-1002, Fatima Al Ali, +971501234567"`,
		"Are pets allowed?",
		`to the owner, "New valuation request: Khalid, +971521112233"`,
		`not sent from preview: to fatima@mail.ae, "We received your request – Marsa Homes"`,
		`not sent from preview: to fatima@mail.ae, "استلمنا طلبك – مرسى هومز"`,
	} {
		if countMessages(logs, want) == 0 {
			t.Errorf("missing in log: %s", want)
		}
	}

	// Missing or wrong fields go back to the form with a message, and nothing is stored.
	bad := func(what, want string, change func(f map[string]string)) {
		t.Helper()
		f := map[string]string{}
		for k, v := range viewing {
			f[k] = v
		}
		change(f)
		if r := lead(f); r.Status != 303 || r.Location != want {
			t.Errorf("%s: %d %s, want %s", what, r.Status, r.Location, want)
		}
	}
	bad("no name", "/l/1?error=error_fields#lead", func(f map[string]string) { f["name"] = " " })
	bad("no phone", "/l/1?error=error_fields#lead", func(f map[string]string) { f["phone"] = "call me" })
	bad("long message", "/l/1?error=error_fields#lead", func(f map[string]string) { f["message"] = strings.Repeat("x", 2001) })
	bad("bad email", "/l/1?error=error_email#lead", func(f map[string]string) { f["email"] = "fatima" })
	bad("viewing without a date", "/l/1?error=error_date#lead", func(f map[string]string) { f["date"] = "" })
	bad("viewing in the past", "/l/1?error=error_date#lead", func(f map[string]string) { f["date"] = "2020-01-01" })
	bad("question without a message", "/l/1?error=error_fields#lead", func(f map[string]string) { f["kind"] = "enquiry"; f["message"] = "" })
	bad("listing without a permit number", "/l/1?error=error_fields#lead", func(f map[string]string) { f["listing_id"] = "9" })
	bad("arabic", "/l/1?error=error_fields&lang=ar#lead", func(f map[string]string) { f["name"] = ""; f["lang"] = "ar" })
	bad("source elsewhere", "/contact?error=error_fields#lead", func(f map[string]string) { f["name"] = ""; f["source"] = "https://evil.example/" })
	check("error message", get("/l/1?error=error_date").Body, []string{"Please choose a viewing date"}, nil)
	check("unknown error code", get("/l/1?error=error_nope").Body, nil, []string{`class="alert"`})
	// The hidden field that only robots fill in: thanked, not stored, the owner not told.
	bad("honeypot", "/thanks?kind=viewing", func(f map[string]string) { f["website"] = "https://spam.example"; f["name"] = "Robot" })

	// Three leads so far (the rejected ones and the robot's are not stored): the daily job counts them.
	job := func() platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "marsa", platform.TestRequestInput{Job: "daily_report"})
		if err != nil || r.Status != 200 {
			t.Fatalf("daily report: %v %+v", err, r)
		}
		return r
	}
	if r := job(); !strings.HasPrefix(r.Body, "3 new leads in the last 24 hours, 0 waiting over 24 hours, 1 listing without a permit number, ") {
		t.Fatalf("daily report: %s", r.Body)
	}
	logs, _ = p.Logs("marsa", logFilterAll())
	for _, want := range []string{`to the owner, "Leads: 3 new leads in the last 24 hours, 0 waiting over 24 hours, 1 listing without a permit number"`,
		"  viewing: 1", "  enquiry: 1", "  valuation: 1", "MH-1009 Full-floor penthouse", "Listings available for more than 60 days: ", "MH-1003 Furnished studio"} {
		if countMessages(logs, want) == 0 {
			t.Errorf("daily report lacks: %s", want)
		}
	}
	if anyLog(logs, "Robot") {
		t.Error("the honeypot lead reached the owner")
	}

	// At most five leads per phone in a day: two are stored, three more pass, the sixth is refused.
	for i := 0; i < 3; i++ {
		if r := lead(viewing); r.Status != 303 || r.Location != "/thanks?kind=viewing" {
			t.Fatalf("lead %d from the same phone: %+v", i+3, r)
		}
	}
	over := lead(viewing)
	if over.Status != 429 || !strings.Contains(over.Body, "contact us on WhatsApp") || !strings.Contains(over.Body, "https://wa.me/971500000000") {
		t.Fatalf("sixth lead from one phone: %d\n%s", over.Status, over.Body)
	}
	// The same person with another number but the same email is refused too; someone else is not.
	other := map[string]string{"kind": "callback", "name": "Fatima Al Ali", "phone": "+971559998877", "email": "fatima@mail.ae", "source": "/contact"}
	if r := lead(other); r.Status != 429 {
		t.Fatalf("same email, other phone: %d", r.Status)
	}
	other["email"] = ""
	if r := lead(other); r.Status != 303 || r.Location != "/thanks?kind=callback" {
		t.Fatalf("callback from another visitor: %+v", r)
	}
	if r := job(); !strings.HasPrefix(r.Body, "7 new leads in the last 24 hours, ") {
		t.Fatalf("daily report after the limit: %s", r.Body)
	}

	// The owner books viewings in the admin panel (on_admin_change); leads carry the listing reference for the calendar.
	admin := func(in platform.TestAdminInput) platform.TestResponse {
		t.Helper()
		r, err := p.TestRequest(ctx, "marsa", platform.TestRequestInput{Admin: &in})
		if err != nil || r.Status != 200 {
			t.Fatalf("admin %+v: %v %+v\n%s", in, err, r, logText(r))
		}
		return r
	}
	book := func(id int64, values map[string]any, want string) platform.TestResponse {
		t.Helper()
		r := admin(platform.TestAdminInput{Action: "update", Table: "leads", ID: id, Values: values, By: "owner@marsahomes.example"})
		if r.Body != want {
			t.Fatalf("lead %d %v: %q, want %q\n%s", id, values, r.Body, want, logText(r))
		}
		return r
	}
	// mailed checks the newest email written to the preview log (preview never sends email).
	// lastMail is the newest email written to the preview log (preview never sends email).
	lastMail := func() string {
		t.Helper()
		events, err := p.Logs("marsa", applog.Filter{Stage: "preview", Source: "email", Limit: 1})
		if err != nil || len(events) == 0 {
			t.Fatalf("no email in the log: %v", err)
		}
		return events[0].Message
	}
	mailed := func(_ platform.TestResponse, wants ...string) {
		t.Helper()
		m := lastMail()
		for _, w := range wants {
			if !strings.Contains(m, w) {
				t.Errorf("email lacks %q:\n%s", w, m)
			}
		}
	}
	r := book(1, map[string]any{"notes": "Called, wants the evening"}, "saved")
	if r.Admin.Row["listing_ref"] != "MH-1001" || r.Admin.Row["agent_id"] != int64(1) {
		t.Fatalf("lead from the listing page: %v", r.Admin.Row)
	}
	r = book(1, map[string]any{"status": "viewing_booked", "preferred_date": "2099-01-16", "preferred_time": "18:00"}, "viewing confirmation emailed to fatima@mail.ae")
	mailed(r, `not sent from preview: to fatima@mail.ae, "Viewing confirmed: MH-1001 on 2099-01-16 at 18:00 – Marsa Homes"`, "Hello Fatima Al Ali",
		"Two-bedroom apartment with full marina view", "Dubai Marina", "Time: 18:00", "Omar Al Hashimi\n+971500000001\nomar@marsahomes.example",
		"https://marsa--preview.renoxium.test/l/1")
	// Saving the booked lead again sends nothing; moving the viewing sends the new time.
	book(1, map[string]any{"notes": "Bring the parking card"}, "viewing unchanged")
	mailed(book(1, map[string]any{"preferred_time": "19:30"}, "new viewing time emailed to fatima@mail.ae"), `"Viewing confirmed: MH-1001 on 2099-01-16 at 19:30 – Marsa Homes"`)
	// Without a date there is no email; in Arabic for an Arabic lead; no email address, no email.
	book(2, map[string]any{"status": "viewing_booked"}, "no date: visitor not emailed")
	mailed(book(2, map[string]any{"preferred_date": "2099-02-01"}, "viewing confirmation emailed to fatima@mail.ae"),
		`"تأكيد المعاينة: MH-1002 في 2099-02-01 الساعة سيتم تأكيده – مرسى هومز"`, "مرحباً Fatima Al Ali", "/l/2?lang=ar")
	book(3, map[string]any{"status": "viewing_booked", "preferred_date": "2099-02-02", "preferred_time": "10:00"}, "no email address: call the visitor on +971521112233")
	book(3, map[string]any{"preferred_time": "5pm"}, "no date: visitor not emailed")
	// A lead the owner types in for a listing gets its reference and agent; a sold listing is never linked.
	r = admin(platform.TestAdminInput{Action: "insert", Table: "leads", Values: map[string]any{"listing_id": 10, "kind": "viewing", "name": "Walk-in",
		"phone": "+971500001111", "email": "walkin@mail.ae", "status": "viewing_booked", "preferred_date": "2099-03-01", "preferred_time": "11:00"}})
	if r.Body != "viewing confirmation emailed to walkin@mail.ae" || r.Admin.Row["listing_ref"] != "MH-1010" || r.Admin.Row["agent_id"] == nil {
		t.Fatalf("lead typed in by the owner: %s %v\n%s", r.Body, r.Admin.Row, logText(r))
	}
	if m := lastMail(); !strings.Contains(m, "to walkin@mail.ae") || !strings.Contains(m, "MH-1010") || strings.Contains(m, "/l/10") {
		t.Errorf("the email to the walk-in, without a link to the sold listing:\n%s", m)
	}
	if r := admin(platform.TestAdminInput{Action: "update", Table: "listings", ID: 1, Values: map[string]any{"status": "sold"}}); r.Body != "nothing to do" {
		t.Errorf("listing change: %s", r.Body)
	}
	if r := get("/l/1"); r.Status != 404 {
		t.Errorf("sold listing: %d", r.Status)
	}
}

func TestListingsUAETools(t *testing.T) {
	p := applyListings(t)
	ctx := context.Background()
	call := func(name string, args map[string]any) (string, bool) {
		t.Helper()
		r, err := p.TestRequest(ctx, "marsa", platform.TestRequestInput{Method: "POST", Path: "/mcp",
			JSON: map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}})
		if err != nil || r.Status != 200 {
			t.Fatalf("%s: %v %+v", name, err, r)
		}
		var rpc struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct{ Text string }
			}
		}
		json.Unmarshal([]byte(r.Body), &rpc)
		if len(rpc.Result.Content) == 0 {
			t.Fatalf("%s: %s", name, r.Body)
		}
		return rpc.Result.Content[0].Text, rpc.Result.IsError
	}
	ok := func(name string, args map[string]any) string {
		t.Helper()
		out, failed := call(name, args)
		if failed {
			t.Fatalf("%s failed: %s", name, out)
		}
		return out
	}
	out := ok("search_listings", map[string]any{"purpose": "sale", "community": "marina"})
	for _, want := range []string{`"reference":"MH-1001"`, `"price":2350000`, `"permit_number":"SAMPLE-7100000001"`, `"url":"https://marsa--preview.renoxium.test/l/1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("search lacks %s: %s", want, out)
		}
	}
	if strings.Contains(out, "MH-1009") || strings.Contains(out, "MH-1011") {
		t.Errorf("search shows a listing without a permit number or a rental: %s", out)
	}
	if out := ok("search_listings", nil); strings.Contains(out, "MH-1009") || strings.Contains(out, "MH-1010") || strings.Contains(out, "MH-1012") || !strings.Contains(out, "MH-1008") {
		t.Errorf("search without arguments: %s", out)
	}
	if out := ok("search_listings", map[string]any{"purpose": "rent", "bedrooms": 0, "max_price": 10000, "lang": "ar"}); !strings.Contains(out, `"reference":"MH-1003"`) ||
		!strings.Contains(out, `"rent_period":"monthly"`) || !strings.Contains(out, "استوديو مفروش") || strings.Contains(out, "MH-1002") {
		t.Errorf("studio search: %s", out)
	}
	if out := ok("get_listing", map[string]any{"id": 1}); !strings.Contains(out, `"permit_number":"SAMPLE-7100000001"`) || !strings.Contains(out, `"broker_number":"SAMPLE-50101"`) ||
		!strings.Contains(out, `"office_registration":{"label":"ORN","number":"SAMPLE-12345"}`) {
		t.Errorf("listing: %s", out)
	}
	for _, id := range []int{9, 10, 12, 999} {
		if out, failed := call("get_listing", map[string]any{"id": id}); !failed {
			t.Errorf("get_listing %d should fail: %s", id, out)
		}
	}
	if out := ok("agency_info", nil); !strings.Contains(out, `"name":"Marsa Homes"`) || !strings.Contains(out, `"number":"SAMPLE-12345"`) ||
		!strings.Contains(out, `"list_your_property_page":"https://marsa--preview.renoxium.test/list-your-property"`) || !strings.Contains(out, "Priya Nair") {
		t.Errorf("agency info: %s", out)
	}
	// With no leads the report still tells the owner about the listing that has no permit number.
	if r, err := p.TestRequest(ctx, "marsa", platform.TestRequestInput{Job: "daily_report"}); err != nil || r.Status != 200 ||
		!strings.HasPrefix(r.Body, "0 new leads in the last 24 hours, 0 waiting over 24 hours, 1 listing without a permit number, ") {
		t.Errorf("daily report without leads: %v %+v", err, r)
	}
}

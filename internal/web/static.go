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
	"store-uae":         phone("screen-store-home.webp", "The home page of the Al Noor Perfumes demo shop: Premium perfume collection, Oud, musk and amber from the UAE for every moment, a Discover the collection button, the numbers 8 products, 4 collections and prices from AED 60, and an oval photo of desert dunes at dusk in a gold ring"),
	"store-ar":          phone("screen-store-home-ar.webp", "The Al Noor Perfumes demo shop in Arabic: a search field and perfumes in rounded cards with Best seller and New labels, their collection and AED prices, laid out right to left"),
	"booking-uae":       phone("screen-booking.webp", "The home page of the Lamsa Beauty Lounge demo: Where calm meets care, Hair, nails and skincare in Jumeirah, every visit, a Book an appointment button, a sample rating, the numbers 6 services, 3 categories, 3 specialists and 2 classes, and a rounded photo of a calm spa bathroom"),
	"home-service-uae":  phone("screen-home-service.webp", "The home page of the Bayti Home Services demo: Cleaning, AC, handyman, Home care, done right, Book a visit and How it works buttons, a sample rating, the numbers 6 services, 5 areas served and 2 crews, and a rounded photo of a tidy living room"),
	"reservation-uae":   phone("screen-reservation.webp", "The home page of the Sahara Bay Tours and Rentals demo: a rounded photo card of camels on desert dunes with Dunes at sunset, the Marina by night, a See tours and rentals button, and a white search box for what, date and guests with a dark green Search button"),
	"quote-portal-uae":  phone("screen-quote-portal.webp", "The home page of the Bayan Business Setup demo in navy and gold: Your company in Dubai, set up clearly, with Ask for a quote and See services and prices buttons, a Licensed in the UAE badge and a photo of the Dubai skyline at night with a diagonal lower edge"),
	"listings-uae":      phone("screen-listing.webp", "The home page of the Marsa Homes demo: Homes in Dubai, known street by street, with Browse properties and List your property buttons, a photo of Downtown Dubai at sunset and a search bar for purpose, location, type and price"),
	"clinic-queue-uae":  phone("screen-clinic.webp", "The home page of the Shifa Family Clinic demo: See a doctor today, without the waiting room, a Take a number button, the numbers 8 waiting now, 10 seen today, 5 rooms open and 20 min average wait today, and an illustration of a waiting room with a screen calling A012 to Room 2"),
	"delivery-tracking": phone("screen-delivery.webp", "Delivery #1 of the Al Noor Flowers demo for Mariam: the courier's position on a map of Dubai and the progress Preparing, On the way, Delivered, with On the way reached"),
	"admin-calendar": {File: "screen-admin-calendar.webp", W: 1400, H: 920,
		Alt: "The owner's admin panel of the Lamsa Beauty Lounge demo: the bookings calendar for one day, one lane each for Layla, Maryam and Priya, with nine appointments from 10:00 to 19:00, each with the customer's name and service, marked confirmed or pending"},
}

// screensAR are the same demos in Arabic, for the Arabic home page.
var screensAR = map[string]screen{
	"store-uae":        phone("screen-store-home-ar.webp", "متجر عطور النور التجريبي بالعربية: فئات العطور بصور مقوّسة وحقل بحث وبطاقات عطور بأسعارها بالدرهم، من اليمين إلى اليسار"),
	"booking-uae":      phone("screen-booking-ar.webp", "الصفحة الرئيسية لتجربة صالون لمسة بيوتي لاونج بالعربية: عناية بالشعر والأظافر والبشرة في جميرا، مع زر احجزي موعداً"),
	"home-service-uae": phone("screen-home-service-ar.webp", "الصفحة الرئيسية لتجربة بيتي للخدمات المنزلية بالعربية: تنظيف وتكييف وصيانة، مع زر احجز زيارة وصورة لغرفة معيشة مرتبة"),
	"reservation-uae":  phone("screen-reservation-ar.webp", "الصفحة الرئيسية لتجربة صحارى باي للرحلات والتأجير بالعربية: رحلات سفاري وجولات بحرية وتأجير سيارات، مع صورة لقافلة جمال تعبر الكثبان"),
	"quote-portal-uae": phone("screen-quote-portal-ar.webp", "الصفحة الرئيسية لتجربة بيان لتأسيس الأعمال بالعربية: شركتك في دبي بتأسيس واضح، مع زر اطلب عرض سعر وصورة لأفق دبي ليلاً"),
	"clinic-queue-uae": phone("screen-clinic-ar.webp", "الصفحة الرئيسية لتجربة عيادة شفاء العائلية بالعربية: قابل الطبيب اليوم دون الجلوس في قاعة الانتظار، مع زر احصل على رقم وعدد المنتظرين والغرف المفتوحة"),
	"listings-uae":     phone("screen-listing-ar.webp", "الصفحة الرئيسية لتجربة مرسى هومز بالعربية: منازل في دبي نعرفها شارعاً شارعاً، مع زر تصفح العقارات وصورة لوسط دبي عند الغروب"),
	"admin-calendar": {File: "screen-admin-calendar.webp", W: 1400, H: 920,
		Alt: "لوحة تحكم صالون لمسة التجريبي: تقويم الحجوزات ليوم واحد، بعمود لكل من ليلى ومريم وبريا، وتسعة مواعيد من 10:00 إلى 19:00 مع اسم العميلة والخدمة وحالة الحجز"},
}

// demoNameAR names each demo on the Arabic home page.
var demoNameAR = map[string]string{
	"store-uae":        "تجربة متجر العطور",
	"booking-uae":      "تجربة الصالون",
	"home-service-uae": "تجربة تنظيف المنازل",
	"reservation-uae":  "تجربة رحلات السفاري",
	"quote-portal-uae": "تجربة تأسيس الشركات",
	"listings-uae":     "تجربة الوسيط العقاري",
	"clinic-queue-uae": "تجربة طابور العيادة",
}

// screensWide are the demos' home pages at desktop size (960x600), for the
// template cards in the console.
var screensWide = map[string]screen{
	"store-uae":         wide("wide-store-home.webp", "The Al Noor Perfumes demo shop on a desktop"),
	"booking-uae":       wide("wide-booking.webp", "The Lamsa Beauty Lounge demo on a desktop"),
	"home-service-uae":  wide("wide-home-service.webp", "The Bayti Home Services demo on a desktop"),
	"reservation-uae":   wide("wide-reservation.webp", "The Sahara Bay Tours and Rentals demo on a desktop"),
	"quote-portal-uae":  wide("wide-quote-portal.webp", "The Bayan Business Setup demo on a desktop"),
	"listings-uae":      wide("wide-listing.webp", "The Marsa Homes demo on a desktop"),
	"clinic-queue-uae":  wide("wide-clinic.webp", "The Shifa Family Clinic queue demo on a desktop"),
	"delivery-tracking": wide("wide-delivery.webp", "The Al Noor Flowers delivery tracking demo on a desktop"),
}

func wide(file, alt string) screen { return screen{File: file, Alt: alt, W: 960, H: 600} }

// exampleJob is what the owner does with each template, as the landing
// page and the gallery group them, in their words.
var exampleJob = map[string]string{
	"store-uae":         "I sell products",
	"booking-uae":       "I take bookings",
	"home-service-uae":  "I take bookings",
	"reservation-uae":   "I take bookings",
	"quote-portal-uae":  "I send quotes",
	"listings-uae":      "I send quotes",
	"clinic-queue-uae":  "I serve walk-ins",
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
	"clinic-queue-uae": "Clinic queue demo",
}

// sitePath is one of the three ways in on the landing page: what the owner
// does, and the demos that show it.
type sitePath struct {
	ID, Title, Text string
	Screen          screen
	Demos           []demoLink
}

type demoLink struct{ Label, URL string }

func (s *Site) sitePaths(cards []galleryCard, ar bool) []sitePath {
	byName := map[string]galleryCard{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	pick := func(names ...string) []demoLink {
		var out []demoLink
		for _, n := range names {
			if c, ok := byName[n]; ok {
				if ar {
					out = append(out, demoLink{Label: demoNameAR[n], URL: c.DemoURL + "/?lang=ar"})
					continue
				}
				out = append(out, demoLink{Label: demoName[n], URL: c.DemoURL + "/"})
			}
		}
		return out
	}
	if ar {
		return []sitePath{
			{ID: "sell", Title: "أبيع منتجات", Screen: screensAR["store-uae"], Demos: pick("store-uae"),
				Text: "متجر بسلة مشتريات وصفحة دفع، لمتجر عطور أو بوتيك أو كعك يُطلب قبل أيام. يدفع العملاء بالبطاقة أو نقداً عند الاستلام."},
			{ID: "book", Title: "أستقبل حجوزات", Screen: screensAR["booking-uae"], Demos: pick("booking-uae", "home-service-uae", "reservation-uae"),
				Text: "يختار العملاء وقتاً متاحاً ويدفعون عربوناً، لصالون أو شركة تنظيف أو رحلة سفاري."},
			{ID: "quote", Title: "أرسل عروض أسعار", Screen: screensAR["quote-portal-uae"], Demos: pick("quote-portal-uae", "listings-uae"),
				Text: "يطلب العملاء عرض سعر ويقبلونه ويدفعون العربون عبر الإنترنت، لمكتب تأسيس شركات أو وسيط عقاري."},
		}
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
	{ID: "clinics", Tab: "Clinics and walk-ins", Example: "clinic-queue-uae",
		Customers: []string{"Take a number on their phone from a QR code, or at the reception tablet", "See how many are ahead and the estimated wait, live", "Get one WhatsApp message when two patients are ahead", "Follow their place in English or Arabic"},
		Owner:     []string{"A waiting-room screen for your TV that calls numbers to rooms", "Call next, call again or mark a no-show from a staff page", "The queue starts empty every morning", "Patients per day and the average wait, by email every night"}},
}

// tradesAR are the same kinds of business in Arabic, in the same order.
var tradesAR = []trade{
	{ID: "salon", Tab: "الصالونات والسبا", Example: "booking-uae",
		Customers: []string{"اختيار الخدمة والمصففة والوقت المتاح", "دفع عربون بالبطاقة، أو الدفع في الصالون", "الانضمام إلى حصة بمقاعد محدودة، تُدفع لكل فصل أو شهرياً", "عند الإلغاء في الوقت المحدد يُستردّ العربون فوراً"},
		Owner:     []string{"تقويم يومي بعمود لكل مصففة", "لا حجوزات مزدوجة: الأوقات المتاحة تُحسب من ساعات عملك وحجوزات كل مصففة", "جدول الغد يصلك بالبريد الإلكتروني كل مساء", "عند نقل حجز أو إلغائه يصل العميل بريد إلكتروني"}},
	{ID: "shop", Tab: "المتاجر", Example: "store-uae",
		Customers: []string{"التصفح حسب الفئة مع البحث والتصفية", "اختيار مقاس أو خيار له سعره ومخزونه", "الدفع بالبطاقة أو نقداً عند الاستلام، مع رموز الخصم", "اختيار تاريخ التوصيل للكعك أو الزهور المصنوعة حسب الطلب"},
		Owner:     []string{"كل طلب مع حالته في لوحتك", "رسوم توصيل، وتوصيل مجاني فوق مبلغ تحدده", "مخزون لكل خيار من خيارات المنتج", "فواتير ضريبية بعد إضافة رقم التسجيل الضريبي"}},
	{ID: "home", Tab: "الخدمات المنزلية", Example: "home-service-uae",
		Customers: []string{"اختيار الخدمة والمنطقة وفترة الوصول", "إضافة صورة للمشكلة", "حجز زيارات أسبوعية أو كل أسبوعين تُدفع بالبطاقة تلقائياً", "متابعة الفريق على الخريطة يوم الزيارة"},
		Owner:     []string{"عدد محدد من الفرق لكل فترة، فلا تتجاوز الحجوزات طاقتك", "صفحة على الهاتف لكل فريق: في الطريق، وصل، اكتمل، استُلم النقد", "رسوم إضافية حسب المنطقة تُضاف إلى السعر", "الإلغاء قبل المهلة التي تحددها يُستردّ مبلغه تلقائياً"}},
	{ID: "tours", Tab: "الجولات والتأجير", Example: "reservation-uae",
		Customers: []string{"حجز مقاعد في رحلة بتاريخ محدد، بسعر للبالغين وسعر للأطفال", "استئجار سيارة أو معدات باليوم، مع سعر أسبوعي", "إضافات مثل مقعد طفل أو التوصيل", "قسيمة تُعرض يوم الرحلة"},
		Owner:     []string{"المقاعد والمخزون تُحسب من الحجوزات، فلا بيع زائد", "عربون عبر الإنترنت والباقي يوم الرحلة", "مبلغ تأمين كحجز على البطاقة تحرّره أو تخصم منه عند الإرجاع", "رحلات الغد وعمليات التسليم والإرجاع تصلك بالبريد الإلكتروني"}},
	{ID: "property", Tab: "الوسطاء العقاريون", Example: "listings-uae",
		Customers: []string{"البحث حسب البيع أو الإيجار والمنطقة وعدد الغرف والسعر", "رقم التصريح ورمز QR ظاهران في كل إعلان", "طلب معاينة أو طرح سؤال دون تسجيل الدخول", "الاتصال بالوكيل أو مراسلته على واتساب من صفحة الإعلان"},
		Owner:     []string{"العملاء المحتملون في لوحتك مع حالة المتابعة", "المعاينات في تقويم بعمود لكل وكيل", "الإعلان الذي لا يحمل رقم تصريح يبقى مخفياً", "ملخص يومي بالبريد الإلكتروني"}},
	{ID: "services", Tab: "خدمات الأعمال", Example: "quote-portal-uae",
		Customers: []string{"إرسال استفسار مع التاريخ المطلوب والميزانية", "قراءة عرض السعر وقبوله عبر الإنترنت", "دفع العربون والباقي بالبطاقة أو بالتحويل البنكي", "متابعة سير العمل ومشاركة المستندات في بوابة خاصة"},
		Owner:     []string{"كتابة عروض الأسعار في لوحتك، مع إضافة ضريبة 5% أو شمولها", "سجل مدفوعات لكل عرض سعر", "يصل العميل بريد إلكتروني عند تحديث عرض السعر", "استرداد مدفوعات البطاقة عند إلغاء العرض، إن اخترت ذلك"}},
	{ID: "clinics", Tab: "العيادات والزوار بلا موعد", Example: "clinic-queue-uae",
		Customers: []string{"أخذ رقم من الهاتف عبر رمز QR أو من جهاز الاستقبال", "معرفة عدد من قبلهم والانتظار المتوقع مباشرة", "رسالة واحدة على واتساب عندما يكون قبلهم مريضان", "متابعة الدور بالعربية أو الإنجليزية"},
		Owner:     []string{"شاشة لقاعة الانتظار على التلفاز تنادي الأرقام إلى الغرف", "نداء التالي أو إعادة النداء أو تسجيل الغياب من صفحة الموظفين", "الطابور يبدأ فارغاً كل صباح", "عدد المرضى في اليوم ومتوسط الانتظار بالبريد كل ليلة"}},
}

// siteTrades fills the trades with their demos; a trade whose demo is not
// in the gallery is left out.
func (s *Site) siteTrades(cards []galleryCard, ar bool) []trade {
	byName := map[string]galleryCard{}
	for _, c := range cards {
		byName[c.Name] = c
	}
	var out []trade
	list, shots, query := trades, screens, ""
	if ar {
		list, shots, query = tradesAR, screensAR, "?lang=ar"
	}
	for _, t := range list {
		c, ok := byName[t.Example]
		if !ok {
			continue
		}
		t.Screen, t.DemoURL, t.AdminURL = shots[t.Example], c.DemoURL+"/"+query, c.AdminURL
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

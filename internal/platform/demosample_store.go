package platform

func init() {
	demoSamples["store-uae"] = map[string][]map[string]any{"testimonials": {
		{"quote": "The Royal Oud lasts from the morning until I am home again. I ordered on Sunday and it was at my door on Tuesday.",
			"quote_ar": "العود الملكي يدوم من الصباح حتى أعود إلى البيت. طلبته يوم الأحد ووصلني يوم الثلاثاء.",
			"name":     "Hessa K. (sample)"},
		{"quote": "I sent the gift set to my mother in Al Ain. It arrived wrapped, with the invoice by email, exactly as promised.",
			"quote_ar": "أرسلت طقم الهدايا إلى والدتي في العين. وصل مغلّفاً والفاتورة بالبريد الإلكتروني، تماماً كما وُعدت.",
			"name":     "Omar S. (sample)"},
		{"quote": "Paying cash on delivery made the first order easy. The white musk is soft and clean, I have reordered twice.",
			"quote_ar": "الدفع عند الاستلام جعل الطلب الأول سهلاً. المسك الأبيض ناعم ونظيف، وأعدت طلبه مرتين.",
			"name":     "Priya N. (sample)"},
	}}
}

// Four more products for the demo only, so its home page shows two rows of
// products and the new arrivals; an owner's shop starts with the template's four.
func init() {
	demoExtras["store-uae"] = map[string]demoExtra{"products": {key: "name", rows: []map[string]any{
		{"name": "Bakhoor Al Noor", "name_ar": "بخور النور", "description": "Oud wood chips soaked in rose and amber oils, for the mabkhara. 40 g jar.", "description_ar": "رقائق عود مشبعة بزيوت الورد والعنبر، للمبخرة. عبوة 40 غ.", "category": "Oud", "category_ar": "عود", "price": 85, "stock": 40, "active": 1},
		{"name": "Musk Tahara Oil", "name_ar": "زيت مسك الطهارة", "description": "Alcohol-free white musk oil with a roll-on. 12 ml.", "description_ar": "زيت مسك أبيض خالٍ من الكحول مع كرة دوارة. 12 مل.", "category": "Musk", "category_ar": "مسك", "price": 60, "stock": 35, "active": 1},
		{"name": "Amber Oud Intense", "name_ar": "عنبر وعود إنتنس", "description": "Deep amber over smoky oud. 75 ml eau de parfum.", "description_ar": "عنبر عميق فوق عود دخاني. 75 مل.", "category": "Amber", "category_ar": "عنبر", "price": 210, "stock": 4, "active": 1, "badge": "new"},
		{"name": "Discovery Set", "name_ar": "طقم التجربة", "description": "Five 2 ml samples of our oud, musk and amber scents in a gift box.", "description_ar": "خمس عينات 2 مل من عطور العود والمسك والعنبر في علبة هدية.", "category": "Gifts", "category_ar": "هدايا", "price": 75, "stock": 25, "active": 1, "badge": "new"},
	}, photos: map[string]map[string]string{
		"Bakhoor Al Noor":   {"image": "store-story.webp"},
		"Musk Tahara Oil":   {"image": "store-oud-gift-set.webp"},
		"Amber Oud Intense": {"image": "store-royal-oud.webp"},
		"Discovery Set":     {"image": "store-white-musk.webp"},
	}}}
}

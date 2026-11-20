package platform

func init() {
	demoSamples["booking-uae"] = map[string][]map[string]any{"testimonials": {
		{"quote": "Nobody rushed me. Layla listened first, and the cut still falls into place weeks later.",
			"quote_ar": "لم يستعجلني أحد. استمعت ليلى أولاً، وما زالت القصة تنسدل كما يجب بعد أسابيع.",
			"name":     "Hessa A. (sample)", "area": "Haircut & Blow-dry", "area_ar": "قص الشعر مع سشوار"},
		{"quote": "I booked a gel manicure online at midnight and paid the deposit by card. The time was kept for me to the minute.",
			"quote_ar": "حجزت مانيكير جل عبر الإنترنت عند منتصف الليل ودفعت العربون بالبطاقة، وكان موعدي محفوظاً بالدقيقة.",
			"name":     "Farah K. (sample)", "area": "Gel Manicure & Pedicure", "area_ar": "مانيكير وباديكير جل"},
		{"quote": "The Moroccan bath is the calmest hour of my month. A quiet villa, and only one guest at a time.",
			"quote_ar": "الحمام المغربي أهدأ ساعة في شهري. فيلا هادئة وضيفة واحدة في كل مرة.",
			"name":     "Anita R. (sample)", "area": "Moroccan Bath", "area_ar": "حمام مغربي"},
	}}
	demoSettings["booking-uae"] = map[string]map[string]any{"business": {"rating": 4.9, "review_count": 3}}
}

package platform

func init() {
	demoSamples["home-service-uae"] = map[string][]map[string]any{"testimonials": {
		{"quote": "The crew arrived inside the two-hour window, worked through the whole flat and left the kitchen spotless.",
			"quote_ar": "وصل الفريق خلال موعد الساعتين، ونظّف الشقة كلها وترك المطبخ لامعاً.",
			"name":     "Hana K. (sample)", "area": "Dubai Marina", "area_ar": "دبي مارينا"},
		{"quote": "Booked the AC service at night, paid by card, and the technician cleaned all three units the next morning.",
			"quote_ar": "حجزت صيانة المكيفات ليلاً ودفعت بالبطاقة، ونظّف الفني المكيفات الثلاثة في صباح اليوم التالي.",
			"name":     "Omar S. (sample)", "area": "JLT", "area_ar": "أبراج بحيرات جميرا"},
		{"quote": "The price online was the price I paid, area surcharge included. The receipt was on my booking page the same day.",
			"quote_ar": "السعر المعلن هو ما دفعته، شاملاً رسوم المنطقة. والإيصال كان في صفحة حجزي في اليوم نفسه.",
			"name":     "Priya N. (sample)", "area": "Arabian Ranches", "area_ar": "المرابع العربية"},
	}}
	demoSettings["home-service-uae"] = map[string]map[string]any{"business": {"rating": 4.8, "review_count": 3}}
}

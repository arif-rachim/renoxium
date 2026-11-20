package platform

func init() {
	demoSamples["quote-portal-uae"] = map[string][]map[string]any{"testimonials": {
		{"quote": "The quote listed every government fee before we paid a dirham, and the licence arrived in the second week as promised.",
			"quote_ar": "ذكر عرض السعر كل رسم حكومي قبل أن ندفع درهماً واحداً، ووصلت الرخصة في الأسبوع الثاني كما وُعدنا.",
			"name":     "Daniel R. (sample)", "company": "Founder, trading company", "company_ar": "مؤسس شركة تجارية"},
		{"quote": "I followed each step of my Golden Visa in the portal and paid the balance only when it was issued.",
			"quote_ar": "تابعت كل خطوة من تأشيرتي الذهبية في البوابة ودفعت الباقي عند صدورها فقط.",
			"name":     "Leila H. (sample)", "company": "Consultant", "company_ar": "مستشارة"},
		{"quote": "VAT registration was done in a week. One written quote, one receipt per payment, no surprises.",
			"quote_ar": "أُنجز التسجيل الضريبي في أسبوع. عرض سعر مكتوب واحد وإيصال لكل دفعة، بلا مفاجآت.",
			"name":     "Imran K. (sample)", "company": "Owner, design studio", "company_ar": "صاحب استوديو تصميم"},
	}}
	demoSettings["quote-portal-uae"] = map[string]map[string]any{"business": {"years_in_business": 9}}
}

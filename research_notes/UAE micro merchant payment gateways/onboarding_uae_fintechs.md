# Merchant onboarding documentation requirements of UAE-native payment-link / payment-gateway fintechs (state as of November 2026)

All URLs accessed 2026-11-22. Evidence grades used below:
- **[FETCHED]** = I opened the provider's page and the wording is quoted from it.
- **[SNIPPET]** = wording came from a search-result extract of the provider's page; the page itself returned HTTP 403 or was not opened. Treat as probable, not confirmed.
- **[SECONDARY]** = press / third party.
- **[INFERRED]** = my inference, no direct source.

Pages that blocked direct access (HTTP 403): help.mamopay.com (all articles), hub.zbooni.com, magnati.com. botim.money/faqs redirects to an index page with no merchant content.

Provisional leniency ranking (most lenient first) for an individual with no trade license — see the Inferences blocks for reasoning:
1. Ziina (Personal / Professional) — no license, fixed AED 50,000/month receive cap
2. Nomod (Individual account) — no license, freelance permit optional; no published cap found
3. Mamo (freelancer route) — no license per help centre [SNIPPET]; Emirates ID mandatory
4. PayTabs (freelancer route) — needs a freelancer certificate (not license-free)
5. Stripe UAE — needs trade license OR freelancer permit
6. Zbooni — "business legal documents" required; no stated individual exemption
7. Botim Money / PayBy — trade license required even at simplified tier (AED 6,000/day)
8. Tap, Telr, Magnati, Network International, Paymob, Foloosi — trade license / commercial registration required (Paymob and Foloosi: no document list found)
9. Careem Pay, e& money, Payit, Aani — not self-serve merchant acquiring products for unlicensed individuals; Aani is reachable only via one's bank/PSP

## 1. Account types offered and documents required for each

### Takeaway
Only Ziina, Nomod and (per help-centre extracts) Mamo publish an explicit route for an individual with no trade license; PayTabs and Stripe accept a freelance permit in place of a trade license; every other provider's published checklist starts with a trade license / commercial registration. Emirates ID is universal; a bank proof showing IBAN and account name is near-universal.

### Cited Findings

**Ziina**
- Three tiers: Personal, Professional, verified Business. Professional: "no Trade License is required to get started"; available immediately. [FETCHED] — [Ziina help centre](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- Verified Business requires "submitting your Trade license". [FETCHED] — [Ziina help centre](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- Business verification: "You'll need a valid trade license, and the app will show you exactly which other documents we need for your business." Steps: profile icon > "Become Verified" banner > upload. [FETCHED] — [Ziina help (Intercom)](https://intercom.help/ziina-8bdf318a7612/en/articles/12607091-what-are-the-document-requirements-and-steps-for-business-account-verification-and-activation-on-ziina)
- Ziina puts licensing responsibility on the user: customers must "ensure they hold any trade license, permit, tax registration, or other approval" required for their business; payments accepted only after completing "the verification checks". (Article dated 9 July 2026.) [FETCHED] — [Ziina blog](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)

**Nomod** (added: not on the original list but clearly relevant)
- Two account types. Business: "Registered businesses with a valid trade license and a business bank account." Individual: "Freelancers with freelancer permits, consultants, or anyone selling without a trade license." [FETCHED] — [Nomod docs](https://nomod.com/docs/account/set-up-account)
- Business documents: Emirates ID, trade license, proof of address, business bank account details, shareholder document or Power of Attorney in certain cases. Individual documents: Emirates ID, proof of address, bank account details, freelance permit if you have one. [FETCHED] — [Nomod docs](https://nomod.com/docs/account/set-up-account)

**Mamo (Mamo Business)** — help centre blocked; all Mamo items are [SNIPPET]
- Trade license "is not required if you are a freelancer, i.e. you do not yet have a registered business." [SNIPPET] — [Mamo help: Business documents](https://help.mamopay.com/en/articles/9932398-business-documents)
- "If you are a freelancer: You must submit a copy of your Emirates ID, copy of passport will not be accepted in these cases." [SNIPPET] — [Mamo help: Business owners](https://help.mamopay.com/en/articles/9932463-business-owners)
- Proof of address is required: "The goal here is to establish that you and your business physically exist and operate in the UAE." [SNIPPET] — [Mamo help: Business documents](https://help.mamopay.com/en/articles/9932398-business-documents)
- Verification failures listed include "Missing identification ... KYC documents like Emirates ID for you or the business owners" and missing "Trade License or Proof of Address". [SNIPPET] — [Mamo help: Why some businesses fail verification](https://help.mamopay.com/en/articles/9932467-why-some-businesses-fail-verification)

**PayTabs**
- Freelancer route for UAE. KYC list: "Freelancer certificate"; "Passport, Emirates ID and residence permit or a VISA copy for identification purposes"; "Account details through bank account statement or any other document". Posted 4 Sep 2023, updated 18 Sep 2025. [FETCHED] — [PayTabs](https://ai.paytabs.com/en/get-onboarded-with-your-freelancer-certificate/)
- Standard onboarding requires listing all owners with shareholding percentages as shown on the trade licence and uploading documents. [SNIPPET] — [PayTabs support](https://support.paytabs.com/en/support/solutions/articles/60000716345-merchant-s-onboarding-journey)

**Stripe UAE** (added as benchmark; not UAE-native)
- "Trade license or freelancer permit issued within the UAE" is required for sole establishments, free zone establishments, LLCs and branches. [FETCHED] — [Stripe support](https://support.stripe.com/questions/uae-account-activation-requirements)
- Stripe "cannot support users who do not have a trade license/permit". [SNIPPET] — [Stripe support](https://support.stripe.com/questions/uae-business-verification-requirements)

**Zbooni** — help centre blocked; [SNIPPET]
- Verification requires a copy of a valid passport or national ID, a bank account in your name, and business legal documents which "could include a trade license, commercial registration, or other relevant legal documents" depending on business type. A trade license is described as required to verify the business is legally registered. [SNIPPET] — [Zbooni Account Activation FAQs](https://hub.zbooni.com/hc/en-us/articles/20286395081629-Account-Activation-FAQs)
- A free-zone blog states a business license is needed to activate a Zbooni account. [SECONDARY, promotional] — [Meydan Free Zone](https://www.meydanfz.ae/blog/how-to-sell-on-zbooni)

**Botim Money (formerly PayBy)**
- Simplified Due Diligence documents: "Trade License," "Emirates ID of all Partners," "Memorandum/Articles of Association," "Merchant Onboarding Form." Enhanced Due Diligence adds "Tenancy Contract," "3-month company bank statements," "VAT Certificate." [FETCHED] — [botim money pricing](https://www.botim.money/pricing)
- "MOA/AOA (Exempted for freelancers and other identified categories)". [SNIPPET] — [botim money FAQs](https://www.botim.money/faqs)
- PayBy support: "New registration process will only require the following: 1. Trade Licence 2. Emirates ID of Owner and Authorized Signatory". [SNIPPET] — [PayBy support](https://support.payby.com/en/)

**Telr**
- Sole proprietorships: "Trade License."; "A copy of the passport and visa of the owner."; "Share certificate / Local Service Agent contract / MOA."; bank proof "3 months' bank statement, or online company bank statement, or a letter from your bank for a new bank account (must contain IBAN, account number, account name, bank name, bank branch, SWIFT code)." Partnerships/corporations: trade license (all pages), passport and visa of all partners, "MOA and all amendments", same bank proof. No license-free option listed. [FETCHED] — [Telr documents checklist](https://telr.com/documents-checklist)

**Tap Payments**
- Business documents: business license or commercial registration, authorised signatory's name, list of owners/shareholders (examples: commercial license, CR, MOA, VAT certificate, proof of company address). National ID front and back for signatory and anyone owning 20% or more; passport for foreign individuals; residential addresses of UBOs and signatory. Live online presence "must be live and include" product descriptions, pricing, About Us, T&Cs, refund policy, privacy policy, checkout page. "In some cases, additional documents are required based on the ownership type of the business and/or activity." Page is not UAE-specific and has no freelancer rule. [FETCHED] — [Tap support](https://support.tap.company/en/support/solutions/articles/153000242639)

**Magnati** — site blocked; [SNIPPET]
- Digital merchant onboarding requires Emirates ID (passport for non-residents), a valid trade license, proof of account (cancelled cheque, account statement, welcome letter, etc.), and where applicable power of attorney / constitutional documents; free-zone merchants also need an incumbency certificate. [SNIPPET] — [Magnati Digital Merchant Onboarding FAQs](https://www.magnati.com/?p=37341)

**Network International (N-Genius)** — [SNIPPET]
- Business payments page lists valid trade license, passports and Emirates IDs for shareholders with 25%+ ownership, controlling owners and authorised signatories, constitutional documents, IBAN proof and VAT certificate. [SNIPPET] — [Network International](https://network.ae/en/processing-solutions/business-payments)
- DET-licensed Dubai businesses: DET trade license, authorised signatory and partner IDs, company bank account details, signed application form. [SNIPPET] — [Network x DET](https://www.network.ae/en/contact-us/det-network)
- A merchant self-onboarding page exists. [SNIPPET, contents not read] — [Network self-onboarding](https://www.network.ae/en/contact-us/merchant-self-onboarding)

**Aani (Al Etihad Payments)**
- Small merchants and sole proprietors are enrolled by their licensed institution: licensed institutions "need to enroll small merchants and sole proprietors on Aani, after obtaining their consent." Document requirements are not specified; users are directed to their bank or PSP. [FETCHED] — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)

**Careem Pay**
- As an acceptance method through Tap, "Careem Pay is only available for corporates in UAE" (Tap FAQ dated Feb 2024). [SNIPPET, dated] — [Tap Freshdesk](https://tappayments.freshdesk.com/en/support/solutions/articles/153000140158-careem-pay-faqs)

### Inferences
- [INFERRED] Ziina's Professional account and Nomod's Individual account are the only two clearly documented products where an Emirates-ID-holding resident with no license and no permit can accept card payments as a seller. Mamo probably belongs in the same group but I could not open its pages.
- [INFERRED] Mamo's wording "freelancer, i.e. you do not yet have a registered business" suggests "freelancer" there means "unregistered individual", not "holder of a freelance permit" — so a permit is likely optional. Unconfirmed.
- [INFERRED] One search extract attributed an "individual setup: Emirates ID, Proof of address, Bank account information, Freelance Permit (if available)" list to Mamo; the wording is identical to Nomod's page, so it may be a misattribution. I have recorded it under Nomod only.
- [INFERRED] Botim's freelancer MOA exemption implies freelance-permit holders are accepted, but still with a license/permit document; it does not indicate an Emirates-ID-only route.
- [INFERRED] Zbooni's "depending on your business type" leaves room for individuals but nothing published confirms it; treat as license-required.

### Gaps
- Paymob UAE: no official document checklist found (searches returned only licensing press).
- Foloosi: no official document checklist found.
- e& money, Payit (FAB), Klip: no merchant-onboarding material found; I could not confirm whether any of them offers a merchant product to individuals. Payit returned no results at all.
- YallaPay: not researched (no results surfaced).
- Careem Pay: no current (2026) merchant-acceptance terms found; the only source is a 2024 Tap FAQ.
- Website / social-media link requirement: confirmed only for Tap (full live website). Not confirmed for Ziina, Mamo, Nomod, Zbooni.
- UBO threshold details found only for Tap (20%) and Network / Stripe (25%).

## 2. Is a trade license mandatory? Limits for unlicensed individuals and upgrade triggers

### Takeaway
Ziina is the only provider with a clearly published cap for unlicensed users: AED 50,000 per card transaction and AED 50,000 received per calendar month, fixed; a trade license lifts this to AED 90,000 per transaction and AED 2,000,000 per month. Botim publishes AED 6,000/day at its simplified tier but that tier still needs a trade license.

### Cited Findings
- Ziina Personal: "Card payments received: up to 50,000 AED per transaction"; "Total Volume Received in a Calendar Month: 50,000 AED"; "Minimum transfer amount: 2 AED". Professional has the same limits. [FETCHED] — [Ziina help centre](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- "Personal accounts have fixed limits that can't be increased." A Business account is recommended if you regularly receive more than 50,000 AED a month in card payments. [FETCHED] — [Ziina help centre](https://ziina.com/help-center/6857582-what-are-the-transaction-limits-for-my-personal-account)
- Ziina verified Business: "up to 90,000 AED per transaction and up to 2,000,000 AED received per calendar month", plus multi-user access and a Verified Badge; applying carries a "1,000 AED fee waiver bonus". [FETCHED] — [Ziina help centre](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- Conflicting Ziina figures: search extracts of other Ziina pages mention a weekly 1,500 AED card limit and a 3,600 AED per-transaction limit, and an older page says verified business accounts have "no limits". [SNIPPET] — [Ziina card limits article](https://ziina.com/help-center/15136431-whats-the-difference-between-the-1-500-aed-and-20-000-aed-limits); a third-party summary dated Oct 2026 reports AED 1,500 card payments per week, AED 25,000 sent and AED 50,000 received per month [SECONDARY] — [Freedom251](https://freedom251.com/apps/ziina/)
- Botim Money simplified due diligence: "Accept up to AED 6,000 per day."; enhanced: "Daily payment limit based on risk profile." [FETCHED] — [botim money pricing](https://www.botim.money/pricing)
- Botim: for higher limits the merchant is monitored for one to two months and given AED 10,000, with additional documents required. [SNIPPET] — [botim money FAQs](https://www.botim.money/faqs)
- Nomod: Individual account needs no trade license; no limits for individuals are stated on the setup page. [FETCHED] — [Nomod docs](https://nomod.com/docs/account/set-up-account)
- Mamo: trade license not required for freelancers. [SNIPPET] — [Mamo help](https://help.mamopay.com/en/articles/9932398-business-documents)
- Trade license mandatory per published checklists: Telr [FETCHED] — [Telr](https://telr.com/documents-checklist); Tap (license or commercial registration) [FETCHED] — [Tap support](https://support.tap.company/en/support/solutions/articles/153000242639); Magnati [SNIPPET] — [Magnati](https://www.magnati.com/?p=37341); Network International [SNIPPET] — [Network](https://network.ae/en/processing-solutions/business-payments); Botim Money [FETCHED] — [botim money pricing](https://www.botim.money/pricing)
- Freelance permit accepted instead of trade license: PayTabs [FETCHED] — [PayTabs](https://ai.paytabs.com/en/get-onboarded-with-your-freelancer-certificate/); Stripe [FETCHED] — [Stripe](https://support.stripe.com/questions/uae-account-activation-requirements)
- Aani: "Aani will support payments of up to 50,000 AED at the initial stage." [FETCHED] — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/); Mashreq Biz: collect "up to AED 50,000 per transaction" via QR or merchant tag [SNIPPET] — [Mashreq](https://www.mashreq.com/en/uae/business/banking-solutions/forex-transfer/aani/)
- Legal context: a trade licence is required to run any kind of business in the UAE, with no exceptions for size or turnover. [SECONDARY] — [The National, 29 Dec 2025](https://www.thenationalnews.com/business/money/2025/12/29/do-i-need-to-register-a-business-for-home-based-work)

### Inferences
- [INFERRED] The weekly 1,500 AED / 3,600 AED Ziina figures most likely relate to the Ziina Card (spending) or to an older regime rather than to receiving limits; the two pages I opened directly both say AED 50,000. The report writer should present AED 50,000/month as current and flag the discrepancy.
- [INFERRED] Ziina's upgrade trigger is effectively volume: once a seller needs more than AED 50,000 per month, the only path is trade-license verification. No evidence was found of Ziina proactively demanding a license below the cap, but its terms shift licensing responsibility to the user.
- [INFERRED] Ziina's policy appears to have changed over time: an older help page described personal accounts with single-transaction limits up to AED 90,000, whereas the current page fixes AED 50,000 and adds a "Professional" tier between Personal and Business.
- [INFERRED] A provider tolerating unlicensed sellers does not make the selling activity itself lawful; the report should separate "provider onboarding leniency" from "legal requirement to hold a license".

### Gaps
- Mamo and Nomod: no published volume caps for unlicensed individuals found; unknown whether they exist or what triggers a license request.
- Zbooni, PayTabs freelancer accounts: no limits found.
- No provider publishes an explicit "upgrade trigger" other than Ziina's cap and Botim's monitoring period.

## 3. Payout bank account ownership, bank restrictions and settlement timing

### Takeaway
Payout accounts must match the account holder: personal name for individuals, legal business name for licensed entities (explicit for Zbooni, Tap, Nomod, Telr). No provider was found restricting payouts to specific UAE banks. Settlement ranges from one day (Botim) to one to two business days (Ziina), with same-day options at Nomod on a paid plan.

### Cited Findings
- Ziina: free cashouts to your bank account "typically land within one to two business days"; "For some eligible users, payouts can be near-instant." Users are advised to check "monthly cash out limits". [FETCHED] — [Ziina blog, July 2026](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)
- Nomod: bank details need the "Account title, exactly as it appears on your bank statement" (usually the business name for business accounts). Payouts: "Get paid the next day, in two days or in five days, or even on the same day" (same-day with Membership). [FETCHED] — [Nomod docs](https://nomod.com/docs/account/set-up-account)
- Mamo: asks for "your IBAN and proof that this bank account is truly yours or in the name of your business." [SNIPPET] — [Mamo help: Getting started](https://help.mamopay.com/en/articles/7200023-getting-started-with-mamo-business); marketing copy says it settles the balance to your UAE bank account on a daily basis [SNIPPET] — [Mamo](https://mamopay.com)
- Zbooni: requires "a bank account in your name"; a business bank account must match the business legal documents provided. [SNIPPET] — [Zbooni](https://hub.zbooni.com/hc/en-us/articles/20286395081629-Account-Activation-FAQs)
- Tap: proof of a business bank account (IBAN certificate or statement); "Make sure the account name in the banking records is in English."; account name must match the legal name on the commercial license. [FETCHED] — [Tap support](https://support.tap.company/en/support/solutions/articles/153000242639)
- Telr: bank statement or bank letter that "must contain IBAN, account number, account name, bank name, bank branch, SWIFT code". [FETCHED] — [Telr](https://telr.com/documents-checklist)
- PayTabs freelancers: "Account details through bank account statement or any other document". [FETCHED] — [PayTabs](https://ai.paytabs.com/en/get-onboarded-with-your-freelancer-certificate/)
- Botim Money: "One-day payment settlement", "Automated settlement". [FETCHED] — [botim money pricing](https://www.botim.money/pricing)
- Magnati: proof of account by cancelled cheque, account statement or welcome letter. [SNIPPET] — [Magnati](https://www.magnati.com/?p=37341)
- Aani: funds "transfer and settle ... anytime, in less than 10 seconds", directly into the linked account; fees "determined by licensed financial institutions and payment service providers". [FETCHED] — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)

### Inferences
- [INFERRED] For an unlicensed individual on Ziina, Nomod or Mamo, a personal UAE current/savings account in the individual's own name is what will be accepted; third-party accounts will not be.
- [INFERRED] Using a personal bank account for regular business receipts can itself draw scrutiny from the bank; this is a bank-side risk, not a provider rule, and no source was collected on it.

### Gaps
- Ziina: no explicit statement found that the cash-out IBAN must be in the user's own name (very likely, unverified), and the monthly cash-out limit figure was not found.
- No explicit bank exclusions (e.g. digital banks such as Wio, Mashreq Neo, Liv) found for any provider.
- Settlement timing not found for Zbooni, Telr, Tap, PayTabs, Paymob, Foloosi, Magnati, Network International.

## 4. Approval time and whether onboarding is fully digital

### Takeaway
App-first providers advertise minutes to about 48 hours; published times exist only for Ziina ("Register in minutes"), Nomod ("around 48 hours") and Magnati ("under 15 minutes" to register). UAE Pass was not confirmed for any provider.

### Cited Findings
- Ziina: "Register in minutes"; activation follows verification; Professional account "available immediately". Business verification is done in-app by uploading documents. [FETCHED] — [Ziina blog](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork); [Ziina help](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- Nomod: review after submission "typically takes around 48 hours." [FETCHED] — [Nomod docs](https://nomod.com/docs/account/set-up-account)
- Magnati: digital merchant onboarding lets you register "in under 15 minutes"; owners and partners validated through a liveness check and digital signature. [SNIPPET] — [Magnati](https://www.magnati.com/?p=37341)
- Botim Money: simplified due diligence lets merchants start immediately. [SNIPPET] — [botim money pricing](https://www.botim.money/pricing)
- Mamo: missing documents are re-submitted through the Mamo Dashboard (digital). [SNIPPET] — [Mamo help](https://help.mamopay.com/en/articles/9932467-why-some-businesses-fail-verification)
- Network International: for DET-licensed Dubai businesses, document submission, verification and tracking are done online; N-Genius merchants otherwise provide documents to a Network representative. [SNIPPET] — [Network x DET](https://www.network.ae/en/contact-us/det-network); [Network developers](https://www.network.ae/en/merchant-solutions/ecommerce-payments/developers)
- Telr: checklist directs other cases to sales@telr.com / +971 4 314 6999 (sales-assisted). [FETCHED] — [Telr](https://telr.com/documents-checklist)
- Aani: merchants download the Aani app and complete registration and authentication to link their payment accounts. [FETCHED] — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)
- Unverified third-party claim that Telr onboarding takes three to five business days. [SECONDARY, low quality] — [Dubai South Business Hub](https://www.dubaisouthbh.com/blogs/payment-gateway-uae)

### Inferences
- [INFERRED] Ziina, Nomod and Mamo are fully digital (app/dashboard upload); Telr, Tap, Network and PayTabs involve a sales or account-manager step for most merchants.

### Gaps
- UAE Pass: no provider page I read mentions UAE Pass login or UAE Pass-based eKYC. Unverified for all.
- No approval time found for Mamo, Zbooni, Tap, PayTabs, Paymob, Foloosi, Botim (beyond "immediately").

## 5. Licensing status of each provider

### Takeaway
Ziina is the most fully licensed of the small-seller fintechs (CBUAE SVF since June 2024, Retail Payment Services reported October 2026). Telr, Tap and Paymob hold CBUAE Retail Payment Services licences; Foloosi was last seen at in-principle approval; Mamo's only confirmed registration is with the DFSA (DIFC).

### Cited Findings
- Ziina: "Ziina Payment LLC is licensed and regulated by the Central Bank of the UAE." [FETCHED] — [Ziina blog](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)
- Ziina received its CBUAE SVF licence (reported June 2024). [SECONDARY] — [Zawya](https://www.zawya.com/en/press-release/companies-news/ziina-becomes-first-venture-backed-start-up-to-secure-uae-central-bank-svf-license-ovmyunso)
- Ziina obtained a CBUAE Retail Payment Services (Category III) licence covering domestic fund transfers, merchant acquiring and payment aggregation, plus Exchange Business authorisation (reported October 2026). [SECONDARY] — [The Paypers](https://thepaypers.com/fintech/news/ziina-secures-cbuae-retail-payment-services-licence); [Wamda](https://www.wamda.com/index.php/ar/2026/10/uaes-ziina-secures-central-bank-approvals-retail-payments-exchange-services)
- Ziina Ltd also appears on the DFSA public register. [SNIPPET] — [DFSA](https://dfsa.ae/public-register/firms/ziina-ltd)
- Mamo Limited (trading name Mamo) is on the DFSA public register; it secured a licence to operate from DIFC. [SNIPPET / SECONDARY] — [DFSA](https://dfsa.ae/public-register/firms/mamo-limited); [The National](https://www.thenationalnews.com/business/technology/dubai-s-mamo-secures-licence-to-operate-from-difc-as-demand-for-digital-payments-surges-1.1251059)
- Telr: granted the Retail Payment Services licence by the CBUAE. [SECONDARY] — [Zawya](https://www.zawya.com/en/press-release/companies-news/telr-has-been-granted-the-retail-payment-services-license-from-the-central-bank-of-the-uae-ck1whaku)
- Tap Payments: awarded a Retail Payment Services licence by the CBUAE. [SECONDARY] — [Tap blog](https://blog.tap.company/central-bank-of-the-uae-awards-tap-payments-full-license-gcc)
- Paymob: CBUAE Retail Payment Services licence (January 2025) covering merchant acquiring, payment aggregation and domestic fund transfers. [SECONDARY] — [Wamda](https://www.wamda.com/2025/01/paymob-secures-uae-central-bank-retail-payment-services-licence)
- Foloosi: received CBUAE in-principle approval for a Category III licence. [SECONDARY] — [Zawya](https://www.zawya.com/en/press-release/companies-news/foloosi-secures-cbuae-in-principle-approval-1021659)
- Botim Money: site claims a "Regulatory license from UAE" without naming the licence type; company is part of Astra Tech; legal entity listed as Botim Money Technology L.L.C (formerly PayBy Technology). [FETCHED / SECONDARY] — [botim money pricing](https://www.botim.money/pricing); [Zawya company profile](https://www.zawya.com/company/5083420411/payby-technology)
- e& money holds a CBUAE Finance Company licence (lending). [SECONDARY] — [Trade Arabia](https://www.tradearabia.com/News/389165/eand-money-gets-Finance-Company-Licence%2C-to-offer-range-of-new-services)
- Careem's wallet was authorised by the CBUAE. [SECONDARY] — [The National, 2022](https://www.thenationalnews.com/business/money/2022/04/21/careem-enters-fintech-sector-with-a-digital-wallet-and-peer-to-peer-transfers)
- Network International and Magnati secured regulatory approvals to merge. [SECONDARY] — [Zawya](https://www.zawya.com/en/press-release/companies-news/network-international-and-magnati-secure-key-regulatory-approvals-for-merger-dblz0gj5)
- Aani is operated by Al Etihad Payments, a CBUAE subsidiary. [SECONDARY] — [The National](https://thenationalnews.com/business/money/2023/10/16/uae-central-bank-subsidiary-unveils-instant-payments-platform-for-digital-transactions)

### Inferences
- [INFERRED] Mamo likely serves onshore merchants via its DFSA authorisation plus a partner acquirer, or has since obtained a CBUAE licence; I found no evidence either way.
- [INFERRED] Whether the Network–Magnati merger has completed, and whether onboarding has been unified, is unknown; treat their checklists as still separate.

### Gaps
- The CBUAE public register was not consulted directly; every CBUAE licence above rests on press reports or the provider's own statement.
- No licence information found for Zbooni, Nomod, PayTabs UAE, Payit, Klip, YallaPay.
- Whether Foloosi's in-principle approval has converted to a full licence is unknown.
- Botim Money / PayBy licence type (SVF vs RPS) not confirmed.

## 6. Payment methods an individual can accept

### Takeaway
Ziina gives unlicensed users the broadest toolkit (payment links, QR, cards and Apple Pay, tap-to-pay on phone; website integration and NFC cards on Professional). Nomod offers links, invoices and in-person payments in five currencies. BNPL (Tabby/Tamara) and Aani acceptance were not confirmed for any individual-tier account.

### Cited Findings
- Ziina Personal: "payment links, QR codes, or wallet-to-wallet transactions". Professional: "website integration, QR code stands, NFC cards, tap to pay and branded payment links". [FETCHED] — [Ziina help centre](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- Ziina: customers can pay via Apple Pay or cards on payment links; Tap to Pay lets you "Turn your iPhone or Android into a point-of-sale terminal."; QR codes and website checkout also offered. [FETCHED] — [Ziina blog](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)
- Nomod: "Take payments using Links", "Send Invoices", "Accept In-Person payments", "Accept payments in AED, USD, EUR, GBP and SAR". [FETCHED] — [Nomod docs](https://nomod.com/docs/account/set-up-account)
- Botim Money: domestic and international cards and Alipay+ wallet payments; listed rate "1.5% + 1 AED"; "No set up fees". [FETCHED] — [botim money pricing](https://www.botim.money/pricing)
- PayBy deployed a POS terminal with built-in QR code and payment-link generation. [SECONDARY] — [Zawya](https://www.zawya.com/en/press-release/companies-news/payby-deploys-uaes-first-pos-machine-with-built-in-qr-code-and-payment-link-generation-lv5fypkp)
- PayTabs freelancers can accept payments "in different modes and from different channels" (no specifics). [FETCHED] — [PayTabs](https://ai.paytabs.com/en/get-onboarded-with-your-freelancer-certificate/)
- Aani: customers pay small merchants "by scanning a QR code (static or dynamic)", by accepting a request to pay, or via a merchant tag instead of an IBAN; domestic AED only. [FETCHED] — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)
- Individuals can also simply request money from other Aani users through their bank app, credited once the payer accepts. [SNIPPET] — [Mashreq Aani](https://mashreq.com/en/uae/neo/foreign-exchange/aani/)
- Careem Pay as a checkout method works similarly to Apple Pay with confirmation in the Careem app; available via Tap to corporates only. [SNIPPET, 2024] — [Tap Freshdesk](https://tappayments.freshdesk.com/en/support/solutions/articles/153000140158-careem-pay-faqs)

### Inferences
- [INFERRED] For an unlicensed individual, bank-app Aani request-to-pay is a zero-onboarding fallback (instant, no card fees, AED 50,000 per transaction) but supports no cards or Apple Pay and offers no checkout link in the gateway sense.
- [INFERRED] Tabby/Tamara integrations generally require a registered business; no individual-tier provider page mentioned them.

### Gaps
- Google Pay, Aani-at-checkout and Tabby/Tamara availability on Ziina, Mamo, Nomod individual tiers: not found.
- Mamo's and Zbooni's accepted payment methods and fees: not retrieved (help centres blocked).
- Fees for Ziina were not captured (page refers to https://ziina.com/fees, not opened).

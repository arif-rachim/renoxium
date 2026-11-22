# Global payment platforms and payout/disbursement solutions in the UAE for unlicensed micro-merchants, gig workers and freelancers (as of 22 November 2026)

Evidence labels used throughout:
- **[OFFICIAL-FETCHED]** = provider's own page, opened and read on 22 Nov 2026.
- **[OFFICIAL-SNIPPET]** = provider's own page, seen only as a search-result excerpt (page not opened, or returned HTTP 403). Treat as probably right but re-check.
- **[SECONDARY]** = press, aggregator, blog or forum. **[UNVERIFIED]** = single weak source or conflicting.

Not researched to source level (no usable result in the time available): Zand, Tarabut, Qashio, Pemo, C3Pay/Edenred, Checkout.com onboarding rules, Adyen UAE rates. These are listed under Gaps, not filled from memory.

---

## 1. Can an individual without a trade license open an account and receive customer payments? What documents are required?

### Takeaway
Among the providers checked, only Ziina (Personal and "Professional" accounts) and PayPal/Payoneer-type wallets offer a route that does not start with a UAE trade licence or freelance permit; Stripe states explicitly that it does not serve unlicensed individuals, and the UAE business banks (Wio Business, Mashreq NeoBiz, RAKBANK) are reported to require at least a freelance permit. A freelance permit (not a full trade licence) is the minimum that unlocks Stripe, Mamo and the business bank accounts.

### Cited Findings

**Stripe UAE — NO without licence/permit**
- [OFFICIAL-SNIPPET] "You also must have a trade license or freelancer permit issued in the UAE to be able to use Stripe. We don't currently support individuals without a trade license/permit or NGOs, non-profits or government organisations." — [Stripe Support: what types of businesses can open a UAE account](https://support.stripe.com/questions/what-types-of-businesses-can-open-a-uae-account)
- [OFFICIAL-FETCHED] Activation requires a "Trade license or freelancer permit issued within the UAE" for all business types; sole establishments need only that, LLCs also need the memorandum of association and proof of an active bank account (statement dated within last six months). Expired licences must be renewed. — [Stripe Support: UAE account activation requirements](https://support.stripe.com/questions/uae-account-activation-requirements)
- [OFFICIAL-FETCHED] Identity documents: UAE/GCC nationals resident in UAE = passport + Emirates ID; other nationals resident in UAE = passport + Emirates ID + residence visa (visa optional for Emirates IDs issued on/after 11 April 2022); non-residents = passport only. Colour copies, no screenshots. — [Stripe Support: UAE account activation requirements](https://support.stripe.com/questions/uae-account-activation-requirements)
- [OFFICIAL-SNIPPET] A licence issued outside the UAE is not accepted; common rejection causes are name mismatch, emirate mismatch between licence and account address, and expired licence. — [Stripe Support: UAE business verification requirements](https://support.stripe.com/questions/uae-business-verification-requirements); [Troubleshooting UAE verification issues](https://support.stripe.com/questions/troubleshooting-uae-verification-issues)
- [OFFICIAL-FETCHED] Stripe Connect: "Any connected account holders will need to provide a valid trade license issued within the UAE"; individuals without a licence cannot sign up for a connected account. Supported connected-account business types are sole establishments / free-zone establishments / branches, and LLCs / free-zone LLCs / branches. — [Stripe Support: Connect availability in the UAE](https://support.stripe.com/questions/connect-availability-in-the-uae)
- [SECONDARY, UNVERIFIED] Shopify community posts suggest Stripe support sometimes makes exceptions; nothing official supports this. — [Shopify Community thread](https://community.shopify.com/t/payment-gateway-issue-in-uae/397472)

**Ziina — YES (Personal and Professional accounts)**
- [OFFICIAL-SNIPPET] "Once your professional account is activated you can start receiving payments right away - no Trade License is required to get started." — [Ziina Help: difference between Personal, Professional and Business accounts](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- [OFFICIAL-FETCHED] Ziina's own blog (dated 9 July 2026) says "Register in minutes and start accepting payments after completing the verification checks", but adds that the user remains responsible for holding any trade licence, permit or tax registration their activity requires. It does not list the documents. — [Ziina blog: collect payments without extensive paperwork](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)
- [SECONDARY] Verified Business accounts require trade-licence verification and give higher limits and multi-user access. — [Freedom251 Ziina summary](https://freedom251.com/apps/ziina/)
- [SECONDARY] Ziina is described as a licensed financial institution via a CBUAE Stored Value Facility authorisation. — [Lean blog: Ziina and Lean bring Open Finance payments to life](https://leantech.me/blog/from-framework-to-reality-ziina-lean-bring-open-finance-payments-to-life)

**Mamo — businesses and freelancers; licence expected**
- [OFFICIAL-SNIPPET] Mamo is "available to businesses and freelancers registered and operating in the UAE"; onboarding collects "documentation that validates your business operations including your trade licence and MOA". — [Mamo Help: Getting started with Mamo Business](https://help.mamopay.com/en/articles/7200023-getting-started-with-mamo-business)
- No source found stating that an individual with only an Emirates ID can open a Mamo Business account. (Mamo's pricing and payouts pages returned HTTP 403 to direct fetch on 22 Nov 2026.)

**PayPal UAE**
- [OFFICIAL-FETCHED] PayPal publishes a UAE merchant fee schedule (last updated 28 October 2026), confirming UAE accounts can receive commercial payments. The page does not state onboarding documents. — [PayPal UAE merchant fees](https://www.paypal.com/ae/webapps/mpp/merchant-fees)
- No official source was retrieved on whether a UAE PayPal *business* account requires a trade licence, or on what a *personal* account may receive commercially.

**Payoneer**
- [OFFICIAL-SNIPPET] Payoneer markets a freelancer product and UAE/AED payments for freelancers and Amazon.ae sellers. — [Payoneer freelancer solutions](https://www.payoneer.com/solutions/freelancer-solutions/); [Payoneer blog: get paid in AED](https://blog.payoneer.com/e-sellers/access-amazon-ae-and-freelance-clients-in-the-united-arab-emirates-thriving-ecommerce-market-and-get-paid-in-aed)
- No official statement found on UAE individual sign-up documents (Emirates ID vs. licence).

**Wise — licensed, launch not confirmed**
- [OFFICIAL-SNIPPET] Wise announced (14 Oct 2025) final CBUAE approval for Stored Value Facilities and Retail Payment Services (Category 2) licences, enabling it to launch the Wise Account and Wise Business in the UAE "in the future". — [Wise newsroom](https://newsroom.wise.com/en-CEU/255401-wise-secures-central-bank-final-approval-for-stored-value-facilities-and-retail-payment-services-licenses-in-the-uae)
- [SECONDARY] Wise still needed a separate CBUAE no-objection for remittance services. — [Gulf News](https://gulfnews.com/business/banking/after-india-wise-now-offers-remittances-in-uae-after-central-bank-approval-1.500306285)
- No source found confirming that UAE residents can open a Wise account as of Nov 2026.

**Revolut — licensed June 2026, not launched (per latest source found)**
- [OFFICIAL-SNIPPET] Revolut obtained CBUAE Stored Value Facilities and Retail Payment Services (Category II) licences, announced 17 June 2026, following in-principle approval in September 2025; it is "building and preparing its local product offering ahead of a full-scale launch". — [Revolut news](https://www.revolut.com/news/revolut_obtains_approval_for_stored_value_facilities_and_retail_payment_services_licences_from_the_central_bank_of_the_uae/); [WAM](https://www.wam.ae/en/article/c0rrihe-revolut-obtains-approval-for-stored-value)
- [OFFICIAL-SNIPPET] Revolut received VARA in-principle approval for virtual-asset services (release dated 15 July 2026). — [Revolut news](https://www.revolut.com/en-LT/news/revolut_receives_in_principle_approval_to_provide_crypto_services_in_uae/)

**Business banks (Wio Business, Mashreq NeoBiz, RAKBANK RAKstarter)**
- [OFFICIAL-SNIPPET via Dubai Chambers] Mashreq guidance: "freelancers with a freelancer permit will be able to open Mashreq Business account". — [Mashreq business banking account opening guidelines (Dubai Chambers)](https://dubaichambers.com/documents/d/dubai-chambers-1/mashreq-business-banking-account-opening-guidelines_v1-2)
- [SECONDARY] Licensed freelancers can open Wio Business (Essential plan, single user, multi-currency IBAN); freelance permits such as IFZA's are reported as accepted at Wio, Mashreq NeoBiz, Zand and RAKstarter. — [Gulf News: freelancers and the right bank account](https://gulfnews.com/living-in-uae/banking/freelancing-in-the-uae-heres-how-to-open-the-right-bank-account-1.500098023); [Velmont Crest guide](https://velmontcrest.ae/insights/freelancer-bank-account-uae-2026-guide/)
- [SECONDARY, UNVERIFIED] One blog claims 15-20% of freelance applications get a "soft decline", usually from an activity-code mismatch. Single source. — [Belong blog](https://getbelong.com/blog/bank-accounts-for-freelancers-uae.md)

**Aani (instant payments) — depends on the enrolling bank/PSP**
- [OFFICIAL-FETCHED] Al Etihad Payments' FAQ for "small merchants and sole proprietors": Aani is offered only through licensed financial institutions and PSPs, which must enrol the merchant; customers pay by static/dynamic QR, Request to Pay, or a merchant proxy tag (no IBAN needed). The FAQ does not state any trade-licence requirement. — [AEP FAQ: small merchants and sole proprietors](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)

**Acquirer-grade PSPs (Checkout.com, Amazon Payment Services, Adyen)**
- No source was found stating that any of these onboard individuals without a licence; none of the pages surfaced address it. — see Gaps.

### Inferences
- The practical ladder for a UAE resident is: (1) no licence: Ziina Personal/Professional, person-to-person Aani/bank transfer, possibly PayPal/Payoneer for foreign clients; (2) freelance permit: Stripe, Mamo, Wio Business/NeoBiz/RAKstarter; (3) full trade licence: everything else.
- Ziina's wording ("no Trade License is required to get started") is an onboarding statement, not a legal opinion: Ziina itself says the seller is responsible for holding whatever licence their activity requires. A platform that relies on Ziina Professional for unlicensed sellers shifts, rather than removes, the licensing question.
- Stripe Connect cannot be used to pay unlicensed UAE gig workers: every UAE connected account needs a UAE trade licence.

### Gaps
- Exact Ziina Professional KYC documents (Emirates ID, selfie, proof of activity) were not found on a fetched page.
- PayPal UAE and Payoneer UAE onboarding documents for individuals: no official source retrieved.
- Whether Wise is live for UAE residents in Nov 2026: unknown; latest sources are Oct 2025.
- Whether Revolut launched between June and Nov 2026: no source found; latest (June-July 2026) says not yet.
- Zand, Checkout.com, Amazon Payment Services, Adyen onboarding rules for individuals: not found.
- The CBUAE register was not consulted directly; all licence statements above come from company releases or press.

---

## 2. Payout method to a UAE bank account: supported, AED, timing, minimums, fees, name-matching

### Takeaway
Stripe, Ziina, Mamo and PayPal all settle to a UAE bank account in AED; published timing ranges from near-instant (Ziina for eligible users, Stripe Instant Payouts at 1.5%) to 1-2 business days (Ziina standard) to "up to about 7 business days" (PayPal, secondary source). No provider page retrieved states its name-matching rule explicitly.

### Cited Findings
- **Stripe** [OFFICIAL-FETCHED]: standard payouts are free; Instant Payouts cost 1.5% of volume (minimum AED 2.00); USD payouts cost 1% of USD payout volume (minimum USD 25.00). — [Stripe UAE pricing](https://stripe.com/ae/pricing)
- **Stripe** [OFFICIAL-FETCHED]: the activation-requirements page does not say whether the bank account must be AED-denominated or must match the account holder's name. — [Stripe Support: UAE account activation requirements](https://support.stripe.com/questions/uae-account-activation-requirements)
- **Ziina** [OFFICIAL-FETCHED]: cashouts are free (excluding bank fees), and the fees page shows the same "Free" under a "Future pricing (from 1 November 2026)" column. — [Ziina fees](https://ziina.com/fees)
- **Ziina** [OFFICIAL-FETCHED]: "Free cashouts to your bank account typically arrive within one to two business days, subject to eligibility"; some eligible users get near-instant payouts; payouts go to a linked UAE bank account. — [Ziina blog, 9 July 2026](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)
- **Mamo** [OFFICIAL-SNIPPET]: same-day settlement is an add-on of +0.75% (Growth) or +0.5% (Premium). — [Mamo Help: November 2024 pricing changes](https://help.mamopay.com/en/articles/10071872-november-2024-pricing-changes)
- **PayPal** [OFFICIAL-FETCHED, table layout ambiguous]: withdrawal to a bank account in AED has no fee above AED 750 when no currency conversion is involved, otherwise AED 11.00; a separate line reads 1.50% with a USD 15.00 minimum per USD withdrawal; withdrawal to a card is AED 20.00 flat. Page last updated 28 October 2026. — [PayPal UAE merchant fees](https://www.paypal.com/ae/webapps/mpp/merchant-fees)
- **PayPal** [SECONDARY]: third-party guides read the same schedule as "AED 750 and above free, below AED 750 costs AED 11", and say transfers to a UAE bank can take up to about 7 business days. — [Lilach Bullock: PayPal in UAE](https://www.lilachbullock.com/paypal-in-uae/); [Avyanco guide](https://avyanco.com/using-paypal-in-uae-for-business/)
- **PayPal + e& money** [OFFICIAL-SNIPPET]: e& announced (15 Oct 2025) that e& money would become the UAE's first PayPal-linked digital wallet with instant AED withdrawals. — [e& news](https://www.eand.com/en/news/15-oct-25-eand-money-will-become-uaes-first-paypal-linked-digital-wallet.html); [Khaleej Times](https://www.khaleejtimes.com/business/telecom/uae-e-money-partners-paypal-instant-withdrawals)
- **Payoneer** [SECONDARY]: balance can be withdrawn to a UAE bank in AED or another supported currency; Payoneer says withdrawals typically arrive within 24 hours, depending on bank and location. — [Lilach Bullock: Payoneer in UAE](https://www.lilachbullock.com/payoneer-in-uae/)
- **Payoneer** [SECONDARY, UNVERIFIED, conflicts with Payoneer marketing]: community reports say Payoneer stopped issuing new local AED receiving-account details. — [Lilach Bullock: Payoneer in UAE](https://www.lilachbullock.com/payoneer-in-uae/); contradicted by [Payoneer blog](https://blog.payoneer.com/e-sellers/access-amazon-ae-and-freelance-clients-in-the-united-arab-emirates-thriving-ecommerce-market-and-get-paid-in-aed)
- **Aani** [OFFICIAL-FETCHED]: payments are instant, 24x7, settled in "less than 10 seconds", up to AED 50,000 per payment "at the initial stage". — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/); [AEP: About Aani](https://aep.ae/en/services/about-aani/)
- **Aani** [SECONDARY]: the Dh50,000 cap is per transaction; daily/monthly caps are set by each institution. — [The National, 3 Aug 2026](https://thenationalnews.com/business/banking/2026/08/03/uae-expands-federal-payment-channels-with-aani-and-jaywan)
- **Careem Pay** [SECONDARY, dated]: wallet balances can be withdrawn to any UAE bank account from the app. — [Gulf Business](https://gulfbusiness.com/?p=210540)

### Inferences
- For an unlicensed individual, Ziina is the only checked provider with both a no-licence onboarding statement and free AED cashout.
- PayPal's sub-AED 750 fee (AED 11) is material for micro-merchants with small balances; batching withdrawals above AED 750 avoids it.

### Gaps
- Name-matching rule (bank account name must equal account holder): not stated on any page retrieved for Stripe, Ziina, Mamo, PayPal or Payoneer.
- Minimum payout amounts: not found for Stripe UAE, Ziina, Mamo.
- Stripe UAE standard payout schedule (days): not on the pages fetched.
- Payoneer UAE AED withdrawal fee and timing: no official UAE figure found.

---

## 3. Payout APIs / mass disbursement: what the paying platform must hold, recipient KYC, per-payout cost, rail

### Takeaway
The only published per-payout price found is Mamo's AED 5 per domestic payout (0.5% international); everything else (Lean, NymCard, Checkout.com, Hubpay, Fuze) is contract-priced and aimed at licensed businesses. No retrieved source documents the KYC a platform must perform on recipient gig workers, or names the domestic rail used by each payout API.

### Cited Findings
- **Mamo payouts** [OFFICIAL-SNIPPET; page returned HTTP 403 on direct fetch]: "Domestic payouts fee is AED 5 per payout" (custom for Enterprise); "International payouts fee is 0.5% of the transfer amount to anywhere in the world"; payouts can be triggered by platform events, scheduled, or built through the API. — [Mamo payouts](https://www.mamopay.com/business/payouts)
- **Mamo payouts** [OFFICIAL-SNIPPET]: domestic payouts are AED 5 on both Growth and Premium plans. — [Mamo Help: pricing plans and subscriptions](https://help.mamopay.com/en/articles/7234173-pricing-plans-and-subscriptions)
- **Stripe Connect** [OFFICIAL-FETCHED]: supported set-ups are foreign platform + UAE connected accounts, and UAE platform + UAE connected accounts. UAE platforms can use only Custom connected accounts in the UAE, and only destination charges and separate charges and transfers (destination charges with `on_behalf_of` not yet supported for UAE platforms). "It is not possible to create UAE connected accounts where the platform is liable". Every connected account needs a UAE trade licence. — [Stripe Support: Connect availability in the UAE](https://support.stripe.com/questions/connect-availability-in-the-uae)
- **Stripe Connect** [OFFICIAL-FETCHED]: Connect pricing starts at 0.25% for platforms that set their own payments pricing. — [Stripe UAE pricing](https://stripe.com/ae/pricing)
- **Lean Technologies** [SECONDARY / company material]: licensed and regulated by ADGM, holds CBUAE in-principle approval under the Open Finance framework; its collections product uses the Open Finance framework, Al Tareq for payment initiation and Aani for instant settlement; Lean says it has enabled A2A payments in the UAE since 2022. — [Lean blog: Lean Collections](https://leantech.me/ksa/en/blog/lean-collections-automated-repayments-in-the-uae); [Zawya: Lean receives Open Finance IPA](https://www.zawya.com/en/press-release/companies-news/lean-technologies-receives-key-regulatory-approval-under-uaes-open-finance-framework-usvuh2xi)
- **Open Finance go-live** [SECONDARY / company releases]: Lean and Ziina reported the first live customer-initiated Open Finance payment on 15 January 2026; Pay10 describes itself as the first licensed TPP under the CBUAE framework and went live with FAB (dates vary April-May 2026 across sources); Commercial Bank of Dubai reported multi-bank payment initiation in its app on 17 August. — [Wamda](https://www.wamda.com/2026/06/lean-ziina-launch-uaes-tap-pay-bank-experience-open-finance); [Gulf News: FAB, Pay10 go live](https://gulfnews.com/business/banking/fab-pay10-go-live-with-uae-open-finance-initiative-1.500394316); [FF News: CBD multi-bank payment initiation](https://ffnews.com/news/commercial-bank-of-dubai-becomes-first-uae-bank-to-launch-multi-bank-payment-initiation)
- **Open Finance licensing** [SECONDARY]: third parties need a CBUAE Open Finance licence for data exchange and/or payment initiation; the CBUAE had not published a timetable for the second phase covering exchange houses and payment providers. — [Payment Expert, 21 Oct 2026](https://paymentexpert.com/2026/10/21/uae-fintech-hub-open-finance-2026/)
- **NymCard** [OFFICIAL-SNIPPET]: partnership with Visa Direct announced 27 November 2025 (near-real-time delivery to 11bn+ card/account endpoints in 190+ markets), framed mainly around cross-border transfers for banks and enterprises. — [NymCard newsroom](https://nymcard.com/2025/11/27/nymcard-today-announced-a-partnership-with-visa-direct-visas-global-real-time-money-movement-network/)
- **NymCard** [OFFICIAL]: principal Mastercard member; September 2024 tie-up with Mastercard Move for cross-border transfers to 47 countries. — [Mastercard newsroom](https://www.mastercard.com/news/eemea/en/newsroom/press-releases/en/2024/september/nymcard-leverages-mastercard-move-to-offer-fast-and-efficient-cross-border-payments-across-47-countries)
- **Checkout.com** [SECONDARY]: launched a Visa Direct push-to-card product in the UAE for near-real-time disbursements to eligible Visa cards. — [The Paypers](https://thepaypers.com/payments/news/checkoutcom-and-visa-launch-push-to-card-solution-in-the-uae)
- **Worldpay** [OFFICIAL, Mastercard release Aug 2025]: UAE push-to-card via Mastercard Move, explicitly naming "gig economy payouts" among use cases (domestic and cross-border). — [Mastercard newsroom](https://www.mastercard.com/news/eemea/en/newsroom/press-releases/en/2025-1/august/worldpay-leverages-mastercard-move-to-empower-consumers-and-businesses-to-send-and-receive-payments-quickly-and-securely-in-the-uae)
- **Fuze** [SECONDARY]: secured a CBUAE Retail Payment Services and Card Schemes (RPSCS) licence, letting it offer businesses payment acceptance, settlement and payouts. — [CairoScene](https://cairoscene.com/Business/Fuze-Secures-License-to-Provide-Card-Payment-Services-in-the-UAE)
- **Hubpay** [SECONDARY]: ADGM-licensed money services provider; launched a Receivables capability (payment links, payment intents) and a self-serve SME cross-border platform. — [TechAfrica News](https://techafricanews.com/2024/12/18/hubpay-launches-self-serve-platform-to-empower-uae-smes-in-cross-border-trade/); [VCPost](https://vcpost.com/articles/128476/20240917/from-fractured-to-fluid-hubpay-builds-fintech-platform-for-uae-businesses-and-beyond.htm)
- **e& money (salary)** [OFFICIAL-SNIPPET, 12 May 2026]: waived all salary-processing fees for employers; each employee gets a free IBAN account and free virtual and physical card. — [e& news](https://www.eand.com/en/news/12-may-26-eandmoney-support-business-and-employees.html); [Gulf News](https://gulfnews.com/business/banking/new-e-salary-platform-offers-uae-workers-free-iban-accounts-cashback-1.500537763)
- **Botim money (salary)** [OFFICIAL-SNIPPET]: WPS-accredited payroll that pays salaries into botim wallets; virtual-IBAN wallets with no minimum salary, no minimum balance and no monthly maintenance fee. — [Astra Tech FAQ: botim money payroll solution](https://astratech.ae/platform-terms/faq/botim-money-payroll-solution/)
- **Botim** [SECONDARY]: co-branded salary card with City Exchange; first fintech to issue a Jaywan prepaid card for WPS payees. — [Zawya](https://www.zawya.com/en/press-release/companies-news/botim-money-and-city-exchange-partner-to-advance-digital-salary-solutions-and-financial-inclusion-in-the-uae-eagzueu2); [GDN Online](https://www.gdnonline.com/Details/1411093/Botim-becomes-first-Fintech-to-issue-Jaywan-Prepaid-Card)
- **Careem captain payouts** [SECONDARY, 2020-2023]: Hala/Careem drivers received payments up to twice a week into Careem Pay wallets (2023); a 2020 Visa partnership aimed to give captains instant access to earnings, UAE first. — [The National, 3 July 2023](https://www.thenationalnews.com/business/money/2023/07/03/hala-adds-digital-wallet-to-expedite-payments-to-drivers); [The National, 2020](https://www.thenationalnews.com/business/careem-partners-with-visa-to-give-its-drivers-instant-access-to-earnings-1.1040551)
- **Unbanked workers** [SECONDARY]: prepaid salary cards from exchange houses (Al Ansari, LuLu), e& money and Payit are the common routes. — [Gulf News](https://gulfnews.com/ask-gulf-news/ask-gulf-news-how-to-send-money-without-a-bank-account-in-the-uae-1.500358425)

### Inferences
- A platform paying many UAE gig workers has three realistic patterns: (a) bank-account payouts through a licensed PSP such as Mamo at a flat per-payout fee, where the recipient needs only an IBAN; (b) push-to-card via Visa Direct / Mastercard Move through Checkout.com, Worldpay or NymCard; (c) wallet/payroll rails (e& money, Botim, Careem Pay), which are built around employer-employee salary (WPS) and not obviously open to independent contractors.
- At AED 5 flat, Mamo's payout costs 5% of a AED 100 payout and 0.5% of a AED 1,000 payout, so batching small earnings matters.
- In every pattern found, the paying platform is a licensed business customer of a regulated PSP; no source describes an unlicensed individual operating a payout API.
- WPS-linked products presuppose an employment relationship registered with MOHRE; that inference follows from their being described as salary/WPS products, and was not confirmed against a regulator source.

### Gaps
- Recipient-side KYC obligations for payout APIs (whether the PSP or platform must identify each gig worker): no source found.
- Which rail Mamo uses for domestic payouts (UAEFTS vs IPP/Aani): not stated in anything retrieved.
- Pricing for Lean, NymCard, Checkout.com push-to-card, Worldpay, Hubpay, Fuze: none published.
- Whether Lean offers an outbound payouts product (as opposed to pay-by-bank collections) for gig payouts: not confirmed.
- Ziina payouts API: no source found describing one.
- C3Pay/Edenred fees, Tarabut UAE payments, Qashio and Pemo (spend-management; relevance to gig payouts unclear): searches returned nothing usable.
- Current (2026) Careem captain payout terms: only 2020-2023 sources found.
- NymCard CBUAE stored-value in-principle approval (headline dated Sept 2026 seen in results): not verified.

---

## 4. Published pricing

### Takeaway
Card acceptance for small UAE sellers clusters at 2.9% + AED 1 (Stripe domestic, Ziina, Mamo Growth), with PayPal noticeably dearer (3.40% domestic / 4.40% international plus a fixed fee, and 3-4% FX spread). Acquirer-grade PSPs do not publish UAE rates.

### Cited Findings

| Provider | Domestic cards | International cards | FX | Payout / withdrawal | Monthly | Evidence |
|---|---|---|---|---|---|---|
| Stripe UAE | 2.9% + AED 1.00 | +1% | +1% if conversion needed | Standard free; Instant 1.5% (min AED 2); USD payouts 1% (min USD 25) | none listed | OFFICIAL-FETCHED |
| Ziina | 2.9% + AED 1 (payer-side wording; see note) | 2.9% + AED 1 | not stated | Cashout free (excl. bank fees) | Lite free; Violet AED 100/mo or AED 996/yr incl. VAT | OFFICIAL-FETCHED |
| Mamo Growth | 2.9% + AED 1 | not retrieved | not retrieved | AED 5 per domestic payout; 0.5% international; same-day settlement +0.75% | AED 0 | OFFICIAL-SNIPPET |
| Mamo Premium | 2.7% + AED 0.8 | not retrieved | "favorable FX" | AED 5 domestic; same-day +0.5% | AED 99 (cut from AED 149) | OFFICIAL-SNIPPET |
| PayPal UAE | 3.40% + fixed fee | 4.40% + fixed fee | 3.0% (converting receipts/balance); 4.0% (sending / late refunds) | Bank: free above AED 750, else AED 11; card: AED 20 | none listed | OFFICIAL-FETCHED |
| Amazon Payment Services | 2.80% + AED 1 | not found | not found | weekly settlement (standard) | not found | SECONDARY (aggregator) |
| Checkout.com | contract-priced | contract-priced | - | - | minimum monthly fees reported (not UAE-specific) | SECONDARY |
| Adyen | Interchange++ ; no setup fee | - | - | - | - | OFFICIAL-SNIPPET |

- Stripe figures, plus disputes at AED 60.00 per dispute received and Connect from 0.25%. — [Stripe UAE pricing](https://stripe.com/ae/pricing)
- Ziina: receiving payments is listed as "Free" for all methods on both Personal and Business tabs; 2.9% + AED 1 is listed against sending by UAE debit card, UAE credit card, international card, Apple Pay or Google Pay; payments from non-Ziina users to Personal accounts by card cost 2.9% + AED 1, and to Business accounts are listed as "Free". All fees attract 5% VAT on the fee only. Card top-ups 2.5% (Lite). — [Ziina fees](https://ziina.com/fees)
- Ziina conflict: Ziina's business help page says "2.9% + 1 AED with any debit or credit card (UAE or international), Apple Pay, or Google Pay" for business receipts, an older Ziina blog cites 2.6% + AED 1, and a personal-account fee schedule took effect 8 November 2025. — [Ziina Help: fees to use Ziina Business](https://ziina.com/help-center/6847430-what-are-the-fees-to-use-ziina-business); [Ziina Help: personal account fees](https://ziina.com/help-center/12259572-what-are-the-fees-for-a-personal-account); [Ziina blog: Stripe alternatives](https://ziina.com/blog/stripe-alternatives-with-cost-effective-transaction-fees)
- Mamo plan figures and the Premium price cut. — [Mamo Help: pricing plans and subscriptions](https://help.mamopay.com/en/articles/7234173-pricing-plans-and-subscriptions); [Mamo Help: November 2024 pricing changes](https://help.mamopay.com/en/articles/10071872-november-2024-pricing-changes)
- Mamo: an aggregator lists Growth at 3.045% + AED 1.05, which equals the official rate with 5% VAT added to the fee. — [kredit.ae Mamo](https://kredit.ae/business/mamo)
- PayPal: UAE is not in the volume-tier table, and the AED fixed fee is not listed on the page. Last updated 28 October 2026. — [PayPal UAE merchant fees](https://www.paypal.com/ae/webapps/mpp/merchant-fees)
- Payoneer (global, not UAE-specific): no setup or monthly fee; withdrawal in local currency of the bank's country "up to 3%"; same-currency withdrawal flat fee (e.g. USD 1.50 for USD-to-USD); third-party reviews cite roughly 1.2%-4% where conversion is involved. — [Payoneer fees](https://www.payoneer.com/about/fees/); [Wise review of Payoneer](https://wise.com/us/blog/payoneer-review)
- Amazon Payment Services 2.80% + AED 1 and weekly settlement; Checkout.com priced in its merchant agreement. — [kredit.ae payment gateways](https://kredit.ae/business/payment-gateways)
- Checkout.com minimum monthly fees, volume-based pricing (BigCommerce doc, not UAE-specific). — [BigCommerce support](https://support.bigcommerce.com/articles/Public/Connecting-with-Checkoutcom)
- Adyen: no setup fee, Interchange++ model. — [Adyen pricing (en_AE)](https://www.adyen.com/en_AE/pricing)
- Wio Business: Essential plan reported at AED 99/month with no minimum balance (plans roughly AED 99-149). — [Gulf News](https://gulfnews.com/living-in-uae/banking/freelancing-in-the-uae-heres-how-to-open-the-right-bank-account-1.500098023)
- Mashreq NeoBiz: sources conflict — "Lite plan at 200 dirhams a month" vs "AED 0 for first 12 months then AED 150/month". [UNVERIFIED] — [Gulf News](https://gulfnews.com/living-in-uae/banking/freelancing-in-the-uae-heres-how-to-open-the-right-bank-account-1.500098023); contradicted by [Business Bank Index](https://businessbankindex.pages.dev/ae/for-freelancers)
- RAKBANK RAKstarter: marketed with no minimum balance. — [Gulf News](https://gulfnews.com/living-in-uae/banking/freelancing-in-the-uae-heres-how-to-open-the-right-bank-account-1.500098023)
- Botim salary card: AED 285 card issuance, 2% cross-border fee, monthly fee waiver at the higher of AED 4,000 or qualifying inflows. [OFFICIAL-SNIPPET; may be outdated] — [Astra Tech schedule of charges](https://astratech.ae/platform-terms/schedule-of-charges/)
- Aani: fees are set by the merchant's bank or PSP; AEP publishes no figures. — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)

### Inferences
- On a AED 100 domestic card sale: Stripe / Ziina / Mamo Growth cost about AED 3.90 before VAT on the fee; PayPal costs AED 3.40 plus an unpublished fixed fee.
- Ziina's fee page is worded from the payer's side and is internally inconsistent with its business help article on who bears the 2.9% + AED 1; the report writer should not state Ziina's merchant rate as settled without re-checking.

### Gaps
- PayPal's AED fixed fee per transaction: not on the fee page.
- Mamo international-card and FX rates: pricing page blocked (HTTP 403).
- UAE rates for Checkout.com, Adyen (Interchange++ mark-up) and official Amazon Payment Services pricing/setup/monthly fees: not published or not found.
- Aani merchant fee levels charged by individual banks/PSPs: not found.
- Payoneer UAE-specific fee table: not found.
- Zand, Qashio, Pemo, C3Pay pricing: not researched to source.

---

## 5. Known restrictions or risks for UAE individuals

### Takeaway
The clearest hard limits are Ziina's personal-account caps (AED 50,000 received per month, AED 3,600 per transaction, AED 1,500 per week by card, not increasable) and Aani's AED 50,000 per-payment cap; Stripe's main risk is rejection or re-verification failure tied to licence validity. Evidence on freezes and reserves is anecdotal only.

### Cited Findings
- **Ziina Personal** [OFFICIAL-SNIPPET]: monthly receiving cap AED 50,000; per-transaction cap AED 3,600; weekly card-payment receiving cap AED 1,500; personal limits are fixed and cannot be increased. — [Ziina Help: personal account transaction limits](https://ziina.com/help-center/6857582-what-are-the-transaction-limits-for-my-personal-account); [Ziina Help: 1,500 vs 20,000 AED limits](https://ziina.com/help-center/15136431-whats-the-difference-between-the-1-500-aed-and-20-000-aed-limits)
- **Ziina Verified Business** [OFFICIAL-SNIPPET]: per-transaction limit AED 90,000; money received per month AED 140,000. — [Ziina Help: verified business limits](https://intercom.help/ziina-8bdf318a7612/en/articles/6847433-what-are-the-transaction-limits-for-my-verified-business-account)
- **Ziina Professional** [OFFICIAL-SNIPPET]: has its own limits page (figures not retrieved); upgrading to Professional or Verified Business gives higher receiving limits. — [Ziina Help: Professional account limits](https://ziina.com/help-center/6955251-what-are-the-transaction-limits-for-your-ziina-professional-account)
- **Ziina** [OFFICIAL-FETCHED]: the seller is responsible for holding any trade licence, permit or tax registration their business requires; advises checking monthly cash-out limits against expected sales. — [Ziina blog, 9 July 2026](https://ziina.com/blog/how-can-i-collect-payments-without-extensive-paperwork)
- **Stripe** [OFFICIAL-SNIPPET]: expired licence/permit cannot be accepted; Stripe runs continuous verification in the UAE. — [Troubleshooting UAE verification issues](https://support.stripe.com/questions/troubleshooting-uae-verification-issues); [Continuous Verification in the UAE](https://support.stripe.com/questions/continuous-verification-in-the-uae)
- **Stripe Connect** [OFFICIAL-FETCHED]: platform-liable connected accounts cannot be created in the UAE. — [Connect availability in the UAE](https://support.stripe.com/questions/connect-availability-in-the-uae)
- **Aani** [OFFICIAL-FETCHED]: AED 50,000 cap per payment at the initial stage; users are told to verify recipients/requesters and report unauthorised payments to their provider. — [AEP FAQ](https://aep.ae/en/frequently-asked-questions/small-merchants-and-sole-proprietors/)
- **PayPal** [OFFICIAL-FETCHED]: 3.0% conversion spread on received funds and 4.40% international rate make cross-border receipts costly for UAE sellers. — [PayPal UAE merchant fees](https://www.paypal.com/ae/webapps/mpp/merchant-fees)
- **Payoneer** [SECONDARY, UNVERIFIED]: reports that new local AED receiving details are no longer issued, said mainly to affect business entities. — [Lilach Bullock: Payoneer in UAE](https://www.lilachbullock.com/payoneer-in-uae/)
- **Business banks** [SECONDARY, UNVERIFIED]: single-blog claim of 15-20% soft declines for freelance applicants. — [Belong blog](https://getbelong.com/blog/bank-accounts-for-freelancers-uae.md)

### Inferences
- A micro-merchant on a Ziina Personal account who takes card payments hits the AED 1,500/week card cap almost immediately; the Professional tier is the realistic no-licence option for anyone selling regularly.
- Stripe's continuous verification means a lapsed freelance permit is an ongoing account risk, not only an onboarding hurdle.
- Using a personal wallet for commercial receipts without the licence the activity legally requires is a compliance risk carried by the individual (Ziina says so explicitly); whether a given activity requires a licence is a legal question outside these sources.

### Gaps
- Reserve, rolling-hold and freeze policies for UAE accounts at Stripe, PayPal, Ziina, Mamo: no UAE-specific official statement retrieved; no reliable data on incidence.
- Ziina Professional limit figures: page identified but not read.
- PayPal UAE receiving caps for unverified or personal accounts: not found.
- Any CBUAE rule on commercial use of personal wallets/SVF accounts: not researched; the CBUAE register and rulebook were not consulted directly.

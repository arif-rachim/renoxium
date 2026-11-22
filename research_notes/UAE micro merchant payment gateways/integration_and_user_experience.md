# Integration complexity, developer experience and real-world reliability of UAE payment gateways / payment-link / payout providers (state as of 22 Nov 2026)

Research date: 22 November 2026. Scope: UAE only. Method: web searches plus fetches of official docs/help pages. Most findings below come from search-result summaries of the cited pages (only three pages were fetched in full: Stripe continuous verification, Telr sub-merchant onboarding, Ziina website-integration index). Several official pages (e.g. Mamo help center) returned HTTP 403 to direct fetch. **Official facts and anecdotal reports are labelled separately. Coverage is uneven: Ziina, Mamo, Stripe, Telr, Tap, PayTabs, Paymob and N-Genius are reasonably covered; PayBy/Botim, Zbooni, Foloosi, Checkout.com, Amazon Payment Services, PayPal and Payoneer are thin — see Gaps in each section.** Reddit (r/dubai, r/UAE), App Store and Google Play review content could not be retrieved by the search tool at all.

## No-code options (payment links, QR, invoices, tap-to-pay, WhatsApp/Instagram selling, storefronts)

### Takeaway
Ziina, Mamo, Zbooni, Paymob, Telr and PayTabs all offer zero-code payment links; Ziina is the only one found with a documented personal (no trade licence) tier that can accept payment links/QR, but with very low caps (AED 1,500 card receipts per rolling 7 days). Evidence for PayBy/Botim, Foloosi, N-Genius, Checkout.com, APS, PayPal and Payoneer no-code features was not retrieved.

### Cited Findings
**Ziina (official)**
- Personal accounts can send/receive money using payment links, QR codes or wallet-to-wallet transfers; links do not expire and can be shared repeatedly — [Ziina help: receiving payments](https://ziina.com/help-center/4012095-receiving-payments), [Does the Ziina link expire](https://intercom.help/ziina-8bdf318a7612/en/articles/7915523-does-the-ziina-link-expire)
- Three tiers exist: Personal, Professional, Business — [Ziina help: account differences](https://ziina.com/help-center/6847490-what-is-the-difference-between-personal-professional-and-business-accounts)
- Personal account limits are fixed: AED 18,000 wallet balance limit, AED 18,000 single withdrawal limit, AED 2 minimum received transfer; another help article lists AED 3,600 single-received-transfer maximum (the two articles differ — verify) — [Ziina help: personal limits](https://ziina.com/help-center/6857582-what-are-the-transaction-limits-for-my-personal-account)
- Card payments received on a personal account are capped at AED 1,500 per rolling 7 days; upgrading to Professional or Verified Business raises it — [Ziina help: 1,500 vs 20,000 AED limits](https://ziina.com/help-center/15136431-whats-the-difference-between-the-1-500-aed-and-20-000-aed-limits)
- Ziina's own comparison page lists website integration, QR code stands, NFC cards, tap to pay and branded payment links as collection options (tier availability not stated) — [Ziina comparison](https://ziina.com/business/comparison)

**Mamo (official)**
- Payment links product page; subscription payment links can also be created via API — [Mamo payment links](https://www.mamopay.com/business/payment-links), [Mamo help: API integrations](https://help.mamopay.com/en/articles/7234144-api-integrations)
- Invoices API exists alongside Payment Links and Payouts APIs (third-party catalogue of Mamo's OpenAPI specs) — [apis.io Mamo](https://apis.io/providers/mamo/)

**Zbooni (mostly vendor/partner marketing)**
- UAE chat-commerce platform: merchants sell over WhatsApp, Instagram, Telegram; generates a payment link or QR code per order; customers pay by Visa, Mastercard, Apple Pay or Mada inside the conversation (vendor-written profile) — [yespress Zbooni profile](https://yespress.io/zbooni.md)
- Launched cShop Market (chat-to-shop marketplace) in the UAE in April 2023 — [yespress Zbooni profile](https://yespress.io/zbooni.md)
- Historic positioning as a payment service for micro businesses — [The National](https://thenationalnews.com/business/money/zbooni-the-uae-payment-service-helping-micro-businesses-process-transactions-1.852272)

**Paymob UAE (official)**
- Developer portal lists "Payment links / PayMe" as the fastest option: best for freelancers, invoicing, social selling; time to launch "minutes"; technical level "none" — [Paymob docs index](https://developers.paymob.com/paymob-docs/llms.txt)

**Telr (official docs)**
- Payment links are created in the dashboard without code; bulk creation via batch file upload; links can be tested in sandbox before activation; a "payment handle" no-code product also exists — [Telr docs: payment link](https://telr-docs.cashfree.com/payments/no-code/payment-link), [Telr docs: payment handle](https://telr-docs.cashfree.com/payments/no-code/payment-handle). Note: these pages sit on a Cashfree-branded docs host (telr-docs.cashfree.com), not docs.telr.com.

**PayTabs (official docs)**
- PayLinks API and invoices (an `invoice` object in the hosted-payment-page request turns it into an invoice) — [PayTabs PayLinks](https://docs.paytabs.com/PT2-API-Endpoints/Integration-Types-Manuals/PayLinks-APIs/PayLinks-Step-3-Initiating-Payment/PayLinks-Initiating-Payment), [PayTabs HPP initiating payment](https://docs.paytabs.com/PT2-API-Endpoints/Integration-Types-Manuals/Hosted-Payment-Page/HPP-Step-3-Initiating-Payment/HPP-Initiating-Payment)

**Foloosi**
- Separate consumer app (recharge/bill pay) and "Foloosi Business: Merchant App" exist on the App Store — [App Store: Foloosi Business](https://apps.apple.com/us/app/foloosi-business-merchant-app/id6445989980)

### Inferences
- For an unlicensed individual seller, Ziina Personal is the only documented self-serve route found, and the AED 1,500/week card cap makes it unsuitable for anything beyond very small volumes; a Professional account (requirements not retrieved) is the intended upgrade.
- Zbooni is the most WhatsApp/Instagram-native of the set, but all evidence found is promotional.

### Gaps
- Tap-to-pay on phone: not confirmed per provider/tier (Ziina lists it only on a marketing comparison page; nothing retrieved for Mamo, Paymob, N-Genius, Tap, PayBy).
- No no-code feature evidence retrieved for PayBy/Botim, Foloosi (beyond app existence), N-Genius pay-by-link, Tap Payments, Checkout.com, Amazon Payment Services, Stripe Payment Links in the UAE, PayPal (PayPal.Me/invoicing availability in UAE) or Payoneer payment requests.
- Hosted storefront features (Zbooni cShop detail, Ziina/Mamo storefronts) not verified from official pages.
- Ziina Professional account requirements (freelance permit vs. none) and limits not retrieved ([limits article exists](https://intercom.help/ziina-8bdf318a7612/en/articles/6955251-what-are-the-transaction-limits-for-your-ziina-professional-account) but was not read).

## Plugins (Shopify, WooCommerce, Wix, Zid/Salla, etc.)

### Takeaway
Plugin coverage is confirmed for Telr (widest), PayTabs, Paymob, Ziina (WooCommerce only confirmed), Mamo, Foloosi (Magento) and N-Genius (Magento); nothing was found on Wix or Zid/Salla for any provider.

### Cited Findings
- **Telr**: Shopify app in the Shopify App Store with a test mode (Settings → Payments → Telr → enable Test mode) and reconciliation via Shopify Payment ID — [Telr docs: Shopify](https://telr-docs.cashfree.com/payments/shopify/payments), [Shopify App Store: Telr](https://apps.shopify.com/telr?locale=th). Third-party guide lists Telr plugins for WooCommerce, OpenCart, PrestaShop, Shopify, Magento — [Dubai South Business Hub blog](https://www.dubaisouthbh.com/blogs/payment-gateway-uae)
- **PayTabs**: Shopify plugin via Shopify App Store and an official WooCommerce plugin (third-party comparison site) — [checkoutflow PayTabs UAE](https://checkoutflow.pages.dev/ae-en/best-payment-gateway-ecommerce/paytabs-uae/)
- **Paymob**: "Paymob for WooCommerce" on WordPress.org and WooCommerce marketplace; setup by manually entering API, public and secret keys, with different keys for Test and Live mode — [WooCommerce docs: Paymob](https://woocommerce.com/document/paymob-for-woocommerce), [WordPress.org plugin](https://ja.wordpress.org/plugins/paymob-for-woocommerce/). Odoo payment provider (needs HMAC + API/secret/public keys) — [Odoo docs](https://www.odoo.com/documentation/20.0/applications/finance/payment_providers/paymob.html). Community-maintained Moodle plugin supports UAE — [Moodle plugin](https://moodle.org/plugins/paygw_paymob)
- **Ziina**: official WooCommerce plugin (with test mode; needs an API key); custom sites and mobile apps via Payment Intent API. The help-center index does not list Shopify or Wix — [Ziina help: website integration](https://ziina.com/help-center/4007474-website-integration). Ziina does publish a blog post titled "payment gateway for Shopify" (content not read) — [Ziina blog](https://ziina.com/blog/payment-gateway-for-shopify)
- **Mamo**: has an e-commerce plugins page (contents not read) — [Mamo e-commerce plugins](https://www.mamopay.com/business/e-commerce-plugins)
- **Foloosi**: Magento/Adobe Commerce marketplace partner listing — [Adobe marketplace: Foloosi](https://marketplace.magento.com/partner/techfoloos252a)
- **N-Genius (Network International)**: Adobe Commerce plugin whose settings panel includes an onboarding link — [Adobe marketplace: N-Genius](https://commercemarketplace.adobe.com/networkinternational-ngenius.html)

### Inferences
- Telr and PayTabs are the "classic" plugin-rich gateways; Ziina and Mamo lean on links + API, with thinner plugin catalogues.

### Gaps
- No evidence retrieved on Wix, Zid or Salla support for any provider (Zid/Salla are KSA-centric).
- Plugin lists for Tap, Stripe UAE (Shopify does not use Stripe as a third-party gateway in most markets — not verified for UAE), PayPal, Checkout.com, APS, PayBy, Zbooni not retrieved.
- Mamo's and Ziina's Shopify support status unverified.

## API quality (docs, REST, sandbox without verification, webhooks, SDKs, hosted vs embedded, PCI scope)

### Takeaway
All providers with retrieved evidence publish public REST docs; sandbox access before business verification is confirmed only for N-Genius (free test account) and effectively Ziina (`test: true` flag, though an API key from an account is still needed), while Mamo explicitly requires a verified account before sandbox access and APS requires emailing support for a test account.

### Cited Findings
**Ziina (official)**
- Payment Intent API; pass `test: true` when creating a payment intent, completes with any card number/expiry/CVV, no money charged — [Ziina docs: create payment intent](https://docs.ziina.com/api-reference/payment-intent/create)
- No separate sandbox host: test and live share the same API — [Ziina docs: code companion](https://docs.ziina.com/code-companion.md)
- Webhooks: only one event, `payment_intent.status.updated`; registered via `POST /webhook`; one URL per account — [Ziina docs: webhook](https://docs.ziina.com/api-reference/webhook). A third-party guide also mentions `refund.status.updated` (conflicts with official docs) and says deliveries are unsigned unless a secret is set — [Hookdeck guide](https://hookdeck.com/webhooks/platforms/guide-to-ziina-webhooks-features-and-best-practices)

**Mamo (official)**
- REST/JSON API documented on readme.io — [Mamo API reference](https://mamopay.readme.io)
- Each merchant gets its own sandbox (replica of production with dashboard and API key) at sandbox.dev.business.mamopay.com, but "before accessing the sandbox, ensure that your account has been verified" — [Mamo help: API integrations](https://help.mamopay.com/en/articles/7234144-api-integrations)
- Production base path business.mamopay.com/manage_api/v1; APIs for Payment Links, Invoices, Payouts — [apis.io Mamo](https://apis.io/providers/mamo/)
- Mamo uses Stripe and Checkout.com as underlying processors (historical press) — [Stripe customer story](https://stripe.com/nz/customers/mamo), [Checkout.com newsroom](https://www.checkout.com/en-ae/news/uae-leading-business-payments-startup-mamo-selects-checkout-com-to-fuel-startup-growth-across-mena)

**Paymob UAE (official)**
- UAE environment at uae.paymob.com; API/SDK integration needs API secret key, public key, integration IDs and a webhook endpoint; Postman collection and developer reference available — [Paymob docs index](https://developers.paymob.com/paymob-docs/llms.txt)
- Payment-link endpoints (`/api/ecommerce/payment-links`, create and cancel) per a third-party mirror of the OpenAPI spec — [apis.io Paymob Payment Links](https://apis.io/apis/paymob/paymob-payment-links-api/)

**Telr (official docs)**
- Mobile SDKs for Android, iOS, React Native (npm; iOS 15.1+), Flutter — [Telr docs: Flutter](https://docs.telr.com/reference/flutter), [Telr docs: React Native](https://docs.telr.com/reference/react-native)
- Live vs test mode is determined by the API key used — [Telr API reference overview](https://telr-docs.cashfree.com/api-reference/overview)
- Legacy XML request API still documented — [Telr docs: XML request layout](https://docs.telr.com/reference/xml-request-layout)
- A developer blog says Telr "is fine; just expect to spend more time on the integration and account setup" (single-author opinion) — [dev.to](https://dev.to/erum_qamar_3a765d76797704/how-to-integrate-uae-payment-gateways-telr-paytabs-into-an-app-4fic)

**PayTabs (official docs)**
- Hosted Payment Page: transaction API returns a redirect link; "ideal for merchants with PCI SAQ A or those without any PCI levels" — [PayTabs HPP](https://docs.paytabs.com/PT2-API-Endpoints/Integration-Types-Manuals/Hosted-Payment-Page/HPP-Step-1-Setup-and-Activate-Integration-Method/HPP-Step-1-Landing)
- Managed Form (paylib.js) requires SAQ A-EP — [PayTabs Managed Form](https://docs.paytabs.com/PT2-API-Endpoints/Integration-Types-Manuals/Managed-Form/Managed-Form-Step-3-Initiating-Payment/Managed-Form-Initiating-Payment)
- Direct payment methods (higher PCI) also exist; UAE request domain https://secure.paytabs.com/payment/request — [PayTabs PT2 intro](https://docs.paytabs.com/PT2-API-Endpoints/Introduction/)
- Node.js backend package `paytabs_pt2` with `createPaymentPage()` incl. iFramed pages — [PayTabs NodeJS docs](https://docs.paytabs.com/Backend-Web-Packages/NodeJs/NodeJS-Step-3-Initiating-the-payment/Initiating-The-Payment)

**Tap Payments (official)**
- Public developer docs with marketplace, charges and settlement sections — [Tap developer docs](https://developers.tap.company/docs/overview). A Ziina-authored comparison says Tap offers APIs and sandbox environments (competitor source) — [Ziina blog](https://ziina.com/blog/top-payment-gateways-UAE)

**Network International N-Genius Online (official)**
- "Sign up for a free test account to access your API keys and sandbox environment"; Postman collections; sandbox base api-sandbox.network.global; service-account API key + outlet reference — [Network developer portal](https://developer.network.global/), [network.ae developers](https://www.network.ae/en/merchant-solutions/ecommerce-payments/developers)
- A Payouts API Postman collection for UAT exists — [Postman: N-Genius Payouts](https://documenter.gw.postman.com/view/metadata/2sB34eKNWp)

**Amazon Payment Services (official)**
- Separate test endpoint sbcheckout.payfort.com; to get a test account you email merchantsupport-ps@amazon.com; UAE test card listed — [APS docs: getting started with testing](https://paymentservices.amazon.com/docs/EN/12.html)

**PayBy / Botim (official)**
- Botim Money developer docs: create-order API returns an order session token; supports redirect URL, optional customer ID for saved cards, and splitting a payment across other Botim member IDs / phone numbers — [Botim Money developers: createOrder](https://developers.botim.money/docs/createorder)

**Foloosi**
- Flutter plugin `foloosi_plugins` v1.0.16 on pub.dev (unverified uploader; no Swift Package Manager support); older community packages `foloosi_flutter_payment`, `foloosi_payment` — [pub.dev foloosi_plugins](https://pub.dev/packages/foloosi_plugins/changelog), [pub.dev foloosi_flutter_payment](https://pub.dev/packages/foloosi_flutter_payment)

**Stripe UAE (official)**
- Stripe publishes a UAE-specific developer getting-started guide — [stripe.dev blog](https://stripe.dev/blog/getting-started-with-stripe-in-the-uae)

### Inferences
- Lowest PCI burden paths (redirect/hosted = SAQ A) are available at least from PayTabs (explicit) and by design from Ziina (hosted payment intent redirect) and Mamo links; PayTabs is the only provider whose docs were found mapping integration types to SAQ levels explicitly.
- Ziina's API is deliberately minimal (one webhook event, one webhook URL per account) — easy to start, limited for complex flows.
- Stripe test mode is generally usable before activation (general Stripe behaviour; not verified against a UAE-specific page in this session).

### Gaps
- Checkout.com: Flow/Frames SDKs, sandbox self-signup and webhooks not retrieved (only KYB page found).
- PayPal UAE and Payoneer: API/Checkout availability for UAE accounts not retrieved.
- SDK matrices (web/iOS/Android/Flutter/RN) not retrieved for Ziina, Mamo, Paymob, Tap, N-Genius, Stripe, Checkout.com, APS, PayBy.
- Whether Ziina webhooks fire for test-mode payments: not stated officially.
- Whether Telr, PayTabs, Paymob, Tap sandbox keys can be obtained before KYC: not confirmed.
- GitHub SDK repo health (stars, last commit) not checked for any provider.

## Platform/marketplace capabilities (splits, sub-merchant onboarding, payout APIs, onboarding unlicensed individuals)

### Takeaway
Tap, Telr, Mamo, Checkout.com and Stripe Connect all have split/sub-merchant constructs, but every one for which evidence was found requires each sub-merchant to pass its own KYC/KYB — Telr and Stripe explicitly require a trade licence per sub-merchant. No provider was found that officially lets a platform onboard unlicensed individual sellers under its own account; payout APIs (Mamo, N-Genius, Stripe Global Payouts) are the nearest workaround for paying individuals.

### Cited Findings
- **Stripe (official)**: Connect in the UAE requires connected account holders to have a valid UAE trade licence; individuals without one are not supported — [Stripe support: Connect availability in the UAE](https://support.stripe.com/questions/connect-availability-in-the-uae) (page undated). Jan 2026 changelog adds the UAE as a Global Payouts cross-border destination (AED wire; recipient form asks for company name/address) — [Stripe changelog 2026-01-28](https://docs.stripe.com/changelog/clover/2026-01-28/cross-border-payouts-new-countries), [Stripe recipient creation options](https://edge-docs.stripe.com/global-payouts/recipient-creation-options). Self-serve Connect cross-border payouts are limited to listed regions — [Stripe docs: cross-border payouts](https://docs.stripe.com/connect/cross-border-payouts). Stablecoin payouts list the UAE as a recipient country for individuals but only US platforms can use them — [Stripe docs: crypto payouts](https://docs.stripe.com/crypto/crypto-payouts)
- **Telr (official, page fetched)**: Split Payment feature; merchant adds sub-merchants (name, email, split type percentage/flat, payout interval weekly/monthly, payout minimum); the sub-merchant then supplies bank details (IBAN, SWIFT), company type, Emirates ID, bank statement, **trade licence**, visa and passport. Payout fees are deducted from the primary merchant's settlement — [Telr docs: onboarding sub-merchants](https://docs.telr.com/reference/onboarding-submerchant)
- **Tap Payments (official)**: Marketplace product: each business gets a `destination_id`; charges can be split across multiple businesses with a marketplace fee; two key sets (Marketplace keys for onboarding, Merchant keys for processing); each business passes KYC; payments can be accepted immediately after onboarding but payouts are unavailable while the business is pending approval — [Tap docs: marketplace overview](https://developers.tap.company/docs/marketplace-overview), [Tap docs: marketplace getting started](https://developers.tap.company/docs/marketplace-getting-started). Tap holds a UAE Central Bank payment services licence — [The Paypers](https://thepaypers.com/payments/news/tap-payments-gains-uae-licence-achieves-full-gcc-fintech-compliance). Partner case: Sterve used Tap onboarding APIs for 4,000+ micro-SMB customers — [Tap blog](https://blog.tap.company/sterve-partners-with-tap-for-smb-customers)
- **Mamo (official)**: Marketplace payments via recipients and disbursements; Payouts API for bulk payouts and status checks; payouts to UAE accounts and limited international accounts (US, UK, EU, subject to eligibility); international payout fee listed as 0.5% — [Mamo help: marketplace payments](https://help.mamopay.com/en/articles/13571079-marketplace-payments-with-mamo), [Mamo help: marketplace integration guide](https://help.mamopay.com/en/articles/16211593-marketplace-integration-guide), [Mamo payouts](https://www.mamopay.com/business/payouts), [Mamo marketplaces](https://www.mamopay.com/industry/marketplaces)
- **Checkout.com (official)**: Platforms product with sub-entities; "a sub-entity cannot process payments or receive payouts until it has passed the verification checks" — [Checkout.com docs: collect verification information](https://www.checkout.com/docs/platforms/onboard-sub-entities/collect-verification-information). Holds a UAE Central Bank acquiring licence covering merchant acquiring, payment aggregation and cross-border transfers — [Checkout.com newsroom](https://www.checkout.com/en-ae/news/checkout-com-becomes-the-first-global-payments-platform-to-secure-acquiring-license-from-the-uae-central-bank)
- **N-Genius**: Payouts API exists (UAT Postman collection) — [Postman](https://documenter.gw.postman.com/view/metadata/2sB34eKNWp)
- **PayBy/Botim**: create-order API supports splitting a payment to other Botim member IDs or phone numbers — [Botim Money developers](https://developers.botim.money/docs/createorder)
- **Third-party claim**: Polar (merchant of record) says it pays UAE sellers via Stripe Connect Express — [Polar docs](https://polar.sh/docs/merchant-of-record/supported-countries)

### Inferences
- A UAE platform wanting to pay unlicensed gig workers/individual sellers cannot use sub-merchant onboarding at Telr or Stripe; the realistic pattern is platform-as-merchant-of-record collecting funds and using a payout API (Mamo Payouts, N-Genius Payouts) to pay individuals' bank accounts. Whether those payout products accept individual (non-business) beneficiaries without the platform holding its own licence/authorisation was not confirmed — this is a regulatory question as much as a product one.
- Botim's split-to-phone-number feature is the only found mechanism that natively targets individuals (wallet holders), but documentation depth and eligibility are unknown.

### Gaps
- Mamo marketplace help articles returned 403 on fetch: KYC requirements for marketplace recipients (individual vs. licensed) not verified.
- Tap UAE-specific KYC documents for sub-businesses (trade licence required?) not retrieved.
- Ziina, Paymob UAE, PayTabs, Foloosi, Zbooni, APS, PayPal, Payoneer marketplace/split/payout capabilities in the UAE not retrieved (Payoneer mass payouts and PayPal Payouts exist globally; UAE specifics unverified).
- No provider statement found permitting onboarding of unlicensed individuals under a platform's account.

## Typical time from signup to first live payment

### Takeaway
Reliable official timelines were not found; third-party (often competitor- or affiliate-written) sources suggest minutes for link products (Paymob PayMe, Ziina personal), ~24 hours for Mamo, under a day to 3–5 business days for PayTabs, and 3–5 business days for Telr and Tap, with self-contradictory figures for Tap.

### Cited Findings
- Paymob: payment links/PayMe "time to launch: minutes" (official docs table; presumably post-account approval) — [Paymob docs index](https://developers.paymob.com/paymob-docs/llms.txt)
- Mamo: partner press release advertises onboarding "in just 24 hours" for SMEs — [Checkout.com newsroom](https://www.checkout.com/en-ae/news/uae-leading-business-payments-startup-mamo-selects-checkout-com-to-fuel-startup-growth-across-mena)
- Telr: 3–5 business days for a UAE free-zone startup with trade licence and bank account (third-party blog) — [Dubai South Business Hub blog](https://www.dubaisouthbh.com/blogs/payment-gateway-uae)
- PayTabs: "setup can be completed in under one day" per one source; "most accounts approved within 3–5 business days" per another, whose own comparison table is inconsistent — [checkoutflow PayTabs UAE](https://checkoutflow.pages.dev/ae-en/best-payment-gateway-ecommerce/paytabs-uae/), [checkoutflow best gateway UAE](https://checkoutflow.pages.dev/ae-en/best-payment-gateway-ecommerce/best-payment-gateway-uae)
- Tap: 3–5 days per one comparison, which elsewhere on the same page says 7–14 days (self-contradictory, competitor-authored) — [Dodo Payments blog](https://dodopayments.com/blogs/tap-payments-alternatives)
- Stripe UAE: requires a UAE-issued trade licence or freelancer permit, Emirates ID for UAE-resident owners/managers, MoA for LLCs/FZ-LLCs, all licence managers added as Controllers, and a supported UAE bank account; common rejection causes are expired licence, non-UAE licence, name mismatch, emirate mismatch, representative missing from licence — [Stripe: UAE business verification requirements](https://support.stripe.com/questions/uae-business-verification-requirements), [Stripe: troubleshooting UAE verification](https://support.stripe.com/questions/troubleshooting-uae-verification-issues), [Stripe: supported UAE banks](https://support.stripe.com/questions/supported-banks-in-the-uae)
- Checkout.com: KYB requires business info, UBOs ≥25%, and for UAE-registered merchants a tax registration number — [Checkout.com support: verify your business](https://support.checkout.com/hc/en-us/articles/15455000851346-Verify-your-business)
- PayPal UAE: business account needs a UAE trade licence or freelance permit plus ID (third-party guide, not PayPal) — [UAE Expert Hub](https://www.uaeexperthub.com/paypal-available-uae-guide/)
- Zbooni: licensing partner says a trade licence is needed to activate a Zbooni seller account and that no UAE business bank account is needed (partner marketing, not Zbooni terms) — [Meydan Free Zone blog](https://www.meydanfz.ae/blog/how-to-sell-on-zbooni)

### Inferences
- Stripe's acceptance of a "freelancer permit" makes it viable for licensed freelancers, but not for wholly unlicensed sellers.
- Timelines are dominated by KYB document readiness (trade licence, Emirates ID, UAE bank account), not technical integration.

### Gaps
- No official onboarding SLA found for any provider. No timelines at all for Ziina Business, N-Genius, APS, Checkout.com, Foloosi, PayBy, Zbooni, PayPal, Payoneer, Stripe.
- Checkout.com and APS minimum-volume expectations for small merchants not found.

## Ongoing compliance overhead (re-KYC, reserves, document requests, restricted categories)

### Takeaway
Stripe documents its UAE re-verification cycle precisely (renew trade licence/Emirates ID on expiry or charges and payouts pause); Ziina and Mamo terms/responses indicate post-termination or risk holds of 45–120 days (Ziina) and holds "until dispute risk disappears", with users reporting up to 540 days (Mamo, Tap).

### Cited Findings
**Official / provider-stated**
- Stripe UAE continuous verification: trade licence/freelancer permit and Emirates ID of every UAE-resident owner/manager must be renewed on expiry; Stripe emails the month after expiry with a deadline at end of that month; during the grace period payments and payouts continue; if missed, charges and payouts are paused — [Stripe: continuous verification in the UAE](https://support.stripe.com/questions/continuous-verification-in-the-uae) (fetched 22 Nov 2026)
- Ziina: per an independent review citing Ziina's FAQ, Ziina may hold funds 45–120 days when it terminates an account to cover chargebacks/disputes; normal cash-out is once a day, within two business days for amounts under AED 25,000; terms allow longer settlement periods — [paymentreview.com Ziina review](https://paymentreview.com/reviews/ziina/)
- Ziina's Trustpilot replies: "as a regulated financial institution, account reviews and any temporary holds on funds follow strict compliance requirements" — [Trustpilot: ziina.com](https://www.trustpilot.com/review/ziina.com?page=1)
- Mamo's Trustpilot reply: it holds pending funds when there is high dispute risk until that risk disappears, calling it standard industry practice — [Trustpilot: mamopay.com](https://br.trustpilot.com/review/mamopay.com)
- Mamo settlement: 2–5 days per one comparison; 5 business days with same-day at a surcharge on the Growth plan (2.9% + AED 1) per another (third-party/competitor sources) — [Ziina blog: top gateways](https://ziina.com/blog/top-payment-gateways-UAE), [Startupik](https://startupik.com/cheapest-uae-payment-gateway-for-startups/)
- PayTabs Trustpilot replies: a suspension was attributed to missing documents and lifted once provided; funds are held until all pending client documents are submitted — [Trustpilot: paytabs.com](https://au.trustpilot.com/review/paytabs.com)
- Foloosi: settlement three times a week (Mon/Wed/Fri) and a settlement fee (third-party FAQ listing) — [kredit.ae Foloosi](https://kredit.ae/business/foloosi). Foloosi reported to have secured UAE Central Bank in-principle approval for a Category III Retail Payment Services licence (24 Sep 2026) — [FF News](https://ffnews.com/companies/foloosi)
- PayPal UAE: balance is not held in AED; withdrawal to a UAE bank is converted at PayPal's rate, with a ~3% conversion fee claimed (third-party guides, unverified) — [Noble Core Ventures](https://noblecoreventures.com/paypal-uae-2026/), [UAE Expert Hub](https://www.uaeexperthub.com/paypal-available-uae-guide/). PayPal generally states holds depend on account tenure, activity, business type, disputes; limitations triggered by e.g. sudden changes in selling activity (non-UAE PayPal pages) — [PayPal account limitations](https://www.paypal.com/au/brc/article/account-limitations)
- Tap marketplace: sub-businesses can accept payments before approval but cannot receive payouts until approved — [Tap docs](https://developers.tap.company/docs/marketplace-getting-started)

### Inferences
- Annual licence renewal is a recurring compliance event for every licensed UAE merchant across providers; Stripe is simply the most transparent about it.
- The 540-day figure appears in user reports for both Mamo and Tap, suggesting a common contractual maximum hold tied to card-scheme chargeback windows (inference; contract text not retrieved).

### Gaps
- Rolling-reserve percentages, proof-of-delivery/invoice request practices, volume triggers and restricted-category lists were not retrieved from official terms for any provider.
- PayPal's 21-day new-seller hold was not confirmed for UAE accounts.
- Nothing retrieved for N-Genius, APS, Checkout.com, Paymob, Telr, Zbooni, PayBy, Payoneer ongoing-compliance terms.

## User-reported problems in 2025–2026 (ANECDOTAL)

### Takeaway
All review data below is self-selected, unverified and anecdotal. The clearest recurring pattern is long fund holds after account review/closure: Tap Payments has the worst Trustpilot profile found (1.7/5, 154 reviews; multiple 540-day hold reports), Ziina has a good overall score (4/5, ~550 reviews) but repeated 2026 reports of 45–180-day holds and bot-only support, and Mamo has reports of holds extended to 540 days. No Reddit, App Store or Google Play content could be retrieved.

### Cited Findings (all anecdotal unless noted)
- **Ziina** — Trustpilot TrustScore 4/5, 517–554 reviews depending on snapshot (Jan–Jun 2026 pages). 2026 reviewers report: business account under review/hold for months; "over 300K AED" held for 180 days; funds on hold 45–120 days after deactivation; documents submitted with no response; "you can only talk to a robot". Some reviews praise support. — [Trustpilot: ziina.com p1](https://www.trustpilot.com/review/ziina.com?page=1), [p3](https://www.trustpilot.com/review/ziina.com?page=3), [p5](https://www.trustpilot.com/review/ziina.com?page=5)
- **Mamo** — Trustpilot: a ~March 2026 1-star review reports funds held at least six months with possible extension to 540 days; another reviewer says a hold was released after checks; one praises easy integration and quick support — [Trustpilot CA: mamopay.com](https://ca.trustpilot.com/review/mamopay.com), [Trustpilot CH](https://ch.trustpilot.com/review/mamopay.com). G2: 3.3/5 from only 3 reviews; one describes a UAE business suspended Oct 2025, funds frozen without notice, hold extended to 540 days citing "material risk of chargebacks or regulatory exposure" — [G2: Mamo](https://www.g2.com/products/mamo-pay-for-business/reviews). Capterra: 4.3/5 from 15 verified reviews, high ease-of-use — [Capterra: Mamo Business](https://www.capterra.com/p/275441/Mamo-Pay-for-Business/reviews/)
- **Tap Payments** — Trustpilot 1.7/5 from 154 reviews (another regional page shows TrustScore 2/5); multiple reports of balances held 540 days after account closure, incl. "$9000 frozen for 540 days"; replies are mostly brief apologies — [Trustpilot: tap.company](https://uk.trustpilot.com/review/tap.company), [p2](https://www.trustpilot.com/review/tap.company?page=2). Reviews are GCC-wide, not UAE-specific. (Note: tap.global on Trustpilot is a separate listing and likely a different company — do not conflate.)
- **PayTabs** — Trustpilot 3/5 from ~277 reviews; complaints of slow support and onboarding delays; company replies cite missing documents — [Trustpilot: paytabs.com](https://at.trustpilot.com/review/paytabs.com). Reviews are multi-country.
- **Network International** — an aggregator reports ~1.9/5 on Trustpilot from only 14 reviews, with allegations of abrupt contract termination, retention of reserve funds, settlement delays, fee opacity; also praise for dedicated support. Aggregator's own confidence scores are low (25–37%) — [rfp.wiki: Network International](https://www.rfp.wiki/payments-fraud/network-international)
- **Payoneer** — global (not UAE-specific) 2026 Trustpilot complaints: balances frozen weeks/months with no reason (e.g. $612 frozen since 16 Apr 2026, risk team silent 20 days); account blocked while linking Fiverr; G2 3.2/5 across 370 reviews with account blocking among top dislikes; Capterra UAE listing echoes poor support — [Trustpilot: payoneer.com](https://uk.trustpilot.com/review/www.payoneer.com?page=4), [G2: Payoneer](https://www.g2.com/products/payoneer/reviews), [Capterra UAE: Payoneer](https://www.capterra.ae/reviews/187086/payoneer)
- **Foloosi** — no independent reviews found (SaaSWorthy listing has zero reviews) — [SaaSWorthy: Foloosi](https://www.saasworthy.com/product/foloosi)
- **Independent reviewer caution on Ziina**: a business whose cash flow cannot absorb a frozen balance for weeks or months should look elsewhere — [paymentreview.com](https://paymentreview.com/reviews/ziina/)

### Inferences
- Fund-hold risk is not provider-specific: licensed-fintech aggregators (Ziina, Mamo, Tap) all show the same failure mode — account review → closure → multi-month hold with limited human support. For micro-merchants this is the main operational risk, more than integration difficulty.
- Positive averages (Ziina 4/5, Mamo Capterra 4.3) coexist with severe tail cases; averages understate the severity of the tail.
- Trustpilot samples for Tap and PayTabs are GCC-wide and skew toward aggrieved merchants; sample sizes for N-Genius (14) and Mamo G2 (3) are too small to generalise.

### Gaps
- Reddit r/dubai and r/UAE threads: none surfaced in any search (search tool did not return reddit.com results). App Store / Google Play review text and ratings: not retrieved for any provider.
- No user-report data retrieved for Telr, Paymob UAE, Zbooni, PayBy/Botim, Stripe UAE (freeze/closure anecdotes), PayPal UAE, Checkout.com, Amazon Payment Services.
- No regulator (CBUAE / Sanadak ombudsman) complaint statistics found to corroborate anecdotal patterns.
- Review counts and scores are from search snapshots during 2026; exact values on 22 Nov 2026 were not re-verified page by page.

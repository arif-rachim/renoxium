# Make it yours: services, prices, stylists, hours and contact

**Goal:** the owner replaces the template's sample content with their own in one or two messages.
**Length:** about 2:00. **Starts on:** the chat from lesson 3, with the Noor Beauty Studio preview made.

## Shots

| # | Screen | What happens | ~s |
|---|---|---|---|
| 1 | ChatGPT | The message below is typed and sent | 25 |
| 2 | ChatGPT | "Allow once"; the activity runs: read the app in parts, then exact changes; waiting cut | 25 |
| 3 | ChatGPT | The answer lists what changed; the chat follows | 15 |
| 4 | ChatGPT | The second message (Arabic texts) is typed and sent; activity; answer | 25 |
| 5 | Preview tab | The services and prices, the stylists, the hours, the address and WhatsApp; then the Arabic version | 20 |
| 6 | ChatGPT | Back in the chat: no credits, change as often as you like | 4 |

## Messages typed into ChatGPT (word for word)

1. `Now make it mine. Services: Haircut & Blow-dry 60 min AED 160, Keratin Treatment 120 min AED 650, Gel Manicure 45 min AED 120, Lash Lift 60 min AED 220. Stylists: Noor (hair), Hana (nails and lashes). Open 10am to 9pm, closed Fridays. Address: Shop 3, Al Barsha 1, Dubai. WhatsApp +971 50 123 4567.`
2. `Also check the Arabic version of the site uses the same services and stylists, with Arabic names.`

## What the assistant should do and reply (short)

- **Read the app in parts:** get_app_outline and get_app_parts (the services, staff, business and texts seed rows).
- **Change exactly that:** edit_app on the seed rows (services, staff, business opens/closes/closed_days, address,
  whatsapp), after "Allow once". It does not rewrite the templates.
- **Check the pages** (the checks run after each change; it may also use view_page).
- **Reply** with a short list of the changes and the preview link; nothing is live.

## On-screen captions

| When | English | Arabic (needs a native speaker's check) |
|---|---|---|
| 1 | Say it the way you'd tell a new receptionist | قلها كما تخبر موظف استقبال جديداً |
| 2 | Renoxium reads your site and changes only what you asked | يقرأ Renoxium موقعك ويغيّر ما طلبته فقط |
| 5 | Your services, prices, stylists and hours | خدماتك وأسعارك وفريقك ومواعيدك |
| 6 | No credits: change it as often as you like | بلا أرصدة: غيّر ما تشاء متى تشاء |

## Narration

- 1: "Tell ChatGPT your services, prices, stylists and opening hours, the way you'd tell a new receptionist."
- 2: "Renoxium reads your site in parts and changes exactly what you asked."
- 5: "Here are your own services and prices, your team, and your hours, in English and in Arabic."
- 6: "There are no credits. Change it as often as you like."

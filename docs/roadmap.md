# Renoxium roadmap

Renoxium is one place behind every channel a UAE small business sells through: its own website, its customers'
AI assistants and WhatsApp. Orders, bookings and payments from all of them land in the same app, admin panel and
calendar.

| Channel | Today | Next |
| --- | --- | --- |
| Website | Customers browse, order, book and pay (cards, cash on delivery) | — |
| Customers' AI assistants (ChatGPT, Claude) | Per-app public MCP tools: find products, check free times, get the link to book or pay | Book and pay without leaving the chat (agentic checkout), see below |
| WhatsApp | Customers and owners sign in with a WhatsApp code (owners can sign up with only a number); owners get a WhatsApp message for each order | Customers order and book by chatting with the business's own WhatsApp number (this document's first item) |

## 1. WhatsApp ordering (next)

**Goal:** a customer messages the business's own WhatsApp number ("Do you have Royal Oud 100 ml?", "Haircut tomorrow
after 5?"), gets answers from the app's real data, and ends with a confirmed order or booking and, where needed, a
payment link. The order or booking appears in the admin panel and calendar exactly like one made on the website.

**Done when:**
- An owner connects their own WhatsApp Business number from the admin panel, without contacting Renoxium.
- Incoming messages reach the app; the app answers with product, price, stock and free-time information from its own
  tables, in English or Arabic (the customer's language).
- A customer can complete an order or booking in the chat; card payments use the existing `payments.checkout` link,
  cash on delivery and pay-at-venue need no link.
- The owner sees the conversation next to the order and can take over the chat by hand at any time.
- Nothing is sent outside WhatsApp's rules (24-hour customer service window; template messages only for approved
  templates), and every message counts toward the plan's WhatsApp quota.

**Approach (to be planned in detail):**
1. **Merchant numbers.** Today Renoxium sends from its own number. Ordering needs each business's own number:
   become a Meta Tech Provider and use Embedded Signup so the owner links their WhatsApp Business account from the
   admin panel; store the number ID and token per app, encrypted like `http.fetch` secrets.
2. **Inbound webhook.** A signed Meta webhook (one endpoint for all apps, routed by number ID) stores each message in
   a per-app conversation table and runs an `on_whatsapp` script, the same pattern as `on_payment` and
   `on_admin_change`. Scripts reply with a new `whatsapp.reply({to, text | buttons | list})` inside the 24-hour window.
3. **Ready-made flows in the templates.** `store-uae`, `booking-uae`, `home-service-uae` and `reservation-uae` get an
   `on_whatsapp` script: structured menus (WhatsApp list and button messages) for browse, pick, choose a time,
   confirm, then a payment link or cash. This works without any AI and is predictable.
4. **Free-text understanding (optional, later).** Let the app hand free-text questions to an AI model with the app's
   public MCP tools, so "anything for a gift under 300?" works. Needs a cost model per message and clear limits.
5. **Owner inbox.** An admin-panel page per conversation with the customer, linked orders and bookings, and a
   "take over" switch that stops automatic replies.

**Depends on / risks:**
- Meta business verification and Tech Provider approval (the WhatsApp approval for sign-in codes is still pending).
- Per-conversation WhatsApp pricing in the UAE: decide whether it comes from the plan's quota or is billed on.
- Spam and abuse on public numbers: rate limits per sender, no payment action without the customer's confirmation.
- PDPL: conversations are personal data; retention period, export and deletion like other app data.

## 2. Book and pay inside AI assistants (after WhatsApp ordering)

Today a customer's assistant finds products and free times through the app's public MCP tools and hands over a link.
Next: let the assistant create the order or booking (with the customer's confirmation) and complete payment through
the agentic commerce protocols that ChatGPT and other assistants support, once they are available in the UAE.
Until then, the link stays the safe default.

## Not planned

- Instagram DM ordering: revisit after WhatsApp ordering, using the same conversation and `on_*` script model.

# Video course: Build your business app by chatting with ChatGPT

Short egghead-style lessons (one task each, 1–3 minutes, no intro or recap) for small business owners in the UAE.
They are shown on renoxium.com (landing hero, template cards, connect page) and on Instagram, TikTok and WhatsApp status.
Each lesson folder holds `script.md`, `summary.md` and, after recording, `captions.en.vtt` and `captions.ar.vtt`.
Video files are not kept in git.

The course follows one business: **Noor Beauty Studio**, a women's salon in Al Barsha, Dubai. It then adds a shop, a clinic
queue and an app written from scratch. The booking template's sample business is "Lamsa Beauty Lounge, Jumeirah". The course
uses a different name so viewers see their own business replace the sample.

## Step 0: what was checked (2026-11-23)

- **Free ChatGPT works.** A free ChatGPT account adds Renoxium under **Plugins → Add → Add custom MCP server** without
  developer mode, and every tool runs, including the write tools after "Allow once". This was checked on the reviewer
  account, which is on the free plan.
  - OpenAI's help page still lists custom MCP servers for Business/Enterprise/Edu only ([help article](https://help.openai.com/en/articles/12584461-developer-mode-apps-and-full-mcp-connectors-in-chatgpt-beta)).
  - The connect page was updated to the Plugins steps (branch `docs/chatgpt-free-plugins`).
  - Renoxium is not listed in the public ChatGPT plugin directory. It is added as the owner's own plugin.
- **Choose Renoxium once.** After it is installed, ChatGPT calls Renoxium by itself for questions about the owner's apps, even
  in a new chat that doesn't name it. Typing **@Renoxium** or choosing it from **+** forces it when ChatGPT answers
  without acting.
- **Photos from the chat:** `upload_file` takes only `data_base64`. ChatGPT can pass attached files to tools that declare
  `openai/fileParams` ([reference](https://developers.openai.com/plugins/reference)), and Renoxium does not declare it.
  So lessons 5 and 17 show the admin-panel upload as the way that works. A chat upload is shown only if Renoxium adds
  `fileParams` support first.
- **Tools:** all 22 tools in the table below are registered in `internal/mcpserver/mcpserver.go`. None were added or removed.

## Recording setup

- **ChatGPT parts** are recorded by the agent in the owner's signed-in Chrome, using the reviewer ChatGPT account with
  Memory and chat history references turned off and old test chats deleted. ChatGPT is never faked.
- **Renoxium parts** (admin panel, templates, demos, preview) are recorded on live renoxium.com with the course's own test
  apps only, so no real customer data appears. The template demo banner is hidden.
- **Output per lesson:**
  - web: 1920×1080, narrated, no captions in the picture, with `.vtt` files;
  - social: 1080×1920, narrated, captions burned in.
  - The Arabic captions need a native speaker's check.
- ChatGPT sometimes shows two answers side by side ("Which option do you prefer?"). That take is skipped and recorded again.
- Tools: `renoxium-video` (`rx.mjs`, scenes `ask`, `site`, `open`; `build` makes both videos and the captions).

## Lessons

### A. Getting started
1. **Add Renoxium to ChatGPT.** Plugins → Add → Add custom MCP server, sign in, Allow. Choose Renoxium once in a chat
   (**+** or **@Renoxium**) and ask "What apps do I have?" (list_apps).
2. **What can Renoxium build for me?** The assistant lists the templates and shows one (get_example).

### B. Build the salon site
3. **Build a salon booking site from one message.** It uses create_app from the booking template, view_page, and
   screenshot_preview on a phone. ChatGPT may also read and rename the sample business (get_app_outline, get_app_parts,
   edit_app); the recording keeps whatever it does.
4. **Make it yours.** The name stays Noor. The lesson changes services and prices, stylists, opening hours, address, WhatsApp
   and the Arabic texts (get_app_outline, get_app_parts, edit_app). Point out: no credits, change as often as you like.
5. **Photos and logo.** This lesson uses the admin panel (Data → the services table → a row → Image, and Business → Logo)
   with files named plainly (`noor-logo.png`, `haircut.jpg`). upload_file is shown in the chat only once chat uploads
   work (see Step 0).
6. **Make it look good, without AI slop.** The assistant restyles in css_extra (patch_app), reads the result back (get_app),
   and checks it with screenshot_preview at `mobile`, `tablet` and `desktop`. Then one round of feedback ("the header feels
   crowded on the phone") and the fix.
7. **Test it like a customer:** create_test_visitor, a booking with submit_form, upload_as_visitor, and view_page
   as that visitor (my bookings).
8. **Test what the owner does.** The owner confirms, moves and cancels a booking with change_preview_row, which runs the
   admin hooks. The evening schedule and reminders run once with try_scheduled_jobs.
9. **Publish.** publish returns a confirm link, the owner opens it and presses Publish, and the assistant checks the
   live site (view_live_page). Undo: publish with an earlier `version` (the confirm page says "Roll back").

### C. Run the business: the admin panel (no chat; names as in the sidebar)
10. **Overview:** the tabs Overview / Traffic and payments / Status. They cover visitors and page views, card sales, where
    visitors come from, latest payments, the "Your data" panel, scheduled jobs, addresses and recent errors.
11. **Data:** a table's list (search, sort, columns, pages), adding and editing a row with a photo, deleting, and the
    **List / Calendar** toggle (day and week, one lane per stylist, moving a booking).
12. **Payments and Plan** (two pages): connecting Stripe, refunding all or part, capturing or releasing a card hold,
    customer subscriptions. The Plan page offers Shop (AED 0 + 2% of card payments) or Work app (AED 99 a month per app,
    5 staff included, AED 25 per extra staff).
13. **Customers:** customer accounts, searching, deleting one with their uploaded files.
14. **Settings pages** (separate pages in the sidebar):
    - Notifications (email, WhatsApp);
    - Staff (invite, what staff can and cannot do, remove);
    - Domain;
    - Connections (allowing a site, API keys, test calls from the preview);
    - Rename app;
    - Delete app, with the **Download export** button on that page.
    - Also: the **Open preview** button and the publish page.

### D. When something goes wrong
15. **"Customers say booking fails."** Status tab → Recent errors. The assistant reads the logs (get_logs), fixes the app
    (edit_app), checks it (view_page, submit_form) and publishes it. The error is staged honestly on the course's own test
    app, from a change the owner asked for that broke a page.
16. **Other things that go wrong**, each shown briefly:
    - **A change breaks a page.** The change is saved to the preview and the checks report the broken page; the assistant
      fixes it before publishing. A change is *refused* only when the definition or a script is invalid; that case is
      shown with apply_change.
    - **The preview changed since the assistant read it.** edit_app with an old base version returns "base_version must be
      the newest version…". *Note: that message points to `get_app {only}`, which does not exist. It should be fixed first
      (`internal/platform/partial.go:235`).*
    - **The app name is taken**, or there are too many apps ("this account may have N apps…; for more, contact Renoxium").
    - **The confirm link was not opened**, so the change is still only on the preview.
    - **Card payments asked for before Stripe is connected** ("the owner has not connected Stripe yet").
    - **An outside site that is not allowed in Connections**, or test calls from the preview that are off (2 hours when
      turned on).
    - **A scheduled job that failed**, shown in red on the Status tab.

### E. Beyond the salon
17. **An online shop** from the store template, with products, variants, delivery fee, free delivery over an amount, cash
    on delivery and made-to-order dates.
    - Product photos from the computer, in the admin panel: Products → a product → Image, Photo 2, Photo 3 →
      save → seen on the live shop. Files are named `royal-oud.jpg` etc.
    - Product pages checked with view_page and screenshot_preview.
    - The chat way (upload_file plus edit_app or patch_app) is shown only once chat uploads work.
18. **A clinic queue (work app)** from the clinic template, with its queue jobs (try_scheduled_jobs).
19. **An app no template covers:** staff attendance, written from scratch (get_example with reference_only, apply_change,
    view_page, submit_form).
20. **Rename and delete an app by chatting** (rename_app, delete_app). Each one only gives a link the owner confirms with
    their password. After a rename, the old addresses redirect for a year, and an app can be renamed once every 30 days.

## Coverage table (every tool at least once)

| Tool | Lessons |
| --- | --- |
| list_apps | 1 |
| get_example | 2, 19 |
| get_app | 6 |
| get_app_outline | 4 (3) |
| get_app_parts | 4 (3) |
| get_logs | 15 |
| view_live_page | 9 |
| create_app | 3, 17, 18 |
| apply_change | 16, 19 |
| patch_app | 6 |
| edit_app | 4, 15, 16 (3) |
| upload_file | 5 or 17, only if chat uploads work (see Step 0) |
| publish | 9, 15 |
| rename_app | 20 |
| delete_app | 20 |
| view_page | 3, 7, 15, 17, 19 |
| submit_form | 7, 15, 19 |
| upload_as_visitor | 7 |
| create_test_visitor | 7 |
| screenshot_preview | 3, 6, 17 |
| try_scheduled_jobs | 8, 18 |
| change_preview_row | 8 |

(3) means the tool may also appear in lesson 3, depending on what ChatGPT does.

**Gap:** upload_file is covered only if ChatGPT can hand an attached image to Renoxium. If not, it needs `fileParams`
support in Renoxium, or the course leaves it out and says so.

## Open points before recording

- **Lesson 12.** Connecting Stripe and refunds happen on live renoxium.com with real Stripe. We need a Stripe test account,
  or the lesson is recorded against a local server with the e2e fakes.
- **Lesson 14.** Staff invites and notifications send real emails and WhatsApp messages, so use the course team's own
  addresses.
- **Lesson 16b.** Fix the `get_app {only}` message first.

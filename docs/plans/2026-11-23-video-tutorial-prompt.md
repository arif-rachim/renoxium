# Task: a short video course "Build your business app by chatting with ChatGPT"

You are working in the Renoxium repository (Go). Renoxium builds and hosts websites and apps for small
businesses in the UAE: the owner describes the business to ChatGPT or Claude, which uses the Renoxium
connector (MCP server at `https://www.renoxium.com/mcp`) to build the app on a private preview; the owner
checks it and publishes. Read `README.md`, `internal/web/connect.go` (the connect steps) and
`internal/mcpserver/mcpserver.go` (what the assistant can do) before planning anything.

## Goal

A course of short tutorial videos in the style of egghead.io, showing a small business owner how to build
and run their app **by chatting in the free version of ChatGPT**. The videos go on renoxium.com (landing
page, template cards, connect page) and on Instagram, TikTok and WhatsApp status.

## The egghead.io style, applied here

- One lesson = one task, 1 to 3 minutes (the admin panel tour and the error lessons may run to 5). No intro music, no "hi, welcome", no recap of earlier lessons:
  each lesson stands alone and starts on the screen where the task starts.
- The title is the task, as an action: "Build a salon booking site from one message",
  "Change a price by chatting", "Publish your site".
- Show, do not tell: the viewer sees the exact message typed, the assistant's reply and the result.
  Zoom in on what matters; cut waiting time (the assistant thinking, pages loading).
- Each lesson has a one-paragraph summary and the exact messages typed, to show under the video.
- Plain language for shop owners, not developers. No code on screen.

## The lessons: the course must cover everything

The course follows one business from start to finish (Lamsa, a salon in Jumeirah), then adds a shop, a
clinic queue and an app written from scratch. Together the lessons must use **every one of the 22 tools**
of the Renoxium connector, **every page of the owner's admin panel**, and show what happens when
something goes wrong. Keep the coverage table below true: if a lesson changes, move its tools to another.

### A. Getting started
1. **Add Renoxium to ChatGPT** (connect, sign in, allow), then ask "what apps do I have?" (list_apps).
2. **What can Renoxium build for me?** The assistant lists the templates and shows one (get_example).

### B. Build the salon site
3. **A booking site from one message.** create_app from the booking template; the assistant checks the
   pages (view_page) and shows the home page on a phone (screenshot_preview).
4. **Make it yours:** name, services and prices, stylists, opening hours, address, WhatsApp, Arabic
   texts. The assistant reads the app in parts (get_app_outline, get_app_parts) and changes exactly what
   was asked (edit_app). Point out: no credits, change as often as you like.
5. **Photos and logo:** the owner attaches pictures in the chat (upload_file) and they go on the site.
6. **Make it look good, without AI slop.** The owner asks for a refined look: a brand colour, a font,
   real spacing and hierarchy, no generic gradients, emoji or stock clichés. The assistant restyles in
   the css_extra template (patch_app), reads the result back (get_app), and checks it on a phone, a
   tablet and a desktop (screenshot_preview at each size). Show one round of feedback ("the header
   feels crowded on the phone") and the fix.
7. **Test it like a customer:** a test customer account (create_test_visitor), booking an appointment
   with the form (submit_form), uploading a file as a customer (upload_as_visitor), and the customer's
   bookings page while signed in (view_page as that visitor).
8. **Test what the owner does:** the owner confirms, moves and cancels a booking (change_preview_row,
   which runs the app's admin hooks: emails, refunds) and the evening schedule and reminders run once
   (try_scheduled_jobs).
9. **Publish:** the assistant gives a confirm link (publish); the owner opens it and confirms; the
   assistant checks the live site (view_live_page). Also: publishing an earlier version to undo.

### C. Run the business: the owner's admin panel (no chat; a tour of every page)
10. **Overview:** the three tabs (Overview, Traffic and payments, Status), visitors and page views, card
    sales, where visitors come from, latest payments, your data, scheduled jobs, addresses, recent errors.
11. **Your data:** a table's list (search, sort, choose columns, pages), adding and editing a row with a
    photo, deleting, and the calendar (day and week, one lane per stylist, moving a booking).
12. **Payments and plan:** connecting Stripe, refunding all or part, capturing or releasing a card hold,
    customer subscriptions, and the Plan page (shop with 2% of card payments, or work app per month).
13. **Customers:** customer accounts, searching, deleting one with their uploaded files.
14. **Settings:** notifications (email, WhatsApp), staff (invite, what they can and cannot do, remove),
    your own domain, connections (allowing a site, API keys, test calls from the preview), renaming,
    exporting and deleting an app. Also the Preview link and the publish page.

### D. When something goes wrong
15. **"Customers say booking fails."** The owner sees Recent errors on the Status tab and asks the
    assistant to check the logs and fix it: get_logs, a fix with edit_app, a check with view_page and
    submit_form, then publish. Stage the error honestly on the owner's own test app (for example a
    change the owner asked for that broke a page), never fake the screens.
16. **The other things that go wrong**, each shown briefly with what the owner sees and what to do:
    - the assistant's change is refused because a page would break (the checks after edit_app or
      apply_change), and the assistant fixes it before anything reaches the preview;
    - the preview changed since the assistant read it (edit_app with an old base version);
    - the app name is taken, or the plan does not allow another app;
    - the confirm link was not opened, so the change is still only on the preview;
    - card payments asked for before Stripe is connected;
    - a call to an outside service refused because the owner has not allowed the site in Connections,
      or test calls from the preview are off;
    - a scheduled job that failed, shown on the Status tab.
    Check each against the code before scripting it; leave out any that does not happen as described.

### E. Beyond the salon
17. **An online shop** from the store template: products, variants, delivery fee, free delivery over an
    amount, cash on delivery, made-to-order dates.
18. **A clinic queue (work app)** from the clinic template, with its queue jobs (try_scheduled_jobs).
19. **An app no template covers:** staff attendance written from scratch. The assistant reads the
    reference (get_example with reference_only), writes the whole app (apply_change), and tests it
    (view_page, submit_form).
20. **Rename and delete an app by chatting:** both only give a link the owner confirms (rename_app,
    delete_app), and what happens to the old address.

### Coverage table (every tool must appear at least once)

| Tool | Lessons |
| --- | --- |
| list_apps | 1 |
| get_example | 2, 19 |
| get_app | 6 |
| get_app_outline | 4 |
| get_app_parts | 4 |
| get_logs | 15 |
| view_live_page | 9 |
| create_app | 3, 17, 18 |
| apply_change | 16, 19 |
| patch_app | 6 |
| edit_app | 4, 15, 16 |
| upload_file | 5 |
| publish | 9, 15 |
| rename_app | 20 |
| delete_app | 20 |
| view_page | 3, 7, 15, 19 |
| submit_form | 7, 15, 19 |
| upload_as_visitor | 7 |
| create_test_visitor | 7 |
| screenshot_preview | 3, 6 |
| try_scheduled_jobs | 8, 18 |
| change_preview_row | 8 |

Check this list against the tools in `internal/mcpserver/mcpserver.go` before planning: if a tool was
added, renamed or removed, update the table and tell me.

Use the real product as it is today. Check the current texts and flows in the code and on
renoxium.com before writing a script; do not describe features that do not exist.

## Step 0: check that the free ChatGPT plan can do this (do this first)

Adding Renoxium to ChatGPT by its address needs developer mode, which is on ChatGPT's paid plans. Free
ChatGPT users can only use apps listed in ChatGPT's app directory. Find out, from OpenAI's own help pages
and by checking whether Renoxium is listed in the ChatGPT app directory today, whether a free ChatGPT
account can use Renoxium. Report what you found with links, then:

- If it can: plan the course for free ChatGPT as asked.
- If it cannot yet: stop and tell me before writing scripts. Do not show developer mode in a course
  that promises the free plan. Offer two options: wait for the directory listing, or use Claude's free
  plan (which allows one custom connector) for now.

## What you can and cannot record

- You have no ChatGPT account and must not create one or sign in to anyone's account. The ChatGPT parts
  are recorded by me, from my screen, following your script. Never build a fake or look-alike ChatGPT
  screen to stand in for the real one.
- You record everything on the Renoxium side yourself with Playwright (Chromium is installed; see the
  environment notes): the demos at `demo-<example>` as a customer, the owner's admin panel and calendar,
  the Templates and Use this template pages, the preview on a phone. Use the local server the e2e tests
  start (`e2e/global-setup.mjs`) or a local build; never use real customers' data.

## Deliverables

For each lesson, in `docs/video/<nn>-<slug>/` (text files only; no video files in git):

1. `script.md`: title, one-line goal, the shot list in order (which screen, what happens, how long),
   the exact messages I type into ChatGPT, word for word, and what the assistant should reply in short.
2. `captions.en.vtt` and `captions.ar.vtt`: on-screen text and subtitles, in English and Arabic. Videos
   must work with the sound off. Mark the Arabic as needing a native speaker's check.
3. `summary.md`: the paragraph and the messages to show under the video on the website.

Then:

4. Record the Renoxium-side shots with Playwright (`recordVideo`) at 1920x1080 for the website and
   1080x1920 for phones, reduced motion off, the demo banner hidden. Put the raw files in the scratchpad
   directory, not in the repository, and list them.
5. When I send you my ChatGPT recordings, cut and join them with yours using ffmpeg (install it if it is
   missing), burn in nothing: keep captions as separate .vtt files. Export MP4 (H.264, AAC) in both sizes,
   each lesson under 3 minutes. Send me the finished files.
6. Only after I approve the videos: a separate pull request that shows them on the website (landing page
   hero, template cards, connect page) with the summaries, captions and a poster image, lazy-loaded so
   the page stays fast. Ask me where the video files will be hosted before writing that code.

## Rules

- Work on your own branch; do not push to main. Open a pull request only when I ask.
- Keep texts short and plain; the audience is busy shop owners in the UAE, in English and Arabic.
- Prices and plans in scripts must match the code (`internal/web/landing.go`, the pricing section) at the
  time you write them.
- Report what you could not do and why, plainly, instead of working around it.
- Start by sending me: the result of Step 0, the lesson list you propose with the coverage table, and the
  script for lesson 3.
  Wait for my answer before recording.

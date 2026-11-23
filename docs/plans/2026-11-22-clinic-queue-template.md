# clinic-queue-uae: a walk-in queue template (the first work app)

**Goal:** a finished template for businesses that serve walk-ins in order and take no payments: GP, dental and physiotherapy
clinics first, also barbers, walk-in salons and service counters. It is the first gallery template that is a work app
(AED 99 a month with 5 staff), not a shop.

**Status:** built. `examples/clinic-queue-uae.json`, the demo (`demo-clinic-queue-uae`), `whatsapp.send` in the platform,
Go tests in `internal/platform/clinic_example_test.go` and `wamessage_test.go`, the browser flow in
`e2e/tests/14-clinic-queue.spec.mjs`.

## What it does
- **Take a number** without signing in: on the phone (`/` and `/s/:id`, reached from a QR code at reception) or on the
  reception tablet (`/kiosk`, which shows the number and a QR code to follow it, then resets). Name, mobile, service;
  email optional. One active number per mobile a day; opening hours, closed days, the last number before closing and a
  daily limit come from the clinic row.
- **The patient's ticket** (`/ticket?k=<secret>`): the number, the patients ahead, the estimated wait, and when called the
  room, all updating without a reload. Leaving the queue is one button.
- **Estimated wait** = patients ahead × minutes per patient (the service's, else the clinic's average) ÷ how many see that
  service at once (`services.desks`).
- **The waiting-room screen** (`/screen`) for a TV: the number being called and its room (a chime and the number read out
  once sound is turned on), the latest calls, the next numbers per service, a QR code to take a number.
- **Staff** call the next patient per room (a room calls the ticket letters in `rooms.services`), call again, finish, mark a
  no-show or put a no-show back, on `/staff` (visitor accounts whose email is in the `staff` table) or in the admin panel
  (`on_admin_change` fills the times and the room and updates the screens).
- **Told when close:** when `notify_ahead` (2) patients are ahead, one WhatsApp message (`whatsapp.send`, when
  `whatsapp_alerts` is on) and an email when one was given. Patients who join that close are marked `near` and not messaged.
- **Every ticket belongs to a UAE day**, so the queue starts empty each morning without any job. The job at 00:05 closes
  what was left, writes `daily_stats`, wipes names, numbers and emails older than `keep_contact_days`, updates the screens
  and emails the owner yesterday's numbers. `/staff/report` shows the last 14 days.

## Decisions
- **Live updates on one public channel** (`public:queue`): one publish per change reaches every phone, the screen and the
  staff page. It carries ticket codes, rooms and counts only, never a name or a phone number. Each page works out its
  own place from it; a ticket that is in neither list asks `/ticket?k=…&json=1` for its final state. Without EventSource the pages
  ask `/api/queue` every 20 seconds.
- **WhatsApp to patients needed a platform function.** Scripts could only email visitors and notify the owner, so
  `whatsapp.send({to, text})` was added: Renoxium's approved notice template, at most 200 characters, five a day per
  number, within the account's monthly WhatsApp messages (the quota sign-in codes and owner notices already share) and the
  daily limits. It throws when a limit is reached; the template then falls back to email. Preview only logs it (and a demo
  shows it in the demo inbox).
- **The ticket link is a query parameter** (`/ticket?k=`), not a path parameter: the template seeds no tickets, so the
  route checks render `/ticket` without one, which explains where to find a ticket (a wrong link answers 404). The staff
  pages render for anyone and say how to get on the staff list; only the staff actions answer 403. The Go and browser
  tests cover the full ticket and staff pages.
- **No photos needed:** service cards show the ticket letter; the demo has illustrations drawn for it, not stock photos.
- **The demo is open around the clock** (demo settings 00:00 to 24:00) so visitors can take a number at any hour; a queue
  under way, two weeks of history and a staff list with `*` (anyone signed in) are added by `demosample_clinic.go`.

## Hidden from agents while the MCP server is in review
The builder MCP server (renoxium.com/mcp) is under review for the ChatGPT app directory, and what an MCP client receives must
stay as it was at commit d10b7c5 (guarded by `TestMCPServerIsFrozenForReview` in `internal/mcpserver`, against
`internal/mcpserver/testdata/mcp-snapshot.json`). So `clinic-queue-uae` has `HiddenFromAgents: true`: `get_example` neither
lists nor returns it and `create_app {from_example}` refuses it like an unknown name. The gallery, its demo, "use this
template" on the website and the landing page still offer it.

To offer it to agents after the review:
1. Remove `HiddenFromAgents: true` from its entry in `examples/examples.go`.
2. Name it in the instructions of `internal/mcpserver/mcpserver.go` if wanted ("clinic-queue-uae: clinics, walk-in queues",
   and drop "a clinic queue" from the "no finished example fits" sentence), and add the `whatsapp.send` section to the reference.
3. Regenerate the snapshot: `RENOXIUM_MCP_SNAPSHOT=update go test ./internal/mcpserver -run TestMCPServerIsFrozenForReview`,
   and commit it with the change.
4. In `e2e/tests/13-view-transitions.spec.mjs`, empty `HIDDEN_FROM_AGENTS`.

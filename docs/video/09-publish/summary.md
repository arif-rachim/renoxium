When you're happy with your preview, ask ChatGPT to publish it. You get a confirmation link: open it signed in to
Renoxium and press Publish, and your site is live at its own address. Nothing goes live without your own confirmation.
Ask ChatGPT to check the live site. To undo a later change, publish an earlier version, again confirmed by you.

**Messages used**

1. "I'm happy with noor-beauty-studio. Publish it."
2. "I pressed Publish. Please check the live site is up, and tell me how I can undo a later change if I don't like it."

**What happened (2026-11-23):** publish (Allow once) returned the confirm link for version 7. The confirm page read
"Publish version 7? … Live now: nothing yet. Live data is backed up before the change", and Publish was pressed. The
result was "Published: Version 7 is live at https://noor-beauty-studio.renoxium.app". Then ChatGPT ran "Reviewed app
configuration, live page, and rollback structure" (view_live_page, HTTP 200) and explained rollback: publish an
earlier version, confirmed on renoxium.com. Rolling back restores the definition, not the live records. Small UX note:
the "Published" page shows a signed-out header (Sign in / Create account). Length 1:32.

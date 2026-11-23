Things can go wrong, and you can usually sort them out by saying what you see. The app name may be taken: choose
another. A change ChatGPT says is done may not show: tell it what you see, ask it to check the logs, and ask it to fix
it and check again. Remember that a change stays on the preview until you confirm publishing.

**Messages used:**
- "Create a new booking app for me named noor-beauty-studio."
- "On my Oud House shop (oud-house), show today's gold price on the home page, using
  https://api.gold-api.com/price/XAU."
- "I don't see any gold price on the preview. Please check the logs and tell me why."
- "Yes, apply the fix, and this time check the page really shows the price."
- "Is the gold price on my live shop now?"

**Recorded 2026-11-23:**
- **Name taken:** ChatGPT checked list_apps first and said the name is already used.
- **Gold price:**
  - Version 3 claimed success ("Homepage checked successfully"), but the section had gone into an unused template
    (`home_boutique`).
  - Asked again, it read the logs (get_logs), found its mistake and fixed it in version 4. The preview showed "Today's
    Gold Price $3,657.30".
  - ChatGPT noted that its own page checks don't run browser JavaScript.
- **Preview only:** the change stays on the preview until the owner confirms publishing.
- **Not shown:**
  - The Connections refusal (the price is fetched in the visitor's browser, not through http.fetch, so no host
    approval was needed).
  - A refused definition (apply_change problems).
  - edit_app with an old base version (its message still points to the non-existent `get_app {only}`).
  - The plan's app limit.
  - A failed scheduled job on Status.
  These need a staged app on the local server.
- Length 2:59.

A customer says paying by card fails. Ask ChatGPT to check the logs and test a card checkout on the preview. Look at
its advice carefully before changing anything live. Card payments need Stripe connected first: connect it on your
Payments page, then switch card payments on.

**Messages used:**
1. "A customer says paying by card fails on my Oud House shop (oud-house). Please check the logs, try a card checkout
   on the preview to see what happens, and fix it."
2. "Wait: is Stripe connected for oud-house? What happens if I switch on card payments on the live shop before
   connecting Stripe?"

**Recorded 2026-11-23 (staged honestly: on the live test shop card payments are off and Stripe is not connected):**
- Message 1 ran "Inspected products, tested card checkout, and reviewed order logs" (get_logs, change_preview_row,
  submit_form). ChatGPT found `card_enabled = 0`. The preview card checkout passed through the **payment simulator**,
  and ChatGPT told the owner to switch cards on live.
- Message 2: ChatGPT said it cannot see whether Stripe is connected, and advised keeping cards off until it is.
- **Product gaps:**
  - The agent cannot see whether Stripe is connected; list_apps or get_app could report it.
  - The preview simulator hides that the live checkout would fail.
  - Live settings (card_enabled) can only be changed in the admin panel.
- Not shown: a code fix with edit_app followed by publish, because there was no code bug to fix. Lesson 16 shows a
  fix. Length 1:30.

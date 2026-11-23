Test the owner's side before you go live. Ask ChatGPT to confirm a booking, move it to another time and cancel it,
exactly as you would in your admin panel, and to run the evening schedule and reminders once. Each step sends the
customer the right email (confirmed, changed, cancelled). On the preview nothing is really sent: you see the exact
emails your customers would get.

**Message used**

1. "In noor-beauty-studio, test what I do as the owner: confirm Maya's test booking, move it to 2pm, then cancel it,
   and tell me what the customer receives each time. Then run the evening schedule and reminders once to see what
   they send."

**What happened (2026-11-23):**

- "Inspected booking state and sequenced updates; Reviewed app data, booking flows, updates, cancellations, jobs, and
  email logs" (change_preview_row three times, try_scheduled_jobs).
- The emails were "Booking #1 confirmed…", "Booking #1 changed… 14:00" and "…is cancelled". daily_schedule ran with
  0 reminders, because the booking was already cancelled.
- ChatGPT noted one extra "changed" email caused by its own status reset during the test.
- Recorded in a new chat: the lesson 3–7 chat hit the free plan's **"Chat memory full"** limit. Lesson 1 or a tip
  should mention starting a new chat and naming the app. Length 1:15.

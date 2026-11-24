Renoxium also builds work apps that don't sell anything, like a walk-in queue for a clinic. Patients take a ticket
online or at reception and see how many people are ahead of them; their names never appear on the public board.
ChatGPT builds it from the clinic template, tests a ticket, and runs the scheduled job once on the preview to show
what it does. A work app costs AED 99 a month once published.

**Message used:** "Make me a walk-in queue for my clinic, Al Noor Family Clinic in Deira, Dubai: patients take a ticket
online or at reception and see when it is their turn. Name the app al-noor-clinic. Then run its scheduled queue jobs
once and tell me what they did."

**Recorded 2026-11-23:** "Tested ticket expiration, ran scheduled jobs, and captured mobile preview" (create_app from
clinic-queue-uae, submit_form, try_scheduled_jobs: expire_previous_day_tickets, HTTP 200, 0 expired). Not published.
Length 1:01.

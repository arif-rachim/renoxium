Before real customers arrive, ask ChatGPT to test your site the way a customer would. It makes a test customer account,
books an appointment through your real booking flow, and opens the customer's bookings page while signed in as that
customer. Test accounts and bookings stay on your private preview and never mix with your real customers.

**Message used**

1. "Now test it like a customer would: make a test customer account, book a Haircut & Blow-dry with Noor for next
   Tuesday at 11am as that customer, and show me that customer's bookings page."

**What happened (2026-11-23):** "Verified availability, checked slots, submitted payment booking, and previewed
bookings page" (create_test_visitor, submit_form, view_page as the visitor). The result was booking #1 for Maya Test,
24 Nov 11:00 with Noor, AED 160, pay at venue. upload_as_visitor was not used, because the booking template has no
customer file upload; it needs another lesson or template (see the coverage table). Length 0:43.

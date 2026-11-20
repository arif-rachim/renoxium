# Resources: heavy and unprofitable apps

The Resources section of the console (operators only; in the side menu under Operator) has three tabs, each on its own address: `/operator/resources` (the server), `/operator/usage` (the apps) and `/operator/prices` (unit prices and fair use). One app's page, `/operator/resources/<app>`, has the tabs Summary, Per hour (`/hours`), Scripts and incidents (`/scripts`) and Actions (`/actions`). Together they show which apps use the shared
server most, which make other apps wait, and which cost more than they bring in.

## How it is measured

Every app stage (live and preview) is metered in memory, per hour, and written to the
`app_usage_hourly` table of `platform.db` every 5 minutes (and at shutdown); rows are kept for
90 days. The page always includes the current hour. A request costs two atomic additions; a
script run about a dozen (well under a microsecond next to a run of 100 µs or more).

- **Script time**: how long the app's runs held a script worker, database calls and waits
  included. This is what other apps wait for.
- **CPU**: user + system time of the worker process during the run, read with `getrusage`
  inside the worker (accurate to the microsecond on Linux, also for short runs), plus the time
  the server spent on the run's `db.query` / `db.exec` (SQLite runs in the server process).
  When a worker is killed before it can answer, the run's wall time is counted instead.
- **Waits**: time waiting for the app's own or the account's script slots, and time waiting for
  a free worker. A wait for a worker is shared out over the apps whose runs held the workers at
  that moment: that is the app's **wait caused**.
- **Refused**: runs that gave up after 3 s at the app's or account's limit, or that an operator
  throttle refused.
- **Timeouts / kills**: runs stopped at the 2 s limit or a memory limit; restarts are workers the
  server killed (hard time limit or 256 MiB resident) and started again.
- Requests, bytes sent, realtime peak connections, connection time and messages, `http.fetch`
  calls, emails and WhatsApp messages sent, job runs and job time. Storage (databases, files,
  visitor uploads) is measured when the page is opened.

## Cost, revenue, margin

Set the unit prices at the bottom of the page (stored in `operator_settings` as
`resources:<name>`):

| Setting | Default |
|---|---|
| CPU, AED per CPU-hour | 0.05 |
| Storage, AED per GB-month | 0.10 |
| Bandwidth, AED per GB sent | 0.05 |
| WhatsApp, AED per message | 0.06 |
| Email, AED per email | 0.002 |
| Realtime, AED per connection-hour | 0.0005 |
| Free fair use: script seconds a day | 300 |
| Free fair use: MB sent a day | 500 |

To set the CPU price, divide the server's monthly cost by (vCPUs × 730 hours); add a little for
the disk and memory that are not priced separately.

- **Estimated cost** = CPU-hours × CPU price + GB stored × storage price × days ÷ 30 + GB sent ×
  bandwidth price + messages × their prices + connection-hours × realtime price.
- **Revenue** = the owner's plan price for the period (monthly price × days ÷ 30; only plans paid
  through Stripe, not granted ones), split **equally** over the owner's live apps (over all their
  apps if none is live), plus the Free-plan card fees of the app's own live payments (AED).
- **Margin** = revenue − estimated cost. **Share** is the app's part of all apps' script time.

## Flags

- **Noisy neighbour**: in the top 1% of active apps by script time (at least one app) and other
  apps waited at least 1 s in all for a worker while it held one.
- **Unprofitable heavy**: in the top 10% by estimated cost, costing at least AED 0.01, margin below
  zero. Template demos are never flagged.
- **Error-prone**: at least 3 runs, and at least 2% of runs, stopped at the time or memory limit.
- **Free plan over fair use**: a Free-plan app above either fair-use threshold, averaged per day.

## Actions (app page)

- **Email the owner**: a polite notice of what is heavy with the upgrade link; edit it first.
- **Throttle**: limits only this app to 1 script at a time and/or a number of script runs a
  minute, until a time you choose (at most 30 days). Refused runs get a 503 and an explanation
  in the app log; the owner sees a notice in their admin panel until it ends. "End throttle now"
  lifts it.
- **Suspend** / **Bring back**: the same as on `/operator`.

Every action is written to the app's log (source `operator`) and to the operator audit at the
bottom of the page.

## Alerts

The monitor (every 5 minutes) emails the operators when one app caused more than 30% of all
worker waits over the last 15 minutes, in at least 3 of those minutes and with at least 10 s of
waiting in all, or when one app had 5 or more memory kills in 15 minutes. The email names the app
and links to its resource page; each kind of alert per app is sent at most every 6 hours.

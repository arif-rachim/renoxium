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

- One lesson = one task, 1 to 3 minutes. No intro music, no "hi, welcome", no recap of earlier lessons:
  each lesson stands alone and starts on the screen where the task starts.
- The title is the task, as an action: "Build a salon booking site from one message",
  "Change a price by chatting", "Publish your site".
- Show, do not tell: the viewer sees the exact message typed, the assistant's reply and the result.
  Zoom in on what matters; cut waiting time (the assistant thinking, pages loading).
- Each lesson has a one-paragraph summary and the exact messages typed, to show under the video.
- Plain language for shop owners, not developers. No code on screen.

## The lessons (adjust after Step 0, keep each one short)

1. Add Renoxium to ChatGPT (once): the connect steps, signing in, allowing access.
2. Build a booking site for a salon from one message, then open the preview on a phone.
3. Build an online shop: products, prices, delivery fee, WhatsApp number, cash on delivery.
4. Change things by chatting: a price, a photo, opening hours, Arabic texts. Make the point that
   Renoxium has no credits: change it as often as you like.
5. Publish: the assistant gives a confirm link; the owner opens it and confirms; the site is live.
6. A customer books, the owner is told on WhatsApp, the booking is in the calendar.
7. A clinic queue (work app) from one message.
8. (Optional) Start from a template without chatting: Templates, Use this template, business name.

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
- Start by sending me: the result of Step 0, the lesson list you propose, and the script for lesson 2.
  Wait for my answer before recording.

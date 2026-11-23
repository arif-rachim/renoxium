# Build a salon booking site from one message

**Goal:** the owner sends one message and gets a working booking website on a private preview, checked on a phone.
**Length:** about 1:30. **Starts on:** a new ChatGPT chat with Renoxium already added (lesson 1).
**Business:** Noor Beauty Studio, a women's salon in Al Barsha, Dubai. The account has no app called `noor-beauty-studio`
before recording.

## Shots

| # | Screen | What happens | ~s |
|---|---|---|---|
| 1 | ChatGPT, new chat | Cursor in the message box; zoom on the box | 3 |
| 2 | ChatGPT | The message below is typed (zoomed) and sent | 14 |
| 3 | ChatGPT | "Allow ChatGPT to use Renoxium?" appears; zoom; **Allow once** is clicked | 5 |
| 4 | ChatGPT | The activity line runs (create the app, check the pages, phone screenshot). Waiting is cut and every tool line is kept. | 20 |
| 5 | ChatGPT | The answer shows the phone screenshot of the home page and the preview link; the chat follows to the end | 15 |
| 6 | ChatGPT | The activity is opened so the steps it took are visible | 6 |
| 7 | Preview tab | **Open preview**: the private preview shows "Noor Beauty Studio", services with prices in AED and a **Book** button; scroll once | 12 |
| 8 | ChatGPT | Back in the chat; last line: nothing is live yet | 4 |

Total is about 80 s plus typing.

If ChatGPT asks a question back instead of building (for example "Which services do you offer?"), answer it in one line.
The script then counts that reply as the second message and the lesson keeps it. Don't retake to hide it.

## Messages typed into ChatGPT (word for word)

1. `I run Noor Beauty Studio, a women's salon in Al Barsha, Dubai. Make me a website where customers can book appointments, and show me how it looks on a phone.`

Possible second message, only if ChatGPT asks first:

2. `Use your salon template for now. I'll change the services and prices later.`

## What the assistant should do and reply (short)

- **Create the app.** create_app `noor-beauty-studio` from the booking template (`booking-uae`), after "Allow once".
- **Put in the name.** Most likely it puts "Noor Beauty Studio" (and Al Barsha) in place of the sample "Lamsa Beauty
  Lounge, Jumeirah" with edit_app, after reading the app with get_app_outline and get_app_parts.
- **Check it.** It checks the pages with view_page and the home page on a phone with screenshot_preview (`mobile`).
- **Reply.** The site is ready on a **private preview** (link), the phone screenshot is shown, and **nothing is live** until
  the owner publishes. The services, prices and photos are still the template's samples, which lesson 4 changes.

Record what actually happens. If the tools differ from this list, keep the take and note the tools in `summary.md` and the
coverage table.

## On-screen captions (English; Arabic in captions.ar.vtt after recording)

| When | English | Arabic (needs a native speaker's check) |
|---|---|---|
| 2 | One message: your business, where it is, what customers should do | رسالة واحدة: نشاطك ومكانه وما يفعله العملاء |
| 3 | ChatGPT asks before Renoxium creates anything | يطلب ChatGPT الإذن قبل أن ينشئ Renoxium أي شيء |
| 4 | Renoxium builds it from the salon template and checks every page | يبنيه Renoxium من قالب الصالون ويتحقق من كل صفحة |
| 5 | Checked on a phone | تم التحقق على الهاتف |
| 7 | Your private preview: only you can see it | معاينتك الخاصة: لا يراها غيرك |
| 8 | Nothing is live until you publish | لا يُنشر شيء حتى تنشره أنت |

## Narration (af_heart)

- 2: "Tell ChatGPT about your business in one message: what it is, where it is, and what customers should be able to do."
- 3: "Before Renoxium creates anything, ChatGPT asks. Choose Allow once."
- 4: "Renoxium starts from its salon template, puts in your name, and checks every page."
- 5: "Here it is on a phone."
- 7: "This is your private preview. Only you can see it. The services and prices are samples for now; you'll change them in the next lesson."
- 8: "Nothing is live until you publish."

## Recording commands (renoxium-video)

```
node rx.mjs new 03-booking-site-from-one-message      # size 1920x1080 in video.mjs
node rx.mjs 03 intro scene0                            # new chat, Renoxium chosen
node rx.mjs 03 ask scene1 "I run Noor Beauty Studio, ..."      # waits, allows once, follows, reads
node rx.mjs 03 open scene2 --link "https://noor-beauty-studio--preview"   # preview tab, scroll, back
node rx.mjs 03 build
```

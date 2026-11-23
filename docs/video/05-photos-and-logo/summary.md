Put your own logo and photos on your site. You can attach them in ChatGPT and say where each one goes, but for now
ChatGPT can't hand photos to Renoxium. It says so, and points you to the admin panel. In your admin panel on
renoxium.com, open Business settings to choose your logo and banner photo from your computer. Then open each service
and choose its photo. Save, and they show on your site.

**What happened (2026-11-23)**

- In the chat, the owner attached noor-logo.png, salon-front.jpg and four service photos and said where each goes.
  ChatGPT analysed them, then replied: "I haven't uploaded or applied the images yet because the image-upload
  connection needs the files in a format I can pass to it… upload them through your Renoxium app's admin panel."
  upload_file takes `data_base64` only, and Renoxium does not declare `openai/fileParams`.
- **The admin panel edits live data only** (`internal/platform/admin.go` AdminStage). Before the first publish there
  is no way to add photos, neither from the chat nor in the panel. This part was recorded after lesson 9 (publish).
  The photos appear on the live site at once and reach the preview on the next change.
- Saving each admin form took about 25 seconds on renoxium.com during this recording, cut in the video. Length 2:54.

**Product suggestions:** support `openai/fileParams` in upload_file (or accept a download URL), or let the admin panel
edit preview data before the first publish.

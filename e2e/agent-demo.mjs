// The building agent's tools, end to end, from an empty account: start an
// app from the home-service example, change one CSS rule and one script
// line in a single small edit_app call, make a test visitor, book a visit
// paying cash, look at the booking page on a phone, then cancel it.
// Every step is an MCP tool call; nothing reaches the live site.
//
// Run against any Renoxium server with an agent's access token:
//
//   RENOXIUM_URL=https://renoxium.com RENOXIUM_TOKEN=<access token> node e2e/agent-demo.mjs [app-name]
//
// (get a token by connecting an MCP client; e2e/tests/12-agent-tools.spec.mjs
// runs the same demo on the test server). The screenshot is written to
// booking-mobile.png in the current directory.
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';

// runDemo takes call(name, args) -> structured result (it throws on a tool
// error) and returns what each step produced.
export async function runDemo(call, { app = 'demo-' + Date.now().toString(36), log = console.log, screenshotTo } = {}) {
  const steps = {};
  const size = (args) => JSON.stringify(args).length;
  const step = async (name, tool, args) => {
    const t = Date.now();
    const out = await call(tool, args);
    log(`${name.padEnd(28)} ${tool.padEnd(20)} sent ${String(size(args)).padStart(5)} B  got ${String(JSON.stringify(out).length).padStart(6)} B  ${Date.now() - t} ms`);
    return out;
  };

  // 1. A new app that starts as the finished example; its outline comes back.
  steps.created = await step('create from example', 'create_app', { app, from_example: 'home-service-uae' });
  if (!steps.created.apply?.ok) throw new Error('example did not apply: ' + JSON.stringify(steps.created.apply?.problems));
  const base = steps.created.version;

  // 2. Read just the script to change (not the 180 KB definition).
  steps.book = await step('read one script', 'get_app_parts', { app, paths: ['scripts.book'] });
  const bookLine = 'const f = req.form, b = biz();';
  if (!steps.book.parts['scripts.book'].includes(bookLine)) throw new Error('scripts.book changed; update the demo');

  // 3. One CSS rule and one script line, in one call under 2 KB.
  const edit = {
    app, base_version: base, edits: [
      { target: 'css', old_text: '[hidden]{display:none!important}', new_text: '[hidden]{display:none!important}\nh1{letter-spacing:-.02em}' },
      { target: 'scripts.book', old_text: bookLine, new_text: bookLine + " console.log('demo: booking for service', f.service);" },
    ],
  };
  if (size(edit) >= 2048) throw new Error('edit call is not under 2 KB');
  steps.edited = await step('edit css + script line', 'edit_app', edit);
  if (!steps.edited.ok) throw new Error('edit_app: ' + JSON.stringify(steps.edited.problems));
  const problems = (steps.edited.checks || []).filter((c) => c.error || c.status >= 500);
  if (problems.length) throw new Error('checks failed: ' + JSON.stringify(problems));

  // 4. A signed-in visitor for the preview.
  steps.visitor = await step('test visitor', 'create_test_visitor', { app, name: 'Fatima' });
  const as = steps.visitor.email;

  // 5. Book a visit three days ahead (not a Sunday), paying cash, and follow to the booking page.
  const d = new Date(Date.now() + 4 * 3600e3 + 3 * 86400e3);
  if (d.getUTCDay() === 0) d.setUTCDate(d.getUTCDate() + 1);
  const date = d.toISOString().slice(0, 10);
  steps.booked = await step('book, pay cash', 'submit_form', {
    app, path: '/book', as_visitor: as, follow_redirects: true, form: {
      service: '1', qty: '2', zone: '1', date, slot: '10:00', repeat: 'once', payment: 'cash',
      name: 'Fatima', phone: '+971501234567', address: 'Villa 12, Street 4', flat: 'Villa 12',
    },
  });
  const hop = (steps.booked.redirects || [])[0] || '';
  const bookingPath = (hop.match(/ (\/bookings\/\d+)/) || [])[1];
  if (steps.booked.status !== 200 || !bookingPath) throw new Error('booking: ' + JSON.stringify({ status: steps.booked.status, redirects: steps.booked.redirects }));
  if (!steps.booked.logs.some((l) => l.message.includes('demo: booking for service 1'))) throw new Error('the edited script line did not run');

  // 6. The booking page on a phone, signed in as the visitor.
  try {
    steps.shot = await step('screenshot booking (390px)', 'screenshot_preview', { app, path: bookingPath, viewport: 'mobile', full_page: true, as_visitor: as });
    if (screenshotTo && steps.shot.png) fs.writeFileSync(screenshotTo, steps.shot.png);
  } catch (e) {
    if (!String(e.message).includes('not set up')) throw e;
    log('screenshot_preview: no Chromium on this server, skipped');
  }

  // 7. Cancel it.
  steps.cancelled = await step('cancel', 'submit_form', { app, path: bookingPath + '/cancel', as_visitor: as, follow_redirects: true });
  if (steps.cancelled.status !== 200 || !/Cancelled/.test(steps.cancelled.body)) throw new Error('cancel did not show Cancelled');
  log(`done: ${app} preview ${steps.created.preview_url} (live untouched: version ${steps.created.live_version})`);
  return { app, bookingPath, steps };
}

// From the command line: connect with a token and run.
if (import.meta.url === pathToFileURL(process.argv[1] || '').href) {
  const { Client } = await import('@modelcontextprotocol/sdk/client/index.js');
  const { StreamableHTTPClientTransport } = await import('@modelcontextprotocol/sdk/client/streamableHttp.js');
  const base = process.env.RENOXIUM_URL, token = process.env.RENOXIUM_TOKEN;
  if (!base || !token) {
    console.error('set RENOXIUM_URL and RENOXIUM_TOKEN');
    process.exit(2);
  }
  const client = new Client({ name: 'renoxium-agent-demo', version: '1.0.0' });
  await client.connect(new StreamableHTTPClientTransport(new URL(base + '/mcp'), { requestInit: { headers: { Authorization: 'Bearer ' + token } } }));
  await runDemo(mcpCall(client), { app: process.argv[2], screenshotTo: 'booking-mobile.png' });
  await client.close();
}

// mcpCall adapts an MCP client: structured results, the PNG of an image
// result as .png (bytes), and tool errors thrown.
export function mcpCall(client) {
  return async (name, args) => {
    const r = await client.callTool({ name, arguments: args });
    const text = (r.content || []).filter((c) => c.type === 'text').map((c) => c.text).join('\n');
    if (r.isError) throw new Error(`${name}: ${text}`);
    const out = r.structuredContent || JSON.parse(text);
    const img = (r.content || []).find((c) => c.type === 'image');
    if (img) out.png = Buffer.from(img.data, 'base64');
    return out;
  };
}

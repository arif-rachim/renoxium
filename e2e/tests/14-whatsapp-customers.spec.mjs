// WhatsApp messages to customers, end to end:
//   - an agent builds a form with the platform's WhatsApp box and tries it on
//     preview: the test result shows the message, nothing reaches Meta;
//   - on live, a customer who ticks the box gets the confirmation template
//     from the platform number, named after the business, with a View link
//     to the app;
//   - a webhook without Meta's signature is refused; a signed Stop tap
//     stops the app's messages and the customer is told so.
import crypto from 'node:crypto';
import { test, expect } from '@playwright/test';
import { appURL, RENOXIUM, SECRETS } from '../env.mjs';
import { person, signUp, email, unique, connectAgent, builder, confirmPublish, fake } from '../lib.mjs';

const bookingApp = () => ({
  tables: [{ name: 'business', columns: [{ name: 'name', type: 'text' }] },
    { name: 'bookings', columns: [{ name: 'phone', type: 'text' }] }],
  seed: { business: [{ name: 'Sahara Spa' }] },
  brand: { table: 'business', name: 'name' },
  templates: {
    form: `<!doctype html><html><head><title>Book</title></head><body><h1>Book a massage</h1>
<form method="post" action="/book"><label>Phone <input name="phone" type="tel"></label>
<rx-whatsapp-optin></rx-whatsapp-optin><button>Book</button></form><rx-whatsapp-chat>Chat with us</rx-whatsapp-chat></body></html>`,
  },
  scripts: {
    book: `function handle(req) {
  const phone = req.form.phone;
  const id = db.exec('INSERT INTO bookings (phone) VALUES (?)', phone).lastInsertId;
  const consent = req.form.rx_wa_optin === '1' ? whatsapp.consent({phone: phone}) : {consented: false};
  const sent = whatsapp.send({to: phone, kind: 'confirmation', ref: 'Booking #' + id, text: 'Hot stone massage on Friday 5 Dec 2026, 16:00', link: '/b/' + id});
  return {json: {consent: consent, sent: sent}};
}`,
    wa_in: `function handle(req) {
  const m = req.body;
  if (m.type === 'button' && m.button.id === 'when') return {json: {reply: 'Your massage is on Friday at 16:00.'}};
  return {json: {reply: 'Hello ' + m.name + ', you wrote: ' + m.text, buttons: [{id: 'when', title: 'When is it?'}]}};
}`,
  },
  whatsapp: { on_message: 'wa_in', auto_replies: [{ match: ['price', 'prices'], reply: 'A massage is AED 250.' }] },
  routes: [{ method: 'GET', path: '/', template: 'form' }, { method: 'POST', path: '/book', script: 'book' }],
});

// webhook posts a Meta-style webhook (messages, and the sender's contact) to Renoxium, signed like Meta signs it unless told otherwise.
async function webhook(messages, { sign = true } = {}) {
  const body = JSON.stringify({ object: 'whatsapp_business_account', entry: [{ id: 'waba', changes: [{ field: 'messages',
    value: { metadata: { phone_number_id: '100200300' }, contacts: messages.map((m) => ({ wa_id: m.from, profile: { name: 'Mona' } })), messages } }] }] });
  const headers = { 'Content-Type': 'application/json' };
  if (sign) headers['X-Hub-Signature-256'] = 'sha256=' + crypto.createHmac('sha256', SECRETS.whatsappApp).update(body).digest('hex');
  return fetch(RENOXIUM + '/whatsapp/webhook', { method: 'POST', body, headers });
}

async function waitFor(what, check, timeout = 15_000) {
  const end = Date.now() + timeout;
  for (;;) {
    const v = await check();
    if (v) return v;
    if (Date.now() > end) throw new Error('timed out waiting for ' + what);
    await new Promise((r) => setTimeout(r, 200));
  }
}

let owner, agent, app;

test.beforeAll(async ({ browser }) => {
  owner = await person(browser);
  owner.email = email('owner');
  await signUp(owner.page, owner.email);
  agent = await builder((await connectAgent(owner.page)).access_token);
  app = unique('spa');
});

test('the agent sees the WhatsApp message on preview; nothing is sent', async () => {
  await agent.call('create_app', { app });
  const applied = await agent.call('apply_change', { app, definition: bookingApp() });
  expect(applied.ok).toBeTruthy();
  const page = await agent.call('view_page', { app, path: '/' });
  expect(page.body).toContain('on WhatsApp from Sahara Spa (sent via Renoxium)');
  const token = page.body.match(/name="rx_wa_optin_token" value="([^"]+)"/)[1];
  const phone = '+971502220001';
  const res = await agent.call('submit_form', { app, path: '/book', form: { phone, rx_wa_optin: '1', rx_wa_optin_token: token } });
  expect(res.whatsapp).toEqual([{ to: phone, kind: 'confirmation', link: '/b/1',
    text: 'Sahara Spa confirmed your Booking #1: Hot stone massage on Friday 5 Dec 2026, 16:00. Tap View for the details.' }]);
  expect(JSON.parse(res.body).sent).toMatchObject({ sent: false, preview: true });
  expect(await fake.whatsapp(phone)).toEqual([]);

  // A customer's message on preview: the automatic reply, then the script with its button.
  const price = await agent.call('send_test_whatsapp_message', { app, from: phone, text: 'What are your prices?' });
  expect(price.whatsapp).toEqual([{ to: phone, kind: 'auto', text: 'A massage is AED 250.' }]);
  const hi = await agent.call('send_test_whatsapp_message', { app, from: phone, text: 'hi there' });
  expect(hi.whatsapp).toEqual([{ to: phone, kind: 'reply', text: 'Hello Test Customer, you wrote: hi there', buttons: ['When is it?'] }]);
  const when = await agent.call('send_test_whatsapp_message', { app, from: phone, button_id: 'when' });
  expect(when.whatsapp[0].text).toBe('Your massage is on Friday at 16:00.');
  expect(await fake.whatsapp(phone)).toEqual([]);
});

test('on live a customer who ticks the box gets the confirmation; Stop ends the messages', async ({ browser }) => {
  const pub = await agent.call('publish', { app });
  await confirmPublish(owner.page, pub.confirm_url);

  const phone = '+971502220002';
  const customer = await person(browser);
  await customer.page.goto(appURL(app) + '/');
  await customer.page.getByLabel('Phone').fill(phone);
  await customer.page.getByLabel(/on WhatsApp from Sahara Spa/).check();
  await customer.page.getByRole('button', { name: 'Book' }).click();
  const answer = JSON.parse(await customer.page.locator('body').innerText());
  expect(answer.consent).toEqual({ consented: true });
  expect(answer.sent.sent).toBe(true);

  const msg = await waitFor('the confirmation', async () => (await fake.whatsapp(phone)).find((m) => m.template === 'rx_confirmation'));
  expect(msg.body).toEqual(['Sahara Spa', 'Booking #1', 'Hot stone massage on Friday 5 Dec 2026, 16:00']);
  expect(msg.url).toBe(answer.sent.id);
  expect(msg.payload).toBe('rx-stop:' + answer.sent.id);

  // The View button leads to the booking on the app.
  const view = await fetch(RENOXIUM + '/w/' + msg.url, { redirect: 'manual' });
  expect(view.status).toBe(302);
  expect(view.headers.get('location')).toBe(appURL(app) + '/b/1');

  // Without Meta's signature the Stop tap is refused and changes nothing.
  const stop = { from: phone.slice(1), id: 'wamid.stop.' + Date.now(), type: 'button', button: { payload: msg.payload, text: 'Stop messages' } };
  expect((await webhook([stop], { sign: false })).status).toBe(401);
  expect((await webhook([stop])).status).toBe(200);
  const reply = await waitFor('the opt-out answer', async () => (await fake.whatsapp(phone)).find((m) => m.type === 'text'));
  expect(reply.text).toContain('You will not get more WhatsApp messages from Sahara Spa');

  // The next booking without the box: no consent any more, nothing sent.
  await customer.page.goto(appURL(app) + '/');
  await customer.page.getByLabel('Phone').fill(phone);
  await customer.page.getByRole('button', { name: 'Book' }).click();
  expect(JSON.parse(await customer.page.locator('body').innerText()).sent).toEqual({ sent: false, reason: 'opted_out' });
  expect((await fake.whatsapp(phone)).filter((m) => m.type === 'template')).toHaveLength(1);
});

test('a customer who opens the chat link talks to the app on live', async ({ browser }) => {
  const customer = await person(browser);
  await customer.page.goto(appURL(app) + '/');
  const href = await customer.page.getByRole('link', { name: 'Chat with us' }).getAttribute('href');
  const first = new URL(href).searchParams.get('text');
  expect(href.startsWith('https://wa.me/971500000000?')).toBe(true);
  expect(first).toBe(`Hi Sahara Spa (ref ${app})`);

  // What WhatsApp would deliver when the customer sends that first message.
  const phone = '+971502220003';
  expect((await webhook([{ from: phone.slice(1), id: 'wamid.in.' + Date.now(), type: 'text', text: { body: first + ' is Friday free?' } }])).status).toBe(200);
  const reply = await waitFor('the script reply', async () => (await fake.whatsapp(phone)).find((m) => m.type === 'interactive'));
  expect(reply.text).toBe('Hello Mona, you wrote: Hi Sahara Spa is Friday free?');
  expect(reply.buttons[0].title).toBe('When is it?');

  // The tap on the button comes back to the same app.
  expect((await webhook([{ from: phone.slice(1), id: 'wamid.in2.' + Date.now(), type: 'interactive',
    interactive: { type: 'button_reply', button_reply: { id: reply.buttons[0].id, title: 'When is it?' } } }])).status).toBe(200);
  const answer = await waitFor('the button answer', async () => (await fake.whatsapp(phone)).find((m) => m.type === 'text'));
  expect(answer.text).toBe('Your massage is on Friday at 16:00.');
  // Words of an automatic reply are answered without the script.
  expect((await webhook([{ from: phone.slice(1), id: 'wamid.in3.' + Date.now(), type: 'text', text: { body: 'prices?' } }])).status).toBe(200);
  await waitFor('the automatic reply', async () => (await fake.whatsapp(phone)).find((m) => m.text === 'A massage is AED 250.'));
});

// Helpers shared by the end-to-end tests.
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect } from '@playwright/test';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';
import { RENOXIUM, FAKES, appURL } from './env.mjs';

export const PASSWORD = 'correct horse battery staple';
const here = path.dirname(fileURLToPath(import.meta.url));
export const storeExample = () => JSON.parse(fs.readFileSync(path.join(here, '..', 'examples', 'store-uae.json'), 'utf8'));

// unique gives names that do not clash between tests or runs.
let n = 0;
export const unique = (prefix) => `${prefix}${Date.now().toString(36).slice(-5)}${(++n).toString(36)}`;
export const email = (who) => `${unique(who)}@e2e.test`;

// --- Fake services ---
export const fake = {
  async get(p) { return (await fetch(FAKES + '/_fake' + p)).json(); },
  async post(p, body) {
    const r = await fetch(FAKES + '/_fake' + p, { method: 'POST', body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' } });
    if (!r.ok) throw new Error(`fake ${p}: ${r.status} ${await r.text()}`);
    return r.json();
  },
  emails: (to) => fake.get('/emails?to=' + encodeURIComponent(to)),
  whatsapp: (to) => fake.get('/whatsapp?to=' + encodeURIComponent(to)),
  stripeCalls: () => fake.get('/stripe/calls'),
  apiCalls: (host) => fake.get('/extapi/calls?host=' + encodeURIComponent(host)),
  sessions: () => fake.get('/stripe/sessions'),
};

// waitEmail waits for a message to `to` whose subject matches, newer than `after`.
export async function waitEmail(to, subject, { after = 0, timeout = 15_000 } = {}) {
  const end = Date.now() + timeout;
  for (;;) {
    const found = (await fake.emails(to)).filter((m) => m.at >= after && subject.test(m.subject));
    if (found.length) return found.at(-1);
    if (Date.now() > end) throw new Error(`no email to ${to} matching ${subject}; got: ${(await fake.emails(to)).map((m) => m.subject).join(' | ')}`);
    await new Promise((r) => setTimeout(r, 200));
  }
}
export function linkIn(message, startsWith) {
  const links = (message.text + ' ' + (message.html || '')).match(/https?:\/\/[^\s"'<>]+/g) || [];
  const l = links.find((u) => u.startsWith(startsWith));
  if (!l) throw new Error(`no link starting ${startsWith} in "${message.subject}": ${links.join(' ')}`);
  return l.replace(/&amp;/g, '&');
}
export async function waitWhatsApp(to, { after = 0, template, timeout = 15_000 } = {}) {
  const end = Date.now() + timeout;
  for (;;) {
    const found = (await fake.whatsapp(to)).filter((m) => m.at >= after && (!template || m.template === template));
    if (found.length) return found.at(-1);
    if (Date.now() > end) throw new Error(`no WhatsApp message to ${to}`);
    await new Promise((r) => setTimeout(r, 200));
  }
}

// --- Browsers ---
// person opens a browser context with its own client address, so the server's
// per-address rate limits treat each person in a test as someone else.
export async function person(browser, opts = {}) {
  const ip = `10.${rand()}.${rand()}.${rand()}`;
  const ctx = await browser.newContext({ baseURL: RENOXIUM, extraHTTPHeaders: { 'X-E2E-Client-IP': ip }, ...opts });
  const page = await ctx.newPage();
  page.on('pageerror', (e) => console.log(`[page error] ${e.message}`));
  return { ctx, page, ip };
}
const rand = () => 1 + Math.floor(Math.random() * 250);
// clientIP makes a server-to-server call count as a new agent's address.
export const clientIP = () => ({ 'X-E2E-Client-IP': `10.${rand()}.${rand()}.${rand()}` });

// signUp creates an owner account in the browser and, unless told not to,
// confirms the email through the link in the confirmation message.
export async function signUp(page, address, { confirm = true, password = PASSWORD } = {}) {
  const t = Date.now();
  await page.goto('/signup');
  await page.fill('#email', address);
  await page.fill('#password', password);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible();
  if (confirm) {
    const m = await waitEmail(address, /confirm/i, { after: t });
    await page.goto(linkIn(m, RENOXIUM + '/verify'));
    await expect(page.locator('body')).toContainText(/confirmed/i);
  }
}
export async function signIn(page, address, { password = PASSWORD, next } = {}) {
  await page.goto('/login' + (next ? '?next=' + encodeURIComponent(next) : ''));
  await page.fill('#email', address);
  await page.fill('#password', password);
  await page.getByRole('button', { name: 'Sign in' }).click();
}
export async function signOut(page) {
  await page.goto('/');
  await page.getByRole('button', { name: 'Sign out' }).click();
}

// --- Agent: OAuth 2.1 with dynamic registration and PKCE, then MCP ---
export const REDIRECT = 'http://127.0.0.1:18098/callback';
export async function registerClient(name = 'E2E agent', redirect = REDIRECT) {
  const r = await fetch(RENOXIUM + '/oauth/register', { method: 'POST', headers: { 'Content-Type': 'application/json', ...clientIP() },
    body: JSON.stringify({ client_name: name, redirect_uris: [redirect], grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'], token_endpoint_auth_method: 'none' }) });
  return { status: r.status, body: await r.json().catch(() => ({})) };
}
export function pkce() {
  const verifier = crypto.randomBytes(32).toString('base64url');
  return { verifier, challenge: crypto.createHash('sha256').update(verifier).digest('base64url') };
}
export function authorizeURL(clientID, challenge, state = 'st' + Date.now(), redirect = REDIRECT) {
  const q = new URLSearchParams({ response_type: 'code', client_id: clientID, redirect_uri: redirect, code_challenge: challenge,
    code_challenge_method: 'S256', scope: 'apps', state, resource: RENOXIUM + '/mcp' });
  return '/oauth/authorize?' + q;
}
// approve opens the consent screen in a signed-in page and returns the
// redirect it leads to (the callback is never actually loaded).
// decision is 'allow' (the "Connect … to this account" button) or 'deny' ("Don't connect").
export async function consent(page, url, decision = 'allow') {
  await page.goto(url);
  const nav = page.waitForURL(/127\.0\.0\.1:18098/);
  await page.getByRole('button', { name: decision === 'deny' ? "Don't connect" : /^Connect .+ to this account$/ }).click();
  await nav;
  return new URL(page.url());
}
export async function exchange(fields) {
  const r = await fetch(RENOXIUM + '/oauth/token', { method: 'POST', body: new URLSearchParams(fields),
    headers: { 'Content-Type': 'application/x-www-form-urlencoded', ...clientIP() } });
  return { status: r.status, body: await r.json().catch(() => ({})) };
}
// connectAgent runs the whole OAuth flow for the owner signed in on `page`.
export async function connectAgent(page) {
  const reg = await registerClient();
  expect(reg.status).toBe(201);
  const { verifier, challenge } = pkce();
  const back = await consent(page, authorizeURL(reg.body.client_id, challenge));
  const tok = await exchange({ grant_type: 'authorization_code', code: back.searchParams.get('code'), client_id: reg.body.client_id,
    redirect_uri: REDIRECT, code_verifier: verifier, resource: RENOXIUM + '/mcp' });
  expect(tok.status).toBe(200);
  return { clientID: reg.body.client_id, ...tok.body };
}

// mcp connects to an MCP endpoint. tool() returns the parsed result, or
// throws with the tool's error text (as `.toolError`) when isError is set.
export async function mcp(url, token) {
  const headers = token ? { Authorization: 'Bearer ' + token } : {};
  const client = new Client({ name: 'renoxium-e2e', version: '1.0.0' });
  await client.connect(new StreamableHTTPClientTransport(new URL(url), { requestInit: { headers } }));
  const call = async (name, args = {}) => {
    const r = await client.callTool({ name, arguments: args });
    const text = (r.content || []).filter((c) => c.type === 'text').map((c) => c.text).join('\n');
    if (r.isError) {
      const e = new Error(`${name}: ${text}`);
      e.toolError = text;
      throw e;
    }
    if (r.structuredContent) return r.structuredContent;
    try { return JSON.parse(text); } catch { return text; }
  };
  const tools = async () => (await client.listTools()).tools.map((t) => t.name);
  return { client, call, tools, close: () => client.close() };
}
export const builder = (token) => mcp(RENOXIUM + '/mcp', token);

// choosePlan sets how an app is paid for on its Plan page: a shop, or a
// work app subscribed through the (fake) Stripe Checkout.
export async function choosePlan(page, app, plan = 'shop') {
  await page.goto(`/apps/${app}/plan`);
  if (plan === 'shop') {
    const make = page.getByRole('button', { name: 'Make it a shop' });
    if (await make.count()) await make.click();
    await expect(page.locator('body')).toContainText(/the shop (can go live|takes card payments)/);
    return;
  }
  await page.getByRole('button', { name: /^(Make it a work app|Subscribe), AED 99 a month$/ }).click();
  await page.getByRole('button', { name: 'Pay' }).click();
  await page.waitForURL(new RegExp(`/apps/${app}/plan\\?done=1`));
  await expect(page.getByRole('button', { name: 'Manage subscription' })).toBeVisible();
}

// confirmPublish opens the publish confirmation link and publishes. An app
// whose plan is not chosen yet is made a shop (or `plan`) first.
export async function confirmPublish(page, confirmURL, { plan = 'shop' } = {}) {
  await page.goto(confirmURL);
  if (await page.getByRole('link', { name: 'Choose a plan' }).count()) {
    await choosePlan(page, new URL(confirmURL, RENOXIUM).pathname.split('/')[2], plan);
    await page.goto(confirmURL);
  }
  await page.getByRole('button', { name: /Publish|Roll back/ }).click();
  await expect(page.locator('body')).toContainText(/live/i);
}

// buildStore creates a store-uae app through the agent and publishes it.
export async function buildStore(page, agent, app, { shopName = 'E2E Perfumes', whatsapp = '+971500000001', plan = 'shop' } = {}) {
  await agent.call('create_app', { app });
  const def = storeExample();
  def.seed.store[0].name = shopName;
  if ('whatsapp' in def.seed.store[0]) def.seed.store[0].whatsapp = whatsapp;
  const applied = await agent.call('apply_change', { app, definition: def });
  const pub = await agent.call('publish', { app });
  await confirmPublish(page, pub.confirm_url, { plan });
  return { def, applied, pub, live: appURL(app), preview: appURL(app, true) };
}

// --- A store-uae shop as a shopper uses it ---
// addToCart opens a product from the shop's front page, picks an option
// (a size, with its own price) when given, and adds it to the cart.
export async function addToCart(page, live, product, { option, qty } = {}) {
  await page.goto(live + '/');
  await page.getByRole('link', { name: product }).first().click();
  await expect(page.getByRole('heading', { level: 1, name: product })).toBeVisible();
  if (option) await page.getByRole('radio', { name: option }).check();
  if (qty) await page.getByRole('spinbutton', { name: 'Quantity' }).fill(String(qty));
  await page.getByRole('button', { name: 'Add to cart' }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Added to cart' })).toBeVisible();
}
// fillCheckout fills the cart page's delivery details and picks the payment.
export async function fillCheckout(page, { phone = '0501234567', address = 'Villa 7, Street 12, Al Barsha', emirate = 'Dubai', payment } = {}) {
  await page.getByLabel('Mobile number').fill(phone);
  await page.getByLabel('Delivery address').fill(address);
  await page.getByLabel('Emirate').selectOption(emirate);
  if (payment) await page.getByRole('radio', { name: payment === 'cod' ? /^Cash on delivery/ : /^Card payment/ }).check();
}
export const placeOrder = (page) => page.getByRole('button', { name: 'Place order' }).click();

// ownerWithStore signs a new owner up, connects an agent and publishes a store.
export async function ownerWithStore(browser, opts = {}) {
  const owner = await person(browser);
  const address = opts.email || email('owner');
  await signUp(owner.page, address);
  const token = await connectAgent(owner.page);
  const agent = await builder(token.access_token);
  const app = opts.app || unique('shop');
  const store = await buildStore(owner.page, agent, app, opts);
  return { ...owner, email: address, token, agent, app, ...store };
}

// connectStripe runs the (fake) Stripe onboarding to the end.
export async function connectStripe(page, app, choice = 'Submit and activate') {
  await page.goto(`/apps/${app}/payments`);
  await page.getByRole('button', { name: /Connect Stripe|Continue Stripe setup/ }).click();
  await page.getByRole('button', { name: choice }).click();
  await page.waitForURL(new RegExp(`/apps/${app}/payments`));
}


// portal opens Manage billing and presses one of the (fake) portal's buttons.
export async function portal(page, action) {
  await page.goto('/billing');
  await page.getByRole('button', { name: 'Manage billing' }).click();
  await page.getByRole('button', { name: action }).click();
  await page.waitForURL(/\/billing\?done=1/);
}

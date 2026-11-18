// Fake third-party services for the end-to-end run, on one local port:
//
//   /stripe/...   Stripe API (Accounts v2 + v1 calls Renoxium makes) and the
//                 browser pages Stripe would show: onboarding, Checkout,
//                 Customer Portal. Paying or finishing onboarding sends the
//                 signed webhooks Stripe would send.
//   /resend/...   Resend email API
//   /meta/...     WhatsApp Cloud API
//   /cf/...       Cloudflare custom hostnames API
//   /extapi/<host>/...  the external APIs apps reach with http.fetch
//                 (FETCH_API_BASE sends every call here)
//   /_fake/...    control and inspection for the tests
//
// Everything is kept in memory; nothing leaves this machine.
import http from 'node:http';
import crypto from 'node:crypto';

export function startFakes({ port = 0, renoxiumURL, webhookSecret, billingWebhookSecret, pricePro, priceBusiness }) {
  const state = {
    emails: [], whatsapp: [], stripeCalls: [], apiCalls: [],
    accounts: new Map(),      // acct_ → { id, email, country, charges_enabled, details_submitted }
    links: new Map(),         // link id → { account, return_url, refresh_url }
    sessions: new Map(),      // cs_ → session
    customers: new Map(),     // cus_ → { id, email, metadata }
    subscriptions: new Map(), // sub_ → subscription
    portals: new Map(),       // bps_ → { customer, return_url }
    hostnames: new Map(),     // cf id → hostname object
    txt: new Map(),           // DNS name → TXT value, for domain ownership proofs
    webhookLog: [],
  };
  let seq = 0;
  const id = (prefix) => `${prefix}_${Date.now().toString(36)}${(++seq).toString(36)}`;
  let base = '';

  const sign = (payload, secret) => {
    const t = Math.floor(Date.now() / 1000);
    const v1 = crypto.createHmac('sha256', secret).update(`${t}.${payload}`).digest('hex');
    return `t=${t},v1=${v1}`;
  };
  async function webhook(path, secret, event) {
    const payload = JSON.stringify({ id: id('evt'), object: 'event', ...event });
    const res = await fetch(renoxiumURL + path, { method: 'POST', body: payload,
      headers: { 'Content-Type': 'application/json', 'Stripe-Signature': sign(payload, secret) } });
    state.webhookLog.push({ path, type: event.type, status: res.status });
    if (res.status >= 300) throw new Error(`webhook ${event.type} -> ${res.status} ${await res.text()}`);
  }
  const connectEvent = (type, account, object) => webhook('/stripe/webhook', webhookSecret, { type, account, data: { object } });
  const billingEvent = (type, object) => webhook('/stripe/billing-webhook', billingWebhookSecret, { type, data: { object } });

  const accountJSON = (a) => ({ id: a.id, object: 'account', email: a.email, country: a.country, default_currency: 'aed',
    charges_enabled: a.charges_enabled, payouts_enabled: a.charges_enabled, details_submitted: a.details_submitted });
  const subJSON = (s) => ({ id: s.id, object: 'subscription', customer: s.customer, status: s.status, metadata: s.metadata,
    cancel_at_period_end: s.cancel_at_period_end,
    items: { data: [{ price: { id: s.price }, current_period_end: s.period_end }] } });

  async function readBody(req) {
    const chunks = [];
    for await (const c of req) chunks.push(c);
    return Buffer.concat(chunks).toString();
  }
  // Stripe's form encoding: a[b][c]=v → nested objects.
  function parseForm(raw) {
    const out = {};
    for (const [k, v] of new URLSearchParams(raw)) {
      const parts = k.replace(/\]/g, '').split('[');
      let o = out;
      parts.slice(0, -1).forEach((p) => { o = o[p] ??= {}; });
      o[parts.at(-1)] = v;
    }
    return out;
  }
  const json = (res, status, body) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(body)); };
  const html = (res, body) => { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
    res.end(`<!doctype html><html><head><meta charset="utf-8"><title>Fake Stripe</title><style>body{font:16px system-ui;max-width:520px;margin:40px auto}
button{display:block;margin:8px 0;padding:10px 16px;font:inherit}.note{background:#fff3cd;padding:8px}</style></head><body>
<p class="note">Fake Stripe for the Renoxium end-to-end run. No money moves.</p>${body}</body></html>`); };
  const redirect = (res, to) => { res.writeHead(303, { Location: to }); res.end(); };
  const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
  const stripeError = (res, status, message) => json(res, status, { error: { type: 'invalid_request_error', message } });

  async function stripeAPI(req, res, path, body) {
    const account = req.headers['stripe-account'] || '';
    const form = req.headers['content-type']?.includes('json') ? JSON.parse(body || '{}') : parseForm(body);
    state.stripeCalls.push({ method: req.method, path, account, form });
    let m;
    if (req.method === 'POST' && path === '/v2/core/accounts') {
      const a = { id: id('acct'), email: form.contact_email, country: form.identity?.country || 'AE', charges_enabled: false, details_submitted: false };
      state.accounts.set(a.id, a);
      return json(res, 200, { id: a.id, object: 'v2.core.account', contact_email: a.email });
    }
    if (req.method === 'GET' && (m = path.match(/^\/v1\/accounts\/([\w]+)$/))) {
      const a = state.accounts.get(m[1]);
      return a ? json(res, 200, accountJSON(a)) : stripeError(res, 404, 'No such account');
    }
    if (req.method === 'POST' && path === '/v1/account_links') {
      if (!state.accounts.has(form.account)) return stripeError(res, 400, 'No such account');
      const l = id('link');
      state.links.set(l, { account: form.account, return_url: form.return_url, refresh_url: form.refresh_url });
      return json(res, 200, { object: 'account_link', url: `${base}/stripe/pages/onboard/${l}` });
    }
    if (req.method === 'POST' && path === '/v1/checkout/sessions') {
      const s = { id: id('cs'), object: 'checkout.session', account, mode: form.mode, form, status: 'open', payment_status: 'unpaid',
        success_url: form.success_url, cancel_url: form.cancel_url, client_reference_id: form.client_reference_id,
        metadata: form.metadata || {}, customer: form.customer, customer_email: form.customer_email };
      if (s.mode === 'payment') {
        if (!state.accounts.get(account)?.charges_enabled) return stripeError(res, 400, 'The account cannot take charges yet');
        s.currency = form.line_items?.[0]?.price_data?.currency;
        s.amount_total = Object.values(form.line_items || {}).reduce((t, li) => t + Number(li.price_data.unit_amount) * Number(li.quantity), 0);
        s.application_fee_amount = Number(form.payment_intent_data?.application_fee_amount || 0);
      } else {
        s.price = form.line_items?.[0]?.price;
      }
      s.url = `${base}/stripe/pages/checkout/${s.id}`;
      state.sessions.set(s.id, s);
      return json(res, 200, { id: s.id, object: 'checkout.session', url: s.url, status: s.status, payment_status: s.payment_status });
    }
    if (req.method === 'POST' && path === '/v1/customers') {
      const c = { id: id('cus'), email: form.email, metadata: form.metadata || {} };
      state.customers.set(c.id, c);
      return json(res, 200, { id: c.id, object: 'customer', email: c.email });
    }
    if (req.method === 'GET' && path === '/v1/subscriptions') {
      const q = new URL(req.url, 'http://x').searchParams;
      const data = [...state.subscriptions.values()].filter((s) => s.customer === q.get('customer')).reverse().map(subJSON);
      return json(res, 200, { object: 'list', data });
    }
    if (req.method === 'DELETE' && (m = path.match(/^\/v1\/subscriptions\/(\w+)$/))) {
      const s = state.subscriptions.get(m[1]);
      if (!s) return stripeError(res, 404, 'No such subscription');
      s.status = 'canceled';
      billingEvent('customer.subscription.deleted', subJSON(s)).catch(() => {}); // the account may be gone already
      return json(res, 200, subJSON(s));
    }
    if (req.method === 'POST' && path === '/v1/billing_portal/sessions') {
      const p = id('bps');
      state.portals.set(p, { customer: form.customer, return_url: form.return_url });
      return json(res, 200, { id: p, object: 'billing_portal.session', url: `${base}/stripe/pages/portal/${p}` });
    }
    return stripeError(res, 404, `fake Stripe has no ${req.method} ${path}`);
  }

  // --- Browser pages ---
  async function stripePage(req, res, path, body) {
    const form = Object.fromEntries(new URLSearchParams(body));
    let m;
    if ((m = path.match(/^\/onboard\/(\w+)$/))) {
      const l = state.links.get(m[1]);
      if (!l) return html(res, '<h1>This link expired</h1>');
      const a = state.accounts.get(l.account);
      if (req.method === 'GET') {
        return html(res, `<h1>Set up payments for ${esc(a.email)}</h1><form method="post">
<button name="do" value="complete">Submit and activate</button><button name="do" value="later">Submit, review pending</button>
<button name="do" value="leave">Save and return later</button></form>`);
      }
      state.links.delete(m[1]); // single use, like Stripe's
      if (form.do === 'complete') { a.details_submitted = true; a.charges_enabled = true; }
      if (form.do === 'later') { a.details_submitted = true; }
      if (form.do !== 'leave') await connectEvent('account.updated', a.id, accountJSON(a));
      return redirect(res, l.return_url);
    }
    if ((m = path.match(/^\/checkout\/(\w+)$/))) {
      const s = state.sessions.get(m[1]);
      if (!s) return html(res, '<h1>No such session</h1>');
      if (req.method === 'GET') {
        const total = s.mode === 'payment' ? `${(s.amount_total / 100).toFixed(2)} ${String(s.currency).toUpperCase()}` : s.price;
        return html(res, `<h1>Pay ${esc(total)}</h1><p id="ref">${esc(s.client_reference_id || '')}</p>
<p id="fee">application fee: ${s.application_fee_amount ?? 0}</p><form method="post">
<label>Email <input name="email" value="${esc(s.customer_email || '')}"></label><label>Name <input name="name" value="Test Buyer"></label>
<button name="do" value="pay">Pay</button><button name="do" value="decline">Card declined</button><button name="do" value="back">Back</button></form>`);
      }
      if (s.status !== 'open') return html(res, '<h1>This session is no longer open</h1>');
      if (form.do === 'decline') return html(res, '<h1 id="declined">Your card was declined</h1><a href="' + esc(s.cancel_url) + '">Back to the shop</a>');
      if (form.do === 'back') return redirect(res, s.cancel_url);
      s.status = 'complete';
      s.payment_status = 'paid';
      if (s.mode === 'payment') {
        s.payment_intent = id('pi');
        await connectEvent('checkout.session.completed', s.account, {
          id: s.id, object: 'checkout.session', status: 'complete', payment_status: 'paid', amount_total: s.amount_total, currency: s.currency,
          client_reference_id: s.client_reference_id, payment_intent: s.payment_intent, metadata: s.metadata,
          customer_details: { email: form.email || s.customer_email, name: form.name } });
      } else {
        const sub = { id: id('sub'), customer: s.customer, status: 'active', price: s.price, metadata: s.metadata,
          cancel_at_period_end: false, period_end: Math.floor(Date.now() / 1000) + 30 * 86400 };
        state.subscriptions.set(sub.id, sub);
        await billingEvent('customer.subscription.created', subJSON(sub));
      }
      return redirect(res, s.success_url);
    }
    if ((m = path.match(/^\/portal\/(\w+)$/))) {
      const p = state.portals.get(m[1]);
      if (!p) return html(res, '<h1>No such portal session</h1>');
      const sub = [...state.subscriptions.values()].reverse().find((s) => s.customer === p.customer && s.status !== 'canceled');
      if (req.method === 'GET') {
        return html(res, `<h1>Billing portal</h1><p id="current">${sub ? esc(`${sub.price} ${sub.status}${sub.cancel_at_period_end ? ' cancels at period end' : ''}`) : 'no subscription'}</p>
<form method="post"><button name="do" value="business">Switch to Business</button><button name="do" value="pro">Switch to Pro</button>
<button name="do" value="cancel_end">Cancel at period end</button><button name="do" value="renew">Renew</button>
<button name="do" value="cancel_now">Cancel now</button><button name="do" value="past_due">Payment fails (past due)</button>
<button name="do" value="return">Return to Renoxium</button></form>`);
      }
      if (sub && form.do !== 'return') {
        let type = 'customer.subscription.updated';
        if (form.do === 'business') sub.price = priceBusiness;
        if (form.do === 'pro') sub.price = pricePro;
        if (form.do === 'cancel_end') sub.cancel_at_period_end = true;
        if (form.do === 'renew') sub.cancel_at_period_end = false;
        if (form.do === 'past_due') sub.status = 'past_due';
        if (form.do === 'cancel_now') { sub.status = 'canceled'; type = 'customer.subscription.deleted'; }
        await billingEvent(type, subJSON(sub));
      }
      return redirect(res, p.return_url);
    }
    return html(res, '<h1>Not found</h1>');
  }

  // --- Cloudflare ---
  function cloudflare(req, res, path, body) {
    const ok = (result) => json(res, 200, { success: true, errors: [], result });
    let m;
    if ((m = path.match(/^\/zones\/[\w]+\/custom_hostnames$/)) && req.method === 'POST') {
      const b = JSON.parse(body);
      if ([...state.hostnames.values()].some((h) => h.hostname === b.hostname)) {
        return json(res, 409, { success: false, errors: [{ code: 1406, message: 'Duplicate custom hostname found.' }], result: null });
      }
      const h = { id: id('cf'), hostname: b.hostname, status: 'pending', verification_errors: ['custom hostname does not CNAME to this zone.'],
        ownership_verification: { name: `_cf-custom-hostname.${b.hostname}`, type: 'txt', value: crypto.randomUUID() },
        ssl: { status: 'pending_validation', validation_records: [], validation_errors: [] } };
      state.hostnames.set(h.id, h);
      return ok(h);
    }
    if ((m = path.match(/^\/zones\/[\w]+\/custom_hostnames\/([\w]+)$/))) {
      const h = state.hostnames.get(m[1]);
      if (!h) return json(res, 404, { success: false, errors: [{ code: 1436, message: 'The custom hostname was not found.' }], result: null });
      if (req.method === 'DELETE') { state.hostnames.delete(m[1]); return ok({ id: h.id }); }
      return ok(h);
    }
    return json(res, 404, { success: false, errors: [{ code: 7003, message: 'fake Cloudflare: no route' }], result: null });
  }

  // --- Control ---
  async function control(req, res, path, body) {
    const q = new URL(req.url, 'http://x').searchParams;
    const b = body ? JSON.parse(body) : {};
    if (path === '/emails') return json(res, 200, state.emails.filter((e) => !q.get('to') || e.to.includes(q.get('to'))));
    if (path === '/whatsapp') return json(res, 200, state.whatsapp.filter((m) => !q.get('to') || m.to === q.get('to').replace(/^\+/, '')));
    if (path === '/stripe/calls') return json(res, 200, state.stripeCalls);
    if (path === '/stripe/sessions') return json(res, 200, [...state.sessions.values()]);
    if (path === '/stripe/subscriptions') return json(res, 200, [...state.subscriptions.values()]);
    if (path === '/stripe/accounts') return json(res, 200, [...state.accounts.values()]);
    if (path === '/webhooks') return json(res, 200, state.webhookLog);
    if (path === '/stripe/refund') {
      // b: { session, amount (minor units), full }
      const s = state.sessions.get(b.session);
      await connectEvent('charge.refunded', s.account, { id: id('ch'), object: 'charge', payment_intent: s.payment_intent,
        amount: s.amount_total, amount_refunded: b.amount, refunded: !!b.full });
      return json(res, 200, { ok: true });
    }
    if (path === '/stripe/expire') {
      const s = state.sessions.get(b.session);
      s.status = 'expired';
      await connectEvent('checkout.session.expired', s.account, { id: s.id, object: 'checkout.session', status: 'expired',
        payment_status: 'unpaid', metadata: s.metadata, client_reference_id: s.client_reference_id });
      return json(res, 200, { ok: true });
    }
    if (path === '/stripe/replay') {
      // Sends the last checkout.session.completed again, as Stripe retries do.
      const s = state.sessions.get(b.session);
      await connectEvent('checkout.session.completed', s.account, { id: s.id, object: 'checkout.session', status: 'complete', payment_status: 'paid',
        amount_total: s.amount_total, currency: s.currency, payment_intent: s.payment_intent, metadata: s.metadata, customer_details: {} });
      return json(res, 200, { ok: true });
    }
    if (path === '/cf/activate') {
      const h = [...state.hostnames.values()].find((x) => x.hostname === b.hostname);
      if (!h) return json(res, 404, { error: 'no such hostname' });
      h.status = 'active'; h.ssl.status = 'active'; h.verification_errors = [];
      return json(res, 200, h);
    }
    if (path === '/cf/hostnames') return json(res, 200, [...state.hostnames.values()]);
    if (path === '/extapi/calls') return json(res, 200, state.apiCalls.filter((c) => !q.get('host') || c.host === q.get('host')));
    if (path === '/dns/txt') { state.txt.set(b.name, b.value); return json(res, 200, { ok: true }); } // b: { name, value }
    return json(res, 404, { error: 'no such control' });
  }

  const server = http.createServer(async (req, res) => {
    try {
      const url = new URL(req.url, 'http://x');
      const body = await readBody(req);
      const p = url.pathname;
      if (p.startsWith('/stripe/pages/')) return await stripePage(req, res, p.slice('/stripe/pages'.length), body);
      if (p.startsWith('/stripe/')) {
        if (!String(req.headers.authorization || '').match(/^(Basic|Bearer) /)) return stripeError(res, 401, 'No API key');
        return await stripeAPI(req, res, p.slice('/stripe'.length), body);
      }
      if (p === '/resend/emails' && req.method === 'POST') {
        const m = JSON.parse(body);
        for (const to of m.to) state.emails.push({ to, from: m.from, subject: m.subject, text: m.text, html: m.html, reply_to: m.reply_to, at: Date.now() });
        return json(res, 200, { id: id('email') });
      }
      if (p.startsWith('/meta/') && req.method === 'POST') {
        const m = JSON.parse(body);
        const params = m.template.components.flatMap((c) => c.parameters.map((x) => x.text));
        state.whatsapp.push({ to: m.to, template: m.template.name, language: m.template.language.code, params, at: Date.now() });
        return json(res, 200, { messaging_product: 'whatsapp', messages: [{ id: id('wamid') }] });
      }
      if (p.startsWith('/extapi/')) {
        // A courier-style API: /rates needs a Bearer key, /echo repeats what it was sent.
        const [host, ...rest] = p.slice('/extapi/'.length).split('/');
        const apiPath = '/' + rest.join('/');
        const auth = req.headers.authorization || '';
        state.apiCalls.push({ host, path: apiPath, method: req.method, query: Object.fromEntries(url.searchParams), authorization: auth, body, at: Date.now() });
        if (apiPath === '/rates') {
          if (!auth.startsWith('Bearer ')) return json(res, 401, { error: 'missing key' });
          return json(res, 200, { price: 25, currency: 'AED', emirate: url.searchParams.get('emirate') });
        }
        if (apiPath === '/echo') { res.writeHead(200, { 'Content-Type': 'text/plain' }); return res.end('you sent: ' + auth); }
        return json(res, 404, { error: 'no such endpoint' });
      }
      if (p.startsWith('/cf/')) return cloudflare(req, res, p.slice(3), body);
      if (p === '/dns') { // DNS over HTTPS, JSON form: only the TXT records set through /_fake/dns/txt exist
        const name = url.searchParams.get('name');
        const answer = state.txt.has(name) ? [{ name, type: 16, TTL: 60, data: `"${state.txt.get(name)}"` }] : [];
        return json(res, 200, { Status: 0, Answer: answer });
      }
      if (p.startsWith('/_fake/')) return await control(req, res, p.slice(6), body);
      res.writeHead(404); res.end('fake: not found');
    } catch (err) {
      console.error('fake services:', err);
      if (!res.headersSent) json(res, 500, { error: { type: 'api_error', message: String(err.message || err) } });
    }
  });
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => {
    base = `http://127.0.0.1:${server.address().port}`;
    resolve({ server, state, base, close: () => new Promise((r) => server.close(r)) });
  }));
}

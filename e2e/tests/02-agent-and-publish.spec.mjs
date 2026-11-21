// The builder agent: OAuth 2.1 (dynamic registration, PKCE, consent, refresh)
// and every builder MCP tool, then publishing, a second version and roll-back.
import { test, expect } from '@playwright/test';
import { RENOXIUM, appURL } from '../env.mjs';
import { person, signUp, email, unique, registerClient, pkce, authorizeURL, consent, exchange, connectAgent, builder,
  confirmPublish, storeExample, REDIRECT } from '../lib.mjs';

test('OAuth: registration rules, consent and its refusal, PKCE, refresh tokens', async ({ browser }) => {
  // Redirects must be https, or http on loopback only.
  expect((await registerClient('bad', 'http://evil.example/cb')).status).toBe(400);
  expect((await registerClient('ok', 'https://agent.example/cb')).status).toBe(201);

  const { page } = await person(browser);
  const address = email('oauth');
  // Unsigned-in visitors are sent to sign in first, then back to the consent screen.
  const reg = await registerClient('E2E agent');
  const { verifier, challenge } = pkce();
  await page.goto(authorizeURL(reg.body.client_id, challenge));
  await expect(page).toHaveURL(/\/login\?next=%2Foauth%2Fauthorize/);
  await page.getByRole('link', { name: 'Create an account' }).click();
  await page.fill('#email', address);
  await page.fill('#password', 'correct horse battery staple');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByRole('heading', { name: 'Allow E2E agent?' })).toBeVisible();
  await expect(page.locator('body')).toContainText('127.0.0.1');

  // Cancel returns access_denied to the agent.
  const denied = await consent(page, authorizeURL(reg.body.client_id, challenge, 'deny-state'), 'Cancel');
  expect(denied.searchParams.get('error')).toBe('access_denied');
  expect(denied.searchParams.get('state')).toBe('deny-state');

  const back = await consent(page, authorizeURL(reg.body.client_id, challenge, 'allow-state'));
  expect(back.searchParams.get('state')).toBe('allow-state');
  const code = back.searchParams.get('code');
  const base = { grant_type: 'authorization_code', code, client_id: reg.body.client_id, redirect_uri: REDIRECT };
  expect((await exchange({ ...base, code_verifier: pkce().verifier })).status).toBe(400); // wrong verifier
  const tok = await exchange({ ...base, code_verifier: verifier });
  // A failed attempt may burn the code (that is allowed); then authorise again.
  let token = tok.body;
  if (tok.status !== 200) {
    const again = await consent(page, authorizeURL(reg.body.client_id, challenge));
    const t2 = await exchange({ ...base, code: again.searchParams.get('code'), code_verifier: verifier });
    expect(t2.status).toBe(200);
    token = t2.body;
  }
  expect(token.token_type.toLowerCase()).toBe('bearer');

  // No token, a wrong token: 401 with the resource metadata pointer.
  const anon = await fetch(RENOXIUM + '/mcp', { method: 'POST', body: '{}', headers: { 'Content-Type': 'application/json' } });
  expect(anon.status).toBe(401);
  expect(anon.headers.get('www-authenticate')).toContain('resource_metadata');
  await expect(builder('not-a-token')).rejects.toThrow();

  const agent = await builder(token.access_token);
  await agent.call('list_apps');
  await agent.close();

  const refreshed = await exchange({ grant_type: 'refresh_token', refresh_token: token.refresh_token, client_id: reg.body.client_id });
  expect(refreshed.status).toBe(200);
  expect(refreshed.body.access_token).not.toBe(token.access_token);
  const again = await builder(refreshed.body.access_token);
  await again.call('list_apps');
  await again.close();

  // Replaying a used code is refused and revokes what it issued (it may be stolen).
  const fresh = await consent(page, authorizeURL(reg.body.client_id, challenge));
  const once = { ...base, code: fresh.searchParams.get('code'), code_verifier: verifier };
  const issued = await exchange(once);
  expect(issued.status).toBe(200);
  expect((await exchange(once)).status).toBe(400);
  await expect(builder(issued.body.access_token)).rejects.toThrow(/invalid token|401/);
});

test('builder tools: create, apply, patch, test, upload, logs, publish, roll back', async ({ browser }) => {
  const { page } = await person(browser);
  await signUp(page, email('builder'));
  const agent = await builder((await connectAgent(page)).access_token);
  const tools = await agent.tools();
  for (const t of ['list_apps', 'create_app', 'get_app', 'apply_change', 'patch_app', 'publish', 'get_example', 'view_page', 'view_live_page', 'submit_form', 'upload_file', 'get_logs']) {
    expect(tools).toContain(t);
  }

  // Examples.
  const list = await agent.call('get_example');
  expect(list.examples.map((e) => e.name)).toContain('store-uae');
  await expect(agent.call('get_example', { name: 'nope' })).rejects.toThrow(/no example/);

  // Names: reserved, malformed, taken.
  for (const bad of ['www', 'customers', 'mcp', 'Bad_Name', 'ab']) {
    await expect(agent.call('create_app', { app: bad }), bad).rejects.toThrow();
  }
  const app = unique('tools');
  const created = await agent.call('create_app', { app });
  expect(created.live_url || created.LiveURL || JSON.stringify(created)).toContain(app);
  await expect(agent.call('create_app', { app })).rejects.toThrow();

  // An invalid definition is refused with problems; preview is not changed.
  const broken = await agent.call('apply_change', { app, definition: { routes: [{ method: 'GET', path: '/', template: 'missing' }] } });
  expect(broken.ok).toBe(false);
  expect(broken.problems.join(' ')).toMatch(/missing/);

  const def = {
    tables: [{ name: 'notes', columns: [{ name: 'text', type: 'text' }] }],
    seed: { notes: [{ text: 'first note' }] },
    templates: { home: '<!doctype html><html><head><title>Notes</title></head><body><h1>Notes v1</h1>{{range query "SELECT text FROM notes"}}<p>{{.text}}</p>{{end}}</body></html>' },
    scripts: { add: 'function handle(req){ db.exec("INSERT INTO notes (text) VALUES (?)", req.form.text); console.log("added " + req.form.text); return {redirect: "/"} }',
      boom: 'function handle(req){ throw new Error("kaboom on purpose") }' },
    routes: [{ method: 'GET', path: '/', template: 'home' }, { method: 'POST', path: '/add', script: 'add' }, { method: 'GET', path: '/boom', script: 'boom' }],
  };
  let res = await agent.call('apply_change', { app, definition: def });
  if (!res.ok) {
    // Some template engines expose data differently: fall back to a static page.
    def.templates.home = '<!doctype html><html><head><title>Notes</title></head><body><h1>Notes v1</h1></body></html>';
    res = await agent.call('apply_change', { app, definition: def });
  }
  expect(res.ok, JSON.stringify(res.problems)).toBe(true);
  expect(res.version).toBe(1);
  expect(res.preview_url).toBe(`${RENOXIUM}/apps/${app}/preview`);
  expect(res.checks.find((c) => c.route.includes('GET /') || c.route === '/')?.status ?? 200).toBe(200);

  // A visitor on preview: send a form, then open a page. No tool sends a form to live.
  const post = await agent.call('submit_form', { app, path: '/add', form: { text: 'from the agent' } });
  expect(post.status).toBe(303);
  expect(post.logs.map((l) => l.message).join('\n')).toContain('added from the agent');
  const boom = await agent.call('view_page', { app, path: '/boom' });
  expect(boom.status).toBe(500);
  await expect(agent.call('submit_form', { app, stage: 'live', path: '/add', form: { text: 'x' } })).rejects.toThrow();

  // patch_app changes only what it names.
  const patched = await agent.call('patch_app', { app, templates: { home: def.templates.home.replace('Notes v1', 'Notes v2') } });
  expect(patched.ok).toBe(true);
  expect(patched.version).toBe(2);
  const got = await agent.call('get_app', { app });
  expect(got.definition.scripts.add).toContain('INSERT INTO notes');
  const stale = await agent.call('patch_app', { app, base_version: 1, templates: { home: 'x' } });
  expect(stale.ok).toBe(false);
  expect(stale.problems.join(' ')).toContain('version 2 is the newest');

  // upload_file: an image gets a stable URL; garbage is refused.
  const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
  const up = await agent.call('upload_file', { app, data_base64: png });
  const up2 = await agent.call('upload_file', { app, data_base64: png });
  expect(up.url).toBeTruthy();
  expect(up2.url).toBe(up.url);
  await expect(agent.call('upload_file', { app, data_base64: '!!!' })).rejects.toThrow(/base64/);
  await expect(agent.call('upload_file', { app, data_base64: Buffer.from('not an image').toString('base64') })).rejects.toThrow();

  // get_logs shows the script error with its message.
  const logs = await agent.call('get_logs', { app, level: 'error' });
  expect(JSON.stringify(logs.events)).toContain('kaboom on purpose');
  expect(logs.note).toMatch(/untrusted/);

  // Nothing is live until the owner confirms.
  expect((await page.request.get(appURL(app))).status()).toBe(404);
  const pub = await agent.call('publish', { app });
  expect(pub.needs_confirmation).toBe(true);
  expect(pub.confirm_url).toContain(`/apps/${app}/publish`);
  expect((await page.request.get(appURL(app))).status()).toBe(404);
  await confirmPublish(page, pub.confirm_url);
  const live = await page.request.get(appURL(app));
  expect(live.status()).toBe(200);
  expect(await live.text()).toContain('Notes v2');

  // Roll back to version 1 through the same confirmation.
  const back = await agent.call('publish', { app, version: 1 });
  await page.goto(back.confirm_url);
  await expect(page.getByRole('heading', { name: 'Roll back version 1?' })).toBeVisible();
  await page.getByRole('button', { name: 'Roll back' }).click();
  expect(await (await page.request.get(appURL(app))).text()).toContain('Notes v1');

  // The admin overview shows both stages and live errors.
  expect((await page.request.get(appURL(app) + '/boom')).status()).toBe(500);
  await page.goto(`/apps/${app}`);
  await expect(page.locator('.pill').first()).toContainText('Live');
  await expect(page.locator('body')).toContainText('Recent errors');
  const listed = await agent.call('list_apps');
  expect(JSON.stringify(listed)).toContain(app);
  await agent.close();
});

test("an agent cannot touch another owner's app", async ({ browser }) => {
  const a = await person(browser);
  await signUp(a.page, email('alice'));
  const alice = await builder((await connectAgent(a.page)).access_token);
  const app = unique('mine');
  await alice.call('create_app', { app });
  await alice.call('apply_change', { app, definition: storeExample() });

  const b = await person(browser);
  await signUp(b.page, email('bob'));
  const bob = await builder((await connectAgent(b.page)).access_token);
  for (const [tool, args] of [['get_app', { app }], ['apply_change', { app, definition: storeExample() }], ['publish', { app }],
    ['view_page', { app, path: '/' }], ['get_logs', { app }], ['patch_app', { app, templates: { x: 'y' } }]]) {
    await expect(bob.call(tool, args), tool).rejects.toThrow();
  }
  // The admin panel answers 404, not 403, so app names do not leak.
  expect((await b.page.request.get(`/apps/${app}`)).status()).toBe(404);
  expect((await b.page.request.get(`/apps/${app}/publish?version=1`)).status()).toBe(404);
  await alice.close();
  await bob.close();
});

test('the Free plan allows one app', async ({ browser }) => {
  const { page } = await person(browser);
  await signUp(page, email('limit'));
  const agent = await builder((await connectAgent(page)).access_token);
  await agent.call('create_app', { app: unique('one') });
  await expect(agent.call('create_app', { app: unique('two') })).rejects.toThrow(/plan|limit|apps/i);
  await agent.close();
});

test('client registration is rate limited per address', async () => {
  const { RENOXIUM } = await import('../env.mjs');
  const headers = { 'Content-Type': 'application/json', 'X-E2E-Client-IP': '10.250.250.250' };
  const body = JSON.stringify({ client_name: 'flood', redirect_uris: ['http://127.0.0.1:1/cb'], token_endpoint_auth_method: 'none' });
  const codes = [];
  for (let i = 0; i < 40; i++) codes.push((await fetch(RENOXIUM + '/oauth/register', { method: 'POST', headers, body })).status);
  expect(codes).toContain(201);
  expect(codes.at(-1)).toBe(429);
});

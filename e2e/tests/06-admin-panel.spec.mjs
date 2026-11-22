// The owner's admin panel: data tables (add, edit with an image, search,
// delete), changes showing in the live shop, staff (invite, what they may and
// may not do, removal), a custom domain from DNS records to serving traffic,
// and deleting a visitor.
import http from 'node:http';
import { test, expect } from '@playwright/test';
import { PORT } from '../env.mjs';
import { person, ownerWithStore, signUp, email, waitEmail, fake, upgrade } from '../lib.mjs';

let shop;
test.beforeAll(async ({ browser }) => {
  shop = await ownerWithStore(browser);
  await upgrade(shop.page, 'Pro'); // staff and domains need a paid plan
});

const PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==', 'base64');

test('products: add with a photo, edit, hide, search, delete; the shop follows', async () => {
  const { page } = shop;
  await page.goto(`/apps/${shop.app}`);
  await page.locator('.sidenav').getByRole('link', { name: 'Products', exact: true }).click();
  await expect(page.locator('.pagehead .sub')).toContainText('4 rows');

  await page.getByRole('link', { name: 'Add' }).click();
  await page.fill('#f-name', 'Saffron Rose');
  await page.fill('#f-price', '180');
  await page.fill('#f-stock', '5');
  if (await page.locator('#f-name_ar').count()) await page.fill('#f-name_ar', 'وردة الزعفران');
  if (await page.locator('#f-image').count()) {
    await page.locator('#f-image').setInputFiles({ name: 'rose.png', mimeType: 'image/png', buffer: PNG });
  }
  const active = page.locator('input[type=checkbox][name=active]');
  if (await active.count() && !(await active.isChecked())) await active.check();
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.locator('.ok')).toHaveText('Saved.');
  const id = page.url().match(/\/t\/products\/(\d+)/)?.[1];
  expect(id).toBeTruthy();

  const home = await (await page.request.get(shop.live + '/')).text();
  expect(home).toContain('Saffron Rose');
  if (await page.locator('img.thumb').count()) {
    const src = await page.locator('img.thumb').getAttribute('src');
    expect((await page.request.get(src)).status()).toBe(200);
  }

  // A wrong value is shown on its field and nothing is saved.
  await page.evaluate(() => document.querySelector('#f-price').setAttribute('type', 'text'));
  await page.fill('#f-price', 'cheap');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.locator('.error')).toContainText('Check the highlighted fields');

  await page.goto(`/apps/${shop.app}/t/products/${id}`);
  await page.fill('#f-price', '199');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.locator('.ok')).toHaveText('Saved.');
  expect(await (await page.request.get(`${shop.live}/p/${id}`)).text()).toContain('199');

  await page.goto(`/apps/${shop.app}/t/products?q=Saffron`);
  await expect(page.locator('tbody tr')).toHaveCount(1);
  await page.goto(`/apps/${shop.app}/t/products?q=nothing-like-this`);
  await expect(page.locator('body')).toContainText('Nothing matches');

  await page.goto(`/apps/${shop.app}/t/products/${id}`);
  await page.getByRole('button', { name: `Delete Products #${id}` }).or(page.getByRole('button', { name: new RegExp(`^Delete .*#${id}$`) })).click();
  await page.waitForURL(new RegExp(`/apps/${shop.app}/t/products$`));
  expect(await (await page.request.get(shop.live + '/')).text()).not.toContain('Saffron Rose');
  expect((await page.request.get(`${shop.live}/p/${id}`)).status()).toBe(404);
});

test('staff: invite, sign in, edit data; no payments, staff or publishing; removal', async ({ browser }) => {
  const { page } = shop;
  const staffEmail = email('staff');
  const t = Date.now();
  await page.goto(`/apps/${shop.app}/staff`);
  await page.fill('#email', staffEmail);
  await page.getByRole('button', { name: 'Add staff member' }).click();
  await expect(page.locator('ul.plain')).toContainText(staffEmail);
  await expect(page.locator('body')).toContainText('1 of 2 on your plan');
  const invite = await waitEmail(staffEmail, new RegExp(`help run ${shop.app}`), { after: t });
  expect(invite.reply_to).toBe(shop.email);

  const staff = await person(browser);
  await signUp(staff.page, staffEmail);
  await staff.page.goto('/');
  await expect(staff.page.getByRole('heading', { name: 'Shared with you' })).toBeVisible();
  await staff.page.getByRole('link', { name: shop.app, exact: true }).click();
  await expect(staff.page.locator('body')).toContainText('You have staff access to this app.');
  await staff.page.goto(`/apps/${shop.app}/t/orders`);
  await expect(staff.page.locator('h1')).toBeVisible();
  await staff.page.goto(`/apps/${shop.app}/t/products/2`);
  await staff.page.fill('#f-price', '97');
  await staff.page.getByRole('button', { name: 'Save' }).click();
  await expect(staff.page.locator('.ok')).toHaveText('Saved.');
  for (const p of ['payments', 'staff', 'domain', 'notifications', 'publish?version=1']) {
    expect((await staff.page.request.get(`/apps/${shop.app}/${p}`)).status(), p).toBe(404);
  }
  const del = await staff.page.request.post(`/apps/${shop.app}/visitors/1/delete`, { form: {} });
  expect([403, 404]).toContain(del.status());

  await page.goto(`/apps/${shop.app}/staff`);
  await page.locator('li', { hasText: staffEmail }).getByRole('button', { name: 'Remove' }).click();
  await expect(page.locator('body')).toContainText('No staff yet.');
  expect((await staff.page.request.get(`/apps/${shop.app}/t/orders`)).status()).toBe(404);
});

// get asks the server for a page as if it came to a custom domain through the tunnel.
const asHost = (host, path = '/') => new Promise((resolve, reject) => {
  http.get({ host: '127.0.0.1', port: PORT, path, headers: { Host: host } }, (res) => {
    let body = '';
    res.on('data', (c) => { body += c; });
    res.on('end', () => resolve({ status: res.statusCode, body }));
  }).on('error', reject);
});

test('custom domain: refused names, DNS records, active, served, removed', async () => {
  const { page } = shop;
  const host = `www.${shop.app}.example`;
  await page.goto(`/apps/${shop.app}/domain`);
  for (const bad of ['not a domain', 'other.localhost', 'www.renoxium.com']) {
    await page.fill('#host', bad);
    await page.getByRole('button', { name: 'Add domain' }).click();
    await expect(page.locator('.error')).toBeVisible();
  }
  await page.fill('#host', `https://${host.toUpperCase()}/`);
  await page.getByRole('button', { name: 'Add domain' }).click();
  // First the owner proves the domain is theirs; Cloudflare hears nothing before that.
  await expect(page.locator('body')).toContainText(`${host} · prove it is yours`);
  await expect(page.locator('tbody')).toContainText(`_renoxium.${host}`);
  const proof = (await page.locator('tbody code').first().textContent()).trim();
  expect(proof).toMatch(/^renoxium-verify=[0-9a-f]{32}$/);
  await page.getByRole('button', { name: 'Check now' }).click();
  await expect(page.locator('body')).toContainText('prove it is yours');
  expect((await fake.get('/cf/hostnames')).find((h) => h.hostname === host)).toBeUndefined();
  await fake.post('/dns/txt', { name: `_renoxium.${host}`, value: proof });
  await page.getByRole('button', { name: 'Check now' }).click();
  await expect(page.locator('body')).toContainText(`${host} · waiting for DNS`);
  await expect(page.locator('tbody')).toContainText('customers.localhost');
  await expect(page.locator('tbody')).toContainText(`_cf-custom-hostname.${host}`);
  await expect(page.locator('body')).toContainText('Not ready yet');
  expect((await asHost(host)).status).toBe(404);

  await fake.post('/cf/activate', { hostname: host });
  await page.getByRole('button', { name: 'Check now' }).click();
  await expect(page.locator('.ok')).toHaveText('active');
  await expect(page.locator('body')).toContainText(`Your app is live at https://${host}`);
  const served = await asHost(host);
  expect(served.status).toBe(200);
  expect(served.body).toContain('E2E Perfumes');

  await page.getByRole('button', { name: 'Remove domain' }).click();
  await expect(page.locator('#host')).toBeVisible();
  expect((await asHost(host)).status).toBe(404);
  expect((await fake.get('/cf/hostnames')).find((h) => h.hostname === host)).toBeUndefined();
});

test('the owner deletes a visitor account', async ({ browser }) => {
  const v = await person(browser);
  const address = email('visitor');
  await v.page.goto(`${shop.live}/_rx/auth/signup?next=/`);
  await v.page.fill('#name', 'Gone Soon');
  await v.page.fill('#email', address);
  await v.page.fill('#password', 'visitor password 1');
  await v.page.getByRole('button', { name: 'Create account' }).click();
  await v.page.waitForURL(shop.live + '/');

  const { page } = shop;
  await page.goto(`/apps/${shop.app}/visitors?q=${encodeURIComponent(address)}`);
  page.once('dialog', (d) => d.accept()); // the panel asks before deleting
  await page.locator('tr', { hasText: address }).getByRole('button', { name: 'Delete' }).click();
  await page.waitForLoadState('networkidle');
  await page.goto(`/apps/${shop.app}/visitors?q=${encodeURIComponent(address)}`);
  await expect(page.locator('body')).toContainText('Nobody matches');
  // Their session ends with the account.
  await v.page.goto(shop.live + '/orders');
  await expect(v.page).toHaveURL(/_rx\/auth\/login/);
});

// On a phone every console page is one column: no panel beside another,
// nothing wider than the screen, in light and dark.
test('the console on a phone: one column, nothing wider than the screen', async ({ browser }) => {
  for (const scheme of ['light', 'dark']) {
    const ctx = await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: scheme, storageState: await shop.page.context().storageState() });
    const page = await ctx.newPage();
    for (const path of ['/', '/templates', '/billing', '/account', '/connect', `/apps/${shop.app}`, `/apps/${shop.app}/sales`, `/apps/${shop.app}/status`,
      `/apps/${shop.app}/payments`, `/apps/${shop.app}/staff`, `/apps/${shop.app}/domain`, `/apps/${shop.app}/connections`, `/apps/${shop.app}/notifications`,
      `/apps/${shop.app}/rename`, `/apps/${shop.app}/delete`, `/apps/${shop.app}/t/products`, `/apps/${shop.app}/t/products/new`]) {
      await page.goto(path);
      const r = await page.evaluate(() => ({
        wide: document.documentElement.scrollWidth - innerWidth,
        cols: [...document.querySelectorAll('.bento,.grid2,.settings-grid')].map((e) => getComputedStyle(e).gridTemplateColumns.trim().split(/\s+/).length).filter((n) => n > 1).length,
      }));
      expect(r, `${scheme} ${path}`).toEqual({ wide: 0, cols: 0 });
    }
    await ctx.close();
  }
});

// A published store-uae shop as shoppers see it: catalogue, Arabic, product
// options, cart, discount codes, visitor accounts (email and WhatsApp),
// cash-on-delivery orders with their emails, the invoice, the badge, the
// shop's own MCP for shoppers' AI assistants, and the preview's simulated checkout.
import { test, expect } from '@playwright/test';
import { appURL } from '../env.mjs';
import { person, ownerWithStore, email, waitEmail, waitWhatsApp, linkIn, mcp, fake } from '../lib.mjs';

let shop; // one shop for the whole file
test.beforeAll(async ({ browser }) => {
  shop = await ownerWithStore(browser, { shopName: 'E2E Perfumes' });
});

async function visitorSignUp(page, live, { name = 'Sara Shopper', address = email('shopper'), next = '/cart' } = {}) {
  await page.goto(`${live}/_rx/auth/signup?next=${encodeURIComponent(next)}`);
  await page.fill('#name', name);
  await page.fill('#email', address);
  await page.fill('#password', 'shopper password 1');
  await page.getByRole('button', { name: 'Create account' }).click();
  await page.waitForURL(new RegExp(next.replace(/[?]/g, '\\?') + '$'));
  return address;
}
async function addToCart(page, live, product, variant) {
  await page.goto(live + '/');
  await page.locator('a.card', { hasText: product }).first().click();
  if (variant) await page.locator('#variant').selectOption({ label: variant });
  await page.locator('#add').click();
}
async function fillDelivery(page, payment) {
  await page.fill('#phone', '0501234567');
  await page.fill('#address', 'Villa 7, Street 12, Al Barsha');
  await page.locator('#emirate').selectOption('Dubai');
  await page.locator(`input[name=payment][value=${payment}]`).check();
}

test('catalogue, Arabic, product options and the badge', async ({ browser }) => {
  const { page } = await person(browser);
  await page.goto(shop.live);
  await expect(page.locator('body')).toContainText('E2E Perfumes');
  for (const p of ['Royal Oud', 'White Musk', 'Amber Night', 'Oud Gift Set']) await expect(page.locator('a.card', { hasText: p })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Made with Renoxium' })).toBeVisible(); // Free plan

  await page.locator('a[href*="lang=ar"]').first().click();
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl');
  await expect(page.locator('body')).toContainText('عود');
  await page.locator('a[href*="lang=en"]').first().click();
  await expect(page.locator('html')).not.toHaveAttribute('dir', 'rtl');

  await page.locator('a.card', { hasText: 'Royal Oud' }).first().click();
  await expect(page.locator('body')).toContainText('250');
  await page.locator('#variant').selectOption({ label: '100 ml' });
  await expect(page.locator('body')).toContainText('420');

  expect((await page.request.get(shop.live + '/p/999')).status()).toBe(404);
  const icon = await page.request.get(shop.live + '/favicon.ico');
  expect(icon.status()).toBe(200);
  expect(icon.headers()['content-type']).toBe('image/png');
  // The cart asks the shop for current prices of the keys it holds (product.variant).
  const api = await (await page.request.get(shop.live + '/api/products?ids=2,1.2')).json();
  expect(api.products.map((p) => p.name)).toEqual(expect.arrayContaining(['White Musk', 'Royal Oud – 100 ml']));
});

test('visitor accounts: sign-up, confirm, sign-out, sign-in, password reset', async ({ browser }) => {
  const { page } = await person(browser);
  const t = Date.now();
  const address = await visitorSignUp(page, shop.live, { next: '/' });
  const confirm = await waitEmail(address, /Confirm your email/, { after: t });
  expect(confirm.from).toContain(`${shop.app} via Renoxium`); // sent in the app's name
  await page.goto(linkIn(confirm, shop.live));
  await expect(page.locator('body')).toContainText(/confirmed/i);

  await page.goto(shop.live + '/_rx/auth/logout');
  await page.getByRole('button', { name: 'Sign out' }).click();
  await page.goto(shop.live + '/_rx/auth/login');
  await page.fill('#email', address);
  await page.fill('#password', 'wrong password 1');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.locator('.error')).toBeVisible();

  const t2 = Date.now();
  await page.getByRole('link', { name: 'Forgot your password?' }).click();
  await page.fill('#email', address);
  await page.getByRole('button', { name: 'Send me a link' }).click();
  const reset = await waitEmail(address, /Reset your password/, { after: t2 });
  await page.goto(linkIn(reset, shop.live));
  await page.fill('#password', 'new shopper password');
  await page.getByRole('button', { name: 'Save and sign in' }).click();
  await page.goto(shop.live + '/orders');
  await expect(page).toHaveURL(new RegExp(`${shop.live}/orders`));

  // The visitor is listed for the owner, who can delete them.
  await shop.page.goto(`/apps/${shop.app}/visitors`);
  await expect(shop.page.locator('tbody')).toContainText(address);
  await shop.page.fill('input[name=q]', address);
  await shop.page.getByRole('button', { name: 'Search' }).click();
  await expect(shop.page.locator('tbody tr')).toHaveCount(1);
});

test('sign-in with WhatsApp: code, wrong code, quota count', async ({ browser }) => {
  const { page } = await person(browser);
  const phone = '+971501112233';
  const t = Date.now();
  await page.goto(shop.live + '/_rx/auth/phone?next=/');
  await page.fill('#phone', '050 111 2233');
  await page.getByRole('button', { name: 'Send code' }).click();
  await expect(page.getByRole('heading', { name: 'Enter the code' })).toBeVisible();
  const msg = await waitWhatsApp(phone, { after: t, template: 'login_code' });
  const code = msg.params[0];
  expect(code).toMatch(/^\d{6}$/);

  await page.fill('#code', code === '000000' ? '111111' : '000000');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.locator('.error')).toBeVisible();
  await page.fill('#code', code);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(shop.live + '/');

  await shop.page.goto('/billing');
  await expect(shop.page.locator('body')).toContainText(/WhatsApp sign-in codes this month: [1-9]\d* of 50/);
  await shop.page.goto(`/apps/${shop.app}/visitors`);
  await expect(shop.page.locator('tbody')).toContainText(phone);
});

test('cash on delivery: discount code, order page, emails to shopper and owner, invoice, owner sets it shipped', async ({ browser }) => {
  const { page } = await person(browser);
  const address = await visitorSignUp(page, shop.live);
  await addToCart(page, shop.live, 'Royal Oud', '100 ml');
  await addToCart(page, shop.live, 'White Musk');
  await page.goto(shop.live + '/cart');
  await expect(page.locator('body')).toContainText('Royal Oud');
  await expect(page.locator('body')).toContainText('White Musk');

  // A wrong discount code sends the shopper back with a message.
  await fillDelivery(page, 'cod');
  await page.fill('#coupon', 'NOPE');
  await page.locator('form[action="/checkout"] button').click();
  await expect(page.locator('body')).toContainText('This discount code is not valid.');

  const t = Date.now();
  await fillDelivery(page, 'cod');
  await page.fill('#coupon', 'welcome10');
  await page.locator('form[action="/checkout"] button').click();
  await page.waitForURL(/\/orders\/\d+\?placed=1/);
  const order = page.url().match(/\/orders\/(\d+)/)[1];
  await expect(page.locator('body')).toContainText('Confirmed, pay on delivery');
  // 420 + 95 = 515, minus 10% = 463.50, free delivery over 200.
  await expect(page.locator('body')).toContainText('463.50');

  const toShopper = await waitEmail(address, new RegExp(`#${order}|order`, 'i'), { after: t });
  expect(toShopper.text).toContain('463.50');
  const toOwner = await waitEmail(shop.email, new RegExp(`New order #${order}`), { after: t });
  expect(toOwner.text).toContain('Royal Oud');

  await page.goto(shop.live + '/orders');
  await expect(page.locator('body')).toContainText(`#${order}`);
  await page.goto(`${shop.live}/orders/${order}/invoice`);
  await expect(page.locator('body')).toContainText(/invoice/i);
  await expect(page.locator('body')).toContainText('463.50');

  // Another visitor cannot see this order.
  const other = await person(browser);
  await visitorSignUp(other.page, shop.live, { next: '/' });
  const peek = await other.page.goto(`${shop.live}/orders/${order}`);
  expect(peek.status()).toBe(404);

  // The owner changes the status in the admin panel; the shopper sees it.
  await shop.page.goto(`/apps/${shop.app}/t/orders/${order}`);
  await shop.page.locator('select[name=status]').selectOption('shipped');
  await shop.page.getByRole('button', { name: 'Save' }).click();
  await expect(shop.page.locator('.ok')).toHaveText('Saved.');
  await page.goto(`${shop.live}/orders/${order}`);
  await expect(page.locator('body')).toContainText('Shipped');

  // The coupon use was counted.
  await shop.page.goto(`/apps/${shop.app}/t/coupons`);
  await expect(shop.page.locator('tbody')).toContainText('WELCOME10');
});

test('card payment is refused politely while Stripe is not connected', async ({ browser }) => {
  const { page } = await person(browser);
  await visitorSignUp(page, shop.live);
  await addToCart(page, shop.live, 'Amber Night');
  await page.goto(shop.live + '/cart');
  await fillDelivery(page, 'card');
  await page.locator('form[action="/checkout"] button').click();
  await expect(page).toHaveURL(/\/cart\?error=error_payment/);
  await expect(page.locator('body')).toContainText('Card payment is not available right now.');
  expect((await fake.stripeCalls()).filter((c) => c.path === '/v1/checkout/sessions' && c.form.metadata?.rx_app === shop.app)).toHaveLength(0);
});

test("the shop's MCP for shoppers' AI assistants needs no sign-in", async () => {
  const assistant = await mcp(shop.live + '/mcp');
  const tools = await assistant.tools();
  expect(tools.sort()).toEqual(['get_product', 'search_products', 'store_info']);
  const found = await assistant.call('search_products', { query: 'oud' });
  expect(JSON.stringify(found)).toContain('Royal Oud');
  const info = await assistant.call('store_info');
  expect(JSON.stringify(info)).toContain('E2E Perfumes');
  await assistant.close();
});

test('preview: simulated checkout pays or fails without Stripe', async () => {
  // The owner's signed preview link opens the preview with a cookie.
  const { page } = shop;
  await page.goto(`/apps/${shop.app}/preview`);
  await page.waitForURL(new RegExp(appURL(shop.app, true).replace(/[.]/g, '\\.')));
  const preview = appURL(shop.app, true);
  await page.goto(`${preview}/_rx/auth/signup?next=/cart`);
  await page.fill('#name', 'Preview Buyer');
  await page.fill('#email', email('preview'));
  await page.fill('#password', 'preview password 1');
  await page.getByRole('button', { name: 'Create account' }).click();
  for (const [decision, status] of [['Pay (simulated)', 'Paid'], ['Simulate a failed payment', 'Payment failed']]) {
    await addToCart(page, preview, 'White Musk');
    await page.goto(preview + '/cart');
    await fillDelivery(page, 'card');
    await page.locator('form[action="/checkout"] button').click();
    await expect(page.getByRole('heading', { name: /^Pay / })).toBeVisible();
    await expect(page.locator('.note')).toContainText('simulated checkout');
    await page.getByRole('button', { name: decision }).click();
    await page.waitForURL(/\/orders\/\d+/);
    await expect(page.locator('body')).toContainText(status);
  }
});

// Card payments through (fake) Stripe Connect: onboarding states, Checkout on
// the shop's own account with the shop's 2% fee, webhooks settling the
// order, declined and abandoned payments, paying again, expiry, refunds,
// repeated webhooks, owner notices by email and WhatsApp, and the sales tiles.
import { test, expect } from '@playwright/test';
import { RENOXIUM } from '../env.mjs';
import { person, ownerWithStore, email, waitEmail, waitWhatsApp, fake, connectStripe, addToCart, fillCheckout, placeOrder } from '../lib.mjs';

let shop;
test.beforeAll(async ({ browser }) => {
  shop = await ownerWithStore(browser);
});

async function shopper(browser) {
  const s = await person(browser);
  await s.page.goto(`${shop.live}/_rx/auth/signup?next=/`);
  await s.page.fill('#name', 'Card Buyer');
  await s.page.fill('#email', email('buyer'));
  await s.page.fill('#password', 'buyer password 12');
  await s.page.getByRole('button', { name: 'Create account' }).click();
  await s.page.waitForURL(shop.live + '/');
  return s;
}
// orderByCard puts one White Musk (95 + 15 delivery) in the cart and goes to
// the (fake) Stripe Checkout page.
async function orderByCard(page) {
  await addToCart(page, shop.live, 'White Musk');
  await page.goto(shop.live + '/cart');
  await fillCheckout(page, { phone: '0507654321', address: 'Office 3, Tower 1, JLT', emirate: 'Dubai', payment: 'card' });
  await placeOrder(page);
  await expect(page.getByRole('heading', { name: 'Pay 110.00 AED' })).toBeVisible();
  const ref = await page.locator('#ref').innerText();
  const session = (await fake.sessions()).find((s) => s.client_reference_id === ref && s.status === 'open');
  return { ref, order: ref.replace('order-', ''), session };
}

test('onboarding: explanation first, then leave, review pending, ready', async () => {
  const { page } = shop;
  await page.goto(`/apps/${shop.app}/payments`);
  await expect(page.locator('.note')).toContainText('Before you start, have these ready.');
  await expect(page.locator('.note')).toContainText('trade licence');
  await expect(page.locator('body')).toContainText('as a shop Renoxium takes 2% of each card payment');

  await connectStripe(page, shop.app, 'Save and return later');
  await expect(page.locator('body')).toContainText('connected but not ready to take payments yet');
  const created = (await fake.stripeCalls()).find((c) => c.path === '/v2/core/accounts' && c.form.contact_email === shop.email);
  expect(created.form.dashboard).toBe('full');
  expect(created.form.defaults.responsibilities).toEqual({ fees_collector: 'stripe', losses_collector: 'stripe' });

  // Stripe's refresh link (an expired onboarding link) starts a new one.
  await page.goto(`/apps/${shop.app}/payments/connect`);
  await page.getByRole('button', { name: 'Submit, review pending' }).click();
  await page.waitForURL(/payments\?returned=1/);
  await expect(page.locator('body')).toContainText('Stripe is reviewing your details');

  await connectStripe(page, shop.app, 'Submit and activate');
  await expect(page.locator('.ok')).toHaveText('Stripe is connected and ready to take payments.');
  // Only one Stripe account per app, however often the owner clicks.
  const accounts = (await fake.get('/stripe/accounts')).filter((a) => a.email === shop.email);
  expect(accounts).toHaveLength(1);
});

test('owner turns on WhatsApp notices with a code', async () => {
  const { page } = shop;
  const phone = '+971509998877';
  await page.goto(`/apps/${shop.app}/notifications`);
  await expect(page.locator('body')).toContainText(`emailed to ${shop.email}`);
  const t = Date.now();
  await page.fill('#phone', '+971 50 999 8877');
  await page.getByRole('button', { name: 'Send code' }).click();
  const msg = await waitWhatsApp(phone, { after: t });
  await page.fill('#code', '000001' === msg.params[0] ? '000002' : '000001');
  await page.getByRole('button', { name: 'Confirm' }).click();
  await expect(page.locator('.error')).toBeVisible();
  await page.fill('#code', msg.params[0]);
  await page.getByRole('button', { name: 'Confirm' }).click();
  await expect(page.locator('body')).toContainText(`WhatsApp notices go to ${phone}`);
});

test('a card payment: 2% fee on Free, webhook marks it paid, owner told by email and WhatsApp, repeats ignored', async ({ browser }) => {
  const { page } = await shopper(browser);
  const t = Date.now();
  const { ref, order, session } = await orderByCard(page);
  expect(session.account).toMatch(/^acct_/);                 // a direct charge on the shop's own account
  expect(session.application_fee_amount).toBe(220);          // 2% of AED 110.00
  expect(session.metadata).toEqual({ rx_app: shop.app, rx_reference: ref });
  expect(session.success_url).toBe(`${shop.live}/orders/${order}?placed=1`);

  await page.getByRole('button', { name: 'Pay' }).click();
  await page.waitForURL(`${shop.live}/orders/${order}?placed=1`);
  await expect(page.locator('body')).toContainText('Paid');

  const mail = await waitEmail(shop.email, /Payment received: AED 110.00/, { after: t });
  expect(mail.text).toContain(ref);
  const wa = await waitWhatsApp('+971509998877', { after: t, template: 'owner_notice' });
  expect(wa.params.join(' ')).toContain('Payment received');

  // Stripe retries a webhook: nothing happens twice.
  await fake.post('/stripe/replay', { session: session.id });
  await new Promise((r) => setTimeout(r, 1000));
  const mails = (await fake.emails(shop.email)).filter((m) => m.at >= t && /Payment received/.test(m.subject));
  expect(mails).toHaveLength(1);

  // The owner sees it under Payments and in the sales tiles.
  await shop.page.goto(`/apps/${shop.app}/payments`);
  await expect(shop.page.locator('tbody')).toContainText(ref);
  await shop.page.goto(`/apps/${shop.app}`);
  await expect(shop.page.locator('.kpis')).toContainText('AED 110.00');
});

test('declined card, back to the shop, pay again later', async ({ browser }) => {
  const { page } = await shopper(browser);
  const { order } = await orderByCard(page);
  await page.getByRole('button', { name: 'Card declined' }).click();
  await expect(page.locator('#declined')).toBeVisible();
  await page.getByRole('link', { name: 'Back to the shop' }).click();
  await page.waitForURL(`${shop.live}/orders/${order}`);
  await expect(page.locator('body')).toContainText('Waiting for payment');

  // "Pay now" on the order page starts a new Checkout for the same order.
  await page.getByRole('button', { name: 'Pay now' }).click();
  await expect(page.getByRole('heading', { name: 'Pay 110.00 AED' })).toBeVisible();
  await page.getByRole('button', { name: 'Pay' }).click();
  await page.waitForURL(new RegExp(`/orders/${order}`));
  await expect(page.locator('body')).toContainText('Paid');
});

test('abandoned and expired checkouts', async ({ browser }) => {
  const { page } = await shopper(browser);
  const first = await orderByCard(page);
  await page.getByRole('button', { name: 'Back' }).click();
  await page.waitForURL(`${shop.live}/orders/${first.order}`);
  await expect(page.locator('body')).toContainText('Waiting for payment');

  await fake.post('/stripe/expire', { session: first.session.id });
  await page.reload();
  await expect(page.locator('body')).toContainText('Payment not completed');
});

test('refunds made in the Stripe Dashboard reach the order', async ({ browser }) => {
  const { page } = await shopper(browser);
  const { order, session } = await orderByCard(page);
  await page.getByRole('button', { name: 'Pay' }).click();
  await page.waitForURL(new RegExp(`/orders/${order}`));
  await expect(page.locator('body')).toContainText('Paid');

  await fake.post('/stripe/refund', { session: session.id, amount: 1500, full: false });
  await page.reload();
  await expect(page.locator('body')).toContainText('Partly refunded');
  await fake.post('/stripe/refund', { session: session.id, amount: 11000, full: true });
  await page.reload();
  await expect(page.locator('body')).toContainText('Refunded');
});

test('webhooks need a valid Stripe signature', async () => {
  const body = JSON.stringify({ id: 'evt_forged', type: 'checkout.session.completed', account: 'acct_x', data: { object: { metadata: { rx_app: shop.app } } } });
  for (const sig of ['', 't=1,v1=00', `t=${Math.floor(Date.now() / 1000)},v1=${'a'.repeat(64)}`]) {
    const r = await fetch(RENOXIUM + '/stripe/webhook', { method: 'POST', body, headers: { 'Stripe-Signature': sig } });
    expect(r.status).toBe(400);
    const b = await fetch(RENOXIUM + '/stripe/billing-webhook', { method: 'POST', body, headers: { 'Stripe-Signature': sig } });
    expect(b.status).toBe(400);
  }
});

test('cash-on-delivery orders reach the owner on WhatsApp too; notices can be turned off', async ({ browser }) => {
  const { page } = await shopper(browser);
  const t = Date.now();
  await addToCart(page, shop.live, 'Amber Night');
  await page.goto(shop.live + '/cart');
  await fillCheckout(page, { phone: '0507654321', address: 'Office 3, Tower 1, JLT', emirate: 'Dubai', payment: 'cod' });
  await placeOrder(page);
  await page.waitForURL(/\/orders\/\d+\?placed=1/);
  const wa = await waitWhatsApp('+971509998877', { after: t, template: 'owner_notice' });
  expect(wa.params[0]).toBe(shop.app);
  expect(wa.params.join(' ')).toContain('New order');

  await shop.page.goto(`/apps/${shop.app}/notifications`);
  await expect(shop.page.locator('body')).toContainText(/[1-9]\d* of 50 used this month/);
  await shop.page.getByRole('button', { name: 'Turn off WhatsApp notices' }).click();
  await expect(shop.page.locator('#phone')).toBeVisible();
});

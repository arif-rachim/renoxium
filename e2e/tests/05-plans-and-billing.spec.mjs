// How apps are paid for: a shop (2% of card payments, staff and its own
// domain), a work app through Stripe Checkout (no fee, more staff, the
// subscription in the Customer Portal), and a work app paused when its
// subscription ends. And the account: free (3 apps) or a member (a card
// saved, which charges nothing: 50 apps).
import { test, expect } from '@playwright/test';
import { person, ownerWithStore, email, fake, choosePlan, portal, connectStripe, addToCart, fillCheckout, placeOrder } from '../lib.mjs';
import { SECRETS } from '../env.mjs';

test('a shop, then a work app: what each allows, the subscription, and a pause when it ends', async ({ browser }) => {
  const shop = await ownerWithStore(browser); // published as a shop
  const { page } = shop;

  await page.goto('/billing');
  await expect(page.locator('body')).toContainText('Your account: Free');
  await expect(page.locator('body')).toContainText('shops pay 2% of their card payments');
  await expect(page.locator('table')).toContainText(shop.app);
  await expect(page.locator('table')).toContainText('Shop');
  await page.goto(`/apps/${shop.app}/domain`);
  await expect(page.locator('#host')).toBeVisible();
  await page.goto(`/apps/${shop.app}/staff`);
  await expect(page.locator('body')).toContainText('0 of 2 on your plan');

  // A work app through Checkout: one customer, the work app price, the app in metadata.
  await choosePlan(page, shop.app, 'work');
  const sub = (await fake.sessions()).filter((s) => s.mode === 'subscription').at(-1);
  expect(sub.price).toBe(SECRETS.priceWorkApp);
  expect(sub.metadata.rx_app).toBe(shop.app);
  await expect(page.locator('body')).toContainText(/Renews \d+ \w+ \d{4}/);
  await page.goto(`/apps/${shop.app}/staff`);
  await expect(page.locator('body')).toContainText('0 of 5 on your plan');

  // A paid work app's card payments carry no Renoxium fee.
  await connectStripe(page, shop.app);
  const buyer = await person(browser);
  await buyer.page.goto(`${shop.live}/_rx/auth/signup?next=/`);
  await buyer.page.fill('#name', 'Work Buyer');
  await buyer.page.fill('#email', email('workbuyer'));
  await buyer.page.fill('#password', 'buyer password 12');
  await buyer.page.getByRole('button', { name: 'Create account' }).click();
  await addToCart(buyer.page, shop.live, 'Oud Gift Set');
  await buyer.page.goto(shop.live + '/cart');
  await fillCheckout(buyer.page, { phone: '0501231234', address: 'Flat 1, Marina', emirate: 'Dubai' });
  await placeOrder(buyer.page);
  await expect(buyer.page.locator('#fee')).toHaveText('application fee: 0');

  // The subscription lives in the Customer Portal; a failed renewal keeps the app.
  await portal(page, 'Payment fails (past due)');
  await page.goto(`/apps/${shop.app}/plan`);
  await expect(page.locator('body')).toContainText('past_due');
  expect((await page.request.get(shop.live)).status()).toBe(200);

  // When it ends, the work app is paused until it is renewed.
  await portal(page, 'Cancel now');
  await expect.poll(async () => (await page.request.get(shop.live)).status(), { timeout: 40_000, intervals: [2000] }).toBe(402);
  await page.goto(`/apps/${shop.app}/plan`);
  await expect(page.getByRole('button', { name: 'Subscribe, AED 99 a month' })).toBeVisible();
  const customers = (await fake.stripeCalls()).filter((c) => c.path === '/v1/customers' && c.form.email === shop.email);
  expect(customers).toHaveLength(1);
});

test('closing the Checkout page without paying leaves the work app unpaid', async ({ browser }) => {
  const shop = await ownerWithStore(browser);
  const { page } = shop;
  await page.goto(`/apps/${shop.app}/plan`);
  await page.getByRole('button', { name: 'Make it a work app, AED 99 a month' }).click();
  await page.getByRole('button', { name: 'Back' }).click();
  await page.waitForURL(new RegExp(`/apps/${shop.app}/plan$`));
  await expect(page.getByRole('button', { name: 'Make it a work app, AED 99 a month' })).toBeVisible();
  expect((await page.request.get(shop.live)).status()).toBe(200); // still a shop
});

test('saving a card makes a member: 50 apps, the same per-app prices; removing it keeps the apps', async ({ browser }) => {
  const shop = await ownerWithStore(browser); // one app, a shop
  const { page } = shop;
  await page.goto('/billing');
  await expect(page.locator('body')).toContainText('A free account may have 3 apps');
  await expect(page.locator('body')).toContainText('Up to 3 apps');

  // Checkout in setup mode on the account's customer: nothing is charged.
  await page.getByRole('button', { name: 'Save a card' }).click();
  await expect(page.locator('h1')).toHaveText('Save a card');
  const setup = (await fake.sessions()).filter((s) => s.mode === 'setup').at(-1);
  expect(setup.customer).toMatch(/^cus_/);
  expect(setup.price).toBeUndefined();
  await page.getByRole('button', { name: 'Save card', exact: true }).click();
  await page.waitForURL(/\/billing\?done=1/);
  await expect(page.locator('body')).toContainText('Your account: Member');
  await expect(page.locator('body')).toContainText('Visa ending 4242');
  await expect(page.locator('body')).toContainText('shops pay 2% of their card payments');
  expect((await fake.get('/webhooks')).filter((w) => w.path === '/stripe/billing-webhook' && w.type === 'setup_intent.succeeded').at(-1).status).toBe(204);

  // A member's shop still pays the 2% fee.
  await connectStripe(page, shop.app);
  const buyer = await person(browser);
  await buyer.page.goto(`${shop.live}/_rx/auth/signup?next=/`);
  await buyer.page.fill('#name', 'Member Buyer');
  await buyer.page.fill('#email', email('memberbuyer'));
  await buyer.page.fill('#password', 'buyer password 12');
  await buyer.page.getByRole('button', { name: 'Create account' }).click();
  await addToCart(buyer.page, shop.live, 'Oud Gift Set');
  await buyer.page.goto(shop.live + '/cart');
  await fillCheckout(buyer.page, { phone: '0501231299', address: 'Flat 2, Marina', emirate: 'Dubai' });
  await placeOrder(buyer.page);
  await expect(buyer.page.locator('#fee')).not.toHaveText('application fee: 0');

  // Up to 50 apps: a fourth is accepted.
  for (let i = 2; i <= 4; i++) await shop.agent.call('create_app', { app: `${shop.app}-${i}` });
  await page.goto('/');
  await expect(page.locator('body')).toContainText('4 of 50 apps used');

  // The card removed in the portal: free again, the 4 apps stay, a fifth is refused.
  await portal(page, 'Remove card');
  await expect(page.locator('body')).toContainText('Your account: Free');
  await page.goto('/');
  await expect(page.locator('body')).toContainText('4 of 3 apps used');
  expect((await page.request.get(shop.live)).status()).toBe(200);
  await expect(shop.agent.call('create_app', { app: `${shop.app}-5` })).rejects.toThrow(/A free account may have 3 apps/);

  // An expired card does not count.
  await page.goto('/billing');
  await page.getByRole('button', { name: 'Save a card' }).click();
  await page.getByRole('button', { name: 'Save an expired card' }).click();
  await page.waitForURL(/\/billing\?done=1/);
  await expect(page.locator('body')).toContainText('Your account: Free');
  await expect(page.locator('body')).toContainText('card expired');
});

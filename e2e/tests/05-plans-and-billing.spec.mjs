// Renoxium's own plans: Free → Pro through Stripe Checkout, what each plan
// unlocks (no fee, no badge, more apps, staff, domains), changes and
// cancellation in the Customer Portal, payment trouble, back to Free.
import { test, expect } from '@playwright/test';
import { person, ownerWithStore, email, unique, fake, upgrade, portal, connectStripe, addToCart, fillCheckout, placeOrder } from '../lib.mjs';
import { SECRETS } from '../env.mjs';

test('upgrade, change plan, cancel, and what each plan allows', async ({ browser }) => {
  const shop = await ownerWithStore(browser);
  const { page, agent } = shop;

  await page.goto('/billing');
  await expect(page.locator('body')).toContainText('Current plan: Free');
  await expect(page.locator('body')).toContainText(/Apps\s*1 of 1/);
  await expect(page.locator('body')).toContainText('takes 2% of card payments');
  await page.goto(`/apps/${shop.app}/domain`);
  await expect(page.locator('body')).toContainText('included in the Pro and Business plans');
  await page.goto(`/apps/${shop.app}/staff`);
  await expect(page.locator('body')).toContainText('Staff need the Pro or Business plan');

  // Pro through Checkout: one customer, the right price, the account in metadata.
  await upgrade(page, 'Pro');
  const sub = (await fake.sessions()).filter((s) => s.mode === 'subscription').at(-1);
  expect(sub.price).toBe(SECRETS.pricePro);
  expect(sub.metadata.rx_plan).toBe('pro');
  await expect(page.locator('body')).toContainText(/active · renews \d+ \w+ \d{4}/);
  await expect(page.locator('body')).toContainText(/Apps\s*1 of 3/);
  await expect(page.locator('body')).toContainText('of 500');

  // The badge is gone within the plan cache time; card payments carry no fee.
  await expect.poll(async () => (await (await page.request.get(shop.live)).text()).includes('Made with Renoxium'), { timeout: 40_000, intervals: [2000] }).toBe(false);
  await connectStripe(page, shop.app);
  const buyer = await person(browser);
  await buyer.page.goto(`${shop.live}/_rx/auth/signup?next=/`);
  await buyer.page.fill('#name', 'Pro Buyer');
  await buyer.page.fill('#email', email('probuyer'));
  await buyer.page.fill('#password', 'buyer password 12');
  await buyer.page.getByRole('button', { name: 'Create account' }).click();
  await addToCart(buyer.page, shop.live, 'Oud Gift Set');
  await buyer.page.goto(shop.live + '/cart');
  await fillCheckout(buyer.page, { phone: '0501231234', address: 'Flat 1, Marina', emirate: 'Dubai' });
  await expect(buyer.page.getByRole('radio', { name: /^Card payment/ })).toBeChecked(); // card is the default payment
  await placeOrder(buyer.page);
  await expect(buyer.page.locator('#fee')).toHaveText('application fee: 0');

  // More apps, staff and a domain are now allowed.
  await agent.call('create_app', { app: unique('second') });
  await page.goto(`/apps/${shop.app}/domain`);
  await expect(page.locator('#host')).toBeVisible();
  await page.goto(`/apps/${shop.app}/staff`);
  await expect(page.locator('body')).toContainText('0 of 2 on your plan');

  // Choosing a plan again while paying goes to the portal, not a second subscription.
  await page.goto('/billing');
  await expect(page.getByRole('button', { name: 'Choose Business' })).toHaveCount(0);

  await portal(page, 'Switch to Business');
  await expect(page.locator('body')).toContainText('Current plan: Business');
  await expect(page.locator('body')).toContainText('of 2,000');
  await page.goto(`/apps/${shop.app}/staff`);
  await expect(page.locator('body')).toContainText('0 of 10 on your plan');

  await portal(page, 'Cancel at period end');
  await expect(page.locator('body')).toContainText(/Business[\s\S]*ends \d+ \w+ \d{4}/);
  await portal(page, 'Renew');
  await expect(page.locator('body')).toContainText(/renews \d+ \w+ \d{4}/);

  // A failed renewal keeps the plan while Stripe retries.
  await portal(page, 'Payment fails (past due)');
  await expect(page.locator('body')).toContainText('Current plan: Business');
  await expect(page.locator('body')).toContainText('past_due');

  await portal(page, 'Cancel now');
  await expect(page.locator('body')).toContainText('Current plan: Free');
  await expect(page.locator('body')).toContainText(/Apps\s*2 of 1/); // existing apps keep running
  await expect.poll(async () => (await (await page.request.get(shop.live)).text()).includes('Made with Renoxium'), { timeout: 40_000, intervals: [2000] }).toBe(true);
  await expect(agent.call('create_app', { app: unique('third') })).rejects.toThrow();
  const customers = (await fake.stripeCalls()).filter((c) => c.path === '/v1/customers' && c.form.email === shop.email);
  expect(customers).toHaveLength(1);
});

test('closing the Checkout page without paying keeps the Free plan', async ({ browser }) => {
  const { page } = await person(browser);
  const { signUp } = await import('../lib.mjs');
  await signUp(page, email('nopay'));
  await page.goto('/billing');
  await page.getByRole('button', { name: 'Choose Pro' }).click();
  await page.getByRole('button', { name: 'Back' }).click();
  await page.waitForURL(/\/billing$/);
  await expect(page.locator('body')).toContainText('Current plan: Free');
});

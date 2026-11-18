// The operator panel: only for confirmed operator accounts; figures, search,
// giving a plan without payment, suspending and bringing back an app, abuse
// reports.
import { test, expect } from '@playwright/test';
import { OPERATOR } from '../env.mjs';
import { person, ownerWithStore, signUp, signIn, email, PASSWORD } from '../lib.mjs';

test('operator panel', async ({ browser }) => {
  const shop = await ownerWithStore(browser);

  // Not for owners, and not for an unconfirmed operator address.
  expect((await shop.page.request.get('/operator')).status()).toBe(404);
  const op = await person(browser);
  await signUp(op.page, OPERATOR, { confirm: false }).catch(async () => signIn(op.page, OPERATOR));
  const confirmed = !(await op.page.locator('body').innerText()).includes('Confirm your email');
  if (!confirmed) {
    expect((await op.page.request.get('/operator')).status()).toBe(404);
    const { waitEmail, linkIn } = await import('../lib.mjs');
    const { RENOXIUM } = await import('../env.mjs');
    await op.page.goto(linkIn(await waitEmail(OPERATOR, /confirm/i), RENOXIUM + '/verify'));
  }
  const page = op.page;
  await page.goto('/operator');
  await expect(page.getByRole('heading', { name: 'Renoxium operator' })).toBeVisible();
  for (const tile of ['Accounts', 'Subscription revenue', 'Apps', 'WhatsApp codes', 'Server']) {
    await expect(page.locator('.tile').getByText(tile, { exact: true })).toBeVisible();
  }

  // Search by the owner's email lists the account and its app.
  await page.fill('input[name=q]', shop.email);
  await page.getByRole('button', { name: 'Search' }).click();
  await expect(page.locator('table').first()).toContainText(shop.email);
  const appRow = page.locator('tr', { hasText: shop.app });
  await expect(appRow).toContainText('v1');

  // Give Pro without payment: the owner sees it, and it survives a billing sync.
  const userRow = page.locator('tr', { hasText: shop.email }).first();
  await userRow.locator('select[name=plan]').selectOption('pro');
  await userRow.getByRole('button', { name: 'Set' }).click();
  await expect(page.locator('tr', { hasText: shop.email }).first()).toContainText('pro granted');
  await shop.page.goto('/billing?done=1');
  await expect(shop.page.locator('body')).toContainText('Current plan: Pro');
  await expect.poll(async () => (await (await shop.page.request.get(shop.live)).text()).includes('Made with Renoxium'), { timeout: 40_000, intervals: [2000] }).toBe(false);

  // Suspend: the shop answers 451 with a notice; bring it back.
  await page.fill('input[name=q]', shop.email);
  await page.getByRole('button', { name: 'Search' }).click();
  await page.locator('tr', { hasText: shop.app }).locator('input[name=reason]').fill('e2e check');
  await page.locator('tr', { hasText: shop.app }).getByRole('button', { name: 'Suspend' }).click();
  await expect(page.locator('tr', { hasText: shop.app })).toContainText('suspended: e2e check');
  const off = await shop.page.request.get(shop.live);
  expect(off.status()).toBe(451);
  expect(await off.text()).toContain('suspended by Renoxium');
  await page.locator('tr', { hasText: shop.app }).getByRole('button', { name: 'Bring back' }).click();
  await expect.poll(async () => (await shop.page.request.get(shop.live)).status()).toBe(200);

  // Abuse reports arrive in the panel and can be marked handled.
  const reporter = await person(browser);
  await reporter.page.goto('/abuse');
  await reporter.page.fill('#address', shop.live);
  await reporter.page.fill('#details', 'Operator test report');
  await reporter.page.getByRole('button', { name: 'Send report' }).click();
  await page.goto('/operator');
  const report = page.locator('.report', { hasText: 'Operator test report' });
  await expect(report).toBeVisible();
  await report.getByRole('button', { name: 'Mark handled' }).click();
  await expect(page.locator('.report.done', { hasText: 'Operator test report' })).toBeVisible();

  // Back to Free removes the grant.
  await page.fill('input[name=q]', shop.email);
  await page.getByRole('button', { name: 'Search' }).click();
  const row = page.locator('tr', { hasText: shop.email }).first();
  await row.locator('select[name=plan]').selectOption('free');
  await row.getByRole('button', { name: 'Set' }).click();
  await shop.page.goto('/billing');
  await expect(shop.page.locator('body')).toContainText('Current plan: Free');
  expect(PASSWORD).toBeTruthy();
});

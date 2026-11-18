// Deleting an owner account: confirmation, a paid plan cancelled at Stripe
// first, the apps gone, the name free again.
import { test, expect } from '@playwright/test';
import { person, ownerWithStore, signUp, email, connectAgent, builder, fake, upgrade, PASSWORD } from '../lib.mjs';

test('delete an account on a paid plan', async ({ browser }) => {
  const shop = await ownerWithStore(browser);
  const { page } = shop;
  await upgrade(page, 'Pro');
  const sub = (await fake.get('/stripe/subscriptions')).filter((s) => s.status === 'active').at(-1);

  await page.goto('/account');
  await expect(page.locator('body')).toContainText('This deletes your account and 1 app(s)');
  await expect(page.locator('body')).toContainText('Your paid plan is cancelled at once');

  await page.fill('#confirm', 'someone@else.test');
  await page.fill('#dpassword', PASSWORD);
  await page.getByRole('button', { name: 'Delete my account' }).click();
  await expect(page.locator('.error')).toHaveText('Type your email address exactly to confirm.');
  await page.fill('#confirm', shop.email);
  await page.fill('#dpassword', 'not my password');
  await page.getByRole('button', { name: 'Delete my account' }).click();
  await expect(page.locator('.error')).toHaveText('Your password is not right.');
  expect((await fake.get('/stripe/subscriptions')).find((s) => s.id === sub.id).status).toBe('active');

  await page.fill('#confirm', shop.email.toUpperCase());
  await page.fill('#dpassword', PASSWORD);
  await page.getByRole('button', { name: 'Delete my account' }).click();
  await expect(page.locator('body')).toContainText('Your account and 1 app(s) were deleted');

  expect((await fake.get('/stripe/subscriptions')).find((s) => s.id === sub.id).status).toBe('canceled');
  expect((await page.request.get(shop.live)).status()).toBe(404);
  await page.goto('/');
  await expect(page.getByRole('navigation').getByRole('link', { name: 'Sign in' })).toBeVisible();
  await expect(shop.agent.call('list_apps')).rejects.toThrow(); // the agent's token died with the account

  // The app name is held for 90 days: its address is in customers' bookmarks
  // and old emails, so another account cannot start serving pages there.
  const next = await person(browser);
  await signUp(next.page, email('next'));
  const agent = await builder((await connectAgent(next.page)).access_token);
  await expect(agent.call('create_app', { app: shop.app })).rejects.toThrow(/already taken/);
  await agent.close();
});

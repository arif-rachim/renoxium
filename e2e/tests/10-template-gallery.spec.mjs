// The template gallery: anyone browses the templates, tries a demo like a
// customer (signing in with a WhatsApp code from the demo inbox) and looks
// at the owner's admin panel, read-only. Nothing is sent for real.
import { test, expect } from '@playwright/test';
import { appURL, idURL } from '../env.mjs';
import { person, fake } from '../lib.mjs';

test('template gallery and demos', async ({ browser }) => {
  const { page } = await person(browser);

  // Demos come up in the background at startup.
  await expect.poll(async () => (await page.goto(appURL('demo-store-uae') + '/'))?.status(), { timeout: 60_000, intervals: [1000] }).toBe(200);

  await page.goto('/');
  await page.getByRole('link', { name: 'Templates' }).first().click();
  await expect(page).toHaveURL(/\/templates$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Templates', exact: true })).toBeVisible();
  for (const title of ['Online store', 'Appointments and classes', 'Home services', 'Tours and rentals', 'Quotes and client portal', 'Property listings']) {
    await expect(page.locator('.tcard', { hasText: title })).toBeVisible();
  }

  // Try the store demo: it opens without any sign-in, with the demo bar.
  await page.locator('.tcard', { hasText: 'Online store' }).getByRole('link', { name: 'Try the demo' }).click();
  await expect(page).toHaveURL(appURL('demo-store-uae') + '/');
  await expect(page.locator('#rx-demo')).toContainText('Template demo');

  // Sign in with WhatsApp: the code is shown in the demo inbox, never sent.
  await page.goto(appURL('demo-store-uae') + '/_rx/auth/phone');
  await expect(page).toHaveURL(new RegExp('^' + idURL('demo-store-uae')));
  await page.fill('#phone', '+971509990001');
  await page.getByRole('button', { name: 'Send code' }).click();
  const box = page.locator('section', { hasText: 'Demo inbox' });
  await expect(box).toContainText('Sign-in code');
  const code = (await box.innerText()).match(/sign-in code is (\d{6})/)[1];
  await page.fill('#code', code);
  await page.fill('#name', 'Demo Shopper');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(new RegExp('^' + appURL('demo-store-uae')));
  expect(await fake.whatsapp('+971509990001')).toHaveLength(0);

  // The same messages are in the inbox on the demo's own address.
  await page.locator('#rx-demo').getByRole('link', { name: 'Demo inbox' }).click();
  await expect(page.locator('body')).toContainText(code);

  // Someone else's browser has an empty inbox.
  const other = await person(browser);
  await other.page.goto(appURL('demo-store-uae') + '/_rx/demo/inbox');
  await expect(other.page.locator('body')).toContainText('Nothing yet');

  // The owner's admin panel of the booking demo: data and calendar, no way to change them.
  await page.goto('/templates');
  await page.locator('.tcard', { hasText: 'Appointments and classes' }).getByRole('link', { name: 'Admin panel' }).click();
  await expect(page.locator('body')).toContainText('read-only');
  await page.locator('.tables').getByRole('link', { name: /Services/ }).click();
  await expect(page.getByRole('link', { name: 'Add', exact: true })).toHaveCount(0);
  await page.locator('tbody td.id a').first().click();
  await expect(page.locator('fieldset[disabled]')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Save' })).toHaveCount(0);
  await page.goto('/apps/demo-booking-uae/t/bookings/calendar');
  await expect(page.getByRole('link', { name: 'Week' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Add', exact: true })).toHaveCount(0);
});

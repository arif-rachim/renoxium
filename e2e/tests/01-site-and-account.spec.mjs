// The Renoxium site: public pages, owner sign-up, email confirmation,
// sign-in and sign-out, password reset and change, data export, abuse reports.
import fs from 'node:fs';
import { test, expect } from '@playwright/test';
import { RENOXIUM } from '../env.mjs';
import { person, signUp, signIn, signOut, email, waitEmail, linkIn, fake, PASSWORD } from '../lib.mjs';

test('public pages answer', async ({ browser }) => {
  const { page } = await person(browser);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Your online store, built in a chat.' })).toBeVisible();
  await expect(page.locator('#pricing')).toContainText('AED 99');
  await page.getByRole('navigation').getByRole('link', { name: 'Connect' }).click();
  await expect(page.locator('.connect h1').first()).toBeVisible();
  await expect(page.locator('.connect')).toContainText(RENOXIUM + '/mcp');

  for (const [path, text] of [['/privacy', 'Renoxium E2E (FZC)'], ['/terms', 'Renoxium E2E (FZC)']]) {
    await page.goto(path);
    await expect(page.locator('body')).toContainText(text);
  }
  const health = await page.request.get('/healthz');
  expect(health.ok()).toBeTruthy();
  expect((await page.request.get('/version')).ok()).toBeTruthy();
  const llms = await page.request.get('/llms.txt');
  expect(await llms.text()).toContain('/mcp');
  const meta = await (await page.request.get('/.well-known/oauth-authorization-server')).json();
  expect(meta.code_challenge_methods_supported).toContain('S256');
  expect(meta.registration_endpoint).toBe(RENOXIUM + '/oauth/register');
  const prm = await (await page.request.get('/.well-known/oauth-protected-resource/mcp')).json();
  expect(prm.resource).toBe(RENOXIUM + '/mcp');
});

test('sign-up checks its input, confirms the email and can resend the link', async ({ browser }) => {
  const { page } = await person(browser);
  const address = email('signup');

  // A short password is refused by the server too, not only by the browser.
  await page.goto('/signup');
  await page.fill('#email', address);
  await page.fill('#password', 'short');
  await page.evaluate(() => document.querySelector('#password').removeAttribute('minlength'));
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.locator('.error')).toBeVisible();

  await signUp(page, address, { confirm: false });
  await expect(page.locator('body')).toContainText('Confirm your email');
  const first = await waitEmail(address, /confirm/i);
  const t = Date.now();
  await page.getByRole('button', { name: 'Send the link again' }).click();
  const second = await waitEmail(address, /confirm/i, { after: t });
  expect(second.at).toBeGreaterThan(first.at);
  await page.goto(linkIn(second, RENOXIUM + '/verify'));
  await expect(page.locator('body')).toContainText(/confirmed/i);
  await page.goto('/');
  await expect(page.locator('body')).not.toContainText('Confirm your email');
  expect(second.from).toContain('Renoxium');

  // The same address cannot sign up twice.
  const other = await person(browser);
  await other.page.goto('/signup');
  await other.page.fill('#email', address);
  await other.page.fill('#password', PASSWORD);
  await other.page.getByRole('button', { name: 'Create account' }).click();
  await expect(other.page.locator('.error')).toBeVisible();
});

test('sign-in: wrong password, return to the page asked for, sign-out', async ({ browser }) => {
  const { page } = await person(browser);
  const address = email('login');
  await signUp(page, address);
  await signOut(page);
  await expect(page.getByRole('navigation').getByRole('link', { name: 'Sign in' })).toBeVisible();

  await signIn(page, address, { password: 'not the password' });
  await expect(page.locator('.error')).toHaveText('Wrong email or password.');

  // Pages that need an account send the visitor to sign in and back.
  for (const target of ['/account', '/billing']) {
    const c = await person(browser);
    await c.page.goto(target);
    await expect(c.page).toHaveURL(/\/login\?next=/);
    await c.page.fill('#email', address);
    await c.page.fill('#password', PASSWORD);
    await c.page.getByRole('button', { name: 'Sign in' }).click();
    await expect(c.page).toHaveURL(RENOXIUM + target);
    await c.ctx.close();
  }
  // An outside address in next= is ignored.
  await signIn(page, address, { next: '//evil.example/' });
  await expect(page).toHaveURL(RENOXIUM + '/');
});

test('forgotten password: reset link, old password stops working', async ({ browser }) => {
  const { page } = await person(browser);
  const address = email('forgot');
  await signUp(page, address);
  await signOut(page);

  const t = Date.now();
  await page.goto('/login');
  await page.getByRole('link', { name: 'Forgot your password?' }).click();
  await page.fill('#email', address);
  await page.getByRole('button', { name: 'Send the link' }).click();
  // The answer is the same for unknown addresses, so it tells nobody who has an account.
  const known = (await page.locator('body').innerText()).replace(address, 'ADDRESS');
  const nobody = email('nobody');
  await page.goto('/forgot');
  await page.fill('#email', nobody);
  await page.getByRole('button', { name: 'Send the link' }).click();
  expect((await page.locator('body').innerText()).replace(nobody, 'ADDRESS')).toBe(known);

  const m = await waitEmail(address, /password/i, { after: t });
  await page.goto(linkIn(m, RENOXIUM + '/reset'));
  await expect(page.locator('body')).toContainText(address);
  await page.fill('#password', 'a fresh new password');
  await page.getByRole('button', { name: 'Save and sign in' }).click();
  await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible();

  // The link works once.
  await page.goto(linkIn(m, RENOXIUM + '/reset'));
  await expect(page.locator('body')).toContainText(/expired|no longer|invalid|used/i);

  await signOut(page);
  await signIn(page, address);
  await expect(page.locator('.error')).toBeVisible();
  await signIn(page, address, { password: 'a fresh new password' });
  await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible();
});

test('account page: change password signs out other sessions; export downloads a zip', async ({ browser }) => {
  const a = await person(browser);
  const b = await person(browser);
  const address = email('acct');
  await signUp(a.page, address);
  await signIn(b.page, address);
  await expect(b.page.getByRole('heading', { name: 'Your apps' })).toBeVisible();

  await a.page.goto('/account');
  await a.page.fill('#current', 'wrong password!');
  await a.page.fill('#password', 'another good password');
  await a.page.getByRole('button', { name: /Change password/ }).click();
  await expect(a.page.locator('.error')).toContainText('not right');
  await a.page.fill('#current', PASSWORD);
  await a.page.fill('#password', 'another good password');
  await a.page.getByRole('button', { name: /Change password/ }).click();
  await expect(a.page.locator('body')).toContainText('Password changed');

  await b.page.goto('/');
  await expect(b.page.getByRole('navigation').getByRole('link', { name: 'Sign in' })).toBeVisible();

  const [download] = await Promise.all([a.page.waitForEvent('download'), a.page.getByRole('link', { name: 'Download export' }).click()]);
  expect(download.suggestedFilename()).toMatch(/^renoxium-export-\d{4}-\d{2}-\d{2}\.zip$/);
  const head = fs.readFileSync(await download.path()).subarray(0, 2).toString();
  expect(head).toBe('PK');
});

test('abuse report reaches the abuse address', async ({ browser }) => {
  const { page } = await person(browser);
  const t = Date.now();
  await page.goto('/abuse');
  await page.fill('#address', 'https://phishy.localhost');
  await page.fill('#details', 'This app asks for bank passwords.');
  await page.fill('#contact', 'reporter@e2e.test');
  await page.getByRole('button', { name: 'Send report' }).click();
  await expect(page.locator('body')).toContainText(/thank|received|sent/i);
  const m = await waitEmail('abuse@e2e.test', /report|abuse/i, { after: t });
  expect(m.text).toContain('bank passwords');
});

test('forms without the CSRF token are refused', async ({ browser }) => {
  const { page } = await person(browser);
  await signUp(page, email('csrf'));
  const r = await page.request.post('/logout', { form: {} });
  expect(r.status()).toBe(403);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Your apps' })).toBeVisible();
  expect((await fake.emails('nobody@e2e.test')).length).toBe(0);
});

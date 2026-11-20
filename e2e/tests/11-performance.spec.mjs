// Performance budget of the store template's gallery demo on a phone: what
// the premium motion and app feel add must not make the shop slower.
// Third-party requests (web fonts) are blocked so the numbers measure only
// what Renoxium and the template serve. PERF_REPORT=1 only prints them.
import { test, expect, devices } from '@playwright/test';
import { appURL } from '../env.mjs';

const BUDGET = { lcpMs: 2500, cls: 0.1, inlineJsBytes: 30 * 1024, nonImageBytes: 150 * 1024 };
const report = !!process.env.PERF_REPORT;

async function measure(browser, url) {
  const ctx = await browser.newContext({ ...devices['Pixel 7'] });
  const page = await ctx.newPage();
  await page.route(/^https?:\/\/(?![^/]*localhost)/, (r) => r.abort());
  const cdp = await ctx.newCDPSession(page);
  // Fast 4G and a mid-range phone's CPU.
  await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 60, downloadThroughput: (9 * 1024 * 1024) / 8, uploadThroughput: (1.5 * 1024 * 1024) / 8 });
  await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
  await page.addInitScript(() => {
    window.__perf = { lcp: 0, cls: 0 };
    new PerformanceObserver((l) => { for (const e of l.getEntries()) window.__perf.lcp = e.startTime; }).observe({ type: 'largest-contentful-paint', buffered: true });
    new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) window.__perf.cls += e.value; }).observe({ type: 'layout-shift', buffered: true });
  });
  const res = await page.goto(url, { waitUntil: 'networkidle' });
  expect(res.status()).toBe(200);
  await page.waitForTimeout(500);
  const m = await page.evaluate(() => {
    const inline = [...document.querySelectorAll('script:not([src])')].reduce((n, s) => n + new Blob([s.textContent]).size, 0);
    const nav = performance.getEntriesByType('navigation')[0];
    const res = performance.getEntriesByType('resource').filter((r) => r.initiatorType !== 'img' && !/\.(jpe?g|png|webp|avif|gif)(\?|$)/.test(r.name));
    const bytes = (nav ? nav.transferSize || nav.encodedBodySize : 0) + res.reduce((n, r) => n + (r.transferSize || r.encodedBodySize || 0), 0);
    return { lcpMs: Math.round(window.__perf.lcp), cls: Number(window.__perf.cls.toFixed(3)), inlineJsBytes: inline, nonImageBytes: bytes };
  });
  await ctx.close();
  return m;
}

// Demos come up in the background at startup: wait for the demo in a browser page.
async function demoReady(browser, base) {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await expect.poll(async () => (await page.goto(base + '/').catch(() => null))?.status() || 0, { timeout: 60_000, intervals: [1000] }).toBe(200);
  await ctx.close();
}

test('templates with the motion kit stay within the performance budget on a phone', async ({ browser }) => {
  test.setTimeout(240_000);
  for (const [demo, paths] of [['demo-store-uae', ['/', '/p/1', '/?lang=ar']], ['demo-booking-uae', ['/', '/s/1', '/?lang=ar']]]) {
  const base = appURL(demo);
  await demoReady(browser, base);
  for (const path of paths) {
    const m = await measure(browser, base + path);
    console.log(`perf ${demo}${path}: ${JSON.stringify(m)} budget ${JSON.stringify(BUDGET)}`);
    if (report) continue;
    for (const k of Object.keys(BUDGET)) {
      expect.soft(m[k], `${demo}${path} ${k}`).toBeLessThanOrEqual(BUDGET[k]);
    }
  }
  }
});

// The motion kit on a phone: the cart drawer slides in from the end side
// (left in Arabic) with the same total as the cart page, search filters in
// place, and with reduced motion nothing waits for an animation.
test('store motion kit: drawer, search in place, Arabic and reduced motion', async ({ browser }) => {
  const base = appURL('demo-store-uae');
  await demoReady(browser, base);
  const ctx = await browser.newContext({ ...devices['Pixel 7'] });
  await ctx.route(/^https?:\/\/(?![^/]*localhost)/, (r) => r.abort());
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));

  await page.goto(base + '/', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.__same = 1; });
  await page.fill('#q', 'musk');
  await expect(page).toHaveURL(/\?q=musk$/);
  await expect(page.locator('#results .tile')).toHaveCount(1);
  expect(await page.evaluate(() => window.__same)).toBe(1); // no reload

  await page.goto(base + '/p/2', { waitUntil: 'networkidle' });
  await page.locator('#add').click();
  const drawer = page.locator('.drawer.open');
  await expect(drawer).toBeVisible();
  await expect(drawer.locator('.d-line')).toHaveCount(1);
  const total = await drawer.locator('.d-total .price').innerText();
  await drawer.getByRole('link', { name: 'Checkout' }).click();
  await expect(page.locator('#total')).toHaveText(total);

  await page.goto(base + '/p/2?lang=ar', { waitUntil: 'networkidle' });
  await page.locator('#add').click();
  await expect(page.locator('.drawer.open')).toBeVisible();
  const box = await page.locator('.drawer').boundingBox();
  expect(box.x).toBeLessThan(5); // the end side in right-to-left is the left
  await page.keyboard.press('Escape');
  await expect(page.locator('.drawer.open')).toHaveCount(0);
  await ctx.close();

  const calm = await browser.newContext({ viewport: { width: 1280, height: 860 }, reducedMotion: 'reduce' });
  await calm.route(/^https?:\/\/(?![^/]*localhost)/, (r) => r.abort());
  const p2 = await calm.newPage();
  await p2.goto(base + '/', { waitUntil: 'networkidle' });
  await p2.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  await expect(p2.locator('.rv')).toHaveCount(0);
  await calm.close();
  expect(errors).toEqual([]);
});

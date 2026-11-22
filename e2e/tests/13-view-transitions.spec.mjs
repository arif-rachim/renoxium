// The list to detail transition of every business demo with the motion kit:
// a click on a card runs a cross-document view transition, the card's frame
// is one of its named parts (so it moves into the detail page's frame), the
// same happens on the way back, and the named element carries the mask on
// both pages. A snapshot of a bare photo inside a rounded frame would be a
// rectangle: the shape would vanish for the length of the transition.
// Reduced motion switches the transition off.
// A card without a photo moves its title into the page's h1 instead; the
// demos all have photos, so that is checked on fresh apps made from each
// example (their seed rows have no photos), on the owner's private preview.
import { test, expect } from '@playwright/test';
import { appURL, RENOXIUM } from '../env.mjs';
import { person, signUp, email, unique, connectAgent, builder } from '../lib.mjs';

// demo, list page, prefix of the shared view-transition-name, the detail page's path
const NAVS = [
  ['demo-store-uae', '/', 'pimg-', /^\/p\/\d+$/],
  ['demo-booking-uae', '/', 'simg-', /^\/s\/\d+$/],
  ['demo-booking-uae', '/', 'cimg-', /^\/c\/\d+$/],
  ['demo-home-service-uae', '/', 'simg-', /^\/s\/\d+$/],
  ['demo-reservation-uae', '/', 'iimg-', /^\/item\/\d+$/],
  ['demo-listings-uae', '/', 'limg-', /^\/l\/\d+$/],
  ['demo-listings-uae', '/search', 'limg-', /^\/l\/\d+$/],
  ['demo-listings-uae', '/', 'aimg-', /^\/agents\/\d+$/],
  ['demo-listings-uae', '/l/1', 'aimg-', /^\/agents\/\d+$/],
  ['demo-quote-portal-uae', '/', 'kimg-', /^\/packages\/\d+$/],
  ['demo-clinic-queue-uae', '/', 'simg-', /^\/s\/\d+$/],
];

// Runs in every document before its scripts: notes each reveal of the page (a load, or a return from the back/forward
// cache), whether a view transition came with it and which names took part.
function watchReveals() {
  window.__reveals = [];
  window.addEventListener('pagereveal', (e) => {
    const r = { transition: !!e.viewTransition, names: null };
    window.__reveals.push(r);
    if (!e.viewTransition) return;
    e.viewTransition.ready.then(() => {
      r.names = [...new Set(document.getAnimations().map((a) => a.effect && a.effect.pseudoElement).filter(Boolean)
        .map((p) => p.replace(/^.*\((.*)\)$/, '$1')))];
    }, (err) => { r.names = []; r.skipped = String(err); });
  });
}

// The last reveal of the page, once its transition (if any) has started.
async function lastReveal(page) {
  const h = await page.waitForFunction(() => {
    const r = window.__reveals && window.__reveals[window.__reveals.length - 1];
    return r && (!r.transition || r.names) ? r : null;
  });
  return h.jsonValue();
}

// The named element and how it is clipped. "outer" is an ancestor that clips to a shape (a border-radius with overflow,
// or a clip-path) and that the element fills: when there is one, the name is on the wrong element.
function named(name) {
  const el = [...document.querySelectorAll('[style*="view-transition-name"]')].find((e) => e.style.viewTransitionName === name);
  if (!el) return null;
  const shape = (e) => {
    const cs = getComputedStyle(e);
    const round = cs.borderRadius.split(/[ /]+/).some((v) => parseFloat(v) > 0);
    return { round, clips: (round && (cs.overflow !== 'visible' || e.tagName === 'IMG')) || cs.clipPath !== 'none', radius: cs.borderRadius };
  };
  const box = el.getBoundingClientRect();
  let outer = '';
  for (let p = el.parentElement; p && p !== document.body; p = p.parentElement) {
    const b = p.getBoundingClientRect();
    const fills = Math.abs(b.left - box.left) < 2 && Math.abs(b.top - box.top) < 2 && Math.abs(b.width - box.width) < 2 && Math.abs(b.height - box.height) < 2;
    if (fills && shape(p).clips && getComputedStyle(p).overflow !== 'visible') { outer = p.tagName + '.' + p.className; break; }
  }
  const all = [...document.querySelectorAll('[style*="view-transition-name"]')].map((e) => e.style.viewTransitionName);
  return { tag: el.tagName, ...shape(el), outer, twice: all.filter((n, i) => all.indexOf(n) !== i) };
}

async function demoReady(browser, base) {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await expect.poll(async () => (await page.goto(base + '/').catch(() => null))?.status() || 0, { timeout: 60_000, intervals: [1000] }).toBe(200);
  await ctx.close();
}

test('a card moves into its detail page with its mask, and back, in every demo with the motion kit', async ({ browser }) => {
  test.setTimeout(240_000);
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 } });
  await ctx.route(/^https?:\/\/(?![^/]*localhost)/, (r) => r.abort());
  await ctx.addInitScript(watchReveals);
  const page = await ctx.newPage();
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  for (const [demo, list, prefix, detail] of NAVS) {
    const base = appURL(demo), where = `${demo}${list} ${prefix}`;
    await demoReady(browser, base);
    await page.goto(base + list, { waitUntil: 'load' });
    const name = await page.evaluate((prefix) => {
      const el = document.querySelector(`[style*="view-transition-name:${prefix}"]`);
      const a = el && el.closest('a[href]');
      if (!a) return '';
      a.setAttribute('data-vt-link', '');
      return el.style.viewTransitionName;
    }, prefix);
    expect(name, `${where}: a card with a transition name inside a link`).toMatch(new RegExp('^' + prefix + '\\d+$'));
    const card = await page.evaluate(named, name);
    expect.soft(card.twice, `${where}: names used twice on the list page`).toEqual([]);
    expect.soft(card.clips, `${where}: ${name} on the list page carries a mask (border-radius ${card.radius})`).toBe(true);
    expect.soft(card.outer, `${where}: ${name} on the list page is inside a frame that clips it; the name belongs on the frame`).toBe('');

    await page.locator('[data-vt-link]').click();
    await page.waitForURL((u) => detail.test(u.pathname));
    const forward = await lastReveal(page);
    expect.soft(forward.transition, `${where}: the detail page was revealed with a view transition`).toBe(true);
    expect.soft(forward.names || [], `${where}: parts of the transition into the detail page`).toEqual(expect.arrayContaining([name, 'root']));
    const frame = await page.evaluate(named, name);
    expect(frame, `${where}: the detail page has an element named ${name}`).not.toBeNull();
    expect.soft(frame.twice, `${where}: names used twice on the detail page`).toEqual([]);
    expect.soft(frame.outer, `${where}: ${name} on the detail page is inside a frame that clips it; the name belongs on the frame`).toBe('');

    await page.goBack({ waitUntil: 'commit' });
    await page.waitForURL(base + list);
    const back = await lastReveal(page);
    expect.soft(back.transition, `${where}: the list page was revealed with a view transition on the way back`).toBe(true);
    expect.soft(back.names || [], `${where}: parts of the transition back to the list`).toEqual(expect.arrayContaining([name, 'root']));
  }
  await ctx.close();
  expect(errors).toEqual([]);
});

test('reduced motion: pages change without a view transition', async ({ browser }) => {
  const calm = await browser.newContext({ viewport: { width: 1280, height: 860 }, reducedMotion: 'reduce' });
  await calm.route(/^https?:\/\/(?![^/]*localhost)/, (r) => r.abort());
  await calm.addInitScript(watchReveals);
  const page = await calm.newPage();
  for (const [demo, list, prefix, detail] of [NAVS[0], NAVS[3]]) {
    const base = appURL(demo);
    await demoReady(browser, base);
    await page.goto(base + list, { waitUntil: 'load' });
    await page.locator(`a:has([style*="view-transition-name:${prefix}"])`).first().click();
    await page.waitForURL((u) => detail.test(u.pathname));
    expect((await lastReveal(page)).transition, `${demo}: no view transition with reduced motion`).toBe(false);
  }
  await calm.close();
});

// example, prefix of the title's view-transition-name, the detail page's path
const TITLES = [
  ['store-uae', 'pttl-', /^\/p\/\d+$/],
  ['booking-uae', 'sttl-', /^\/s\/\d+$/],
  ['home-service-uae', 'sttl-', /^\/s\/\d+$/],
  ['reservation-uae', 'ittl-', /^\/item\/\d+$/],
  ['listings-uae', 'lttl-', /^\/l\/\d+$/],
  ['quote-portal-uae', 'kttl-', /^\/packages\/\d+$/],
  ['clinic-queue-uae', 'sttl-', /^\/s\/\d+$/],
];

// The named title: a box of its own (class vtt) that hugs its text, so the snapshot is the words and nothing else, and
// that the transition sizes as text (view-transition-class vtt) instead of cropping it like a photo.
function namedTitle(name) {
  const el = [...document.querySelectorAll('[style*="view-transition-name"]')].find((e) => e.style.viewTransitionName === name);
  if (!el) return null;
  const range = document.createRange();
  range.selectNodeContents(el);
  const text = range.getBoundingClientRect(), box = el.getBoundingClientRect(), outer = el.parentElement.getBoundingClientRect();
  const lines = new Set([...range.getClientRects()].map((r) => Math.round(r.top))).size;
  const cs = getComputedStyle(el);
  const all = [...document.querySelectorAll('[style*="view-transition-name"]')].map((e) => e.style.viewTransitionName);
  return { vtt: el.classList.contains('vtt'), display: cs.display, cls: cs.viewTransitionClass, lines, slack: Math.round(box.width - text.width),
    inside: box.left >= outer.left - 1 && box.right <= outer.right + 1, photos: all.filter((n) => /img-/.test(n) && !/^aimg-/.test(n)),
    twice: all.filter((n, i) => all.indexOf(n) !== i) };
}

test('a card without a photo moves its title into the h1 of its page, and back', async ({ browser }) => {
  test.setTimeout(300_000);
  for (const [example, prefix, detail] of TITLES) {
    const { ctx, page } = await person(browser, { viewport: { width: 1280, height: 860 } });
    await ctx.route(/^https?:\/\/fonts\.(googleapis|gstatic)\.com\//, (r) => r.abort());
    await signUp(page, email('vt'));
    const { access_token: token } = await connectAgent(page);
    const agent = await builder(token);
    const app = unique('vt');
    await agent.call('create_app', { app, from_example: example });
    await agent.close();
    await ctx.addInitScript(watchReveals);

    await page.goto(`${RENOXIUM}/apps/${app}/preview`, { waitUntil: 'load' });
    const list = page.url();
    const name = await page.evaluate((prefix) => {
      const el = document.querySelector(`[style*="view-transition-name:${prefix}"]`);
      const a = el && el.closest('a[href]');
      if (!a) return '';
      a.setAttribute('data-vt-link', '');
      return el.style.viewTransitionName;
    }, prefix);
    expect(name, `${example}: a card title with a transition name inside a link`).toMatch(new RegExp('^' + prefix + '\\d+$'));
    for (const side of ['list', 'detail']) {
      if (side === 'detail') {
        await page.locator('[data-vt-link]').click();
        await page.waitForURL((u) => detail.test(u.pathname));
        const forward = await lastReveal(page);
        expect.soft(forward.transition, `${example}: the detail page was revealed with a view transition`).toBe(true);
        expect.soft(forward.names || [], `${example}: parts of the transition into the detail page`).toEqual(expect.arrayContaining([name, 'root']));
      }
      const t = await page.evaluate(namedTitle, name);
      expect(t, `${example}: the ${side} page has an element named ${name}`).not.toBeNull();
      expect.soft(t.vtt && t.display === 'inline-block' && t.cls === 'vtt', `${example} ${side}: ${name} is a box of class vtt (${JSON.stringify(t)})`).toBe(true);
      expect.soft(t.inside, `${example} ${side}: the title's box stays inside its heading`).toBe(true);
      if (t.lines === 1) expect.soft(t.slack, `${example} ${side}: the title's box hugs its text`).toBeLessThanOrEqual(2);
      expect.soft(t.photos, `${example} ${side}: no photo frame is named when the rows have no photo`).toEqual([]);
      expect.soft(t.twice, `${example} ${side}: names used twice`).toEqual([]);
    }
    await page.goBack({ waitUntil: 'commit' });
    await page.waitForURL(list);
    const back = await lastReveal(page);
    expect.soft(back.transition, `${example}: the list page was revealed with a view transition on the way back`).toBe(true);
    expect.soft(back.names || [], `${example}: parts of the transition back to the list`).toEqual(expect.arrayContaining([name, 'root']));
    await ctx.close();
  }
});

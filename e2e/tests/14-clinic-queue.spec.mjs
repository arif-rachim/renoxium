// The clinic-queue-uae template end to end. An agent builds a clinic from
// the template; on preview the nightly job closes yesterday's numbers so the
// queue starts empty; the owner publishes it as a work app. On the live app a
// patient takes a number on their phone and follows their place without
// reloading, a member of staff calls patients from the staff page, the
// waiting-room screen follows every call, and the patient gets one WhatsApp
// message (through the fake WhatsApp Cloud API) when two patients are ahead.
// Every page fits a 390 px phone.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { test, expect } from '@playwright/test';
import { appURL } from '../env.mjs';
import { person, signUp, email, unique, connectAgent, builder, confirmPublish, waitWhatsApp, fake } from '../lib.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const example = () => JSON.parse(fs.readFileSync(path.join(here, '..', '..', 'examples', 'clinic-queue-uae.json'), 'utf8'));
const PHONE = { width: 390, height: 844 };

test.describe.configure({ mode: 'serial' });
let app, live, staffEmail;

// noOverflow fails when anything on the page is wider than the screen.
async function noOverflow(page, where) {
  const wide = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(wide, `${where} is wider than the screen`).toBeLessThanOrEqual(0);
}

test('an agent builds the clinic; the nightly job starts the queue empty; the owner publishes a work app', async ({ browser }) => {
  test.setTimeout(120_000);
  const owner = await person(browser);
  await signUp(owner.page, email('clinic'));
  const { access_token: token } = await connectAgent(owner.page);
  const agent = await builder(token);
  app = unique('clinic');
  staffEmail = email('nurse');
  const def = example();
  // Open around the clock so the test does not depend on the hour it runs at, and one nurse on the staff list.
  Object.assign(def.seed.clinic[0], { name: 'E2E Family Clinic', opens: '00:00', closes: '24:00', last_ticket_minutes: 0 });
  def.seed.staff = [{ email: staffEmail, name: 'Nurse Aisha', active: 1 }];
  await agent.call('create_app', { app });
  const applied = await agent.call('apply_change', { app, definition: def });
  expect(applied.ok).toBe(true);
  for (const c of applied.checks) expect.soft(c.status ?? 200, `${c.route} ${c.path || ''}`).toBeLessThan(500);

  // A number taken "yesterday" on preview: the job closes it and the home page shows an empty queue.
  const taken = await agent.call('submit_form', { app, path: '/take', form: { service: '1', name: 'Late Patient', phone: '0509990001', lang: 'en' } });
  expect(taken.status).toBe(303);
  expect(taken.location).toMatch(/^\/ticket\?k=/);
  const yesterday = new Date(Date.now() + 4 * 3600e3 - 86400e3).toISOString().slice(0, 10);
  const moved = await agent.call('change_preview_row', { app, action: 'update', table: 'tickets', id: 1, values: { day: yesterday } });
  expect(moved.status).toBeLessThan(400);
  const { runs } = await agent.call('try_scheduled_jobs', { app });
  expect(runs.map((r) => r.job)).toEqual(['new_day']);
  expect(runs[0].body).toContain('1 left waiting closed');
  expect((await agent.call('view_page', { app, path: '/' })).body).toContain('Nobody waiting');
  expect((await agent.call('view_page', { app, path: taken.location })).body).toContain('The day ended before your turn');

  const pub = await agent.call('publish', { app });
  await confirmPublish(owner.page, pub.confirm_url, { plan: 'work' });
  await agent.close();
  live = appURL(app);
});

test('take a number, follow it live, told on WhatsApp at two ahead, called to a room on the TV screen', async ({ browser }) => {
  test.setTimeout(180_000);
  const patientPhone = '+97150' + String(Date.now()).slice(-7);
  // The waiting-room screen on a TV.
  const tv = await person(browser, { viewport: { width: 1280, height: 720 } });
  await tv.page.goto(live + '/screen');
  await expect(tv.page.locator('#tv-room')).toHaveText('Please wait for your number');

  // Three patients ahead, from the reception tablet.
  const desk = await person(browser, { viewport: { width: 820, height: 1180 } });
  for (const [name, phone] of [['Omar Saleh', '0509991001'], ['Priya Nair', '0509991002'], ['James Carter', '0509991003']]) {
    await desk.page.goto(live + '/kiosk');
    await desk.page.locator('.q-kbtn', { hasText: 'General practice' }).click();
    await desk.page.fill('#k-name', name);
    await desk.page.fill('#k-phone', phone);
    await desk.page.getByRole('button', { name: 'Get my number' }).click();
    await expect(desk.page.locator('.q-kdone .q-code')).toHaveText(/^A00[1-3]$/);
  }
  await expect(tv.page.locator('#tv-next li').first()).toContainText('A003');

  // The patient, on their phone: the home page, the service, the form, the ticket.
  const patient = await person(browser, { viewport: PHONE, isMobile: true, hasTouch: true });
  const p = patient.page;
  await p.goto(live + '/');
  await expect(p.locator('body')).toContainText('E2E Family Clinic');
  await noOverflow(p, 'home');
  await p.locator('a.q-svc', { hasText: 'General practice' }).click();
  await expect(p).toHaveURL(/\/s\/1$/);
  await expect(p.locator('.q-queue-now')).toContainText('3 waiting');
  await noOverflow(p, 'take a number');
  await p.fill('#name', 'Fatma Al Zaabi');
  await p.fill('#phone', patientPhone.replace('+971', '0'));
  await p.getByRole('button', { name: 'Get my number' }).click();
  await expect(p).toHaveURL(/\/ticket\?k=/);
  await expect(p.locator('.q-code')).toHaveText('A004');
  await expect(p.locator('.js-ahead')).toHaveText('3 patients ahead of you');
  await expect(p.locator('.js-wait')).toHaveText('about 18 min');
  await noOverflow(p, 'ticket');
  const ticketURL = p.url();

  // Staff sign in on the clinic's site and open the staff page.
  const staff = await person(browser, { viewport: PHONE, isMobile: true, hasTouch: true });
  const s = staff.page;
  await s.goto(`${live}/_rx/auth/signup?next=${encodeURIComponent('/staff')}`);
  await s.fill('#name', 'Nurse Aisha');
  await s.fill('#email', staffEmail);
  await s.fill('#password', 'nurse password 1');
  await s.getByRole('button', { name: 'Create account' }).click();
  await s.waitForURL(/\/staff$/);
  await expect(s.getByRole('heading', { name: 'Staff page' })).toBeVisible();
  await noOverflow(s, 'staff page');
  const room = (n) => s.locator('#room-' + n);
  const startWA = Date.now();

  // Room 1 calls A001: the patient's page moves without a reload, the screen calls A001 to Room 1,
  // and with two ahead the patient gets one WhatsApp message.
  await room(1).getByRole('button', { name: 'Call next patient' }).click();
  await expect(room(1).locator('.q-cur b')).toHaveText('A001');
  await expect(tv.page.locator('#tv-code')).toHaveText('A001');
  await expect(tv.page.locator('#tv-room')).toHaveText('Room 1');
  await expect(p.locator('.js-ahead')).toHaveText('2 patients ahead of you');
  await expect(p.locator('.js-wait')).toHaveText('about 12 min');
  const wa = await waitWhatsApp(patientPhone, { after: startWA, template: 'owner_notice' });
  expect(wa.params[0]).toBe(app);
  expect(wa.params[1]).toContain('Ticket A004: 2 patients are ahead of you, about 12 min. Please come back to the waiting room.');
  expect(wa.params[1]).toContain(live + '/ticket?k=');

  // Room 2 calls A002, Room 1 finishes A001 and calls A003: the patient is next, and is not messaged again.
  await room(2).getByRole('button', { name: 'Call next patient' }).click();
  await expect(p.locator('.js-ahead')).toHaveText('1 patient ahead of you');
  await room(1).getByRole('button', { name: 'Done, call next' }).click();
  await expect(room(1).locator('.q-cur b')).toHaveText('A003');
  await expect(p.locator('.js-ahead')).toHaveText('You are next');
  await expect(p.locator('.js-wait')).toHaveText('No wait');

  // Room 2 finishes A002 and calls the patient: their phone says where to go, the screen calls them.
  await room(2).getByRole('button', { name: 'Done, call next' }).click();
  await expect(p.locator('#ticket')).toHaveAttribute('data-status', 'called');
  await expect(p.locator('.q-go')).toContainText('Please go to');
  await expect(p.locator('.js-room')).toHaveText('Room 2');
  await expect(tv.page.locator('#tv-code')).toHaveText('A004');
  await expect(tv.page.locator('#tv-room')).toHaveText('Room 2');
  expect((await fake.whatsapp(patientPhone)).filter((m) => m.at >= startWA)).toHaveLength(1);

  // Room 1's patient did not come: a no-show, shown on their ticket list; then the patient is seen.
  await room(1).getByRole('button', { name: 'No-show' }).click();
  await expect(room(1)).toContainText('Nobody in this room.');
  await expect(s.locator('.q-list', { hasText: 'Just finished' })).toContainText('A003');
  await room(2).getByRole('button', { name: 'Done', exact: true }).click();
  await expect(p.locator('.js-final')).toHaveText('Seen. Thank you for your visit.');

  // The report counts today's patients.
  await s.goto(live + '/staff/report');
  await expect(s.locator('.q-kpis')).toContainText('4');
  await noOverflow(s, 'report');

  // The TV screen, the reception tablet and Arabic pages on a phone, right to left.
  for (const pathname of ['/screen', '/kiosk', '/?lang=ar', '/s/1?lang=ar', '/screen?lang=ar']) {
    await p.goto(live + pathname);
    await noOverflow(p, pathname);
  }
  await p.goto(ticketURL + '&lang=ar');
  await expect(p.locator('html')).toHaveAttribute('dir', 'rtl');
  await expect(p.locator('.js-status')).toHaveText('تمت الزيارة. شكراً لزيارتك.');
  await noOverflow(p, 'Arabic ticket');
});

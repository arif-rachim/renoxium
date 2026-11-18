// Builds the renoxium binary from this checkout, starts the fake services and
// the server on a fresh data directory, and stops both after the run.
import { execFileSync, spawn } from 'node:child_process';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { startFakes } from './fakes.mjs';
import { PORT, FAKES_PORT, RENOXIUM, FAKES, OPERATOR, SECRETS } from './env.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const run = path.join(here, '.run');

export default async function globalSetup() {
  fs.rmSync(run, { recursive: true, force: true });
  fs.mkdirSync(path.join(run, 'data'), { recursive: true });
  const bin = path.join(run, 'renoxium');
  const go = process.env.GO || (fs.existsSync('/usr/local/go/bin/go') ? '/usr/local/go/bin/go' : 'go');
  execFileSync(go, ['build', '-o', bin, './cmd/renoxium'], { cwd: path.join(here, '..'), stdio: 'inherit' });

  const fakes = await startFakes({ port: FAKES_PORT, renoxiumURL: RENOXIUM, webhookSecret: SECRETS.webhook,
    billingWebhookSecret: SECRETS.billingWebhook, pricePro: SECRETS.pricePro, priceBusiness: SECRETS.priceBusiness });

  // The agent's OAuth callback: a page that only says it was reached.
  const callback = http.createServer((req, res) => { res.writeHead(200, { 'Content-Type': 'text/plain' }); res.end('callback'); });
  await new Promise((r) => callback.listen(18098, '127.0.0.1', r));

  const env = {
    PATH: process.env.PATH, HOME: process.env.HOME,
    RESEND_API_KEY: 're_e2e', RESEND_API_URL: `${FAKES}/resend/emails`,
    STRIPE_SECRET_KEY: 'sk_test_e2e', STRIPE_API_BASE: `${FAKES}/stripe`, STRIPE_WEBHOOK_SECRET: SECRETS.webhook,
    STRIPE_PRICE_PRO: SECRETS.pricePro, STRIPE_PRICE_BUSINESS: SECRETS.priceBusiness, STRIPE_BILLING_WEBHOOK_SECRET: SECRETS.billingWebhook,
    WHATSAPP_TOKEN: 'wa_e2e', WHATSAPP_PHONE_NUMBER_ID: '100200300', WHATSAPP_OTP_TEMPLATE: 'login_code',
    WHATSAPP_NOTICE_TEMPLATE: 'owner_notice', WHATSAPP_NOTICE_LANGUAGE: 'en', WHATSAPP_API_BASE: `${FAKES}/meta`,
    CLOUDFLARE_API_TOKEN: 'cf_e2e', CLOUDFLARE_ZONE_ID: 'zone_e2e', CLOUDFLARE_API_BASE: `${FAKES}/cf`, CUSTOM_DOMAIN_TARGET: 'customers.localhost',
    FETCH_API_BASE: `${FAKES}/extapi`, // apps' http.fetch calls go to the fake API
    JOBS_CHECK_EVERY: '1s',            // scheduled jobs are checked every second instead of every 30
    RENOXIUM_OPERATORS: OPERATOR,
    RENOXIUM_LEGAL_NAME: 'Renoxium E2E (FZC)', RENOXIUM_LEGAL_ADDRESS: 'Test address, Sharjah', RENOXIUM_LEGAL_EMAIL: 'legal@e2e.test',
  };
  const log = fs.openSync(path.join(run, 'renoxium.log'), 'w');
  const server = spawn(bin, ['-addr', `127.0.0.1:${PORT}`, '-data', path.join(run, 'data'), '-base-url', RENOXIUM,
    '-apps-domain', 'localhost', '-trusted-ip-header', 'X-E2E-Client-IP', '-workers', '4', '-abuse-email', 'abuse@e2e.test'],
  { env, stdio: ['ignore', log, log] });
  let exited = null;
  server.on('exit', (code) => { exited = code; });

  for (let i = 0; ; i++) {
    if (exited !== null) throw new Error(`renoxium exited with ${exited}; see e2e/.run/renoxium.log`);
    try {
      if ((await fetch(`${RENOXIUM}/healthz`)).ok) break;
    } catch {}
    if (i > 100) throw new Error('renoxium did not answer /healthz within 20 s');
    await new Promise((r) => setTimeout(r, 200));
  }
  return async () => {
    server.kill('SIGTERM');
    await fakes.close();
    callback.close();
  };
}

// The building agent's newer tools end to end, from an empty account, on
// the test server: create_app from an example, edit_app, a test visitor, a
// cash booking followed to its page, a phone screenshot of it, and a
// cancellation. Live is never touched. The demo itself is ../agent-demo.mjs.
import { test, expect } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';
import { person, signUp, email, unique, connectAgent } from '../lib.mjs';
import { RENOXIUM } from '../env.mjs';
import { runDemo, mcpCall } from '../agent-demo.mjs';
import { Client } from '@modelcontextprotocol/sdk/client/index.js';
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js';

test('agent demo: example, edit, test visitor, booking, screenshot, cancel', async ({ browser }, info) => {
  const { page } = await person(browser);
  await signUp(page, email('demo'));
  const { access_token: token } = await connectAgent(page);
  const client = new Client({ name: 'renoxium-e2e-demo', version: '1.0.0' });
  await client.connect(new StreamableHTTPClientTransport(new URL(RENOXIUM + '/mcp'), { requestInit: { headers: { Authorization: 'Bearer ' + token } } }));
  const lines = [];
  const shotFile = info.outputPath('booking-mobile.png');
  const { app, steps } = await runDemo(mcpCall(client), { app: unique('demo'), log: (l) => lines.push(l), screenshotTo: shotFile });
  console.log(lines.join('\n'));

  expect(steps.edited.applied).toHaveLength(2);
  expect(steps.cancelled.body).toContain('Cancelled');
  if (steps.shot) {
    expect(steps.shot.status).toBe(200);
    expect(steps.shot.width).toBe(390);
    expect(fs.statSync(shotFile).size).toBeGreaterThan(10_000);
    await info.attach('booking-mobile', { path: shotFile, contentType: 'image/png' });
  }
  // Nothing went live.
  const apps = await mcpCall(client)('list_apps', {});
  expect(apps.apps.find((a) => a.app === app).live_version).toBe(0);
  await client.close();
});

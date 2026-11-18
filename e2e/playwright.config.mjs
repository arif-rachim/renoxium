import { defineConfig } from '@playwright/test';
import { RENOXIUM } from './env.mjs';

// One Renoxium server and one set of fake services serve every test, so the
// files run one after the other, in name order, each with fresh accounts.
export default defineConfig({
  testDir: './tests',
  fullyParallel: false,
  workers: 1,
  timeout: 90_000,
  expect: { timeout: 10_000 },
  reporter: [['list'], ['html', { open: 'never' }], ['json', { outputFile: 'playwright-report/results.json' }]],
  globalSetup: './global-setup.mjs',
  use: {
    baseURL: RENOXIUM,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: process.env.E2E_VIDEO ? 'on' : 'retain-on-failure',
    locale: 'en-US',
    timezoneId: 'Asia/Dubai',
    // E2E_CHROMIUM: use a Chromium already on the machine instead of Playwright's own download.
    ...(process.env.E2E_CHROMIUM ? { launchOptions: { executablePath: process.env.E2E_CHROMIUM } } : {}),
  },
});

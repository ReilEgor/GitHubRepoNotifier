import { defineConfig } from '@playwright/test';

// The system is started by `make e2e` (scripts/check.sh), which passes the addresses in the environment.
export default defineConfig({
  testDir: './tests',
  // Tests share one database and one Redis but never one row: every test creates its own email and uses its own caller address.
  fullyParallel: true,
  workers: 4,
  retries: 0,
  timeout: 20_000,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://127.0.0.1:18080',
  },
});

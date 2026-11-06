import { defineConfig } from '@playwright/test';
import config from './playwright.config';

export default defineConfig({
  ...config,
  projects: [{ name: 'task-cards', testMatch: 'task-cards.browser.spec.ts' }],
  webServer: [Array.isArray(config.webServer) ? config.webServer[0] : config.webServer!],
});

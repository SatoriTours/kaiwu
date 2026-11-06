import { defineConfig } from '@playwright/test';
import config from './playwright.config';
export default defineConfig({
  ...config,
  projects: [{ name: 'mobile-toolbar', testMatch: 'mobile-toolbar.browser.spec.ts' }],
  webServer: [Array.isArray(config.webServer) ? config.webServer[0] : config.webServer!],
});

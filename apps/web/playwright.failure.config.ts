import { defineConfig } from "@playwright/test";

// Focused suite for the chat failure notice. Shares the existing isolated fake-pi
// harness but keeps artifacts and screenshots out of the repository.
export default defineConfig({
  testDir: "./tests",
  testMatch: "failure.browser.spec.ts",
  outputDir: "/tmp/kaiwu-failure-pw-results",
  workers: 1,
  timeout: 60000,
  use: {
    baseURL: "http://127.0.0.1:17331",
    viewport: { width: 1440, height: 900 },
    launchOptions: {
      executablePath: process.env.CHROMIUM_BIN || "/usr/bin/chromium",
      args: ["--no-sandbox", "--disable-background-networking"],
    },
    headless: true,
  },
  webServer: {
    command: "node tests/server.mjs",
    url: "http://127.0.0.1:17331/healthz",
    timeout: 180000,
    reuseExistingServer: false,
  },
  reporter: "list",
});

import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: "browser.spec.ts",
  workers: 1,
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:17331",
    launchOptions: {
      executablePath: process.env.CHROMIUM_BIN || "/usr/bin/chromium",
      args: ["--no-sandbox", "--disable-background-networking"],
    },
    headless: true,
  },
  webServer: {
    command: "node tests/server.mjs",
    url: "http://127.0.0.1:17331/healthz",
    timeout: 90000,
    reuseExistingServer: false,
  },
  reporter: "list",
});

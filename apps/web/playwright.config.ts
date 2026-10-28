import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  projects: [
    { name: "project-tools", testMatch: "project-tools.browser.spec.ts" },
    { name: "existing", testMatch: "browser.spec.ts" },
    { name: "delivery", testMatch: "delivery.browser.spec.ts", use: { baseURL: "http://127.0.0.1:17332" } },
    { name: "ui-consistency", testMatch: "ui-consistency.browser.spec.ts" },
    { name: "theme", testMatch: "theme.browser.spec.ts" },
    { name: "drawer", testMatch: "drawer.browser.spec.ts" },
  ],
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
  webServer: [
    { command: "node tests/server.mjs", url: "http://127.0.0.1:17331/healthz", timeout: 90000, reuseExistingServer: false },
    { command: "node tests/delivery.server.mjs", url: "http://127.0.0.1:17332/healthz", timeout: 90000, reuseExistingServer: false },
  ],
  reporter: "list",
});

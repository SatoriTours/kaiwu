import { defineConfig } from "@playwright/test";

const port = process.env.KAIWU_VERSION_TEST_PORT || "17551";

export default defineConfig({
  testDir: "./tests",
  testMatch: "version.browser.spec.ts",
  outputDir: "/tmp/kaiwu-version-pw-results",
  workers: 1,
  timeout: 30000,
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    launchOptions: {
      executablePath: process.env.CHROMIUM_BIN || "/usr/bin/chromium",
      args: ["--no-sandbox", "--disable-background-networking"],
    },
    headless: true,
  },
  webServer: {
    command: "node tests/version.server.mjs",
    url: `http://127.0.0.1:${port}/healthz`,
    timeout: 90000,
    reuseExistingServer: false,
    env: {
      KAIWU_VERSION_TEST_PORT: port,
    },
  },
  reporter: "list",
});

import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  testMatch: "chat-turn.browser.spec.ts",
  outputDir: "/tmp/kaiwu-chat-turn-pw-results",
  // Individual-test sharding only; workers:1 keeps execution strictly serial.
  // test:chat-turn uses fresh services between small shards (8 resident limit).
  fullyParallel: true,
  workers: 1,
  timeout: 30000,
  use: {
    baseURL: "http://127.0.0.1:17445",
    launchOptions: {
      executablePath: process.env.CHROMIUM_BIN || "/usr/bin/chromium",
      args: ["--no-sandbox", "--disable-background-networking"],
    },
    headless: true,
  },
  webServer: {
    command: "node tests/chat-turn.server.mjs",
    url: "http://127.0.0.1:17445/healthz",
    timeout: 90000,
    reuseExistingServer: false,
    // The fixture server spawns the real preview child process in its own
    // process group; a hard SIGKILL to the wrapper leaves it orphaned. Give the
    // fixture SIGTERM and time to run its cleanup before Playwright gives up.
    gracefulShutdown: { signal: "SIGTERM", timeout: 15000 },
  },
  reporter: "list",
});

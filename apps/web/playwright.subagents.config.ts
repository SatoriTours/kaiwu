import { defineConfig } from '@playwright/test';
export default defineConfig({
 testDir:'./tests', testMatch:'subagents.browser.spec.ts', workers:1, timeout:30000,
 use:{baseURL:'http://127.0.0.1:17333',headless:true,launchOptions:{executablePath:process.env.CHROMIUM_BIN||'/usr/bin/chromium',args:['--no-sandbox','--disable-background-networking']}},
 webServer:{command:'node tests/subagents.server.mjs',url:'http://127.0.0.1:17333/healthz',timeout:90000,reuseExistingServer:false},
 reporter:'list',
});

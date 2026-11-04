import {defineConfig} from '@playwright/test';
export default defineConfig({
 testDir:'./tests',testMatch:'connections.browser.spec.ts',workers:1,timeout:30000,
 use:{baseURL:'http://127.0.0.1:17331',headless:true,launchOptions:{executablePath:process.env.CHROMIUM_BIN||'/usr/bin/chromium',args:['--no-sandbox','--disable-background-networking']}},
 webServer:{command:'node tests/connections.server.mjs',url:'http://127.0.0.1:17331/healthz',timeout:90000,reuseExistingServer:false},
 reporter:'list',outputDir:process.env.KAIWU_CONNECTION_ARTIFACTS?process.env.KAIWU_CONNECTION_ARTIFACTS+'/browser-results':'test-results/connections',
});

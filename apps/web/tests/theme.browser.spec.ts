import { closeChat, openChat } from './view-navigation';
import { test, expect, type Page, type Locator } from '@playwright/test';
import { mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const artifacts = process.env.KAIWU_THEME_ARTIFACTS;
async function shot(page: Page, name: string) {
  if (!artifacts) return;
  mkdirSync(artifacts, { recursive: true });
  await page.screenshot({ path: resolve(artifacts, name + '.png'), fullPage: true });
}
async function palette(locator: Locator) {
  return locator.evaluate(el => {
    const s = getComputedStyle(el);
    return { background: s.backgroundColor, color: s.color, scheme: s.colorScheme };
  });
}
function luminance(color: string) {
  const rgb = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(v => {
    const x = v / 255;
    return x <= .04045 ? x / 12.92 : ((x + .055) / 1.055) ** 2.4;
  });
  return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722;
}
async function darkSurface(locator: Locator) {
  const colors = await palette(locator);
  expect(luminance(colors.background)).toBeLessThan(.12);
  expect((luminance(colors.color) + .05) / (luminance(colors.background) + .05)).toBeGreaterThanOrEqual(4.5);
}
async function choose(page: Page, name: string) {
  const reopen = await page.locator('.chat-drawer').isVisible();
  if (reopen) await closeChat(page);
  await page.getByRole('combobox', { name: '外观主题', exact: true }).click();
  await page.getByRole('option', { name, exact: true }).click();
  if (reopen) await openChat(page);
}
async function login(page: Page) {
  await page.goto('/');
  const response = await page.request.post('/api/login', {
    data: { token: 'kaiwu-browser-test-token-00000000' },
    headers: { Origin: new URL(page.url()).origin, 'X-Kaiwu-Client': 'web' },
  });
  expect(response.ok(), await response.text()).toBe(true);
  await page.reload();
  await expect(page.locator('.top')).toBeVisible();
}

test('system default renders a dark login and follows live OS changes without a saved override', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/');
  await expect(page.locator('.login-page')).toBeVisible();
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
  await darkSurface(page.locator('.login-page'));
  await darkSurface(page.locator('.login-page .card'));
  await shot(page, 'login-dark-desktop');
  expect(await page.evaluate(() => localStorage.getItem('kaiwu.theme'))).toBeNull();
  await page.emulateMedia({ colorScheme: 'light' });
  await expect.poll(async () => luminance((await palette(page.locator('.login-page'))).background)).toBeGreaterThan(.8);
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
});

for (const preference of ['light', 'dark'] as const) {
  test(`saved ${preference} is applied before the app bundle executes under the opposite OS scheme`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: preference === 'dark' ? 'light' : 'dark' });
    await page.addInitScript(value => localStorage.setItem('kaiwu.theme', value), preference);
    await page.route('**/assets/*.js', route => route.abort());
    await page.goto('/');
    await expect(page.locator('html')).toHaveAttribute('data-theme', preference);
    await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: preference });
    await expect(page.locator('#app')).toBeEmpty();
    expect((await page.request.get('/theme-init.js')).ok()).toBe(true);
  });
}

test('invalid stored preference is normalized to the system default', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('kaiwu.theme', 'invalid-value'));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'system');
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
  await page.getByRole('combobox', { name: '外观主题', exact: true }).click();
  await expect(page.getByRole('option', { name: '跟随系统', exact: true })).toHaveAttribute('aria-selected', 'true');
});

test('manual light and dark survive reload, ignore OS changes, then system restores following', async ({ page, context }) => {
  await page.emulateMedia({ colorScheme: 'dark' });
  await login(page);
  await choose(page, '浅色');
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'light' });
  expect(await page.evaluate(() => localStorage.getItem('kaiwu.theme'))).toBe('light');
  await page.reload();
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'light' });
  await choose(page, '深色');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
  const other = await context.newPage();
  await other.goto('/');
  await expect.poll(() => palette(other.locator('html'))).toMatchObject({ scheme: 'dark' });
  await choose(page, '浅色');
  await expect.poll(() => palette(other.locator('html'))).toMatchObject({ scheme: 'light' });
  await choose(page, '跟随系统');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
  await page.emulateMedia({ colorScheme: 'light' });
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'light' });
  expect(await page.evaluate(() => localStorage.getItem('kaiwu.theme'))).toBeNull();
  await other.close();
});

test('blocked preference storage falls back to system without breaking manual in-memory selection', async ({ page, context }) => {
  await context.addInitScript(() => Object.defineProperty(window, 'localStorage', { configurable: true, get() { throw new DOMException('blocked', 'SecurityError'); } }));
  const errors: string[] = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.goto('/');
  await expect(page.locator('.login-page')).toBeVisible();
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
  await choose(page, '浅色');
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'light' });
  await page.reload();
  await expect.poll(() => palette(page.locator('html'))).toMatchObject({ scheme: 'dark' });
  expect(errors).toEqual([]);
});

test('dark board, messages, Markdown, menus and dialogs remain usable on desktop and 360px without business writes from theme changes', async ({ page }) => {
  const writes: string[] = [];
  let watchWrites = false;
  page.on('request', r => { if (watchWrites && r.method() !== 'GET') writes.push(r.url()); });
  page.on('websocket', socket => socket.on('framesent', e => { const frame = JSON.parse(String(e.payload)); if (watchWrites && (frame.method === 'commands' || /^(git|previews)\//.test(frame.method ?? ''))) writes.push(frame.method); }));
  const prose = '## 主题验收\n\n正文与 `inline()`。\n\n| 项目 | 状态 |\n| --- | --- |\n| UI | 待验收 |\n\n```go\npackage main\nfunc main() { println("dark") }\n```\n\n[文档](https://example.com)';
  const transform = (data: any) => {
    for (const task of Object.values(data.state?.tasks ?? {}) as any[]) {
      if (task.title !== '深色任务') continue;
      const conversation = data.state.conversations[task.conversationId];
      conversation.messages = [
        { id: 'theme-user', role: 'user', text: '请检查主题效果', delivery: 'complete' },
        { id: 'theme-assistant', role: 'assistant', text: prose, delivery: 'complete' },
      ];
    }
  };
  await page.route('**/api/snapshot', async route => {
    const response = await route.fetch(), data = await response.json();
    transform(data); await route.fulfill({ response, json: data });
  });
  await page.routeWebSocket('**/api/socket', client => {
    const server = client.connectToServer();
    client.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (watchWrites && (frame.method === 'commands' || /^(git|previews)\//.test(frame.method ?? ''))) writes.push(frame.method);
      server.send(raw);
    });
    server.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (frame.type === 'snapshot') { transform(frame); client.send(JSON.stringify(frame)); }
      else client.send(raw);
    });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await login(page);
  await page.getByRole('button', { name: '当前项目', exact: true }).click();
  await page.getByRole('menuitem', { name: '添加项目', exact: true }).click();
  await page.locator('input[name=name]').fill('主题验收');
  await page.locator('input[name=path]').fill(readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8') + '-theme');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await expect(page.locator('#chat-title')).toHaveText('项目对话 · 主题验收');
  await page.locator('[data-action=new-topic]').click();
  await page.locator('input[name=name]').fill('主题专题');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await page.getByRole('button', { name: '在 主题专题 新建任务', exact: true }).click();
  await page.locator('input[name=title]').fill('深色任务');
  await page.locator('textarea[name=request]').fill('保持灰绿风格');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await expect(page.locator('#chat-title')).toContainText('深色任务');
  await expect(page).toHaveURL(/\/tasks\//);
  await page.reload(); // Read-only display fixture: no model or resident CLI session.
  await expect(page.locator('.message.assistant').last().getByRole('heading', { name: '主题验收' })).toBeVisible();
  watchWrites = true;
  const before = (await (await page.request.get('/api/snapshot')).json()).state.revision;
  await choose(page, '深色');
  for (const selector of ['.board-side', '.column', '.task-card', '.chat', '.chat .messages', '.message-bubble', '.chat .message.user .text', '.code-block', '#draft']) await darkSurface(page.locator(selector).first());
  await shot(page, 'workbench-dark-desktop');
  await closeChat(page);
  await page.getByRole('button', { name: '当前项目', exact: true }).click();
  await darkSurface(page.getByRole('menu', { name: '项目菜单' }));
  await shot(page, 'menu-dark-desktop');
  await page.keyboard.press('Escape');
  await page.locator('.version-trigger').click();
  await darkSurface(page.getByRole('dialog', { name: '系统版本与部署状态' }));
  await shot(page, 'version-dark-desktop');
  await page.keyboard.press('Escape');
  await openChat(page);
  for (const [width, height] of [[390, 844], [360, 800]]) {
    await page.setViewportSize({ width, height });
    await openChat(page);
    await expect(page.locator('#draft')).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await shot(page, 'chat-dark-' + width);
    await closeChat(page);
    const trigger = page.getByRole('combobox', { name: '外观主题', exact: true });
    await trigger.press('ArrowDown');
    await expect(page.getByRole('option', { name: '深色', exact: true })).toBeFocused();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await shot(page, 'theme-menu-dark-' + width);
    await page.keyboard.press('Escape');
    await expect(trigger).toBeFocused();
    await openChat(page);
  }
  await choose(page, '浅色');
  // Wait for the existing button transition to settle, not a fixed sleep or mid-animation screenshot.
  await expect.poll(async () => luminance((await palette(page.locator('.version-trigger'))).background)).toBeGreaterThan(.8);
  await shot(page, 'workbench-light-mobile');
  expect(writes).toEqual([]);
  expect((await (await page.request.get('/api/snapshot')).json()).state.revision).toBe(before);
});

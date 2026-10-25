import { test, expect, type Page } from '@playwright/test';
import { readFileSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { randomUUID } from 'node:crypto';

async function fixture(page: Page) {
  await page.goto('/');
  const headers = { Origin: new URL(page.url()).origin, 'X-Kaiwu-Client': 'web' };
  const login = await page.request.post('/api/login', { headers, data: { token: 'kaiwu-browser-test-token-00000000' } });
  expect(login.ok(), await login.text()).toBe(true);
  const command = async (body: any) => {
    const state = (await (await page.request.get('/api/snapshot')).json()).state;
    const r = await page.request.post('/api/commands', { headers, data: { ...body, operationId: randomUUID(), revision: state.revision } });
    expect(r.ok(), await r.text()).toBe(true);
    return (await r.json()).createdId;
  };
  const path = readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8');
  const projectId = await command({ type: 'add_project', name: '抽屉验收', path: path + '-drawer-' + randomUUID() });
  const taskId = await command({ type: 'add_task', projectId, title: '独立聊天任务', request: '仅隔离协议验证' });
  const secondId = await command({ type: 'add_task', projectId, title: '另一个任务', request: '隔离上下文' });
  await page.goto('/projects/' + projectId);
  await expect(page.locator('.task-card')).toHaveCount(2);
  return { projectId, taskId, secondId };
}
async function shot(page: Page, name: string) {
  const path = process.env.KAIWU_DRAWER_ARTIFACTS;
  if (!path) return;
  mkdirSync(path, { recursive: true });
  // Capture the settled UI, not the first frame showing only the modal backdrop.
  await page.locator('.chat-drawer').evaluate(async el => {
    await Promise.allSettled(el.getAnimations().map(animation => animation.finished));
  });
  await page.screenshot({ path: resolve(path, name + '.png'), fullPage: true });
}
const drawer = (page: Page) => page.getByRole('dialog', { name: '聊天面板', exact: true });
const close = (page: Page) => page.getByRole('button', { name: '收起聊天', exact: true }).click();

test('board keeps full width; project chat opens as a 720px overlay with dismissal and focus restoration', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await fixture(page);
  await expect(page.locator('#chat')).not.toBeVisible();
  expect(await page.locator('.chat-drawer').evaluate(el => parseFloat(getComputedStyle(el).transitionDuration))).toBeGreaterThan(0);
  const board = await page.locator('.board-side').boundingBox();
  expect(board!.width).toBe(1440);
  await shot(page, 'board-light-desktop');
  const opener = page.getByRole('button', { name: '项目对话', exact: true });
  await opener.click();
  await expect(drawer(page)).toBeVisible();
  await expect.poll(async () => Math.round((await drawer(page).boundingBox())!.x)).toBe(720);
  const box = (await drawer(page).boundingBox())!;
  expect(box.width).toBe(720);
  expect(box.height).toBe(900);
  expect((await page.locator('.board-side').boundingBox())!.width).toBe(board!.width);
  await expect(page.getByRole('button', { name: '收起聊天', exact: true })).toBeFocused();
  await page.keyboard.press('Shift+Tab');
  expect(await drawer(page).evaluate(el => el.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(drawer(page)).not.toBeVisible();
  await expect(opener).toBeFocused();
  await opener.click();
  await page.mouse.click(100, 350);
  await expect(drawer(page)).not.toBeVisible();
  await opener.click();
  await shot(page, 'project-light-desktop');
  await close(page);
  await expect(opener).toBeFocused();
});

test('closing task chat preserves drafts, session identity and a running fakepi reply without lifecycle writes', async ({ page }) => {
  const writes: string[] = [];
  let observing = false;
  await page.routeWebSocket('**/api/socket', client => {
    const server = client.connectToServer();
    client.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (observing && (frame.method === 'commands' || /^(git|previews)\//.test(frame.method ?? ''))) writes.push(frame.method);
      server.send(raw);
    });
    server.onMessage(raw => client.send(raw));
  });
  const { taskId, secondId } = await fixture(page);
  const card = page.locator('.task-card[data-taskid="' + taskId + '"]');
  await card.getByRole('button', { name: '选择任务 独立聊天任务', exact: true }).click();
  await expect(drawer(page)).toBeVisible();
  await page.locator('#draft').fill('__MARKDOWN_STREAM_FIXTURE__');
  await page.locator('#send').click();
  await expect(page.locator('#messages')).toContainText('流式格式');
  await page.locator('#draft').fill('关闭后保留的草稿');
  const snapshot = async () => (await (await page.request.get('/api/snapshot')).json()).state;
  const before = await snapshot(), task = before.tasks[taskId], original = before.conversations[task.conversationId];
  expect(original.runState).toBe('running');
  observing = true;
  page.on('request', r => { if (r.method() !== 'GET') writes.push(r.url()); });
  await close(page);
  await expect.poll(async () => (await snapshot()).conversations[task.conversationId].runState).toBe('idle');
  const finished = await snapshot();
  await card.click();
  await expect(page.locator('#draft')).toHaveValue('关闭后保留的草稿');
  await expect(page.locator('#messages')).toContainText('流式格式');
  await expect(page.locator('#messages')).toContainText('完成');
  await close(page);
  await page.locator('.task-card[data-taskid="' + secondId + '"]').click();
  await expect(page.locator('#draft')).toHaveValue('');
  await expect(page.locator('#messages')).not.toContainText('流式格式');
  await page.locator('#draft').fill('另一任务草稿');
  await close(page);
  await card.click();
  await expect(page.locator('#draft')).toHaveValue('关闭后保留的草稿');
  const after = await snapshot(), current = after.conversations[task.conversationId];
  expect(current.sessionId).toBe(original.sessionId);
  expect(current.sessionFile).toBe(original.sessionFile);
  expect(current.runState).toBe('idle');
  expect(current.messages).toEqual(finished.conversations[task.conversationId].messages);
  expect(after.revision).toBe(finished.revision);
  expect(writes).toEqual([]);
});

test('drawer adapts to mobile and reduced motion, preserves dark theme and lets nested model dialogs close first', async ({ page }) => {
  const { taskId } = await fixture(page);
  await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
  await page.locator('.task-card[data-taskid="' + taskId + '"]').click();
  expect(await drawer(page).evaluate(el => getComputedStyle(el).transitionDuration)).toBe('0s');
  for (const [width, height] of [[1440, 900], [768, 900], [390, 844], [360, 800], [320, 800]]) {
    await page.setViewportSize({ width, height });
    const box = (await drawer(page).boundingBox())!;
    expect(box.width).toBe(width <= 600 ? width : 720);
    expect(box.x + box.width).toBe(width);
    expect(box.height).toBe(height);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await expect(page.locator('#draft')).toBeVisible();
    await shot(page, 'task-dark-' + width);
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.locator('.composer').getByRole('button', { name: /模型|pi/ }).click();
  const model = page.getByRole('dialog', { name: '选择会话模型', exact: true });
  await expect(model).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(model).not.toBeVisible();
  await expect(drawer(page)).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(drawer(page)).not.toBeVisible();
});

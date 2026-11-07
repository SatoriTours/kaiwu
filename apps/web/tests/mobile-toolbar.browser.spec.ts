import { test, expect, type Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { readFileSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';

const longName = '工作台布局与移动端交互细节';
async function fixture(page: Page) {
  await page.goto('/');
  const headers = { Origin: new URL(page.url()).origin, 'X-Kaiwu-Client': 'web' };
  expect((await page.request.post('/api/login', { headers, data: { token: 'kaiwu-browser-test-token-00000000' } })).ok()).toBe(true);
  const snapshot = async () => (await (await page.request.get('/api/snapshot')).json()).state;
  const command = async (body: Record<string, unknown>) => {
    const state = await snapshot();
    const response = await page.request.post('/api/commands', { headers, data: { ...body, revision: state.revision, operationId: randomUUID() } });
    expect(response.ok(), await response.text()).toBe(true);
    return (await response.json()).createdId as string;
  };
  const path = readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8');
  const projectId = await command({ type: 'add_project', name: '手机列头验收', path: path + '-mobile-toolbar-' + randomUUID() });
  const normal = await command({ type: 'add_topic', projectId, name: '账号与登录' });
  const long = await command({ type: 'add_topic', projectId, name: longName });
  const empty = await command({ type: 'add_topic', projectId, name: '稍后考虑' });
  const state = await snapshot();
  const other = Object.values(state.topics).find((t: any) => t.projectId === projectId && t.protected) as any;
  for (const title of ['添加数据库的能力，我要能输入我的每段话', '支持 Windows、macOS 和 Linux 长项目名']) {
    await command({ type: 'add_task', projectId, topicId: other.id, title, request: '只创建，不启动CLI' });
  }
  await command({ type: 'add_task', projectId, topicId: normal, title: '登录恢复', request: '只创建，不启动CLI' });
  await page.goto('/projects/' + projectId);
  return { projectId, normal, long, empty, other: other.id, snapshot };
}
const trigger = (page: Page) => page.getByRole('combobox', { name: '当前看板专题', exact: true });
async function choose(page: Page, id: string) {
  await trigger(page).click();
  await page.locator('[role="option"][data-value="' + id + '"]').click();
}
async function shot(page: Page, name: string) {
  const path = process.env.KAIWU_MOBILE_TOOLBAR_ARTIFACTS;
  if (!path) return;
  mkdirSync(path, { recursive: true });
  const viewport = page.viewportSize()!;
  await page.mouse.move(viewport.width - 2, viewport.height - 2);
  await page.screenshot({ path: resolve(path, name + '.png'), fullPage: true, animations: 'disabled' });
}

test('mobile merges topic selection into one column header and preserves theme backgrounds and 44px actions', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const f = await fixture(page);
  await choose(page, f.other);
  await expect(page.locator('.mobile-board-header')).toBeVisible();
  await expect(page.locator('.column:visible .column-head')).not.toBeVisible();
  await expect(page.locator('.toolbar')).not.toBeVisible();
  for (const theme of ['dark', 'light']) {
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
    for (const width of [320, 360, 390, 430]) {
      await page.setViewportSize({ width, height: 844 });
      await choose(page, f.long);
      await expect(trigger(page)).toHaveAttribute('title', longName);
      await expect(trigger(page)).toHaveAccessibleDescription(longName + '，0 个任务');
      await expect(page.locator('.mobile-topic-count')).toHaveText('0');
      await expect(page.locator('.column:visible .empty')).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
      const header = page.locator('.mobile-board-header');
      expect(await header.evaluate(el => el.scrollWidth > el.clientWidth)).toBe(false);
      for (const button of await header.locator('button:visible').all()) {
        const box = (await button.boundingBox())!;
        expect(box.width).toBeGreaterThanOrEqual(44); expect(box.height).toBeGreaterThanOrEqual(44);
      }
      await expect(page.locator('.column:visible')).toHaveCSS('background-color', theme === 'dark' ? 'rgb(51, 58, 67)' : 'rgb(243, 245, 243)');
      if (width === 320) await shot(page, 'mobile-toolbar-long-' + theme);
      await choose(page, f.other);
      await expect(page.locator('.mobile-topic-count')).toHaveText('2');
      if (width === 390) await shot(page, 'mobile-toolbar-' + theme);
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(page.locator('.mobile-board-header')).not.toBeVisible();
  await expect(page.locator('.toolbar [data-action=new-topic]')).toBeVisible();
  await expect(page.locator('.toolbar [data-action=new-task]')).toBeVisible();
  await expect(page.locator('.column-head:visible')).toHaveCount(4);
});

test('topic switching sends no lifecycle writes and retains selector keyboard focus, while new tasks target the chosen topic', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const writes: string[] = [];
  let watching = false;
  page.on('request', req => { if (watching && req.method() !== 'GET') writes.push(req.url()); });
  page.on('websocket', socket => socket.on('framesent', event => { const frame = JSON.parse(String(event.payload)); if (watching && frame.method === 'commands') writes.push(frame.method); }));
  const f = await fixture(page);
  const before = await f.snapshot();
  watching = true;
  await choose(page, f.normal);
  await expect(trigger(page)).toBeFocused();
  await expect(page.locator('.column:visible .task-card')).toHaveCount(1);
  await trigger(page).press('Enter');
  await page.keyboard.press('End');
  await page.keyboard.press('Escape');
  await expect(trigger(page)).toBeFocused();
  expect(writes).toEqual([]);
  watching = false;
  const after = await f.snapshot();
  expect(after.executions).toEqual(before.executions);
  expect(after.conversations).toEqual(before.conversations);
  await page.getByRole('button', { name: '在 账号与登录 新建任务', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '创建任务卡片', exact: true });
  await expect(dialog).toBeVisible();
  await expect(dialog.locator('input[name=topicId]')).toHaveValue(f.normal);
  await dialog.locator("textarea[name=request]").fill('当前专题新任务' + "\n" + '只创建卡片，不启动CLI');
  await dialog.getByRole('button', { name: '创建', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  const final = await f.snapshot();
  const created = Object.values(final.tasks).find((t: any) => t.title === '当前专题新任务') as any;
  expect(created.topicId).toBe(f.normal);
  expect(created.executionId).toBeFalsy();
});

test('menu footer is not an option and preserves explicit topic creation and protected deletion rules', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const f = await fixture(page);
  await choose(page, f.other);
  await trigger(page).press('Enter');
  const list = page.getByRole('listbox', { name: '当前看板专题', exact: true });
  await expect(list.getByRole('button', { name: '新建专题', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: '删除专题 其他', exact: true })).toHaveCount(0);
  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: '新建专题', exact: true })).toBeFocused();
  await shot(page, 'mobile-toolbar-menu');
  await page.keyboard.press('Enter');
  const create = page.getByRole('dialog', { name: '添加功能专题', exact: true });
  await expect(create).toBeVisible();
  await create.locator('input[name=name]').fill('手机新专题');
  await create.getByRole('button', { name: '保存', exact: true }).click();
  await expect(create).not.toBeVisible();
  await expect(trigger(page)).toHaveAttribute('title', '手机新专题');
  await choose(page, f.normal);
  await trigger(page).click();
  await page.getByRole('button', { name: '删除专题 账号与登录', exact: true }).click();
  const remove = page.getByRole('dialog', { name: '删除专题', exact: true });
  await expect(remove).toBeVisible();
  await expect(remove).toContainText('所有任务将移到');
  await remove.getByRole('button', { name: '取消', exact: true }).click();
  const state = await f.snapshot();
  expect(state.topics.some((topic: any) => topic.id === f.normal)).toBe(true);
  expect(Object.values(state.tasks).some((t: any) => t.topicId === f.normal)).toBe(true);
});

test('short mobile viewports keep the topic menu clickable and offline navigation cannot create topics or tasks', async ({ page, context }) => {
  await page.setViewportSize({ width: 320, height: 440 });
  const f = await fixture(page);
  await choose(page, f.other);
  await trigger(page).click();
  const footer = page.getByRole('button', { name: '新建专题', exact: true });
  await expect(footer).toBeVisible();
  expect(await footer.evaluate(el => { const r = el.getBoundingClientRect(); const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2); return r.bottom <= innerHeight && !!hit && el.contains(hit); })).toBe(true);
  await page.keyboard.press('Escape');
  await context.setOffline(true);
  await expect(page.getByRole('button', { name: '在 其他 新建任务', exact: true })).toBeDisabled();
  await trigger(page).click();
  await expect(footer).toBeDisabled();
  await page.locator('[role=option][data-value="' + f.empty + '"]').click();
  await expect(trigger(page)).toHaveAttribute('title', '稍后考虑');
  await expect(trigger(page)).toBeFocused();
  await context.setOffline(false);
});


test('desktop column actions keep their topic and keyboard location when resizing to mobile', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const f = await fixture(page);
  await choose(page, f.other);

  const desktop = async () => {
    await page.setViewportSize({ width: 1440, height: 900 });
    // Wait for Vue's resize handler, not just the synchronous CSS media query.
    await expect(page.locator('.toolbar [data-action=new-topic]')).toBeVisible();
  };
  // Existing column controls, not the top toolbar; first the current topic.
  await desktop();
  await page.locator('.column[data-topicid="' + f.other + '"] .column-head').getByRole('button', { name: '在 其他 新建任务', exact: true }).focus();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('.mobile-board-header [data-action=new-task]')).toBeFocused();
  await expect(trigger(page)).toHaveAttribute('title', '其他');

  // A different desktop column must not silently send the user to another topic.
  await desktop();
  const normalHeader = page.locator('.column[data-topicid="' + f.normal + '"] .column-head');
  await normalHeader.getByRole('button', { name: '在 账号与登录 新建任务', exact: true }).focus();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('.mobile-board-header [data-action=new-task]')).toBeFocused();
  await expect(trigger(page)).toHaveAttribute('title', '账号与登录');

  // Deletion stays in the menu; resizing must neither open a dialog nor delete.
  await choose(page, f.other);
  const before = await f.snapshot();
  await desktop();
  const remove = normalHeader.getByRole('button', { name: '删除专题 账号与登录', exact: true });
  await remove.focus();
  await expect(remove).toBeFocused();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(trigger(page)).toBeFocused();
  await expect(trigger(page)).toHaveAttribute('title', '账号与登录');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(page.getByRole('listbox', { name: '当前看板专题', exact: true })).toHaveCount(0);
  const after = await f.snapshot();
  expect(after.tasks).toEqual(before.tasks);
  expect(after.topics).toEqual(before.topics);
  expect(after.executions).toEqual(before.executions);
  expect(after.conversations).toEqual(before.conversations);
});

test('project-wide pending count survives the compact header and responsive controls retain focus', async ({ page }) => {
  // View-only pending interaction, not a real model/extension request.
  let projectId = '';
  await page.routeWebSocket('**/api/socket', client => {
    const server = client.connectToServer();
    client.onMessage(raw => server.send(raw));
    server.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (projectId && frame.type === 'patch') return;
      if (projectId && frame.type === 'snapshot') for (const task of Object.values(frame.state.tasks) as any[]) {
        if (task.projectId !== projectId || task.title !== '登录恢复') continue;
        frame.state.conversations[task.conversationId].interactions = [{
          schemaVersion: 1, id: 'view-pending', version: 1, projectId, conversationId: task.conversationId,
          taskId: task.id, source: 'agent_ui', title: '确认显示样本',
          fields: [{ id: 'confirm', type: 'confirm', label: '是否继续', required: true }],
          blocking: true, allowCancel: true, status: 'pending', createdAt: new Date().toISOString(),
        }];
      }
      client.send(JSON.stringify(frame));
    });
  });
  await page.setViewportSize({ width: 390, height: 844 });
  const f = await fixture(page);
  projectId = f.projectId;
  await page.reload();
  await choose(page, f.other);
  await expect(page.locator('.mobile-waiting-toolbar')).toHaveText('3 个任务1 项待你处理');
  await expect(page.locator('.mobile-waiting-toolbar .board-interaction-count')).toBeVisible();
  await trigger(page).focus();
  await page.setViewportSize({ width: 800, height: 844 });
  await expect(page.locator('.toolbar [data-action=new-topic]')).toBeFocused();
  await page.locator('.toolbar [data-action=new-task]').focus();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator('.mobile-board-header [data-action=new-task]')).toBeFocused();
  await shot(page, 'mobile-toolbar-pending');
});

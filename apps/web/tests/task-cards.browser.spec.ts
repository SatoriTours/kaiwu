import { test, expect, type Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { readFileSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';

const title = '添加数据库的能力，我要能输入我的每段话';
const longTitle = '支持 Windows、macOS 和 Linux 长项目名 /workspace/projects/very-long-project-name';
async function fixture(page: Page, count = 2) {
  await page.goto('/');
  const headers = { Origin: new URL(page.url()).origin, 'X-Kaiwu-Client': 'web' };
  const login = await page.request.post('/api/login', { headers, data: { token: 'kaiwu-browser-test-token-00000000' } });
  expect(login.ok(), await login.text()).toBe(true);
  const command = async (body: Record<string, unknown>) => {
    const state = (await (await page.request.get('/api/snapshot')).json()).state;
    const response = await page.request.post('/api/commands', { headers, data: { ...body, operationId: randomUUID(), revision: state.revision } });
    expect(response.ok(), await response.text()).toBe(true);
    return (await response.json()).createdId as string;
  };
  const path = readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8');
  const projectId = await command({ type: 'add_project', name: '卡片验收', path: path + '-cards-' + randomUUID() });
  const ids: string[] = [];
  for (let i = 0; i < count; i++) ids.push(await command({ type: 'add_task', projectId, title: i === 0 ? title : i === 1 ? longTitle : '状态任务 ' + i, request: '结构化登录恢复' }));
  await page.goto('/projects/' + projectId);
  await expect(page.locator('.task-card')).toHaveCount(count);
  return { ids, projectId };
}
async function shot(page: Page, name: string) {
  const dir = process.env.KAIWU_TASK_CARD_ARTIFACTS;
  if (!dir) return;
  mkdirSync(dir, { recursive: true });
  await page.screenshot({ path: resolve(dir, name + '.png'), fullPage: true });
}
async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
  for (const card of await page.locator('.task-card:visible').all()) {
    expect(await card.evaluate(el => el.scrollWidth > el.clientWidth + 1)).toBe(false);
  }
}

test('title leads, identity sits below, and theme-local card styling leaves chat unchanged', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const { ids } = await fixture(page);
  const card = page.locator('[data-taskid="' + ids[0] + '"]');
  await expect(card.locator('.task-number')).toHaveText('KW-' + ids[0].slice(0, 6));
  const heading = (await card.locator('h3').boundingBox())!;
  const identity = (await card.locator('.task-number').boundingBox())!;
  expect(identity.y).toBeGreaterThan(heading.y + heading.height);
  await expect(card.locator('.owner,.avatar')).toHaveCount(0);
  await expect(card.locator('.task-actions .start-task')).toHaveCount(1);
  await expect(card.locator('.task-actions .task-menu-toggle')).toHaveCount(1);
  await expect(card.locator('.task-select')).toHaveAccessibleName('选择任务 ' + title);
  await expect(card.locator('.task-select')).toHaveAccessibleDescription('未开始');
  for (const [theme, color] of [['light', 'rgb(255, 255, 255)'], ['dark', 'rgb(48, 54, 62)']] as const) {
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
    await page.mouse.move(1400, 850);
    await expect(card).toHaveCSS('background-color', color);
    await expect(card).toHaveCSS('padding', '14px');
    await expect(card).toHaveCSS('border-radius', '7px');
    await expect(card.locator('h3')).toHaveCSS('font-size', '14px');
    await expect(page.locator('.column').first()).toHaveCSS('background-color', theme === 'dark' ? 'rgb(51, 58, 67)' : 'rgb(243, 245, 243)');
    await shot(page, 'cards-' + theme + '-desktop');
  }
  await page.evaluate(() => document.documentElement.dataset.theme = 'system');
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(card).toHaveCSS('background-color', 'rgb(48, 54, 62)');
  await expect(page.locator('.column').first()).toHaveCSS('background-color', 'rgb(51, 58, 67)');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(card).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  await expect(page.locator('.column').first()).toHaveCSS('background-color', 'rgb(243, 245, 243)');
  await card.locator('.task-select').click();
  await expect(page.getByRole('dialog', { name: '聊天面板', exact: true })).toBeVisible();
  await expect(page.locator('.chat')).toHaveCSS('background-color', 'rgb(255, 255, 255)');
});

test('mobile and 248px columns retain complete title access and 44px actions without horizontal overflow', async ({ page }) => {
  const { ids } = await fixture(page);
  const first = page.locator('[data-taskid="' + ids[0] + '"]');
  for (const theme of ['light', 'dark']) {
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
    for (const width of [320, 360, 390]) {
      await page.setViewportSize({ width, height: 844 });
      await noOverflow(page);
      const buttons = first.locator('.start-task,.task-menu-toggle');
      for (const button of await buttons.all()) {
        const box = (await button.boundingBox())!;
        expect(box.width).toBeGreaterThanOrEqual(44);
        expect(box.height).toBeGreaterThanOrEqual(44);
      }
      await expect(page.getByRole('combobox', { name: '当前看板专题', exact: true })).toBeVisible();
      await expect(page.locator('[data-taskid="' + ids[1] + '"] .task-select')).toHaveAttribute('title', longTitle);
      if (width === 390) await shot(page, 'cards-' + theme + '-mobile');
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await first.locator('xpath=ancestor::section[contains(@class,"column")]').evaluate(el => {
    (el as HTMLElement).style.flex = '0 0 248px';
    (el as HTMLElement).style.width = '248px';
  });
  await noOverflow(page);
  await first.locator('.task-select').focus();
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await expect(first.locator('.task-select')).toBeFocused();
  await expect(first.locator('.task-select')).toHaveCSS('outline-width', '2px');
});

test('real fake-pi development preserves Git identity, menu reachability and action isolation', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const { ids } = await fixture(page);
  const card = page.locator('[data-taskid="' + ids[0] + '"]');
  // First-card menu must not be clipped by the independently scrolling column.
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await card.getByRole('button', { name: '任务操作', exact: true }).click();
    const menu = page.getByRole('menu', { name: '任务操作', exact: true });
    await expect(menu).toBeVisible();
    expect(await menu.evaluate(el => {
      const r = el.getBoundingClientRect();
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + 12);
      return r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight && !!hit && el.contains(hit);
    })).toBe(true);
    await expect(page.getByRole('dialog', { name: '聊天面板', exact: true })).not.toBeVisible();
    await page.keyboard.press('Escape');
    await expect(card.getByRole('button', { name: '任务操作', exact: true })).toBeFocused();
  }
  await card.getByRole('button', { name: '开始开发', exact: true }).click();
  await expect(card.locator('.task-status')).toHaveText('待验收');
  await page.getByRole('button', { name: '收起聊天', exact: true }).click();
  await expect(card.locator('.task-identity .git-state')).toBeVisible();
  const snapshot = async () => (await (await page.request.get('/api/snapshot')).json()).state;
  const before = await snapshot();
  await card.getByRole('button', { name: '任务操作', exact: true }).click();
  await page.getByRole('menuitem', { name: '标记任务完成', exact: true }).click();
  const confirm = page.getByRole('dialog', { name: '标记任务完成', exact: true });
  await expect(confirm).toBeVisible();
  await confirm.getByRole('button', { name: '取消', exact: true }).click();
  const after = await snapshot();
  expect(after.tasks[ids[0]].status).toBe(before.tasks[ids[0]].status);
  expect(after.tasks[ids[1]].status).toBe('idle');
  expect(after.conversations[after.tasks[ids[0]].conversationId].sessionId).toBe(before.conversations[before.tasks[ids[0]].conversationId].sessionId);
});


test('render-only state snapshots keep attention, Git, subagents and delivery independent', async ({ page }) => {
  // These are view fixtures, never real execution/CI evidence.
  let ids: string[] = [];
  const statuses = ['idle', 'running', 'waiting_user', 'needs_confirmation', 'completed', 'failed', 'interrupted'];
  function project(data: any) {
    if (!data.state || ids.length !== statuses.length) return;
    const state = data.state;
    state.executions ??= {};
    state.taskGit ??= {};
    state.subagents ??= {};
    state.deliveries ??= {};
    ids.forEach((id, index) => {
      const task = state.tasks[id];
      if (!task) return;
      const status = statuses[index], conversation = state.conversations[task.conversationId];
      task.status = status;
      conversation.runState = status === 'running' ? 'running' : status === 'failed' ? 'failed' : status === 'interrupted' ? 'interrupted' : 'idle';
      conversation.rounds = [];
      conversation.interactions = [];
      if (status !== 'idle') {
        task.executionId = 'view-' + id;
        state.executions[task.executionId] = { id: task.executionId, phase: status === 'needs_confirmation' ? 'awaiting_review' : status === 'completed' ? 'confirmed' : status, finishedAt: status === 'needs_confirmation' ? new Date().toISOString() : undefined, tools: [] };
        state.taskGit[id] = { taskId: id, state: status === 'failed' ? 'merged' : 'dirty', mainBranch: 'main', canMerge: false, dirtyFiles: [] };
      }
      if (status === 'waiting_user') {
        conversation.interactions = [0, 1].map(n => ({
          schemaVersion: 1, id: 'view-question-' + n, version: 1, projectId: task.projectId,
          conversationId: task.conversationId, taskId: id, source: 'agent_ui', title: '设计夹具确认',
          fields: [{ id: 'confirm', type: 'confirm', label: '是否继续', required: true }],
          blocking: true, allowCancel: true, status: 'pending', createdAt: new Date().toISOString(),
          native: { method: 'confirm', upstreamId: 'view-upstream-' + n },
        }));
      }
      if (status === 'running') state.subagents['view-child'] = {
        id: 'view-child', projectId: task.projectId, conversationId: task.conversationId, taskId: id,
        roundId: 'view-round', parentSessionId: 'view-session', title: '只读状态样本',
        taskSummary: '不实际派发', status: 'running', profile: task.profile, startedAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(), version: 1, truncated: false,
      };
      if (status === 'completed') state.deliveries[id] = { taskId: id, phase: 'paused' };
    });
  }
  await page.routeWebSocket('**/api/socket', client => {
    const server = client.connectToServer();
    client.onMessage(raw => server.send(raw));
    server.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (ids.length && frame.type === 'patch') return;
      if (frame.type === 'snapshot') project(frame);
      client.send(JSON.stringify(frame));
    });
  });
  const fixtureState = await fixture(page, statuses.length);
  ids = fixtureState.ids;
  await page.route('**/api/snapshot', async route => {
    const response = await route.fetch(), data = await response.json();
    project(data);
    await route.fulfill({ response, json: data });
  });
  await page.reload();
  await page.setViewportSize({ width: 1440, height: 1200 });
  for (let i = 0; i < ids.length; i++) {
    const card = page.locator('[data-taskid="' + ids[i] + '"]');
    await expect(card.locator('.task-status')).toHaveAttribute('data-status', statuses[i]);
    await expect(card.locator('.task-status')).toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
  }
  const running = page.locator('[data-taskid="' + ids[1] + '"]');
  const waiting = page.locator('[data-taskid="' + ids[2] + '"]');
  const completed = page.locator('[data-taskid="' + ids[4] + '"]');
  await expect(running.locator('.task-auxiliary .subagent-badge')).toContainText('运行');
  await expect(running.locator('.merge-task')).toBeDisabled();
  await expect(running.locator('.start-task')).toBeDisabled();
  await expect(waiting.locator('.task-status')).toContainText('2');
  await expect(waiting.locator('.merge-task')).toBeDisabled();
  await expect(completed.locator('.task-auxiliary')).toContainText('交付 · 已暂停');
  await expect(page.locator('[data-taskid="' + ids[5] + '"] .task-identity')).toContainText('已合并');
  for (const theme of ['light', 'dark']) {
    await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
    await page.mouse.move(1400, 1150);
    await expect(waiting).toHaveCSS('background-color', theme === 'dark' ? 'rgb(48, 54, 62)' : 'rgb(255, 255, 255)');
    await shot(page, 'cards-states-' + theme + '-desktop');
    for (const width of [320, 390]) {
      await page.setViewportSize({ width, height: 1200 });
      await noOverflow(page);
      await running.scrollIntoViewIfNeeded();
      for (const action of await running.locator('.task-actions button').all()) {
        const box = (await action.boundingBox())!;
        expect(box.width).toBeGreaterThanOrEqual(44);
      }
    }
    await page.setViewportSize({ width: 1440, height: 1200 });
  }
});


test('long unread title keeps its new-reply marker visible outside the clamped text', async ({ page }) => {
  // View-only reply snapshot exercises the real unread calculation without starting a model.
  let notify: ((id: string) => void) | undefined;
  let holdPatches = false;
  const unreadTitle = '检查任务卡片在窄屏下的完整标题以及新回复提醒'.repeat(3);
  await page.routeWebSocket('**/api/socket', client => {
    const server = client.connectToServer();
    client.onMessage(raw => server.send(raw));
    server.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (holdPatches && frame.type === 'patch') return;
      if (frame.type === 'snapshot') notify = id => {
        const next = structuredClone(frame), task = next.state.tasks[id];
        // Initial snapshots may precede a watch patch; keep this view-only fixture newer.
        next.state.revision += 1_000_000;
        task.title = unreadTitle;
        next.state.conversations[task.conversationId].messages.push({
          id: 'view-unread', role: 'assistant', text: '只读新回复显示样本', delivery: 'delivered',
        });
        holdPatches = true;
        client.send(JSON.stringify(next));
      };
      client.send(raw);
    });
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  const { ids } = await fixture(page);
  expect(unreadTitle.length).toBeLessThanOrEqual(80);
  await expect.poll(() => !!notify).toBe(true);
  notify!(ids[1]);
  const card = page.locator('[data-taskid="' + ids[1] + '"]');
  await expect(card.getByRole('img', { name: '有新回复' })).toHaveCount(1);
  await card.locator('xpath=ancestor::section[contains(@class,"column")]').evaluate(el => {
    (el as HTMLElement).style.flex = '0 0 248px';
    (el as HTMLElement).style.width = '248px';
  });
  for (const width of [1440, 320, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const theme of ['light', 'dark']) {
      await page.evaluate(value => document.documentElement.dataset.theme = value, theme);
      const marker = card.locator('.unread');
      await expect(marker).toBeVisible();
      expect(await marker.evaluate(el => {
        const r = el.getBoundingClientRect();
        return document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2) === el;
      }), 'reply marker must actually be painted, not hidden by line clamping').toBe(true);
      await expect(card.locator('.task-select')).toHaveAttribute('title', unreadTitle);
      await expect(card.locator('.task-select')).toHaveAccessibleDescription('未开始 有新回复');
      await noOverflow(page);
      if (width === 1440) await shot(page, 'cards-unread-' + theme + '-248');
    }
  }
});

test('resize and real column scroll close menus without losing or stealing focus', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 440 });
  await fixture(page, 6);
  // Server map order is not creation order; start at the visible top card.
  const card = page.locator('.task-card').first();
  const trigger = card.getByRole('button', { name: '任务操作', exact: true });
  const menu = page.getByRole('menu', { name: '任务操作', exact: true });
  const open = async () => {
    await trigger.focus();
    await page.keyboard.press('Enter');
    await expect(menu).toBeVisible();
    await expect(menu.getByRole('menuitem').first()).toBeFocused();
  };
  await open();
  await page.setViewportSize({ width: 1400, height: 440 });
  await expect(menu).not.toBeVisible();
  await expect(trigger).toBeFocused();
  await open();
  const column = card.locator('xpath=ancestor::div[contains(@class,"column-cards")]');
  await column.evaluate(el => el.scrollTop += 20);
  await expect(menu).not.toBeVisible();
  await expect(trigger).toBeFocused();
  expect(await column.evaluate(el => el.scrollTop)).toBeGreaterThan(0);
  await open();
  const outside = page.getByRole('button', { name: '＋ 专题', exact: true });
  await outside.focus();
  await page.setViewportSize({ width: 1440, height: 440 });
  await expect(outside).toBeFocused();
  await expect(menu).not.toBeVisible();
});

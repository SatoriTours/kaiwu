import { test, expect, type Page } from '@playwright/test';
import { readFileSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { randomUUID } from 'node:crypto';
import { spawnSync } from 'node:child_process';

async function fixture(page: Page, request = '仅隔离协议验证') {
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
  const taskId = await command({ type: 'add_task', projectId, title: '独立聊天任务', request });
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
  await expect(page.locator('.board-bottom')).toHaveCount(0);
  await expect(page.getByText('专题组织任务 · 选择卡片沟通 · 点击开始开发执行任务',{exact:true})).toHaveCount(0);
  expect(await page.locator('.chat-drawer').evaluate(el => parseFloat(getComputedStyle(el).transitionDuration))).toBeGreaterThan(0);
  const board = await page.locator('.board-side').boundingBox();
  const columnHeight=(await page.locator('.column').first().boundingBox())!.height;
  expect(board!.width).toBe(1440);
  const column=(await page.locator('.column').first().boundingBox())!;
  expect(board!.y+board!.height-column.y-column.height).toBeLessThanOrEqual(18);
  await shot(page, 'board-light-desktop');
  const opener = page.getByRole('button', { name: '项目对话', exact: true });
  await opener.click();
  await expect(drawer(page)).toBeVisible();
  await expect.poll(async () => Math.round((await drawer(page).boundingBox())!.x)).toBe(720);
  const box = (await drawer(page).boundingBox())!;
  expect(box.width).toBe(720);
  expect(box.height).toBe(900);
  expect((await page.locator('.board-side').boundingBox())!.width).toBe(board!.width);
  expect.soft(Math.abs((await page.locator('.column').first().boundingBox())!.height-columnHeight)).toBeLessThanOrEqual(1);
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


test('compact composer and uncluttered sender labels work in project and task chat', async ({ page }) => {
  const { taskId } = await fixture(page);
  for (const mode of ['project', 'task'] as const) {
    await page.setViewportSize({ width: mode === 'project' ? 1440 : 390, height: 844 });
    await page.emulateMedia({ colorScheme: mode === 'project' ? 'light' : 'dark', reducedMotion: 'reduce' });
    if (mode === 'project') await page.getByRole('button', { name: '项目对话', exact: true }).click();
    else await page.locator('.task-card[data-taskid="' + taskId + '"]').click();
    const input = page.locator('#draft');
    expect.soft((await input.boundingBox())!.height).toBeLessThanOrEqual(50);
    expect.soft((await page.locator('.editor').boundingBox())!.height).toBeLessThanOrEqual(112);
    await input.fill('首行');
    await input.press('Shift+Enter');
    await input.pressSequentially('second line');
    await expect(input).toHaveValue('首行\nsecond line');
    await input.fill(Array.from({ length: 10 }, (_, i) => 'line ' + i).join('\n'));
    expect(await input.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true);
    await page.getByRole('button', { name: '预览 Markdown', exact: true }).click();
    await expect(page.locator('.draft-preview')).toContainText('line 9');
    await page.getByRole('button', { name: '返回编辑', exact: true }).click();
    await expect(input).toBeFocused();
    await input.fill('__MARKDOWN_STREAM_FIXTURE__');
    await page.locator('#send').click();
    await expect(page.locator('#messages .message.assistant')).toBeVisible();
    await expect(page.locator('#messages .message.user .message-meta strong')).toHaveCount(0);
    await expect(page.locator('#messages .message-avatar')).toHaveCount(0);
    await expect(page.locator('#messages .message.assistant .message-meta strong')).toHaveCount(0);
    const info=page.locator('.reply-info');await info.locator('summary').click();await expect(info).toContainText('pi · '+(mode==='project'?'项目':'任务')+' Agent');await info.locator('summary').click();
    await expect(page.locator('#messages .message.user .message-status')).toContainText('已发送');
    await expect(page.locator('#messages .message.user')).toHaveAttribute('aria-label', '我的消息');
    await shot(page, 'compact-' + mode);
    await close(page);
  }
});

test('C cold chat uses readable unboxed replies and no redundant identity or instruction rows', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
  const { taskId } = await fixture(page);
  await page.locator('.task-card[data-taskid="' + taskId + '"] .task-select').click();
  await page.locator('#draft').fill('__MARKDOWN_STREAM_FIXTURE__');
  await page.locator('#send').click();
  await expect(page.locator('.assistant-summary-markdown')).toContainText('完成');
  expect.soft(await page.locator('#messages').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(36, 40, 46)');
  expect.soft(await page.locator('.message.user .text').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(65, 75, 89)');
  expect.soft(await page.locator('.editor').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(51, 58, 67)');
  const prose = page.locator('.assistant-summary-markdown .markdown-body');
  expect.soft(await prose.evaluate(el => getComputedStyle(el).fontSize)).toBe('14px');
  expect.soft(await prose.evaluate(el => getComputedStyle(el).lineHeight)).toBe('26.6px');
  expect.soft(await prose.evaluate(el => getComputedStyle(el).letterSpacing)).toBe('0.14px');
  expect.soft(await page.locator('.message-bubble').evaluate(el => getComputedStyle(el).borderTopWidth)).toBe('0px');
  expect.soft(await page.locator('.message.assistant .message-meta strong').count()).toBe(0);
  expect.soft(await page.locator('.message.assistant .message-status').count()).toBe(0);
  expect.soft(await page.locator('.context-label,.composer-tip').count()).toBe(0);
  expect.soft(await page.locator('#draft').evaluate(el=>getComputedStyle(el).backgroundColor)).toBe('rgba(0, 0, 0, 0)');
  const tools=page.locator('.editor .row');const plus=await tools.getByRole('button',{name:'添加附件',exact:true}).boundingBox();const capture=await tools.getByRole('button',{name:'截图',exact:true}).boundingBox();
  expect.soft(capture!.x-(plus!.x+plus!.width)).toBeLessThanOrEqual(8);
  expect.soft((await page.locator('.chat-filter-bar').boundingBox())!.width).toBeLessThan(160);
  await expect.soft(page.locator('.chat-head').getByRole('button', { name: '只看我的问题', exact: true })).toBeVisible();
  await expect.soft(page.locator('.editor').getByRole('button', { name: '预览 Markdown', exact: true })).toBeVisible();
  await expect.soft(page.getByRole('button', { name: '确认完成', exact: true })).toHaveCount(0);
  await shot(page, 'cold-task-desktop');
  await page.setViewportSize({ width: 390, height: 844 });
  expect.soft(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await shot(page, 'cold-task-mobile');
  await close(page);
});

test('optional card completion confirms only the ended task and leaves readable history and Git untouched', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
  await page.setViewportSize({ width: 1440, height: 900 });
  const { taskId, secondId } = await fixture(page, '结构化登录恢复');
  const card = page.locator('.task-card[data-taskid="' + taskId + '"]');
  await card.getByRole('button', { name: '开始开发', exact: true }).click();
  await expect(page.locator('.assistant-summary-markdown')).toContainText('登录恢复');
  await expect(page.getByRole('button', { name: '确认完成', exact: true })).toHaveCount(0);
  const snapshot = async () => (await (await page.request.get('/api/snapshot')).json()).state;
  const before = await snapshot();
  const priorRound = before.conversations[before.tasks[taskId].conversationId].rounds.at(-1).id;
  const workspace=before.executions[before.tasks[taskId].executionId].workspace;
  const gitProof=()=>[['rev-parse','--verify','HEAD'],['status','--porcelain']].map(args=>{const r=spawnSync('git',['-C',workspace,...args],{encoding:'utf8',timeout:5000});if(r.error)throw r.error;return {code:r.status,output:r.stdout}});
  const priorGit=gitProof();
  await close(page);
  // Inspect the real menu/dialog at the first card on desktop and phone.
  for(const width of [1440,390]){
    await page.setViewportSize({width,height:844});
    await card.getByRole('button',{name:'任务操作',exact:true}).click();
    const menu=page.getByRole('menu',{name:'任务操作',exact:true});await expect(menu).toBeVisible();
    const b=(await menu.boundingBox())!;expect(b.x).toBeGreaterThanOrEqual(0);expect(b.y).toBeGreaterThanOrEqual(0);expect(b.x+b.width).toBeLessThanOrEqual(width);expect(b.y+b.height).toBeLessThanOrEqual(844);
    await shot(page,'completion-menu-'+width);
    await menu.getByRole('menuitem',{name:'标记任务完成',exact:true}).click();
    const confirm=page.getByRole('dialog',{name:'标记任务完成',exact:true});await expect(confirm).toBeVisible();
    expect.soft(await confirm.evaluate(el=>getComputedStyle(el).backgroundColor)).toBe('rgb(44, 49, 56)');
    await shot(page,'completion-dialog-'+width);
    await confirm.getByRole('button',{name:'取消',exact:true}).click();
    expect((await snapshot()).tasks[taskId].status).toBe('needs_confirmation');
  }
  await page.setViewportSize({width:1440,height:900});
  await card.getByRole('button', { name: '任务操作', exact: true }).click();
  await page.getByRole('menuitem', { name: '标记任务完成', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: '标记任务完成', exact: true });
  await expect(dialog).toContainText('不会运行测试、提交、合并、推送或部署');
  await dialog.getByRole('button', { name: '取消', exact: true }).click();
  expect((await snapshot()).tasks[taskId].status).toBe('needs_confirmation');
  await card.getByRole('button', { name: '任务操作', exact: true }).click();
  await page.getByRole('menuitem', { name: '标记任务完成', exact: true }).click();
  await dialog.getByRole('button', { name: '标记完成', exact: true }).click();
  await expect(card.locator('.task-status')).toHaveText('已完成');
  await expect(drawer(page)).not.toBeVisible();
  const after = await snapshot();
  expect(after.tasks[secondId].status).toBe('idle');
  expect(after.conversations[after.tasks[taskId].conversationId].rounds.at(-1).id).toBe(priorRound);
  expect(after.conversations[after.tasks[taskId].conversationId].rounds.at(-1).phase).toBe('confirmed');
  expect(gitProof()).toEqual(priorGit);
  await card.locator('.task-select').click();
  await page.locator('#draft').fill('结构化登录恢复：第二轮改进');
  await page.locator('#send').click();
  await expect(page.locator('.assistant-summary-markdown')).toHaveCount(2);
  const history = page.locator('.assistant-turn-container.historical');
  await expect(history.locator('.assistant-summary-markdown')).toBeVisible();
  await expect(history).not.toContainText('上一轮结果');
  await expect(page.locator('.message.assistant .message-status')).toHaveCount(0);
  await shot(page, 'cold-readable-history');
});

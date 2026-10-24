import { test, expect } from "@playwright/test";
import type { Page, Cookie } from "@playwright/test";
import { mkdtempSync, rmSync, writeFileSync, readFileSync, realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";

const TOKEN = "kaiwu-chat-turn-test-token-00000000";

const createdDirs: string[] = [];

test.afterAll(() => {
  console.log('[fixture-auth]', JSON.stringify({ successfulLogins }));
  for (const dir of createdDirs) {
    try {
      rmSync(dir, { recursive: true, force: true });
    } catch {}
  }
});

let loginAttempt = 0, successfulLogins = 0;
// This isolated worker reuses only its own in-memory fixture cookies, never
// browser storage/drafts. Explicit logout still revokes them server-side.
let loginCookies: Cookie[] = [];
async function login(page: Page, { fresh = false } = {}) {
  const attempt = ++loginAttempt;
  const observe = (response: any) => {
    if (new URL(response.url()).pathname === '/api/login' && response.ok()) successfulLogins++;
    if (new URL(response.url()).pathname === '/api/snapshot') console.log('[auth-ui]', JSON.stringify({ attempt, event: 'snapshot', wall: Date.now(), status: response.status() }));
  };
  page.on('response', observe);
  try {
    if (fresh) await page.context().clearCookies();
    else if (loginCookies.length) await page.context().addCookies(loginCookies);
    await page.goto("/");
    const token = page.getByLabel("本机访问令牌");
    const currentProject = page.getByRole('button', { name: '当前项目', exact: true });
    // An immediate isVisible() can mistake the initial loading screen for an
    // authenticated session. Await one of the real ready states, same timeout.
    await expect(token.or(currentProject)).toBeVisible();
    const visible = await token.isVisible().catch(() => false);
    console.log('[auth-ui]', JSON.stringify({ attempt, event: 'initial-visibility', wall: Date.now(), tokenVisible: visible }));
    if (visible) {
      await token.fill(TOKEN);
      await page.getByRole("button", { name: "进入工作台", exact: true }).click();
    }
    await expect(currentProject).toBeVisible();
    loginCookies = await page.context().cookies('http://127.0.0.1:17445');
  } catch (error) {
    console.log('[auth-ui]', JSON.stringify({ attempt, event: 'failure', wall: Date.now(), tokenVisible: await page.getByLabel('本机访问令牌').isVisible(), projectVisible: await page.getByRole('button', { name: '当前项目', exact: true }).isVisible() }));
    throw error;
  } finally { page.off('response', observe); }
}

async function addProject(page: Page, name: string): Promise<void> {
  const welcome = page.getByRole("button", { name: "＋ 添加项目", exact: true });
  if (await welcome.isVisible().catch(() => false)) {
    await welcome.click();
  } else {
    await page.getByRole("button", { name: "当前项目", exact: true }).click();
    await page.getByRole("menuitem", { name: "添加项目", exact: true }).click();
  }
  const dir = mkdtempSync(join(tmpdir(), "kaiwu-chat-turn-e2e-"));
  createdDirs.push(dir);
  execFileSync("git", ["init", dir], { stdio: "ignore" });
  await page.locator("input[name=name]").fill(name);
  await page.locator("input[name=path]").fill(dir);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText("项目对话");
}

async function newTask(page: Page, project: string, topic: string, title: string, request = "任务对话流") {
  await addProject(page, project);
  await page.locator("[data-action=new-topic]").click();
  await page.locator("input[name=name]").fill(topic);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".column h2").first()).toHaveText(topic);
  await page.getByRole("button", { name: `在 ${topic} 新建任务`, exact: true }).click();
  await page.locator("input[name=title]").fill(title);
  await page.locator("textarea[name=request]").fill(request);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText(title);
}


for (const delayMs of [0, 250, 400]) {
  test(`Fixture login UI-ready: actual unauthenticated snapshot delayed ${delayMs}ms`, async ({ page }) => {
    let held = false;
    await page.route('**/api/snapshot', async route => {
      const response = await route.fetch();
      if (!held && response.status() === 401) {
        held = true;
        await new Promise(resolve => setTimeout(resolve, delayMs));
      }
      await route.fulfill({ response });
    });
    await login(page, { fresh: true });
    expect(held).toBe(true);
    await expect(page.getByRole('button', { name: '当前项目', exact: true })).toBeVisible();
  });
}

test("Task3b: reviewed local merge acknowledges the finished round without a prior completion click", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const probe = await routePreviewSocket(page);
  await login(page);
  await newTask(page, "ReviewedMergeProject", "MergeTopic", "ReviewedMergeTask");
  const card = page.locator(".task-card").first();
  await card.getByRole("button", { name: "开始开发", exact: true }).click();
  await expect(card).toContainText("待验收", { timeout: 15000 });
  await page.locator("#draft").fill("保留合并期间的草稿");
  const taskId = (await card.getAttribute("data-taskid"))!;
  const before = (await (await page.request.get("/api/snapshot")).json()).state;
  const task = before.tasks[taskId];
  const round = before.conversations[task.conversationId].rounds.at(-1);
  expect(round.phase).toBe("finished");
  await card.getByRole("button", { name: "合并到主分支", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect.poll(() => probe.calls.filter(c => c.method === "git/preview").map(c => c.body.mode)).toContain("local");
  const local = dialog.locator('[data-merge-mode="local"]');
  await expect(local).toBeEnabled();
  // No remote is configured in this real temporary repository: local stays usable.
  await expect(dialog).toContainText("远程");
  expect(await dialog.evaluate(el => el.contains(document.activeElement))).toBe(true);
  await dialog.getByLabel('提交说明').fill('键盘审核本地合并');
  await dialog.getByLabel('提交说明').press('Space');
  await expect(dialog.getByLabel('提交说明')).toHaveValue('键盘审核本地合并 ');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  expect(probe.calls.filter(c => c.method === 'git/merge')).toHaveLength(0);
  await card.getByRole('button', { name: '合并到主分支', exact: true }).click();
  await expect(dialog).toBeVisible();
  await local.focus();
  await local.press('Enter');
  await expect(dialog).toContainText("操作完成", { timeout: 15000 });
  const writes = probe.calls.filter(c => c.method === "git/merge");
  expect(writes).toHaveLength(1);
  expect(writes[0].body.mode).toBe("local");
  expect(writes[0].body.roundId).toBe(round.id);
  expect(probe.calls.some(c => c.body?.type === "confirm_result")).toBe(false);
  const after = (await (await page.request.get("/api/snapshot")).json()).state;
  expect(after.conversations[task.conversationId].rounds.at(-1).phase).toBe("confirmed");
  expect(after.conversations[task.conversationId].rounds.at(-1).report?.checks).toEqual(round.report?.checks);
  await dialog.getByRole("button", { name: "取消", exact: true }).click();
  await expect(page.locator("#draft")).toHaveValue("保留合并期间的草稿");
});

test("Task3b files: explicit files-only downloads preserve Unicode names, bytes and drafts without Start", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const probe = await routePreviewSocket(page);
  await login(page);
  await newTask(page, "FileArtifactsProject", "FilesTopic", "FileArtifactsTask");
  const card = page.locator(".task-card").first();
  await card.getByRole("button", { name: "开始开发", exact: true }).click();
  await expect(card).toContainText("待验收", { timeout: 15000 });
  const snapshot = (await (await page.request.get("/api/snapshot")).json()).state;
  const task = snapshot.tasks[(await card.getAttribute("data-taskid"))!];
  const tree = snapshot.executions[task.executionId].workspace;
  expect(tree).toMatch(/^\/tmp\/kaiwu-chat-turn-service-[^/]+\/data\/worktrees\/.+\/tree$/);
  expect(realpathSync(tree)).toBe(tree);
  const pdf = Buffer.from("%PDF-1.7\n中文 文件下载测试\n", "utf8");
  const html = '<script>window.parent.__ARTIFACT_EXECUTED__ = true</script>';
  writeFileSync(join(tree, "report.pdf"), pdf);
  writeFileSync(join(tree, "attachment.html"), html);
  writeFileSync(join(tree, "kaiwu-preview.json"), JSON.stringify({ version: 2, kind: "files", artifacts: [
    { path: "report.pdf", name: "总结 报告.pdf" }, { path: "attachment.html", name: "只下载 不执行.html" },
  ] }));
  await page.locator("#draft").fill("下载前保留的非空草稿");
  await page.locator(".turn-result-actions button").filter({ hasText: /查看结果|打开网页/ }).first().click();
  const region = page.getByRole("region", { name: "任务成果与效果" });
  await expect(region).toBeVisible();
  await expect(region.locator("iframe")).toHaveCount(0);
  const pdfLink = region.getByRole("link", { name: "下载 总结 报告.pdf", exact: true });
  await expect(pdfLink).toBeVisible();
  await pdfLink.focus();
  await expect(pdfLink).toBeFocused();
  const [pdfDownload] = await Promise.all([page.waitForEvent("download"), pdfLink.press('Enter')]);
  expect(pdfDownload.suggestedFilename()).toBe("总结 报告.pdf");
  expect(readFileSync((await pdfDownload.path())!)).toEqual(pdf);
  const [htmlDownload] = await Promise.all([page.waitForEvent("download"), region.getByRole("link", { name: "下载 只下载 不执行.html", exact: true }).click()]);
  expect(htmlDownload.suggestedFilename()).toBe("只下载 不执行.html");
  expect(readFileSync((await htmlDownload.path())!).toString()).toBe(html);
  expect(await page.evaluate(() => (window as any).__ARTIFACT_EXECUTED__)).toBeUndefined();
  expect(page.url()).not.toContain("/preview/");
  expect(probe.startCount()).toBe(0);
  await page.screenshot({ path: '/home/jimxl/.local/state/agent-artifacts/kaiwu/2026-10-24-task-result-actions/task3b-files-desktop.png' });
  for (const width of [360, 390]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(pdfLink).toBeVisible();
    await expect(region.getByRole('button', { name: '返回任务对话', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false);
    await page.screenshot({ path: `/home/jimxl/.local/state/agent-artifacts/kaiwu/2026-10-24-task-result-actions/task3b-files-${width}.png` });
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  // Desktop already keeps chat beside the pane; its chat-back button is mobile-only.
  await region.getByRole("button", { name: "← 返回看板", exact: true }).click();
  await expect(page.locator("#draft")).toHaveValue("下载前保留的非空草稿");
});

for (const pushOnly of [false, true]) {
  test(`Task3b remote UI (mock transport): ${pushOnly ? 'server-proven push only' : 'separate merge and push with local success retained'}`, async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const probe = await routePreviewSocket(page, { pushOnly, phase: pushOnly ? 'needs_check' : 'failed' });
    await login(page);
    await newTask(page, `RemoteUI${pushOnly}`, "RemoteTopic", "RemoteChoiceTask");
    const card = page.locator('.task-card').first();
    await card.getByRole('button', { name: '开始开发', exact: true }).click();
    await expect(card).toContainText('待验收', { timeout: 15000 });
    await card.getByRole('button', { name: '合并到主分支', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toContainText('https://github.com/example/task3b.git');
    await expect(dialog).toContainText('受控远程提交范围');
    await expect(dialog).toContainText('可能触发远程构建／部署');
    const push = dialog.locator('[data-merge-mode="push"]');
    await expect(push).toHaveText(pushOnly ? '推送到远程' : '合并并推送');
    if (pushOnly) await expect(dialog.locator('[data-merge-mode="local"]')).toHaveCount(0);
    else {
      await expect(dialog.locator('[data-merge-mode="local"]')).toBeEnabled();
      await page.screenshot({ path: '/home/jimxl/.local/state/agent-artifacts/kaiwu/2026-10-24-task-result-actions/task3b-merge-review-desktop.png' });
      for (const width of [360, 390]) {
        await page.setViewportSize({ width, height: 800 });
        await push.scrollIntoViewIfNeeded();
        await expect(push).toBeInViewport();
        expect(await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth)).toBe(false);
        await page.screenshot({ path: `/home/jimxl/.local/state/agent-artifacts/kaiwu/2026-10-24-task-result-actions/task3b-merge-review-${width}.png` });
      }
      await page.setViewportSize({ width: 1440, height: 900 });
    }
    await push.click();
    await expect(dialog).toContainText('CI 未检查');
    const writes = probe.calls.filter(c => c.method === 'git/merge');
    expect(writes).toHaveLength(1);
    expect(writes[0].body.mode).toBe('push');
    expect(probe.reviewIDs().push).toBeTruthy();
    expect(probe.reviewIDs().push).not.toBe(probe.reviewIDs().local);
    expect(writes[0].body.previewId).toBe(probe.reviewIDs().push);
    const review = probe.calls.find(c => c.method === 'git/preview' && c.body.mode === 'push')!;
    expect(writes[0].body.roundId).toBe(review.body.roundId);
    expect(writes[0].body).not.toHaveProperty('remoteUrl');
    expect(writes[0].body).not.toHaveProperty('sha');
    if (pushOnly) {
      await expect(dialog).toContainText('结果需要核查');
      const reads = probe.calls.filter(c => c.method.startsWith('git/operations/')).length;
      await dialog.getByRole('button', { name: '只读核查', exact: true }).click();
      await expect.poll(() => probe.calls.filter(c => c.method.startsWith('git/operations/')).length).toBe(reads + 1);
      expect(probe.calls.filter(c => c.method === 'git/merge')).toHaveLength(1);
      await expect(dialog.locator('[data-merge-mode]')).toHaveCount(0);
    } else {
      await expect(dialog).toContainText('本地合并：主分支已合并');
      await expect(dialog).toContainText('远程上传：受控测试：远程拒绝上传');
    }
  });
}

test('Task3b sensitive remote review UI (mock transport): blocked history is visible and never offers push', async ({ page }) => {
  const probe = await routePreviewSocket(page, { pushOnly: false, phase: 'failed', blocked: true });
  await login(page);
  await newTask(page, 'SensitiveHistoryUI', 'RemoteTopic', 'SensitiveHistoryTask');
  const card = page.locator('.task-card').first();
  await card.getByRole('button', { name: '开始开发', exact: true }).click();
  await expect(card).toContainText('待验收', { timeout: 15000 });
  await card.getByRole('button', { name: '合并到主分支', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('note')).toContainText('待上传提交包含需先处理的私密文件或不安全路径');
  await expect(dialog.locator('[data-merge-mode="push"]')).toHaveCount(0);
  await expect(dialog.locator('[data-merge-mode="local"]')).toBeEnabled();
  expect(probe.calls.filter(c => c.method === 'git/merge')).toHaveLength(0);
});

test('Task3b unknown local receipt: a real admitted write is looked up, never re-sent', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const probe = await routePreviewSocket(page);
  await login(page);
  await newTask(page, 'LostMergeReceiptProject', 'MergeTopic', 'LostMergeReceiptTask');
  await page.locator('#draft').fill('未知回执期间保留草稿');
  const card = page.locator('.task-card').first();
  await card.getByRole('button', { name: '开始开发', exact: true }).click();
  await expect(card).toContainText('待验收', { timeout: 15000 });
  await card.getByRole('button', { name: '合并到主分支', exact: true }).click();
  const dialog = page.getByRole('dialog');
  probe.drop('git/merge');
  await dialog.locator('[data-merge-mode="local"]').click();
  await expect(dialog).toContainText('操作回执未知');
  await expect(dialog.locator('[data-merge-mode="local"]')).toBeDisabled();
  // Wait for the actual socket reconnection, not a forced click on an offline UI.
  await expect(page.locator('#send')).toBeEnabled({ timeout: 10000 });
  await dialog.getByRole('button', { name: '重新检查', exact: true }).click();
  await expect(dialog).toContainText('操作完成', { timeout: 15000 });
  const writes = probe.calls.filter(c => c.method === 'git/merge');
  expect(writes).toHaveLength(1);
  expect(probe.calls.some(c => c.method === 'git/operations/' + writes[0].body.operationId)).toBe(true);
  await dialog.getByRole('button', { name: '取消', exact: true }).click();
  await expect(page.locator('#draft')).toHaveValue('未知回执期间保留草稿');
});

test('Task3b static files: direct webpage coexists with explicit downloads and read-only polling', async ({ page, context }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const probe = await routePreviewSocket(page);
  await login(page);
  await newTask(page, 'StaticAndFilesProject', 'OutputTopic', 'StaticAndFilesTask');
  const card = page.locator('.task-card').first();
  await card.getByRole('button', { name: '开始开发', exact: true }).click();
  await expect(card).toContainText('待验收', { timeout: 15000 });
  const snapshot = (await (await page.request.get('/api/snapshot')).json()).state;
  const task = snapshot.tasks[(await card.getAttribute('data-taskid'))!];
  const tree = snapshot.executions[task.executionId].workspace;
  expect(tree).toMatch(/^\/tmp\/kaiwu-chat-turn-service-[^/]+\/data\/worktrees\/.+\/tree$/);
  expect(realpathSync(tree)).toBe(tree);
  writeFileSync(join(tree, 'index.html'), '<h1>本轮网页与附件</h1>');
  writeFileSync(join(tree, 'result.csv'), 'name,value\n中文,42\n');
  writeFileSync(join(tree, 'kaiwu-preview.json'), JSON.stringify({ version: 2, kind: 'static', root: '.', entry: 'index.html', artifacts: [{ path: 'result.csv', name: '数据 结果.csv' }] }));
  await page.locator('#draft').fill('静态网页与附件同时保留草稿');
  const [web] = await Promise.all([context.waitForEvent('page'), page.locator('.turn-result-actions button').filter({ hasText: /查看结果|打开网页/ }).first().click()]);
  await expect(web.getByRole('heading', { name: '本轮网页与附件', exact: true })).toBeVisible();
  await web.close();
  await page.bringToFront();
  await page.getByRole('button', { name: '文件产物（1）', exact: true }).click();
  const link = page.getByRole('link', { name: '下载 数据 结果.csv', exact: true });
  await expect(link).toBeVisible();
  const [download] = await Promise.all([page.waitForEvent('download'), link.click()]);
  expect(download.suggestedFilename()).toBe('数据 结果.csv');
  expect(readFileSync((await download.path())!).toString()).toBe('name,value\n中文,42\n');
  const opens = probe.openCount(), statuses = probe.statusCount();
  await expect.poll(() => probe.statusCount(), { timeout: 6000 }).toBeGreaterThan(statuses);
  expect(probe.openCount()).toBe(opens);
  await expect(link).toBeVisible();
  expect(probe.startCount()).toBe(0);
  await expect(page.locator('#draft')).toHaveValue('静态网页与附件同时保留草稿');
});

// Real WebSocket-backed preview probe: requests are executed by the actual Go
// service; only the reply packet is held or lost, so we exercise real request
// ids and real late responses instead of faking API resolution in the page.
type PreviewSocketProbe = {
  calls: Array<{ method: string; body: any }>;
  hold(method: string): void;
  release(): void;
  drop(method: string): void;
  failNext(method: string, status: number): void;
  openCount(): number;
  seenStates(): string[];
  statusCount(): number;
  startCount(): number;
  reviewIDs(): {local?: string; push?: string};
  operationIds(): string[];
};
async function routePreviewSocket(page: Page, mergeUI?: { pushOnly: boolean; phase: 'failed' | 'needs_check'; blocked?: boolean }): Promise<PreviewSocketProbe> {
  const held: Array<() => void> = [];
  const calls: Array<{ method: string; body: any }> = [];
  const state = { holdMethod: "", dropMethod: "", failMethod: "", failStatus: 0 };
  const seen: string[] = [];
  let pushReview: any;
  const reviewIDs: {local?: string; push?: string} = {};
  await page.routeWebSocket("**/api/socket", client => {
    const server = client.connectToServer();
    const methods = new Map<string, string>();
    client.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      if (frame.method) {
        calls.push({ method: frame.method, body: frame.body });
        methods.set(frame.id, frame.method);
      }
      if (mergeUI && frame.method === 'git/merge') {
        // UI-only remote transport fixture: never forward a push to the real
        // service. Local admission/push correctness is tested separately.
        const b = frame.body;
        client.send(JSON.stringify({ type: 'reply', id: frame.id, status: 200, data: {
          id: b.operationId, projectId: b.projectId, taskId: b.taskId,
          mode: b.mode, roundId: b.roundId, preview: pushReview,
          phase: mergeUI.phase, startedAt: new Date().toISOString(),
          mainAfterSHA: 'd'.repeat(40), taskAfterSHA: pushReview.taskSHA,
          intendedSHA: 'd'.repeat(40), pushAttemptedAt: new Date().toISOString(),
          ciStatus: 'not_checked', error: mergeUI.phase === 'failed' ? '受控测试：远程拒绝上传' : '受控测试：远程回执未知',
        } }));
        return;
      }
      server.send(raw);
    });
    server.onMessage(raw => {
      const frame = JSON.parse(String(raw));
      const method = methods.get(frame.id) ?? "";
      if (frame.type === 'reply' && method === 'git/preview' && ['local','push'].includes(frame.data?.mode)) reviewIDs[frame.data.mode as 'local'|'push'] = frame.data.id;
      if (mergeUI && frame.type === 'reply' && method === 'git/preview' && frame.data?.mode === 'push') {
        // Retain the real local proof/id/round; replace only unsupported remote
        // facts, explicitly a UI fixture, not evidence of a real GitHub push.
        pushReview = { ...frame.data, remoteName: 'origin', remoteUrl: 'https://github.com/example/task3b.git', mainRef: 'refs/heads/main', observedRemoteSHA: 'c'.repeat(40), uploads: [{ sha: 'd'.repeat(40), subject: '受控远程提交范围' }], canPush: !mergeUI.blocked, pushReason: mergeUI.blocked ? '待上传提交包含需先处理的私密文件或不安全路径' : '', pushOnly: mergeUI.pushOnly, alreadyMerged: mergeUI.pushOnly };
        client.send(JSON.stringify({ ...frame, data: pushReview }));
        return;
      }
      if (frame.type === "reply" && method === "previews/status") seen.push(JSON.stringify({st: frame.data?.state, k: frame.data?.kind, u: !!frame.data?.url, m: !!frame.data?.manifestId, c: !!frame.data?.command?.length}));
      if (frame.type === "reply" && state.holdMethod && method === state.holdMethod) {
        held.push(() => { try { client.send(String(raw)); } catch {} });
        return;
      }
      if (frame.type === "reply" && state.failMethod && method === state.failMethod) {
        // The real request already ran on the server; only the receipt the page
        // sees is replaced by a 5xx, i.e. an error that may follow the side effect.
        state.failMethod = "";
        client.send(JSON.stringify({ ...frame, status: state.failStatus, data: undefined, error: { code: "fixture_after_side_effect", message: "fixture 5xx after side effect" } }));
        return;
      }
      if (frame.type === "reply" && state.dropMethod && method === state.dropMethod) {
        state.dropMethod = "";
        void client.close({ code: 1012, reason: "fixture response lost" });
        return;
      }
      client.send(String(raw));
    });
  });
  return {
    calls,
    hold(method) { state.holdMethod = method; },
    release() { state.holdMethod = ""; for (const send of held.splice(0)) send(); },
    drop(method) { state.dropMethod = method; },
    failNext(method, status) { state.failMethod = method; state.failStatus = status; },
    openCount() { return calls.filter(c => c.method === "previews/open").length; },
    seenStates() { return [...seen]; },
    statusCount() { return calls.filter(c => c.method === "previews/status").length; },
    operationIds() { return calls.filter(c => c.method === "previews/start").map(c => String(c.body?.operationId ?? "")); },
    startCount() { return calls.filter(c => c.method === "previews/start").length; },
    reviewIDs() { return {...reviewIDs}; },
  };
}

test.describe("Chat turn and result actions", () => {
  test("Desktop 1440px: No pinned top overview, multiple progress+final merged to 1 bubble, loading to settled, keyboard details, question filter & locate, and draft isolation", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page);
    await newTask(page, "ChatTurnProj", "DevTopic", "BubbleTask1", "任务要求");

    // 1. Verify pinned top ProgressCard overview is removed
    await expect(page.locator(".progress-panel .progress-card")).toHaveCount(0);

    // 2. Click "开始开发" so task execution and worktree are started
    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    // Send prompt with structured login recovery
    await page.locator("#draft").fill("结构化登录恢复 开发测试");
    await page.locator("#send").click();

    // The user message appears
    await expect(page.locator(".message.user").last()).toContainText("结构化登录恢复 开发测试");

    // Wait for assistant reply bubble to finish streaming and settle
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 15000 });
    const assistantMsg = page.locator(".message.assistant").last();
    // Wait for the final report text to arrive
    await expect(assistantMsg).toContainText("登录恢复已实现", { timeout: 15000 });

    const details = assistantMsg.locator("details.assistant-turn-details");
    const summary = assistantMsg.locator("summary.assistant-turn-summary");

    // 3. Native details is closed by default
    expect(await details.getAttribute("open")).toBeNull();

    // 4. Keyboard accessibility: Focus summary and press Enter to toggle open
    await summary.focus();
    await page.keyboard.press("Enter");
    expect(await details.getAttribute("open")).not.toBeNull();
    await expect(assistantMsg.locator(".assistant-turn-body")).toBeVisible();

    // Verify structured plan, results, and checks exist in body
    await expect(assistantMsg.locator(".plan-list")).toBeVisible();
    await expect(assistantMsg.locator(".feature-row").first()).toContainText("刷新自动恢复");
    await expect(assistantMsg.locator(".check-row").first()).toContainText("登录恢复检查");

    // Press Space to toggle close
    await summary.focus();
    await page.keyboard.press("Space");
    expect(await details.getAttribute("open")).toBeNull();

    // Press Space to toggle open again
    await page.keyboard.press("Space");
    expect(await details.getAttribute("open")).not.toBeNull();

    // Nested tool records details: test Space toggling
    const toolDetails = assistantMsg.locator("details.tools-details");
    if (await toolDetails.count() > 0) {
      const toolSummary = toolDetails.locator("> summary");
      await toolSummary.focus();
      await page.keyboard.press("Space");
      expect(await toolDetails.getAttribute("open")).not.toBeNull();
      await page.keyboard.press("Space");
      expect(await toolDetails.getAttribute("open")).toBeNull();
    }

    // Toggle main details closed again for clean state
    await summary.focus();
    await page.keyboard.press("Space");
    expect(await details.getAttribute("open")).toBeNull();

    // 5. Send second user message
    await page.locator("#draft").fill("第二问：请继续补充测试");
    await page.locator("#send").click();

    // Wait for second round/assistant message
    await expect(page.locator(".message.user")).toHaveCount(3);
    await expect(page.locator(".message.assistant")).toHaveCount(3, { timeout: 15000 });

    // 6. Test Display Filter: '只看我的问题'
    await page.getByRole("button", { name: "只看我的问题" }).click();
    // Only user messages should be displayed
    await expect(page.locator(".message.user")).toHaveCount(3);
    await expect(page.locator(".message.assistant")).toHaveCount(0);

    // Test Space key on question to restore all and jump
    const firstQ = page.locator(".message.user").first();
    await firstQ.focus();
    await page.keyboard.press("Space");
    // Should switch back to 'all' automatically
    await expect(page.locator(".message.assistant").first()).toBeVisible();
    await expect(page.getByRole("button", { name: "全部" })).toHaveClass(/active/);

    // Switch back to questions filter
    await page.getByRole("button", { name: "只看我的问题" }).click();
    await expect(page.locator(".message.assistant")).toHaveCount(0);

    // Test Enter key on question to restore all and jump
    const lastQ = page.locator(".message.user").last();
    await lastQ.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".message.assistant").first()).toBeVisible();
    await expect(page.getByRole("button", { name: "全部" })).toHaveClass(/active/);

    // 7. Test preview action button before confirmation
    const resultActionRow = page.locator(".turn-result-actions");
    await expect(resultActionRow).toBeVisible();
    const previewBtn = resultActionRow.locator("button:has-text('查看效果'), button:has-text('查看结果'), button:has-text('打开网页')");
    await expect(previewBtn).toBeVisible();
    await previewBtn.click();
    // Verify draft is preserved and not cleared
    expect(await page.locator("#draft").inputValue()).toBe("");

    // 8. Test Space on Result Actions button ("确认完成")
    const confirmBtn = page.locator(".turn-result-actions button.confirm-btn");
    await expect(confirmBtn).toBeVisible();
    await confirmBtn.focus();
    await page.keyboard.press("Space");

    // Return to board from preview surface to check task card
    const backBtn = page.getByRole("button", { name: "← 返回看板", exact: true });
    if (await backBtn.isVisible().catch(() => false)) {
      await backBtn.click();
    }
    // Task should be marked completed
    await expect(page.locator(".task-card").first()).toContainText("已完成");

    // 9. Verify draft isolation across tasks
    await page.locator("#draft").fill("草稿保留在当前任务1");
    // Create second task in same topic
    await page.getByRole("button", { name: "在 DevTopic 新建任务", exact: true }).click();
    await page.locator("input[name=title]").fill("BubbleTask2");
    await page.locator("textarea[name=request]").fill("第二任务");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.locator("#chat-title")).toContainText("BubbleTask2");

    // Task2 draft must be empty
    expect(await page.locator("#draft").inputValue()).toBe("");

    // Switch back to Task1
    await page.locator(".task-card").filter({ hasText: "BubbleTask1" }).click();
    await expect(page.locator("#chat-title")).toContainText("BubbleTask1");
    // Draft in Task1 is preserved
    expect(await page.locator("#draft").inputValue()).toBe("草稿保留在当前任务1");
    await page.locator("#draft").fill(""); // clean up
  });

  test("Real static preview regression: opens in new tab, loads real HTML/CSS/JS/image, with popup blocker fallback and approval lifecycle", async ({ page, context }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page);
    await newTask(page, "StaticPreviewProj", "WebTopic", "StaticWebTask");

    // 1. Click "开始开发" so task execution and independent worktree are initialized with development tools
    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    // 2. Trigger development with prompt producing static web artifacts
    await page.locator("#draft").fill("生成静态网页产物 测试入口");
    await page.locator("#send").click();

    // Wait for development tool and assistant reply
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });
    const assistantMsg = page.locator(".message.assistant").last();
    await expect(assistantMsg).toBeVisible();

    // 3. Wait for preview button to reflect web presence or result
    const resultActionRow = page.locator(".turn-result-actions");
    await expect(resultActionRow).toBeVisible({ timeout: 15000 });
    const actionBtn = resultActionRow.locator("button:has-text('打开网页'), button:has-text('查看结果'), button:has-text('查看效果')");
    await expect(actionBtn).toBeVisible();

    // Verify popup listener handles new tab or fallback link
    const [newPage] = await Promise.all([
      context.waitForEvent("page", { timeout: 10000 }),
      actionBtn.click(),
    ]);

    await newPage.waitForLoadState("networkidle");
    expect(await newPage.title()).toContain("静态预览网页");
    await expect(newPage.locator("#static-title")).toHaveText("开悟静态网页测试");
    // Verify style
    const color = await newPage.locator("#static-title").evaluate(el => window.getComputedStyle(el).color);
    expect(color).toBe("rgb(24, 112, 60)");
    // Verify JS executed
    const jsReady = await newPage.locator("body").getAttribute("data-js-ready");
    expect(jsReady).toBe("true");
    // Verify PNG image loaded with naturalWidth > 0
    const imgLoaded = await newPage.locator("#static-img").evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0);
    expect(imgLoaded).toBe(true);
    await newPage.close();
  });

  test("F1 regression: popup blocker fallback shows accessible direct link", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page);
    await newTask(page, "BlockedPopupProj", "WebTopic", "BlockedWebTask");

    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    await page.locator("#draft").fill("生成静态网页产物 弹窗拦截测试");
    await page.locator("#send").click();

    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });
    const actionBtn = page.locator(".turn-result-actions").locator("button:has-text('打开网页'), button:has-text('查看结果'), button:has-text('查看效果')");
    await expect(actionBtn).toBeVisible();

    // Mock popup blocker by setting window.open to return null
    await page.evaluate(() => {
      window.open = () => null;
    });

    await actionBtn.click();

    // Expect fallback notice and accessible link to be rendered
    const directLink = page.locator("a.preview-direct-link");
    await expect(directLink).toBeVisible({ timeout: 5000 });
    expect(await directLink.getAttribute("target")).toBe("_blank");
    expect(await directLink.getAttribute("rel")).toContain("noopener");
    expect(await directLink.getAttribute("rel")).toContain("noreferrer");
    const href = await directLink.getAttribute("href");
    expect(href).toMatch(/\/preview\/[0-9a-f]{64}\//);

    // The fallback must really work from the keyboard, not just carry attributes.
    const [linkPage] = await Promise.all([
      page.context().waitForEvent("page", { timeout: 15000 }),
      (async () => {
        await directLink.focus();
        await expect(directLink).toBeFocused();
        await page.keyboard.press("Enter");
      })(),
    ]);
    await linkPage.waitForLoadState("load");
    expect(linkPage.url()).toMatch(/\/preview\/[0-9a-f]{64}\//);
    expect(await linkPage.evaluate(() => window.opener === null)).toBe(true);
    await linkPage.close();
  });

  test("F2 regression: new round during in-flight preview open invalidates old popup navigation", async ({ page, context }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const probe = await routePreviewSocket(page);
    await login(page);
    await newTask(page, "StaleRoundProj", "WebTopic", "StaleRoundTask");

    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    await page.locator("#draft").fill("生成静态网页产物 轮次一");
    await page.locator("#send").click();
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });

    const actionBtn = page.locator(".turn-result-actions").locator("button:has-text('打开网页'), button:has-text('查看结果')").first();
    await expect(actionBtn).toBeVisible();

    probe.hold("previews/open");
    const popupPromise = context.waitForEvent("page", { timeout: 10000 });
    await actionBtn.click();
    const popup = await popupPromise;
    await expect.poll(() => probe.openCount()).toBe(1);

    // A new round lands in the same task while previews/open is still pending.
    await page.locator("#draft").fill("继续迭代新轮次");
    await page.locator("#send").click();
    await expect(page.locator(".message.assistant")).toHaveCount(3, { timeout: 20000 });

    probe.release();
    await page.waitForTimeout(1200);

    // The late receipt must never navigate the reserved blank tab.
    expect(popup.url()).not.toMatch(/\/preview\/[0-9a-f]{64}\//);
    expect(popup.isClosed()).toBe(true);
  });

  test("Preview gesture fixture: trusted DOM events cover in-flight and completed window", async ({ page, context }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.addInitScript(() => {
      (window as any).__previewClicks = [];
      document.addEventListener('click', event => {
        const button = (event.target as Element)?.closest('.turn-result-actions button') as HTMLButtonElement | null;
        if (button && /打开网页|查看结果/.test(button.textContent || '')) (window as any).__previewClicks.push({ wall: performance.timeOrigin + event.timeStamp, trusted: event.isTrusted, enabled: !button.disabled });
      }, true);
    });
    const probe = await routePreviewSocket(page);
    await login(page);
    await newTask(page, "DebounceProj", "WebTopic", "DebounceTask");

    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    await page.locator("#draft").fill("生成静态网页产物 防重测试");
    await page.locator("#send").click();
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });

    const actionBtn = page.locator(".turn-result-actions").locator("button:has-text('打开网页'), button:has-text('查看结果')").first();
    await expect(actionBtn).toBeVisible();

    const popups: Page[] = [];
    context.on("page", p => popups.push(p));

    // Measure real trusted DOM activation, never locator-call invocation time.
    // Resolve coordinates before timing; bringing the parent front is explicit.
    const box = (await actionBtn.boundingBox())!;
    expect(box).toBeTruthy();
    const point = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
    probe.hold("previews/open");
    await actionBtn.click();
    await expect.poll(() => probe.openCount()).toBe(1);
    await page.bringToFront();
    await page.mouse.click(point.x, point.y);
    probe.release();
    await expect.poll(() => popups.length).toBe(1);
    const firstPage = popups[0];
    await firstPage.waitForURL(/\/preview\/[0-9a-f]{64}\//);
    await firstPage.waitForLoadState('load');
    await page.bringToFront();
    expect(await page.evaluate(p => !!document.elementFromPoint(p.x, p.y)?.closest('.turn-result-actions button'), point)).toBe(true);
    const ready = Date.now();
    await page.waitForTimeout(120);
    await page.mouse.click(point.x, point.y);
    await page.waitForTimeout(130);
    await page.mouse.click(point.x, point.y);
    const clicks = await page.evaluate(() => (window as any).__previewClicks as Array<{wall:number; trusted:boolean; enabled:boolean}>);
    expect(clicks).toHaveLength(4);
    expect(clicks.every(c => c.trusted && c.enabled)).toBe(true);
    expect(clicks[2].wall - ready).toBeGreaterThanOrEqual(110);
    expect(clicks[3].wall - ready).toBeGreaterThanOrEqual(240);
    // This stronger bound also proves both activations were within 700ms of
    // completion (completion necessarily follows the original gesture).
    expect(clicks[3].wall - clicks[0].wall).toBeLessThan(700);
    console.log('[trusted-preview-gesture]', JSON.stringify({ready, clicks}));
    await page.waitForTimeout(600);

    expect(probe.openCount()).toBe(1);
    expect(popups.length).toBe(1);
    expect(firstPage.url()).toMatch(/\/preview\/[0-9a-f]{64}\//);

    // The window must not lock the page forever: after it expires an explicit
    // open is allowed again.
    await page.waitForTimeout(900);
    await page.mouse.click(point.x, point.y);
    await expect.poll(() => probe.openCount(), { timeout: 5000 }).toBe(2);
    await expect.poll(() => popups.length, { timeout: 5000 }).toBe(2);

    for (const p of popups) {
      if (!p.isClosed()) await p.close();
    }
  });

  test("F4 regression: A→B→A switch during in-flight preview open cancels the late navigation for good", async ({ page, context }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const probe = await routePreviewSocket(page);
    await login(page);
    await newTask(page, "SwitchProj", "WebTopic", "SwitchTaskA");

    const cardA = page.locator(".task-card").first();
    await cardA.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(cardA).toContainText("待验收", { timeout: 15000 });

    await page.locator("#draft").fill("生成静态网页产物 切换卡片A");
    await page.locator("#send").click();
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });

    await page.getByRole("button", { name: "在 WebTopic 新建任务", exact: true }).click();
    await page.locator("input[name=title]").fill("SwitchTaskB");
    await page.locator("textarea[name=request]").fill("任务B");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.locator("#chat-title")).toContainText("SwitchTaskB");

    await page.locator(".task-card").filter({ hasText: "SwitchTaskA" }).click();
    await expect(page.locator("#chat-title")).toContainText("SwitchTaskA");

    const actionBtn = page.locator(".turn-result-actions").locator("button:has-text('打开网页'), button:has-text('查看结果')").first();
    await expect(actionBtn).toBeVisible();

    probe.hold("previews/open");
    const popupPromise = context.waitForEvent("page", { timeout: 10000 });
    await actionBtn.click();
    const popup = await popupPromise;
    await expect.poll(() => probe.openCount()).toBe(1);

    // A → B → A: the scope returns to the very same task/round, so a naive
    // "value is identical again" check would revive this late receipt.
    await page.locator(".task-card").filter({ hasText: "SwitchTaskB" }).click();
    await expect(page.locator("#chat-title")).toContainText("SwitchTaskB");
    await page.locator(".task-card").filter({ hasText: "SwitchTaskA" }).click();
    await expect(page.locator("#chat-title")).toContainText("SwitchTaskA");

    probe.release();
    await page.waitForTimeout(1200);

    expect(popup.url()).not.toMatch(/\/preview\/[0-9a-f]{64}\//);
    expect(popup.isClosed()).toBe(true);
  });

  test.setTimeout(150000);
  test("F5 regression: command manifest requires explicit authorization, clear on logout, view never re-executes", async ({ page, context }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    const probe = await routePreviewSocket(page);
    await login(page);
    await newTask(page, "CommandProj", "CmdTopic", "CmdTask");

    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    // Prompt produces kaiwu-preview.json command manifest
    await page.locator("#draft").fill("命令网页产物 首次授权测试");
    await page.locator("#send").click();
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });

    const actionBtn = page.locator(".turn-result-actions").locator("button:has-text('打开网页'), button:has-text('查看结果')").first();
    await expect(actionBtn).toBeVisible();

    // Clicking "查看结果" for unapproved command does NOT open popup immediately, but opens ResultPreview pane with authorization button
    await actionBtn.click();
    const resultPane = page.locator(".result-pane");
    await expect(resultPane).toBeVisible({ timeout: 5000 });
    await expect(resultPane.locator("code.preview-command")).toBeVisible();

    // User explicitly authorizes and starts preview
    const startBtn = resultPane.locator("button.preview-start-btn");
    await expect(startBtn).toBeVisible();

    const [cmdPage] = await Promise.all([
      context.waitForEvent("page", { timeout: 15000 }),
      startBtn.click(),
    ]);

    await cmdPage.waitForLoadState("networkidle");
    expect(await cmdPage.title()).toContain("服务进程网页");
    expect(await cmdPage.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
    expect(probe.startCount()).toBe(1);
    await cmdPage.close();

    // The just-completed Start gesture owns its short repeat window (same scope,
    // one preview target), so an immediate second click is intentionally deduped.
    // Wait it out, then the explicit open must reuse the approval and open again.
    await page.waitForTimeout(900);
    // Now clicking preview button in chat will reuse approval and directly open without re-prompting
    const [secondPage] = await Promise.all([
      context.waitForEvent("page", { timeout: 15000 }),
      actionBtn.click(),
    ]);
    await secondPage.waitForLoadState("networkidle");
    expect(await secondPage.title()).toContain("服务进程网页");
    expect(await secondPage.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
    // Viewing an already running preview is read-only: it must not issue another
    // Start with a fresh operation id.
    expect(probe.startCount()).toBe(1);
    await secondPage.close();

    // Explicit Stop through the visible panel ends that preview.
    const stopBtn = page.locator(".result-pane button:has-text('停止预览')");
    await expect(stopBtn).toBeVisible();
    await stopBtn.click();
    await expect(page.locator(".result-pane")).toContainText("服务已停止", { timeout: 10000 });
    await expect(page.locator(".result-pane button.preview-start-btn")).toBeEnabled({ timeout: 10000 });

    // Logout clears the execution approval.
    await page.locator("button.logout").click();
    await expect(page.getByLabel("本机访问令牌")).toBeVisible();

    // Re-login on the same stopped task: the approval is gone, so the panel must
    // ask again and must not issue a Start before the user agrees.
    await login(page);
    await page.getByRole("button", { name: "当前项目", exact: true }).click();
    await page.getByRole("menuitemradio", { name: "CommandProj" }).click();
    await page.locator(".task-card").filter({ hasText: "CmdTask" }).click();
    await expect(page.locator("#chat-title")).toContainText("CmdTask");

    const pagesBeforeAsk = context.pages().length;
    await page.locator(".turn-result-actions button:has-text('查看结果'), .turn-result-actions button:has-text('打开网页')").first().click();
    await expect(page.locator(".result-pane code.preview-command")).toBeVisible();
    await expect(page.locator("button.preview-start-btn")).toBeVisible();
    await page.waitForTimeout(800);
    expect(probe.startCount()).toBe(1);
    expect(context.pages().length).toBe(pagesBeforeAsk);

    // Explicitly authorizing again really starts once more (new run, new id).
    const [approvedPage] = await Promise.all([
      context.waitForEvent("page", { timeout: 20000 }),
      page.locator("button.preview-start-btn").click(),
    ]);
    await approvedPage.waitForLoadState("load");
    expect(await approvedPage.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
    await expect.poll(() => probe.startCount(), { timeout: 10000 }).toBe(2);
    await approvedPage.close();

    // A confirmed Stop inside the same login/task/round/manifest, then ONE click on
    // the chat result action: the approval cache is still valid for this scope, so
    // that single user gesture must issue exactly one new Start (new id) and open a
    // real page — without going through the approval button again.
    const idsBeforeRestart = probe.operationIds();
    await page.locator(".result-pane button:has-text('停止预览')").click();
    await expect(page.locator(".result-pane")).toContainText("服务已停止", { timeout: 10000 });
    const [restartedPage] = await Promise.all([
      context.waitForEvent("page", { timeout: 20000 }),
      actionBtn.click(),
    ]);
    await restartedPage.waitForLoadState("load");
    expect(await restartedPage.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
    await expect.poll(() => probe.startCount(), { timeout: 10000 }).toBe(3);
    const idsAfterRestart = probe.operationIds();
    expect(idsBeforeRestart.length).toBe(2);
    expect(idsAfterRestart.length).toBe(3);
    // Actually all-pairwise distinct: a repeated or reused operation id would make
    // the Set smaller than the number of recorded Start calls.
    expect(new Set(idsAfterRestart).size).toBe(3);
    await restartedPage.close();

    // Contract A — a new login never inherits execution approval. Stop the still
    // running Start-3 preview from the panel that is already open (same login, same
    // round, same manifest) BEFORE logging out, then re-login.
    const stopBtn3 = page.locator(".result-pane button:has-text('停止预览')");
    await expect(stopBtn3).toBeVisible();
    await stopBtn3.click();
    await expect(page.locator(".result-pane")).toContainText("服务已停止", { timeout: 10000 });
    await page.locator("button.logout").click();
    await expect(page.getByLabel("本机访问令牌")).toBeVisible();
    await login(page);
    await page.getByRole("button", { name: "当前项目", exact: true }).click();
    await page.getByRole("menuitemradio", { name: "CommandProj" }).click();
    await page.locator(".task-card").filter({ hasText: "CmdTask" }).click();
    await expect(page.locator("#chat-title")).toContainText("CmdTask");

    // One click on the chat result action must only ask for approval now: no page,
    // no Start beyond the recorded 3.
    const pagesBeforeAskReLogin = context.pages().length;
    await actionBtn.click();
    await expect(page.locator(".result-pane code.preview-command")).toBeVisible();
    await expect(page.locator("button.preview-start-btn")).toBeVisible();
    await page.waitForTimeout(800);
    expect(probe.startCount()).toBe(3);
    expect(context.pages().length).toBe(pagesBeforeAskReLogin);

    // Only the explicit approval click starts once more: real page, Start 4, and
    // four pairwise-distinct operation ids.
    const [approvedPage2] = await Promise.all([
      context.waitForEvent("page", { timeout: 20000 }),
      page.locator("button.preview-start-btn").click(),
    ]);
    await approvedPage2.waitForLoadState("load");
    expect(await approvedPage2.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
    await expect.poll(() => probe.startCount(), { timeout: 10000 }).toBe(4);
    const idsAfterApprove = probe.operationIds();
    expect(idsAfterApprove.length).toBe(4);
    expect(new Set(idsAfterApprove).size).toBe(4);
    await approvedPage2.close();

    // Contract B — a running preview is viewable without approval. Fresh login
    // while Start 4 is still running: one click on the chat result action opens a
    // real page read-only, adds no Start, and shows no command authorization.
    await page.locator("button.logout").click();
    await expect(page.getByLabel("本机访问令牌")).toBeVisible();
    await login(page);
    await page.getByRole("button", { name: "当前项目", exact: true }).click();
    await page.getByRole("menuitemradio", { name: "CommandProj" }).click();
    await page.locator(".task-card").filter({ hasText: "CmdTask" }).click();
    await expect(page.locator("#chat-title")).toContainText("CmdTask");

    const [viewPage] = await Promise.all([
      context.waitForEvent("page", { timeout: 20000 }),
      actionBtn.click(),
    ]);
    await viewPage.waitForLoadState("load");
    expect(await viewPage.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
    expect(probe.startCount()).toBe(4);
    await expect(page.locator(".result-pane code.preview-command")).toHaveCount(0);
    expect(new Set(probe.operationIds()).size).toBe(4);
    await viewPage.close();
  });

  test("F5b: unknown Start receipt (reply dropped after the command really ran) never mints a second write, before or after Stop", async ({ page, context }) => {
    test.setTimeout(90000);
    await page.setViewportSize({ width: 1440, height: 900 });
    const probe = await routePreviewSocket(page);
    await login(page);
    await newTask(page, "UnknownStartProj", "CmdTopic", "UnknownStartTask");

    const card = page.locator(".task-card").first();
    await card.getByRole("button", { name: "开始开发", exact: true }).click();
    await expect(card).toContainText("待验收", { timeout: 15000 });

    await page.locator("#draft").fill("命令网页产物 未知回执测试");
    await page.locator("#send").click();
    await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });

    const actionBtn = page.locator(".turn-result-actions").locator("button:has-text('打开网页'), button:has-text('查看结果')").first();
    await expect(actionBtn).toBeVisible();
    await actionBtn.click();
    const resultPane = page.locator(".result-pane");
    await expect(resultPane.locator("button.preview-start-btn")).toBeVisible({ timeout: 10000 });

    // First authorization: the service really runs the command, but the reply the
    // page receives is lost (socket cut), so the write's outcome is unknown.
    probe.drop("previews/start");
    await resultPane.locator("button.preview-start-btn").click();
    await expect.poll(() => probe.startCount(), { timeout: 20000 }).toBe(1);

    // Repeat the explicit action: it may only reconcile read-only. No second
    // Start with a new id, whatever status says.
    await page.waitForTimeout(900);
    const statusBeforeRepeat = probe.statusCount();
    const pagesBefore = page.context().pages().length;
    await page.bringToFront();
    await expect(actionBtn).toBeEnabled();
    const opensBeforeRepeat = probe.openCount();
    await actionBtn.click();
    await expect.poll(() => probe.openCount()).toBeGreaterThan(opensBeforeRepeat);
    await expect.poll(() => probe.statusCount(), { timeout: 15000 }).toBeGreaterThan(statusBeforeRepeat);
    await page.waitForTimeout(1500);
    expect(probe.startCount()).toBe(1);

    // The preview really runs (read-only open + Status proved it): either the
    // read-only path delivered a real page, or the surrounding status refresh
    // already rendered the same real preview in the panel.
    const pagesNow = page.context().pages();
    const extraPage = pagesNow.length > pagesBefore ? pagesNow[pagesNow.length - 1] : undefined;
    if (extraPage) {
      await extraPage.waitForLoadState("load");
      expect(await extraPage.locator("#proc-title").innerText()).toBe("开悟服务进程测试");
      await extraPage.close();
    } else {
      const frame = resultPane.locator("iframe.task-preview-frame");
      await expect(frame).toBeVisible({ timeout: 10000 });
      const frameTitle = await frame.contentFrame().locator("#proc-title").innerText();
      expect(frameTitle).toBe("开悟服务进程测试");
    }
    expect(probe.startCount()).toBe(1);

    // A confirmed Stop does not clear the unknown receipt: repeating the explicit
    // Start stays blocked on the original id — no new write, no new page.
    const stopBtn = resultPane.getByRole("button", { name: "停止预览", exact: true });
    await expect(stopBtn).toBeVisible();
    await stopBtn.click();
    await expect(page.locator(".result-pane")).toContainText("服务已停止", { timeout: 10000 });
    const pagesAfterStop = page.context().pages().length;
    const startsAfterStop = probe.startCount();
    const repeatStart = resultPane.getByRole("button", { name: /启动预览|重试启动预览/ }).first();
    await page.bringToFront();
    await expect(repeatStart).toBeVisible();
    await expect(repeatStart).toBeEnabled();
    const statusesAfterStop = probe.statusCount();
    await repeatStart.click();
    await expect.poll(() => probe.statusCount()).toBeGreaterThan(statusesAfterStop);
    await expect(resultPane).toContainText("上次启动结果未知");
    await expect.poll(() => context.pages().length).toBe(pagesAfterStop);
    expect(probe.startCount()).toBe(startsAfterStop);
    expect(context.pages().length).toBe(pagesAfterStop);
    await expect(resultPane.locator(".preview-empty")).toContainText("服务已停止");
  });

  test("Mobile responsive 360px & 390px: No horizontal overflow, compact toolbar, full accessibility", async ({ page }) => {
    for (const width of [360, 390]) {
      await page.setViewportSize({ width, height: 800 });
      await login(page);
      await newTask(page, `MobileProj${width}`, "MobileTopic", `MobileTask${width}`);

      // On mobile viewport, switch to 看板 to see card
      const boardTab = page.locator(".mobile-switch button:has-text('看板')");
      if (await boardTab.isVisible()) {
        await boardTab.click();
      }

      const card = page.locator(".task-card").first();
      await card.getByRole("button", { name: "开始开发", exact: true }).click();
      await expect(card).toContainText("待验收", { timeout: 15000 });

      // Switch back to 聊天
      const chatTab = page.locator(".mobile-switch button:has-text('聊天')");
      if (await chatTab.isVisible()) {
        await chatTab.click();
      }

      await page.locator("#draft").fill(`移动端测试 ${width}`);
      await page.locator("#send").click();
      await expect(page.locator(".message.assistant")).toHaveCount(2, { timeout: 20000 });

      // Verify no horizontal overflow in document
      const hasHorizontalOverflow = await page.evaluate(() => {
        return document.documentElement.scrollWidth > window.innerWidth;
      });
      expect(hasHorizontalOverflow).toBe(false);

      // Verify action buttons fit cleanly
      const actionRow = page.locator(".turn-result-actions");
      await expect(actionRow).toBeVisible();
      const rowBox = await actionRow.boundingBox();
      expect(rowBox?.width).toBeLessThanOrEqual(width);
    }
  });
});

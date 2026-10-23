import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const TOKEN = "kaiwu-browser-test-token-00000000";
const ORIGIN = "http://127.0.0.1:17331";
// The exact stored text the Go backend produces for a provider failure.
const PROVIDER_ERROR =
  "模型调用失败；已有内容保留：429 已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)";
const NO_REASON_ERROR = "模型调用失败或被中断；已有内容保留";
const SEND_ERROR = "消息未能确认完成；请检查后明确恢复，不会自动重发";
const SESSION_ERROR = "pi 会话已中断，发送下一条消息时自动接回；历史已保留";

type Fixture = { apply?: (body: any) => void };

const fixtures = new WeakMap<Page, Fixture>();

test.beforeEach(async ({ page }) => {
  const fixture: Fixture = {};
  fixtures.set(page, fixture);
  // The same snapshot shape arrives over HTTP (first load) and over the socket
  // (live updates): patch both, exactly like the existing view-only fixtures.
  await page.route("**/api/snapshot", async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    fixture.apply?.(body);
    await route.fulfill({ response, json: body });
  });
  await page.routeWebSocket("**/api/socket", (client) => {
    const server = client.connectToServer();
    server.onMessage((raw) => {
      const frame = JSON.parse(String(raw));
      if (frame?.type === "snapshot") fixture.apply?.(frame);
      client.send(JSON.stringify(frame));
    });
    client.onMessage((raw) => server.send(String(raw)));
  });
});

function transform(page: Page, apply: (body: any) => void) {
  fixtures.get(page)!.apply = apply;
}

async function login(page: Page) {
  const response = await page.request.post("/api/login", {
    data: { token: TOKEN },
    headers: { Origin: ORIGIN, "X-Kaiwu-Client": "web" },
  });
  expect(response.ok(), await response.text()).toBe(true);
}

async function command(page: Page, body: Record<string, unknown>) {
  const state = (await (await page.request.get("/api/snapshot")).json()).state;
  const response = await page.request.post("/api/commands", {
    data: { ...body, revision: state.revision },
    headers: { Origin: ORIGIN, "X-Kaiwu-Client": "web" },
  });
  expect(response.ok(), await response.text()).toBe(true);
  return (await response.json()).createdId as string;
}

// One project per run: the server rejects a second project for the same path.
let sharedProject: string | undefined;

// Creates only a project and two tasks: no prompt is sent, so no pi session is
// started and the server session budget is untouched.
async function setup(page: Page, label: string) {
  await login(page);
  const path = readFileSync(
    resolve("../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt"),
    "utf8",
  ).trim();
  if (!sharedProject) {
    const state = (await (await page.request.get("/api/snapshot")).json()).state;
    const existing = Object.values(state.projects).find((p: any) => p.path === path) as any;
    sharedProject =
      existing?.id ??
      (await command(page, {
        type: "add_project",
        operationId: "failure-project-" + Date.now(),
        name: "失败提示核对项目",
        path,
      }));
  }
  const projectId = sharedProject;
  const stamp = Date.now() + "-" + label;
  const taskA = await command(page, {
    type: "add_task",
    projectId,
    operationId: "failure-task-a-" + stamp,
    title: "执行失败任务",
    request: "复现失败原因",
  });
  const taskB = await command(page, {
    type: "add_task",
    projectId,
    operationId: "failure-task-b-" + stamp,
    title: "会话中断任务",
    request: "复现中断原因",
  });
  return { projectId, taskA, taskB };
}

function conversationOf(body: any, taskId: string) {
  const task = body.state.tasks[taskId];
  return body.state.conversations[task.conversationId];
}

function failConversation(body: any, taskId: string, runState: string, error: string, text = "开始开发") {
  const conversation = conversationOf(body, taskId);
  conversation.runState = runState;
  conversation.error = error;
  conversation.rounds = [];
  conversation.messages = [{ id: "user-" + taskId, role: "user", text, delivery: "failed" }];
}

function preserveResult(body: any, taskId: string) {
  const task = body.state.tasks[taskId];
  task.status = "needs_confirmation";
  body.state.executions ??= {};
  body.state.executions["legacy-" + taskId] = {
    id: "legacy-" + taskId,
    taskId,
    conversationId: task.conversationId,
    phase: "awaiting_review",
    finishedAt: "2026-01-01T00:00:00Z",
    tools: [],
    summary: "",
  };
  task.executionId = "legacy-" + taskId;
}

async function showChat(page: Page) {
  if (!(await page.locator(".chat").isVisible())) {
    await page.getByRole("button", { name: "◌ 聊天" }).click();
    await expect(page.locator(".chat")).toBeVisible();
  }
}

async function openTask(page: Page, projectId: string, taskId: string, title: string) {
  await page.goto("/projects/" + projectId + "/tasks/" + taskId);
  await showChat(page);
  await expect(page.locator("#chat-title")).toContainText(title);
}

async function selectTask(page: Page, taskId: string, title: string) {
  const card = page.locator('.task-card[data-taskid="' + taskId + '"]');
  await card.getByRole("button", { name: "选择任务 " + title, exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText(title);
}

test("a task failure shows the provider reason inline and keeps the existing result", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const { projectId, taskA } = await setup(page, "inline");
  transform(page, (body) => {
    failConversation(body, taskA, "failed", PROVIDER_ERROR);
    preserveResult(body, taskA);
  });
  await openTask(page, projectId, taskA, "执行失败任务");

  const card = page.locator(".chat-failure");
  await expect(card).toBeVisible();
  await expect(card).toContainText("执行失败");
  await expect(card).not.toContainText("会话接回失败");
  const reason = card.locator(".chat-failure-reason");
  await expect(reason).toBeVisible();
  await expect(reason).toContainText("429");
  await expect(reason).toContainText("已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)");
  await expect(reason).not.toContainText("已有内容保留");
  // The boilerplate prefix stays available, but only behind the expander.
  await expect(card.locator(".chat-failure-full p")).toBeHidden();
  await card.locator(".chat-failure-full summary").click();
  await expect(card.locator(".chat-failure-full p")).toBeVisible();
  await expect(card.locator(".chat-failure-full p")).toContainText("模型调用失败；已有内容保留：");
  // A failure that did not destroy the result must keep the result actions.
  await expect(card).toContainText("此前的结果仍然保留");
  await expect(page.locator(".result-review")).toBeVisible();
  await expect(page.getByRole("button", { name: "确认完成", exact: true })).toBeVisible();
  await page.screenshot({ path: "/tmp/kaiwu-failure-desktop.png" });
  expect(errors).toEqual([]);
});

test("missing reasons, send failures and interruptions are labeled accurately", async ({ page }) => {
  const { projectId, taskA } = await setup(page, "labels");
  transform(page, (body) => failConversation(body, taskA, "failed", NO_REASON_ERROR));
  await openTask(page, projectId, taskA, "执行失败任务");
  const card = page.locator(".chat-failure");
  await expect(card).toContainText("执行失败");
  await expect(card).toContainText("pi 未提供具体原因");
  await expect(card).not.toContainText("429");

  transform(page, (body) => failConversation(body, taskA, "failed", SEND_ERROR));
  await page.reload();
  await showChat(page);
  await expect(card).toContainText("消息发送失败");
  await expect(card).not.toContainText("执行失败");
  await expect(card).toContainText("再次发送");

  transform(page, (body) => failConversation(body, taskA, "interrupted", SESSION_ERROR));
  await page.reload();
  await showChat(page);
  await expect(card).toContainText("会话已中断");
  await expect(card).not.toContainText("会话接回失败");
  // The reconnect explanation stays, the duplicated reason inside the progress
  // panel is gone: the chat notice is the single place showing the cause.
  await expect(page.locator(".auto-recovery")).toContainText("尝试接回原会话");
  await expect(page.locator(".recovery-reason")).toHaveCount(0);
  await page.setViewportSize({ width: 360, height: 780 });
  await showChat(page);
  await expect(card).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow, "360px page overflow").toBeLessThanOrEqual(1);
  await page.screenshot({ path: "/tmp/kaiwu-failure-card-360.png" });
});

test("switching tasks never carries a reason and a new round hides the old one", async ({ page }) => {
  const { projectId, taskA, taskB } = await setup(page, "isolation");
  transform(page, (body) => {
    failConversation(body, taskA, "failed", PROVIDER_ERROR);
    failConversation(body, taskB, "interrupted", SESSION_ERROR, "开始开发");
  });
  await openTask(page, projectId, taskA, "执行失败任务");
  const card = page.locator(".chat-failure");
  await expect(card).toContainText("执行失败");
  await expect(card).toContainText("Token Plan 速率限制");

  await selectTask(page, taskB, "会话中断任务");
  await expect(card).toContainText("会话已中断");
  await expect(card).not.toContainText("Token Plan");

  await selectTask(page, taskA, "执行失败任务");
  await expect(card).toContainText("Token Plan");

  // A new round clears the stored error (backend) and the notice must vanish.
  transform(page, (body) => {
    const conversation = conversationOf(body, taskA);
    conversation.runState = "running";
    conversation.error = "";
  });
  await page.reload();
  await showChat(page);
  await expect(page.locator(".chat-failure")).toHaveCount(0);
  await expect(page.locator("#messages")).not.toContainText("Token Plan");

  // Project scope is independent as well: a healthy project chat shows nothing.
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  await expect(page.locator(".chat-failure")).toHaveCount(0);
});

test("a project conversation failure shows its reason as well", async ({ page }) => {
  const { projectId } = await setup(page, "project");
  transform(page, (body) => {
    const project = body.state.projects[projectId];
    const conversation = body.state.conversations[project.conversationId];
    conversation.runState = "failed";
    conversation.error = PROVIDER_ERROR;
    conversation.messages = [{ id: "p1", role: "user", text: "讨论下一步", delivery: "failed" }];
  });
  await page.goto("/projects/" + projectId);
  await showChat(page);
  const card = page.locator(".chat-failure");
  await expect(card).toBeVisible();
  await expect(card).toContainText("执行失败");
  await expect(card).toContainText("Token Plan 速率限制");
});

test("short messages hug their content and long content stays inside 1440/390/360", async ({ page }) => {
  const { projectId, taskA } = await setup(page, "layout");
  const longChinese = "这是一个很长的中文消息用于验证换行".repeat(12);
  const longURL = "https://example.invalid/" + "路径".repeat(30) + "?query=" + "a".repeat(180);
  const markdown =
    "| 项目 | 状态 |\n| --- | --- |\n| " + "长表格单元格".repeat(12) + " | 完成 |\n\n" +
    "```bash\n" + "echo " + "超长命令".repeat(20) + "\n```\n\n" + longURL;
  transform(page, (body) => {
    const conversation = conversationOf(body, taskA);
    conversation.runState = "idle";
    conversation.error = "";
    conversation.rounds = [];
    conversation.messages = [
      { id: "short", role: "user", text: "开始开发", delivery: "complete" },
      { id: "cn", role: "user", text: longChinese, delivery: "complete" },
      { id: "url", role: "user", text: longURL, delivery: "complete" },
      { id: "md", role: "assistant", text: markdown, delivery: "complete" },
    ];
  });
  await openTask(page, projectId, taskA, "执行失败任务");

  for (const [width, height, shot] of [
    [1440, 900, "/tmp/kaiwu-failure-layout-1440.png"],
    [390, 844, "/tmp/kaiwu-failure-layout-390.png"],
    [360, 780, "/tmp/kaiwu-failure-layout-360.png"],
  ] as const) {
    await page.setViewportSize({ width, height });
    await showChat(page);
    const metrics = await page.evaluate(() => {
      const list = document.querySelector<HTMLElement>("#messages")!;
      const bubbles = Array.from(document.querySelectorAll<HTMLElement>(".message.user .text"));
      const clipped = Array.from(document.querySelectorAll<HTMLElement>(".message .text")).filter(
        (el) => el.scrollWidth > el.clientWidth + 1,
      ).length;
      return {
        container: list.clientWidth,
        widths: bubbles.map((el) => Math.round(el.getBoundingClientRect().width)),
        clipped,
        pageOverflow: document.documentElement.scrollWidth - window.innerWidth,
      };
    });
    expect(metrics.pageOverflow, width + "px page overflow").toBeLessThanOrEqual(1);
    expect(metrics.clipped, width + "px clipped text").toBe(0);
    expect(metrics.widths[0], width + "px short bubble").toBeLessThan(220);
    expect(metrics.widths[0], width + "px short bubble").toBeLessThan(metrics.container * 0.7);
    for (const bubble of metrics.widths.slice(1))
      expect(bubble, width + "px long bubble").toBeLessThanOrEqual(metrics.container);
    await page.screenshot({ path: shot });
  }
});

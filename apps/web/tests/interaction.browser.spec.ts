import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";

/**
 * Real Go + isolated fake-pi integration. Business traffic is never mocked:
 * questions come from the fixture CLI through the real Store/Manager and
 * answers are real commands (Socket `commands`, i.e. POST /api/commands on the
 * server) persisted in SQLite and delivered back to the same fake-pi session.
 *
 * Only the lost-receipt case proxies the WebSocket to drop one reply after the
 * server processed it (network-failure simulation, reported separately).
 */

const TOKEN = "kaiwu-interaction-test-token-00000000";

interface FrameProbe {
  commands: { id: string; body: any }[];
  replies: { id: string; status: number; data: any }[];
}

function frameProbe(page: Page): FrameProbe {
  const state: FrameProbe = { commands: [], replies: [] };
  page.on("websocket", (ws) => {
    if (!ws.url().includes("/api/socket")) return;
    ws.on("framesent", (event) => {
      try {
        const value = JSON.parse(String(event.payload));
        if (value.method === "commands") state.commands.push({ id: value.id, body: value.body });
      } catch {}
    });
    ws.on("framereceived", (event) => {
      try {
        const value = JSON.parse(String(event.payload));
        if (value.type === "reply") state.replies.push(value);
      } catch {}
    });
  });
  return state;
}

async function login(page: Page) {
  await page.goto("/");
  const token = page.getByLabel("本机访问令牌");
  if (await token.isVisible().catch(() => false)) {
    await token.fill(TOKEN);
    await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  }
  await expect(page.getByRole("button", { name: "当前项目", exact: true })).toBeVisible();
}

async function addProject(page: Page, name: string): Promise<void> {
  const welcome = page.getByRole("button", { name: "＋ 添加项目", exact: true });
  if (await welcome.isVisible().catch(() => false)) {
    await welcome.click();
  } else {
    await page.getByRole("button", { name: "当前项目", exact: true }).click();
    await page.getByRole("menuitem", { name: "添加项目", exact: true }).click();
  }
  const dir = mkdtempSync(join(tmpdir(), "kaiwu-interaction-e2e-"));
  execFileSync("git", ["init", dir], { stdio: "ignore" });
  await page.locator("input[name=name]").fill(name);
  await page.locator("input[name=path]").fill(dir);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText("项目对话");
}

async function newTask(page: Page, project: string, topic: string, title: string) {
  await addProject(page, project);
  await page.locator("[data-action=new-topic]").click();
  await page.locator("input[name=name]").fill(topic);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".column h2").first()).toHaveText(topic);
  await page.getByRole("button", { name: `在 ${topic} 新建任务`, exact: true }).click();
  await page.locator("input[name=title]").fill(title);
  await page.locator("textarea[name=request]").fill("交互 E2E 任务");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText(title);
}

async function send(page: Page, text: string) {
  await page.locator("#draft").fill(text);
  await page.locator("#send").click();
}

async function snapshot(page: Page): Promise<any> {
  return page.evaluate(async () => {
    const r = await fetch("/api/snapshot", { headers: { "X-Kaiwu-Client": "web" } });
    return r.json();
  });
}

async function currentConversation(page: Page): Promise<string> {
  const data = await snapshot(page);
  const match = new URL(page.url()).pathname.match(/\/tasks\/([^/]+)/);
  if (!match) throw new Error("not on a task route");
  const task = data.state.tasks[match[1]];
  if (!task) throw new Error("task missing");
  return task.conversationId;
}

function conversationInteractions(data: any, conversationId: string): any[] {
  return data?.state?.conversations?.[conversationId]?.interactions ?? [];
}

async function waitPending(page: Page, conversationId: string, title: string): Promise<any> {
  let found: any = null;
  await expect
    .poll(async () => {
      const data = await snapshot(page);
      found = conversationInteractions(data, conversationId).find(
        (ix) => ix.status === "pending" && ix.title === title,
      );
      return !!found;
    }, { timeout: 20000 })
    .toBe(true);
  return found;
}

async function interactionById(page: Page, conversationId: string, id: string): Promise<any> {
  const data = await snapshot(page);
  return conversationInteractions(data, conversationId).find((ix) => ix.id === id);
}

function field(page: Page, label: string) {
  return page
    .locator(".interaction-field-item")
    .filter({ has: page.locator(".interaction-field-label", { hasText: label }) });
}

async function openHistory(page: Page, title: string) {
  const card = page.locator("details.interaction-history-card").filter({ hasText: title });
  await expect(card).toBeVisible();
  if (!(await card.evaluate((el: HTMLDetailsElement) => el.open))) {
    await card.locator("summary").click();
  }
  return card;
}

async function submitAndReconcile(
  page: Page,
  frames: FrameProbe,
  card: ReturnType<Page["locator"]>,
): Promise<{ cmd: { id: string; body: any }; reply: { id: string; status: number; data: any } }> {
  const start = frames.commands.length;
  await card.locator("button[data-action='submit-interaction']").click();
  await expect.poll(() => frames.commands.length, { timeout: 15000 }).toBeGreaterThan(start);
  const cmd = frames.commands[start];
  let reply: any = null;
  await expect
    .poll(() => {
      reply = frames.replies.find((r) => r.id === cmd.id);
      return !!reply;
    }, { timeout: 20000 })
    .toBe(true);
  return { cmd, reply };
}

test.describe("real Go + fake pi interaction integration", () => {
  test("six field types answer persists, original session continues, history survives reload", async ({ page }) => {
    const frames = frameProbe(page);
    await login(page);
    await newTask(page, "交互六类项目", "交互功能", "六类字段任务");
    const conversationId = await currentConversation(page);
    const sessionBefore = await page.locator("#chat-session").innerText();
    await send(page, "[ix-six] 请开始交互");
    const request = await waitPending(page, conversationId, "部署参数确认");
    expect(request.source).toBe("assistant_question");
    expect(request.fields.map((f: any) => f.type).sort()).toEqual(
      ["confirm", "multi_select", "number", "single_select", "text", "textarea"].sort(),
    );

    const card = page.locator(".interaction-card");
    await expect(card).toBeVisible();
    await expect(card.locator(".interaction-title")).toHaveText("部署参数确认");
    for (const label of ["是否允许继续部署", "变更标题", "详细说明", "目标环境", "启用特性", "端口"]) {
      await expect(card.locator(".interaction-field-label", { hasText: label })).toBeVisible();
    }

    await field(page, "是否允许继续部署").getByRole("button", { name: "不允许", exact: true }).click();
    await field(page, "变更标题").locator("input[type=text]").fill("浏览器变更");
    await field(page, "详细说明").locator("textarea").fill("完整说明来自真实浏览器");
    const env = field(page, "目标环境");
    await env.locator("input[type=radio]").last().check();
    await env.locator(".interaction-other-input").fill("独立专线");
    await env.screenshot({ path: "/tmp/kaiwu-interaction-real-desktop-env.png" });
    const features = field(page, "启用特性");
    await features.locator("input[type=checkbox]").first().check();
    await features.locator("input[type=checkbox]").last().check();
    await features.locator(".interaction-other-input").fill("自定义特性");
    await field(page, "端口").locator("input[type=number]").fill("0");
    await page.locator(".interaction-field-item").filter({ hasText: "补充说明（可选）" }).locator("textarea").fill("浏览器补充");
    await card.evaluate((el: HTMLElement) => { el.scrollTop = 0; });
    await card.screenshot({ path: "/tmp/kaiwu-interaction-real-desktop-filled-top.png" });
    await card.evaluate((el: HTMLElement) => { el.scrollTop = el.scrollHeight; });
    await card.screenshot({ path: "/tmp/kaiwu-interaction-real-desktop-filled.png" });

    const { cmd, reply } = await submitAndReconcile(page, frames, card);
    expect(reply.status).toBe(200);
    expect(reply.data.interaction.status).toBe("answered");
    expect(reply.data.interactionId).toBe(request.id);
    expect(cmd.body.type).toBe("answer_interaction");
    expect(cmd.body.requestId).toBe(request.id);
    expect(cmd.body.answers.allow).toBe(false);
    expect(cmd.body.answers.port).toBe(0);
    expect("env" in cmd.body.answers).toBe(false);
    expect(cmd.body.others.env).toBe("独立专线");
    expect(cmd.body.answers.features).toEqual(["metrics"]);
    expect(cmd.body.others.features).toBe("自定义特性");
    expect(cmd.body.comment).toBe("浏览器补充");

    await expect(page.locator(".message.assistant").last()).toContainText("已收到本轮交互答复");
    await expect(page.locator(".message.user").last()).toContainText("不允许");
    await expect(page.locator(".message.user").last()).toContainText("0 端口");
    await expect(page.locator("#chat-session")).toHaveText(sessionBefore);
    await expect
      .poll(async () => (await interactionById(page, conversationId, request.id))?.response?.delivery, { timeout: 20000 })
      .toBe("accepted");
    const accepted = await interactionById(page, conversationId, request.id);
    expect(accepted.response.messageId).toBeTruthy();
    expect(accepted.response.followUpRoundId).toBe(accepted.response.messageId);

    await page.reload();
    await expect(page.getByRole("button", { name: "当前项目", exact: true })).toBeVisible();
    await expect(page.locator("#chat-title")).toContainText("六类字段任务");
    await expect(page.locator(".interaction-card")).toHaveCount(0);
    const history = await openHistory(page, "部署参数确认");
    await expect(history).toContainText("已发送给 AI");
    await expect(history).toContainText("不允许");
    await expect(history).toContainText("端口: 0 端口");
    await expect(history).toContainText("环境: 其他（独立专线）");
    await expect(history).toContainText("指标");
    await expect(history).toContainText("其他（自定义特性）");
    await expect(history).toContainText("标题: 浏览器变更");

    await page.screenshot({ path: "/tmp/kaiwu-interaction-real-desktop-history.png", fullPage: false });
  });

  test("native confirm false and an untouched empty input use the original waiter", async ({ page }) => {
    const frames = frameProbe(page);
    await login(page);
    await newTask(page, "原生确认项目", "原生功能", "原生确认任务");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-native-confirm] 请弹出原生确认");
    const request = await waitPending(page, conversationId, "原生确认请求");
    expect(request.source).toBe("agent_ui");
    await expect(page.getByText("原生交互", { exact: true })).toBeVisible();
    await expect(page.locator(".interaction-card")).not.toContainText("改用文字回答本次问题");
    const card = page.locator(".interaction-card");
    await card.getByRole("button", { name: "不允许", exact: true }).click();
    const first = await submitAndReconcile(page, frames, card);
    expect(first.reply.status).toBe(200);
    expect(first.cmd.body.answers.confirmed).toBe(false);
    await expect(page.locator(".message.assistant").last()).toContainText("原生确认结果：false");
    await expect
      .poll(async () => (await interactionById(page, conversationId, request.id))?.response?.delivery, { timeout: 20000 })
      .toBe("accepted");

    // A second native dialog in the same conversation: untouched input must be
    // submitted as an explicit empty string.
    await send(page, "[ix-native-empty] 请弹出原生输入");
    const emptyRequest = await waitPending(page, conversationId, "原生空串输入");
    expect(emptyRequest.fields[0].allowEmpty).toBe(true);
    const emptyCard = page.locator(".interaction-card");
    const second = await submitAndReconcile(page, frames, emptyCard);
    expect(second.reply.status).toBe(200);
    expect(second.cmd.body.answers.value).toBe("");
    await expect(page.locator(".message.assistant").last()).toContainText("原生空串结果：present=true len=0");
    await expect
      .poll(async () => (await interactionById(page, conversationId, emptyRequest.id))?.response?.delivery, { timeout: 20000 })
      .toBe("accepted");
  });

  test("pending native request past its deadline leaves the card and is view-only history", async ({ page }) => {
    await login(page);
    await newTask(page, "过期交互项目", "过期功能", "过期任务");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-native-expire] 请弹出会超时的原生确认");
    const request = await waitPending(page, conversationId, "会超时的原生确认");
    await expect(page.locator(".interaction-card")).toBeVisible();
    expect(request.expiresAt).toBeTruthy();
    await expect(page.locator(".interaction-card")).toHaveCount(0, { timeout: 10000 });
    await expect(page.locator(".interaction-card")).toHaveCount(0);
    const history = await openHistory(page, "会超时的原生确认");
    await expect(history).toContainText("已失效");
    await expect(history).not.toContainText("重试发送");
  });

  test("two queued questions answer separately, flush once and never half-dispatch", async ({ page }) => {
    const frames = frameProbe(page);
    await login(page);
    await newTask(page, "多问题项目", "队列功能", "多问题任务");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-six] 第一个问题");
    const first = await waitPending(page, conversationId, "部署参数确认");
    await page.getByRole("button", { name: "改用普通聊天回复 ↗", exact: true }).click();
    await send(page, "[ix-two] 第二个问题");
    const second = await waitPending(page, conversationId, "第二个问题");
    await expect(page.locator(".interaction-queue-nav")).toContainText("1/2");

    const card = page.locator(".interaction-card");
    await field(page, "是否允许继续部署").getByRole("button", { name: "允许", exact: true }).click();
    await field(page, "变更标题").locator("input[type=text]").fill("第一题");
    await field(page, "目标环境").locator("input[type=radio]").first().check();
    await field(page, "启用特性").locator("input[type=checkbox]").first().check();
    await field(page, "端口").locator("input[type=number]").fill("1");
    const firstSubmit = await submitAndReconcile(page, frames, card);
    expect(firstSubmit.reply.status).toBe(200);
    expect(firstSubmit.reply.data.createdId ?? "").toBe("");
    await expect
      .poll(async () => {
        const a = await interactionById(page, conversationId, first.id);
        const b = await interactionById(page, conversationId, second.id);
        return [a?.status, a?.response?.messageId ?? "", b?.status];
      }, { timeout: 20000 })
      .toEqual(["answered", "", "pending"]);

    await expect(page.locator(".interaction-card .interaction-title")).toHaveText("第二个问题");
    const secondCard = page.locator(".interaction-card");
    await secondCard.getByRole("button", { name: "继续", exact: true }).click();
    await field(page, "第二项说明").locator("input[type=text]").fill("第二题说明");
    const secondSubmit = await submitAndReconcile(page, frames, secondCard);
    expect(secondSubmit.reply.status).toBe(200);
    expect(secondSubmit.reply.data.createdId).toBeTruthy();
    await expect(page.locator(".message.assistant").last()).toContainText("已收到本轮交互答复");
    await expect
      .poll(async () => {
        const a = await interactionById(page, conversationId, first.id);
        const b = await interactionById(page, conversationId, second.id);
        return [a?.status, b?.status, a?.response?.messageId, b?.response?.messageId];
      }, { timeout: 20000 })
      .toEqual(["answered", "answered", secondSubmit.reply.data.createdId, secondSubmit.reply.data.createdId]);
  });

  test("lost response is reconciled by operation receipt without a second command", async ({ page }) => {
    let commands = 0;
    let dropped = false;
    await page.routeWebSocket("**/api/socket", (client) => {
      const server = client.connectToServer();
      const commandIds = new Set<string>();
      client.onMessage((raw) => {
        try {
          const value = JSON.parse(String(raw));
          if (value.method === "commands" && value.body?.type === "answer_interaction") {
            commands++;
            commandIds.add(value.id);
          }
        } catch {}
        server.send(raw);
      });
      server.onMessage((raw) => {
        try {
          const value = JSON.parse(String(raw));
          if (!dropped && value.type === "reply" && commandIds.has(value.id)) {
            dropped = true;
            void client.close({ code: 1012, reason: "fixture response lost" });
            return;
          }
        } catch {}
        client.send(raw);
      });
    });
    await login(page);
    await newTask(page, "丢回执项目", "回执功能", "丢回执任务");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-six] 丢回执测试");
    const request = await waitPending(page, conversationId, "部署参数确认");
    const card = page.locator(".interaction-card");
    await field(page, "是否允许继续部署").getByRole("button", { name: "允许", exact: true }).click();
    await field(page, "变更标题").locator("input[type=text]").fill("回执");
    await field(page, "目标环境").locator("input[type=radio]").first().check();
    await field(page, "启用特性").locator("input[type=checkbox]").first().check();
    await field(page, "端口").locator("input[type=number]").fill("2");
    await card.locator("button[data-action='submit-interaction']").click();
    await expect
      .poll(async () => (await interactionById(page, conversationId, request.id))?.status, { timeout: 30000 })
      .toBe("answered");
    await expect(page.locator(".interaction-card")).toHaveCount(0, { timeout: 30000 });
    expect(commands).toBe(1);
  });

  test("draft and pending request survive switching tasks", async ({ page }) => {
    await login(page);
    await newTask(page, "切卡草稿项目", "切卡功能", "切卡任务A");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-six] 切卡前的提问");
    await waitPending(page, conversationId, "部署参数确认");
    await field(page, "变更标题").locator("input[type=text]").fill("尚未提交的草稿");
    await page.getByRole("button", { name: "在 切卡功能 新建任务", exact: true }).click();
    await page.locator("input[name=title]").fill("切卡任务B");
    await page.locator("textarea[name=request]").fill("另一个任务");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.locator("#chat-title")).toContainText("切卡任务B");
    await page
      .locator(".task-card")
      .filter({ has: page.getByRole("heading", { name: "切卡任务A", exact: true }) })
      .click();
    await expect(page.locator(".interaction-card")).toBeVisible();
    await expect(field(page, "变更标题").locator("input[type=text]")).toHaveValue("尚未提交的草稿");
  });

  test("second device answer is authoritative and the stale form cannot overwrite it", async ({ page }) => {
    await login(page);
    await newTask(page, "抢答项目", "抢答功能", "抢答任务");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-six] 双设备抢答");
    const request = await waitPending(page, conversationId, "部署参数确认");
    const firstStatus = await page.evaluate(async (req: any) => {
      const r = await fetch("/api/commands", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Kaiwu-Client": "web" },
        body: JSON.stringify({
          operationId: "device-a-" + Date.now(),
          type: "answer_interaction",
          projectId: req.projectId,
          conversationId: req.conversationId,
          taskId: req.taskId,
          requestId: req.id,
          requestVersion: req.version,
          answerMode: "form",
          answers: { allow: true, note: "A", env: "staging", features: ["metrics"], port: 1 },
        }),
      });
      return r.status;
    }, request);
    expect(firstStatus).toBe(200);
    const secondStatus = await page.evaluate(async (req: any) => {
      const r = await fetch("/api/commands", {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-Kaiwu-Client": "web" },
        body: JSON.stringify({
          operationId: "device-b-" + Date.now(),
          type: "answer_interaction",
          projectId: req.projectId,
          conversationId: req.conversationId,
          taskId: req.taskId,
          requestId: req.id,
          requestVersion: req.version,
          answerMode: "form",
          answers: { allow: false, note: "B", env: "prod", features: ["tracing"], port: 2 },
        }),
      });
      return r.status;
    }, request);
    expect(secondStatus).toBe(409);
    const stored = await interactionById(page, conversationId, request.id);
    expect(stored.status).toBe("answered");
    expect(stored.response.answers.allow).toBe(true);
    expect(stored.response.answers.note).toBe("A");
    await page.reload();
    await expect(page.locator("#chat-title")).toContainText("抢答任务");
    await expect(page.locator(".interaction-card")).toHaveCount(0);
  });

  test("360px viewport keeps the long interaction card usable", async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 740 });
    await login(page);
    await newTask(page, "移动交互项目", "移动功能", "移动长卡片任务");
    const conversationId = await currentConversation(page);
    await send(page, "[ix-six] 移动端长卡片");
    await waitPending(page, conversationId, "部署参数确认");
    const card = page.locator(".interaction-card");
    await expect(card).toBeVisible();
    await card.screenshot({ path: "/tmp/kaiwu-interaction-real-mobile-360-top.png" });
    await field(page, "目标环境").screenshot({ path: "/tmp/kaiwu-interaction-real-mobile-360-env.png" });
    const box = await card.boundingBox();
    expect(box).toBeTruthy();
    expect(box!.width).toBeLessThanOrEqual(360);
    const noHorizontalScroll = await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
    expect(noHorizontalScroll).toBe(true);
    const submit = card.locator("button[data-action='submit-interaction']");
    await submit.scrollIntoViewIfNeeded();
    await expect(submit).toBeVisible();
    const collapse = card.locator("button[data-action='collapse-interaction']");
    await collapse.scrollIntoViewIfNeeded();
    await expect(collapse).toBeVisible();
    const radio = card.locator("input[type=radio]").first();
    const size = await radio.evaluate((el: HTMLElement) => {
      const r = el.getBoundingClientRect();
      return { w: r.width, h: r.height };
    });
    expect(size.w).toBe(16);
    expect(size.h).toBe(16);
    await page.screenshot({ path: "/tmp/kaiwu-interaction-real-mobile-360.png", fullPage: false });
  });
});

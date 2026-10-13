import { test, expect } from "@playwright/test";
import type { Page, BrowserContext, Cookie } from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
test("real build: login, persistent board, isolated sessions, refresh and responsive views", async ({
  page,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await page.getByRole("button", { name: "＋ 添加项目", exact: true }).click();
  await page.locator("input[name=name]").fill("开悟验收项目");
  await page
    .locator("input[name=path]")
    .fill(
      readFileSync(
        resolve(
          "../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt",
        ),
        "utf8",
      ),
    );
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText("项目对话");
  await page.locator("[data-action=new-topic]").click();
  await page.locator("input[name=name]").fill("功能开发");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".column h2").first()).toHaveText("功能开发");
  for (const title of ["任务 A", "任务 B"]) {
    await page
      .getByRole("button", { name: "在 功能开发 新建任务", exact: true })
      .click();
    await page.locator("input[name=title]").fill(title);
    await page.locator("textarea[name=request]").fill("保留独立上下文");
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.locator("#chat-title")).toContainText(title);
  }
  await page
    .locator(".task-card")
    .filter({ has: page.getByRole("heading", { name: "任务 A", exact: true }) })
    .click();
  await page.locator("#draft").fill("只给 A 的消息");
  await page.locator("#send").click();
  await page
    .locator(".task-card")
    .filter({ has: page.getByRole("heading", { name: "任务 B", exact: true }) })
    .click();
  await page.locator("#draft").fill("B 独立草稿");
  await expect(page.locator("#messages")).not.toContainText("只给 A 的消息");
  await expect(page.locator("#draft")).toHaveValue("B 独立草稿");
  await page
    .locator(".task-card")
    .filter({ has: page.getByRole("heading", { name: "任务 A", exact: true }) })
    .click();
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：只给 A 的消息",
  );
  await expect(page.locator("#draft")).toHaveValue("");
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  await expect(page.locator("#messages")).not.toContainText("只给 A 的消息");
  await page.locator("#draft").fill("项目层面的讨论");
  await page.locator("#send").click();
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：项目层面的讨论",
  );
  page.on("dialog", (dialog) => dialog.accept());
  await page.reload();
  await expect(page.locator(".task-card")).toHaveCount(2);
  await page
    .locator(".task-card")
    .filter({ has: page.getByRole("heading", { name: "任务 A", exact: true }) })
    .click();
  await expect(page.locator("#messages")).toContainText("只给 A 的消息");
  for (const [width, height] of [
    [1440, 1100],
    [1024, 900],
    [768, 1024],
    [360, 900],
  ]) {
    await page.setViewportSize({ width, height });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    ).toBe(true);
    if (width === 360)
      expect(
        await page
          .locator(".top")
          .evaluate((el) => el.getBoundingClientRect().height),
      ).toBeLessThan(75);
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page
    .getByRole("button", { name: "删除专题 功能开发", exact: true })
    .click();
  await page
    .getByRole("button", { name: "删除专题，保留任务", exact: true })
    .click();
  await expect(
    page
      .getByRole("region", { name: "其他", exact: true })
      .locator(".task-card"),
  ).toHaveCount(2);
  await expect(page.locator("#messages")).toContainText("只给 A 的消息");
  await page.setViewportSize({ width: 360, height: 900 });
  await page.getByRole("button", { name: "◌ 聊天", exact: true }).click();
  await expect(page.locator("#draft")).toBeVisible();
  await page.getByRole("button", { name: "▦ 看板", exact: true }).click();
  await expect(page.locator("#board")).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  for (const [topic, title] of [
    ["界面体验", "打磨看板与对话切换"],
    ["服务接入", "验证独立 pi 会话"],
  ]) {
    await page.locator("[data-action=new-topic]").click();
    await page.locator("input[name=name]").fill(topic!);
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await page
      .getByRole("button", { name: "在 " + topic + " 新建任务", exact: true })
      .click();
    await page.locator("input[name=title]").fill(title!);
    await page.locator("textarea[name=request]").fill("浏览器隔离验收示例");
    await page.getByRole("button", { name: "保存", exact: true }).click();
  }
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  mkdirSync(resolve("../../docs/design/web-service"), { recursive: true });
  await page.screenshot({
    path: resolve("../../docs/design/web-service/desktop.png"),
  });
  await page.setViewportSize({ width: 360, height: 900 });
  await page.screenshot({
    path: resolve("../../docs/design/web-service/mobile.png"),
  });
  expect(errors).toEqual([]);
});

test("missing static files and APIs stay distinct from SPA deep links", async ({
  request,
}) => {
  const asset = await request.get("/assets/missing.js");
  expect(asset.status()).toBe(404);
  const api = await request.get("/api/missing");
  expect(api.status()).toBe(401);
  expect(api.headers()["content-type"]).toContain("application/json");
  const deep = await request.get("/projects/missing/tasks/missing");
  expect(deep.status()).toBe(200);
  expect(deep.headers()["content-type"]).toContain("text/html");
});

test("late snapshot cannot undo logout", async ({ page }) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  let unblock!: () => void;
  let captured!: () => void;
  const arrived = new Promise<void>((resolve) => (captured = resolve));
  const gate = new Promise<void>((resolve) => (unblock = resolve));
  await page.route("**/api/snapshot", async (route) => {
    const response = await route.fetch();
    captured();
    await gate;
    await route.fulfill({ response });
  });
  await page.evaluate(() => dispatchEvent(new PopStateEvent("popstate")));
  await arrived;
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await expect(page.getByLabel("本机访问令牌")).toBeVisible();
  unblock();
  await page.unrouteAll({ behavior: "wait" });
  await expect(page.locator("#chat-title")).toHaveCount(0);
});

test("lost send response is confirmed by receipt without duplicate prompt", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  let requests = 0;
  await page.route("**/api/commands", async (route) => {
    requests++;
    await route.fetch();
    await route.abort("failed");
  });
  await page.locator("#draft").fill("回执丢失仍只发送一次");
  await page.locator("#send").click();
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：回执丢失仍只发送一次",
  );
  await expect(page.locator("#draft")).toHaveValue("");
  expect(requests).toBe(1);
  await page.unrouteAll({ behavior: "wait" });
  await page.locator("#draft").fill("确认后可以新发");
  await page.locator("#send").click();
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：确认后可以新发",
  );
});

test("old login generation cannot clear a new draft through late send receipt", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  let unblock!: () => void, arrived!: () => void;
  const captured = new Promise<void>((resolve) => (arrived = resolve)),
    gate = new Promise<void>((resolve) => (unblock = resolve));
  await page.route("**/api/commands", async (route) => {
    const response = await route.fetch();
    arrived();
    await gate;
    await route.fulfill({ response });
  });
  await page.locator("#draft").fill("旧身份的消息");
  await page.locator("#send").click();
  await captured;
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  await page.locator("#draft").fill("新身份的新草稿");
  unblock();
  await page.unrouteAll({ behavior: "wait" });
  await expect(page.locator("#draft")).toHaveValue("新身份的新草稿");
});

test("switching back to a long conversation restores reading position", async ({
  page,
}) => {
  await page.route("**/api/snapshot", async (route) => {
    const response = await route.fetch(),
      data = await response.json();
    if (!data.state) {
      await route.fulfill({ response });
      return;
    }
    const task = Object.values(data.state.tasks).find(
      (t: any) => t.title === "任务 A",
    ) as any;
    if (task) {
      data.state.conversations[task.conversationId].messages = Array.from(
        { length: 80 },
        (_, i) => ({
          id: "long-" + i,
          role: "assistant",
          delivery: "complete",
          text: ("阅读位置 " + i + "\n").repeat(5),
        }),
      );
    }
    await route.fulfill({ response, json: data });
  });
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  const card = page.locator(".task-card").filter({
    has: page.getByRole("heading", { name: "任务 A", exact: true }),
  });
  await card.click();
  await page.locator("#messages").evaluate((el) => {
    el.scrollTop = 300;
    el.dispatchEvent(new Event("scroll"));
  });
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  await card.click();
  await expect
    .poll(() => page.locator("#messages").evaluate((el) => el.scrollTop))
    .toBe(300);
});

test("composition and plain Enter preserve input; keyboard shortcut sends once", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  let commands = 0;
  page.on("request", (request) => {
    if (request.url().endsWith("/api/commands")) commands++;
  });
  const draft = page.locator("#draft");
  await draft.fill("组合输入");
  await draft.dispatchEvent("keydown", {
    key: "Enter",
    ctrlKey: true,
    isComposing: true,
  });
  await draft.press("Enter");
  expect(commands).toBe(0);
  await expect(draft).toHaveValue("组合输入\n");
  await draft.press("Control+Enter");
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：组合输入",
  );
  expect(commands).toBe(1);
});

test("project form initializes a new directory with optional origin", async ({
  page,
}) => {
  const seed = readFileSync(
    resolve(
      "../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt",
    ),
    "utf8",
  );
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  for (const remote of ["", "git@example.com:team/project.git"]) {
    await page.getByRole("button", { name: "添加项目", exact: true }).click();
    const dir = seed + (remote ? "-with-origin" : "-without-origin");
    await page
      .getByLabel("项目名称", { exact: true })
      .fill(remote ? "带远程地址" : "从零开始");
    await page.getByLabel("项目绝对路径", { exact: true }).fill(dir);
    await expect(page.getByRole("dialog")).toContainText("会自动初始化");
    await page.getByLabel("Git 远程地址（可选）", { exact: true }).fill(remote);
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    const git = (...args: string[]) =>
      execFileSync("git", ["-C", dir, ...args], { encoding: "utf8" }).trim();
    expect(git("rev-parse", "--show-toplevel")).toBe(dir);
    expect(git("remote")).toBe(remote ? "origin" : "");
    if (remote) expect(git("remote", "get-url", "origin")).toBe(remote);
    await page.reload();
    await expect(page.locator(".top select option:checked")).toHaveText(
      remote ? "带远程地址" : "从零开始",
    );
    await expect(page.locator("#chat-title")).toContainText("项目对话");
  }
});

test("remembered login restores in a new browser context and logout revokes it", async ({
  browser,
  page,
  context,
}) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  await page.reload();
  await expect(page.locator("#chat-title")).toBeVisible();
  const saved = await context.storageState();
  const cookie = saved.cookies.find((c) => c.name === "kaiwu_session")!;
  expect(cookie.expires).toBeGreaterThan(Date.now() / 1000 + 29 * 24 * 3600);
  const reopened = await browser.newContext({ storageState: saved });
  try {
    const restored = await reopened.newPage();
    await restored.goto("http://127.0.0.1:17331/");
    await expect(restored.locator("#chat-title")).toBeVisible();
    await restored.getByRole("button", { name: "退出", exact: true }).click();
    await expect(restored.getByLabel("本机访问令牌")).toBeVisible();
    await page.reload();
    await expect(page.getByLabel("本机访问令牌")).toBeVisible();
  } finally {
    await reopened.close();
  }
});

let startCookies: Cookie[] = [];
async function enterStart(page: Page, context: BrowserContext) {
  const cookieFile = resolve(
    readFileSync(
      resolve(
        "../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt",
      ),
      "utf8",
    ),
    "..",
    "browser-session.json",
  );
  if (!startCookies.length && existsSync(cookieFile))
    startCookies = JSON.parse(readFileSync(cookieFile, "utf8"));
  if (startCookies.length) await context.addCookies(startCookies);
  await page.goto("/");
  if (!startCookies.length) {
    await page
      .getByLabel("本机访问令牌")
      .fill("kaiwu-browser-test-token-00000000");
    await page.getByRole("button", { name: "进入工作台", exact: true }).click();
    await expect(page.locator("#chat-title")).toBeVisible();
    startCookies = await context.cookies();
    writeFileSync(cookieFile, JSON.stringify(startCookies), { mode: 0o600 });
  }
  await page
    .getByLabel("当前项目", { exact: true })
    .selectOption({ label: "开悟验收项目" });
  await expect(page.locator("#chat-title")).toContainText("项目对话");
}

test("card starts original task in a worktree and preserves the draft", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  const card = page.locator(".task-card").filter({
    has: page.getByRole("heading", { name: "任务 A", exact: true }),
  });
  await card
    .getByRole("button", { name: "选择任务 任务 A", exact: true })
    .click();
  await page.locator("#draft").fill("不要开始开发");
  await page.locator("#send").click();
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：不要开始开发",
  );
  await expect(card).toContainText("未开始");
  await expect(page.locator(".scope")).toContainText("就绪");
  await page.locator("#draft").fill("保留这份草稿");
  let starts = 0;
  page.on("request", (request) => {
    if (
      request.url().endsWith("/api/commands") &&
      request.postDataJSON().type === "start_task"
    )
      starts++;
  });
  await card.getByRole("button", { name: "开始开发", exact: true }).click();
  await expect(card).toContainText("需要确认");
  await expect(page.locator("#draft")).toHaveValue("保留这份草稿");
  await expect(page.locator(".execution-result")).toContainText("未执行验证");
  expect(starts).toBe(1);
  const st = (await (await page.request.get("/api/snapshot")).json()).state;
  const task = Object.values(st.tasks).find(
    (t: any) => t.title === "任务 A",
  ) as any;
  const x = st.executions[task.executionId];
  expect(existsSync(resolve(x.workspace, "kaiwu-fixture-result.txt"))).toBe(
    true,
  );
  expect(
    existsSync(
      resolve(st.projects[task.projectId].path, "kaiwu-fixture-result.txt"),
    ),
  ).toBe(false);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.screenshot({ path: "/tmp/kaiwu-task-start-desktop.png" });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "◌ 聊天", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  await page.screenshot({ path: "/tmp/kaiwu-task-start-mobile.png" });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("task chat starts development and project chat requires a task", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  await page.locator("#draft").fill("开始开发");
  await page.locator("#send").click();
  await expect(page.getByRole("alert")).toContainText("请先选择");
  await expect(page.locator("#draft")).toHaveValue("开始开发");
  const card = page.locator(".task-card").filter({
    has: page.getByRole("heading", { name: "任务 B", exact: true }),
  });
  await card
    .getByRole("button", { name: "选择任务 任务 B", exact: true })
    .click();
  await page.locator("#draft").fill(" 开始开发！ ");
  await page.locator("#send").click();
  await expect(card).toContainText("需要确认");
  await expect(page.locator("#draft")).toHaveValue("");
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  await expect(page.locator("#draft")).toHaveValue("开始开发");
});

test("start without a model opens selection and preserves the draft", async ({
  page,
  context,
}) => {
  await page.route("**/api/snapshot", async (route) => {
    const response = await route.fetch(),
      data = await response.json();
    if (!data.state) {
      await route.fulfill({ response });
      return;
    }
    data.capabilities.agent = false;
    for (const task of Object.values(data.state.tasks) as any[]) {
      task.status = "idle";
      task.executionId = undefined;
      const c = data.state.conversations[task.conversationId];
      c.runState = "idle";
      c.profile.model = "";
      c.messages = [];
      c.sessionFile = undefined;
    }
    await route.fulfill({ response, json: data });
  });
  let dispatched = 0;
  await page.route("**/api/commands", async route => { dispatched++; await route.continue(); });
  await enterStart(page, context);
  const card = page.locator(".task-card").first();
  await card.getByRole("button", { name: "开始开发", exact: true }).click();
  const picker = page.getByRole('dialog', { name: '选择会话模型', exact: true });
  await expect(picker).toBeVisible();
  await picker.getByRole('button', { name: '取消', exact: true }).click();
  await page.locator("#draft").fill("开始开发");
  await expect(page.locator("#send")).toBeEnabled();
  await page.locator("#send").click();
  await expect(picker).toBeVisible();
  await picker.getByRole('button', { name: '取消', exact: true }).click();
  await expect(page.locator("#draft")).toHaveValue("开始开发");
  expect(dispatched).toBe(0);
});

test("Markdown messages render safely and copy code on HTTP without the clipboard API", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.setViewportSize({ width: 1440, height: 900 });
  await enterStart(page, context);
  const source = 'package main\n\nfunc main() { println("<hello> & world") }';
  const content = [
    "内容如下：",
    "",
    "## 格式验收",
    "",
    "**重点** 与 `inline()`，~~删除~~。",
    "",
    "- [x] 已完成",
    "- [ ] 待确认",
    "",
    "> 引用内容",
    "",
    "| 项目 | 状态 |",
    "| --- | --- |",
    "| API | 通过 |",
    "",
    "```go",
    source,
    "```",
    "",
    "```unknown-language",
    "<untouched> & value",
    "```",
    "",
    "[安全链接](https://example.com/docs)",
    "[危险链接](javascript:alert(1))",
    "",
    '<img src=x onerror="window.__unsafe=1"><script>window.__unsafe=2</script>',
  ].join("\n");
  await page.locator("#draft").fill(content);
  await page.locator("#send").click();
  const reply = page.locator(".message.assistant").last();
  await expect(reply.getByRole("heading", { name: "格式验收" })).toBeVisible();
  await expect(reply.locator("table td").first()).toHaveText("API");
  await expect(reply.locator("blockquote")).toHaveText("引用内容");
  await expect(reply.locator('input[type="checkbox"]:checked')).toBeDisabled();
  await expect(reply.locator("pre code").first()).toHaveText(source);
  await expect(reply.locator("pre code .hljs-keyword").first()).toBeVisible();
  await expect(reply.locator("pre code").nth(1)).toHaveText(
    "<untouched> & value",
  );
  await expect(reply.getByRole("link", { name: "安全链接" })).toHaveAttribute(
    "href",
    "https://example.com/docs",
  );
  expect(await reply.locator('a[href^="javascript:"]').count()).toBe(0);
  expect(await reply.locator("img,script,iframe").count()).toBe(0);
  expect(await page.evaluate(() => (window as any).__unsafe)).toBeUndefined();
  // Exercise the actual legacy copy path used on private-LAN HTTP, then read the real clipboard.
  await page.evaluate(() => {
    (window as any).fixtureClipboard = navigator.clipboard;
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: undefined,
    });
  });
  await reply
    .getByRole("button", { name: "复制代码", exact: true })
    .first()
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => (window as any).fixtureClipboard.readText()),
    )
    .toBe(source);
  await expect(reply.getByRole("status")).toContainText("已复制");
  await page.screenshot({ path: "/tmp/kaiwu-markdown-desktop.png" });
  await page.reload();
  await expect(
    page
      .locator(".message.assistant")
      .last()
      .getByRole("heading", { name: "格式验收" }),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "◌ 聊天", exact: true }).click();
  await page
    .locator(".message.assistant")
    .last()
    .getByRole("heading", { name: "格式验收" })
    .scrollIntoViewIfNeeded();
  await page.screenshot({ path: "/tmp/kaiwu-markdown-mobile.png" });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
});

test("Markdown draft preview preserves source and stays in its conversation", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  const source =
    '## 项目草稿\n\n**保留原文**\n\n```json\n{"name": "kaiwu"}\n```';
  await page.locator("#draft").fill(source);
  await page
    .getByRole("button", { name: "预览 Markdown", exact: true })
    .click();
  const preview = page.getByLabel("当前草稿预览", { exact: true });
  await expect(
    preview.getByRole("heading", { name: "项目草稿" }),
  ).toBeVisible();
  await expect(preview.locator("code .hljs-attr").first()).toBeVisible();
  await expect(page.locator("#draft")).toBeHidden();
  await page.getByRole("button", { name: "返回编辑", exact: true }).click();
  await expect(page.locator("#draft")).toHaveValue(source);
  await expect(page.locator("#draft")).toBeFocused();
  await page
    .getByRole("button", { name: "预览 Markdown", exact: true })
    .click();
  await page
    .getByRole("button", { name: "选择任务 任务 A", exact: true })
    .click();
  await expect(page.getByLabel("当前草稿预览", { exact: true })).toHaveCount(0);
  await page.locator("#draft").fill("任务草稿");
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  await expect(page.locator("#draft")).toHaveValue(source);
  await page
    .getByRole("button", { name: "预览 Markdown", exact: true })
    .click();
  await preview.focus();
  await preview.press("Control+Enter");
  await expect(page.locator("#draft")).toHaveValue("");
  await expect(page.locator("#draft")).toBeVisible();
  await expect(
    page
      .locator(".message.user")
      .last()
      .getByRole("heading", { name: "项目草稿" }),
  ).toBeVisible();
  await expect(
    page.locator(".message.assistant").last().locator("pre code").first(),
  ).toHaveText('{"name": "kaiwu"}');
  const snapshot = (await (await page.request.get("/api/snapshot")).json())
    .state;
  const projectID = await page
    .getByLabel("当前项目", { exact: true })
    .inputValue();
  expect(
    snapshot.conversations[snapshot.projects[projectID].conversationId].messages
      .filter((m: any) => m.role === "user")
      .at(-1).text,
  ).toBe(source);
});

test("WebSocket updates render an unfinished code fence before the reply ends", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  await page.locator("#draft").fill("__MARKDOWN_STREAM_FIXTURE__");
  await page.locator("#send").click();
  const reply = page.locator(".message.assistant").last();
  await expect(reply.getByRole("heading", { name: "流式格式" })).toBeVisible();
  await expect(reply.locator("pre code")).toContainText("package main");
  await expect(page.locator(".scope")).toContainText("回复中");
  await expect(reply.locator("strong").last()).toHaveText("完成");
  await expect(reply.locator("pre code")).toContainText('println("ready")');
  await expect(page.locator(".scope")).toContainText("就绪");
});

test("merge icon and chat approve real Git integration and track later edits", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  await page.getByRole("button", { name: "添加项目", exact: true }).click();
  const seed = readFileSync(
    resolve(
      "../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt",
    ),
    "utf8",
  );
  const dir = seed + "-merge";
  await page.getByLabel("项目名称", { exact: true }).fill("合并验收");
  await page.getByLabel("项目绝对路径", { exact: true }).fill(dir);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await page.locator("[data-action=new-task]").click();
  await page.locator("input[name=title]").fill("集成功能");
  await page.locator("textarea[name=request]").fill("隔离开发并合并");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  const card = page.locator(".task-card").filter({
    has: page.getByRole("heading", { name: "集成功能", exact: true }),
  });
  await card.getByRole("button", { name: "开始开发", exact: true }).click();
  await expect(card).toContainText("需要确认");
  await expect(card).toContainText("待提交");
  await card.getByRole("button", { name: "合并到主分支", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("Kaiwu");
  await expect(page.getByRole("dialog")).toContainText(
    "kaiwu-fixture-result.txt",
  );
  await page.getByRole("button", { name: "取消", exact: true }).click();
  expect(existsSync(resolve(dir, "kaiwu-fixture-result.txt"))).toBe(false);
  await page.locator("#draft").fill("合并到主分支");
  await page.locator("#send").click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.getByRole("button", { name: "提交并合并", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("合并完成");
  await expect(card).toContainText("已合并");
  expect(
    readFileSync(resolve(dir, "kaiwu-fixture-result.txt"), "utf8"),
  ).toContain("隔离开发并合并");
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  const state = (await (await page.request.get("/api/snapshot")).json()).state;
  const task = Object.values(state.tasks).find(
    (t: any) => t.title === "集成功能",
  ) as any;
  const workspace = state.executions[task.executionId].workspace;
  writeFileSync(resolve(workspace, "after-merge.txt"), "new work");
  await expect(card).toContainText("待提交", { timeout: 12000 });
  page.on("dialog", (d) => d.accept());
  await page.reload();
  await expect(card).toContainText("待提交");
});

test("late merge preview cannot reopen over another conversation", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  await page
    .getByLabel("当前项目", { exact: true })
    .selectOption({ label: "合并验收" });
  const card = page
    .locator(".task-card")
    .filter({
      has: page.getByRole("heading", { name: "集成功能", exact: true }),
    });
  await card
    .getByRole("button", { name: "选择任务 集成功能", exact: true })
    .click();
  let unblock!: () => void;
  const gate = new Promise<void>((r) => (unblock = r));
  let held = false;
  await page.route("**/api/git/preview", async (route) => {
    const response = await route.fetch();
    held = true;
    await gate;
    await route.fulfill({ response });
  });
  await card.getByRole("button", { name: "合并到主分支", exact: true }).click();
  await expect.poll(() => held).toBe(true);
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  const received = page.waitForResponse((r) =>
    r.url().endsWith("/api/git/preview"),
  );
  unblock();
  await received;
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.locator("#chat-title")).toContainText("项目对话");
});

test("lost merge response and missing receipt retry the same operation", async ({
  page,
  context,
}) => {
  await enterStart(page, context);
  await page
    .getByLabel("当前项目", { exact: true })
    .selectOption({ label: "合并验收" });
  const card = page
    .locator(".task-card")
    .filter({
      has: page.getByRole("heading", { name: "集成功能", exact: true }),
    });
  await card.getByRole("button", { name: "合并到主分支", exact: true }).click();
  let drop = true,
    miss = true;
  const ids: string[] = [];
  await page.route("**/api/git/merge", async (route) => {
    ids.push(route.request().postDataJSON().operationId);
    if (drop) {
      drop = false;
      await route.fetch();
      await route.abort("failed");
    } else await route.continue();
  });
  await page.route("**/api/git/operations/*", async (route) => {
    if (miss) {
      miss = false;
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({
          error: { code: "not_found", message: "尚未查到回执" },
        }),
      });
    } else await route.continue();
  });
  await page.getByRole("button", { name: "提交并合并", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("回执");
  await page.getByRole("button", { name: "提交并合并", exact: true }).click();
  await expect.poll(() => ids.length).toBe(2);
  expect(ids[1]).toBe(ids[0]);
  await expect(page.getByRole("dialog")).toContainText("合并完成");
});

test("pi models: discover, filter, save defaults and choose an existing empty conversation", async ({ page, context }) => {
  await enterStart(page, context);
  await page.getByRole('button', { name: '默认模型', exact: true }).click();
  const picker = page.getByRole('dialog', { name: '默认模型', exact: true });
  await expect(picker).toContainText('检测到 3 个可用模型');
  await picker.getByLabel('提供商', { exact: true }).selectOption('fixture');
  await picker.getByLabel('搜索模型', { exact: true }).fill('another');
  await expect(picker.getByLabel('模型', { exact: true }).locator('option')).toHaveCount(2);
  await picker.getByLabel('模型', { exact: true }).selectOption('another-test');
  await expect(picker.getByLabel('思考级别', { exact: true })).toHaveCount(0);
  await picker.getByRole('button', { name: '保存模型' }).click();
  await expect(picker).toHaveCount(0);
  const modelProjectPath = resolve(readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8'), '..', 'model-picker-project');
  const created = await page.evaluate(async (path) => {
    const snap = await (await fetch('/api/snapshot')).json();
    const result = await (await fetch('/api/commands', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Kaiwu-Client': 'web' }, body: JSON.stringify({ type: 'add_project', operationId: crypto.randomUUID(), revision: snap.state.revision, name: '模型选择验证', path }) })).json();
    return result.createdId as string;
  }, modelProjectPath);
  await page.getByLabel('当前项目').selectOption(created);
  await page.locator('#draft').fill('选择模型时保留的草稿');
  await page.getByRole('button', { name: '选择会话模型', exact: true }).click();
  const scoped = page.getByRole('dialog', { name: '选择会话模型', exact: true });
  await expect(scoped.getByLabel('模型', { exact: true })).toHaveValue('another-test');
  await scoped.getByLabel('提供商', { exact: true }).selectOption('second');
  await scoped.getByLabel('模型', { exact: true }).selectOption('isolated-test');
  await scoped.getByRole('button', { name: '保存模型' }).click();
  await expect(scoped).toHaveCount(0);
  await expect(page.locator('#draft')).toHaveValue('选择模型时保留的草稿');
  const selected = await page.evaluate(async () => (await (await fetch('/api/snapshot')).json()).state);
  expect(selected.profile.model).toBe('another-test');
  const c = selected.conversations[selected.projects[created].conversationId];
  expect(c.profile.provider).toBe('second');
  expect(c.profile.model).toBe('isolated-test');
  expect(c.messages).toHaveLength(0);
  expect(JSON.stringify(selected)).not.toContain('must-not-reach-browser');
  await page.locator('#send').click();
  await expect(page.locator('#messages')).toContainText('[隔离协议测试] 已收到：选择模型时保留的草稿');
  await expect(page.getByRole('button', { name: '选择会话模型', exact: true })).toBeDisabled();
});

test("pi models: errors, empty catalog and mobile selection retain draft", async ({ page, context }) => {
  await enterStart(page, context);
  await page.setViewportSize({ width: 360, height: 900 });
  await page.route('**/api/models*', route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'models_unavailable', message: 'pi 检测暂时失败' } }) }));
  await page.getByRole('button', { name: '默认模型', exact: true }).click();
  const picker = page.getByRole('dialog', { name: '默认模型', exact: true });
  await expect(picker).toContainText('pi 检测暂时失败');
  await expect(picker.getByRole('button', { name: '保存模型' })).toBeDisabled();
  await page.unroute('**/api/models*');
  await page.route('**/api/models*', route => route.fulfill({ contentType: 'application/json', body: '{"models":[]}' }));
  await picker.getByRole('button', { name: '重新检测' }).click();
  await expect(picker).toContainText('pi 尚无可用模型');
  await page.unroute('**/api/models*');
  await picker.getByRole('button', { name: '重新检测' }).click();
  await expect(picker).toContainText('检测到 3 个可用模型');
  await picker.getByLabel('提供商', { exact: true }).selectOption('fixture');
  await picker.getByLabel('模型', { exact: true }).selectOption('isolated-test');
  await picker.getByLabel('思考级别', { exact: true }).selectOption('medium');
  expect(await picker.evaluate(el => el.getBoundingClientRect().width)).toBeLessThanOrEqual(360);
  await picker.getByRole('button', { name: '保存模型' }).click();
  await expect(picker).toHaveCount(0);
  const snap = await page.evaluate(async () => (await (await fetch('/api/snapshot')).json()).state);
  expect(snap.profile.thinking).toBe('medium');
});

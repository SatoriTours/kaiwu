import { test, expect } from "@playwright/test";
import type { Page, BrowserContext } from "@playwright/test";
import { readFileSync, mkdirSync } from "node:fs";
import { resolve } from "node:path";

const TOKEN = "kaiwu-browser-test-token-00000000";
const ORIGIN = "http://127.0.0.1:17331";
const ARTIFACTS=process.env.KAIWU_QUIET_SHOTS || "/tmp/kaiwu-quiet-shots";
mkdirSync(ARTIFACTS,{recursive:true});
// The exact stored text the Go backend produces for a provider failure.
const PROVIDER_ERROR =
  "模型调用失败；已有内容保留：429 已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)";
const NO_REASON_ERROR = "模型调用失败或被中断；已有内容保留";
const SEND_ERROR = "消息未能确认完成；请检查后明确恢复，不会自动重发";
const SESSION_ERROR = "pi 会话已中断，发送下一条消息时自动接回；历史已保留";

type Fixture = { apply?: (body: any) => void; onCommand?: (frame:any)=>void; publish?:()=>void };

const fixtures = new WeakMap<Page, Fixture>();

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({colorScheme:"dark",reducedMotion:"reduce"});
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
      if (frame?.type === "snapshot") {
        fixture.apply?.(frame);
        fixture.publish=()=>{const next=structuredClone(frame);fixture.apply?.(next);client.send(JSON.stringify(next));};
      }
      client.send(JSON.stringify(frame));
    });
    client.onMessage((raw) => {fixture.onCommand?.(JSON.parse(String(raw)));server.send(String(raw));});
  });
});

function transform(page: Page, apply: (body: any) => void) {
  fixtures.get(page)!.apply = apply;
}

let authCookies: Awaited<ReturnType<BrowserContext["cookies"]>> | undefined;
async function login(page: Page) {
  if (authCookies) { await page.context().addCookies(authCookies); return; }
  const response = await page.request.post("/api/login", {
    data: { token: TOKEN },
    headers: { Origin: ORIGIN, "X-Kaiwu-Client": "web" },
  });
  expect(response.ok(), await response.text()).toBe(true);
  authCookies = await page.context().cookies();
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
  // Task deep links open the drawer themselves; do not race its async mount
  // by clicking a background project button under an already-open modal.
  if (!new URL(page.url()).pathname.includes("/tasks/") && !(await page.locator(".chat").isVisible())) {
    await page.getByRole("button", { name: "项目对话", exact:true }).click();
  }
  await expect(page.locator(".chat")).toBeVisible();
}

async function openTask(page: Page, projectId: string, taskId: string, title: string) {
  await page.goto("/projects/" + projectId + "/tasks/" + taskId);
  await showChat(page);
  await expect(page.locator("#chat-title")).toContainText(title);
}

async function selectTask(page: Page, taskId: string, title: string) {
  await page.getByRole("button",{name:"收起聊天",exact:true}).click();
  const card = page.locator('.task-card[data-taskid="' + taskId + '"]');
  await card.getByRole("button", { name: "选择任务 " + title, exact: true }).click();
  await expect(page.locator("#chat-title")).toContainText(title);
}

test("provider failure is a compact turn note and details dialog; old result survives", async ({ page }) => {
  const { projectId, taskA } = await setup(page, "inline");
  transform(page, body => { failConversation(body, taskA, "failed", PROVIDER_ERROR); preserveResult(body, taskA); });
  await openTask(page, projectId, taskA, "执行失败任务");
  const note = page.locator(".turn-notice");
  await expect(note).toHaveCount(1);
  await expect(note).toContainText("本次请求受到限流，请稍后再试。");
  await expect(note.locator(".notice-text")).not.toContainText("Token Plan");
  await expect(page.locator(".chat-failure")).toHaveCount(0);
  expect(await note.evaluate(el => getComputedStyle(el).backgroundColor)).toBe("rgba(0, 0, 0, 0)");
  await note.getByRole("button", {name:"详情",exact:true}).click();
  const dialog=page.getByRole("dialog",{name:"本轮详情"});
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Token Plan");
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(page.locator(".turn-result-actions")).toBeVisible();
  await page.screenshot({path:ARTIFACTS + "/actual-failed-desktop.png"});
});

test("idle disconnection after a finished result is completely silent", async ({page}) => {
  const {projectId,taskA}=await setup(page,"idle");
  transform(page,body=>{
    const c=conversationOf(body,taskA);c.runState="interrupted";c.error=SESSION_ERROR;
    c.messages=[{id:"u",role:"user",text:"完善聊天体验",delivery:"complete"},{id:"a",role:"assistant",text:"已经完成，可以查看结果。",delivery:"complete"}];
    c.rounds=[{id:"u",phase:"finished",startedAt:"2026-10-31T10:00:00Z",finishedAt:"2026-10-31T10:01:00Z",tools:[]}];
    preserveResult(body,taskA);
  });
  await openTask(page,projectId,taskA,"执行失败任务");
  await expect(page.locator(".turn-notice,.chat-failure")).toHaveCount(0);
  await expect(page.locator(".turn-result-actions")).toBeVisible();
  await expect(page.locator("#send")).toBeDisabled();
  await page.locator("textarea").last().fill("再优化一点");
  await expect(page.locator("#send")).toBeEnabled();
  await expect(page.locator("#messages")).not.toContainText("pi 未提供");
  await page.screenshot({path:ARTIFACTS + "/actual-idle-desktop.png"});
});

test("interrupted reply is one neutral line, without generic details at 390 and 360px",async({page})=>{
  const {projectId,taskA}=await setup(page,"interrupt");
  transform(page,body=>{
    failConversation(body,taskA,"interrupted",SESSION_ERROR);
    const c=conversationOf(body,taskA);
    c.rounds=[{id:"user-"+taskA,phase:"interrupted",startedAt:"2026-10-31T10:00:00Z",finishedAt:"2026-10-31T10:01:00Z",tools:[]}];
    c.messages.push({id:"reply",role:"assistant",text:"我会先收敛连接状态提示，让真正的内容保持清晰。",delivery:"uncertain"});
  });
  await openTask(page,projectId,taskA,"执行失败任务");
  const note=page.locator(".turn-notice");
  await expect(note).toHaveCount(1);await expect(note).toContainText("回复已中断");
  await expect(note.getByRole("button",{name:"详情"})).toHaveCount(0);
  for(const width of [390,360]){
    await page.setViewportSize({width,height:844});
    expect(await page.evaluate(()=>document.documentElement.scrollWidth-innerWidth)).toBeLessThanOrEqual(1);
    await expect(note).toBeVisible();
    await page.screenshot({path:ARTIFACTS + "/actual-interrupted-"+width+".png"});
  }
});

test("send uncertainty has no retry; definitely unsent retry preserves user control",async({page})=>{
  const {projectId,taskA}=await setup(page,"send");
  let mode="uncertain";
  transform(page,body=>{
    failConversation(body,taskA,"interrupted",SEND_ERROR,"请继续完善");
    const c=conversationOf(body,taskA);c.messages[0].delivery=mode;
  });
  await openTask(page,projectId,taskA,"执行失败任务");
  const note=page.locator(".turn-notice");
  await expect(note).toHaveCount(1);
  await expect(note).toContainText("发送状态待确认");
  await expect(note.getByRole("button",{name:"重试",exact:true})).toHaveCount(0);
  await expect(page.locator(".message.assistant .assistant-summary-markdown")).toHaveCount(0);
  mode="not_sent";await page.reload();await showChat(page);
  await expect(note).toContainText("未发送");
  await expect(note.getByRole("button",{name:"重试",exact:true})).toBeVisible();
  await page.locator("textarea").last().fill("保留我的新草稿");
  await expect(note.getByRole("button",{name:"重试",exact:true})).toHaveCount(0);
  await expect(page.locator("textarea").last()).toHaveValue("保留我的新草稿");
});

test("task switching and new running round do not inherit global failure",async({page})=>{
 const {projectId,taskA,taskB}=await setup(page,"isolation");
 transform(page,body=>{failConversation(body,taskA,"failed",PROVIDER_ERROR);});
 await openTask(page,projectId,taskA,"执行失败任务");
 await expect(page.locator(".turn-notice")).toContainText("限流");
 await selectTask(page,taskB,"会话中断任务");
 await expect(page.locator(".turn-notice")).toHaveCount(0);
 await selectTask(page,taskA,"执行失败任务");
 await expect(page.locator(".turn-notice")).toContainText("限流");
 transform(page,body=>{const c=conversationOf(body,taskA);c.runState="running";c.error="";c.messages=[{id:"new",role:"user",text:"新一轮",delivery:"accepted"}];c.rounds=[{id:"new",phase:"running",tools:[],startedAt:"2026-10-31T10:00:00Z"}];});
 await page.reload();await showChat(page);
 await expect(page.locator(".turn-notice")).toHaveCount(0);
 await page.getByRole("button",{name:"← 返回项目对话"}).click();
 await expect(page.locator(".turn-notice")).toHaveCount(0);
});

test("historical failure stays at the old turn when new work runs",async({page})=>{
 const {projectId,taskA}=await setup(page,"history");
 transform(page,body=>{
  const c=conversationOf(body,taskA);c.runState="running";c.error="";
  c.messages=[{id:"old",role:"user",text:"旧问题",delivery:"complete"},{id:"answer",role:"assistant",text:"已生成的部分内容",delivery:"complete"},{id:"next",role:"user",text:"新问题",delivery:"accepted"}];
  c.rounds=[{id:"old",phase:"failed",startedAt:"2026-10-31T10:00:00Z",finishedAt:"2026-10-31T10:01:00Z",tools:[],failure:{kind:"execution",detail:PROVIDER_ERROR}},{id:"next",phase:"running",startedAt:"2026-10-31T10:02:00Z",tools:[]}];
 });
 await openTask(page,projectId,taskA,"执行失败任务");
 await expect(page.locator(".turn-notice")).toHaveCount(1);
 await expect(page.locator(".message.assistant").first().locator(".turn-notice")).toContainText("限流");
 await expect(page.locator(".message.assistant").last().locator(".turn-notice")).toHaveCount(0);
 await page.getByRole("button",{name:"只看我的问题",exact:true}).click();
 await expect(page.locator(".turn-notice")).toHaveCount(0);
});

test("project conversation gets the same compact note",async({page})=>{
 const {projectId}=await setup(page,"project");
 transform(page,body=>{const c=body.state.conversations[body.state.projects[projectId].conversationId];c.runState="failed";c.error=PROVIDER_ERROR;c.messages=[{id:"p1",role:"user",text:"讨论下一步",delivery:"failed"}];});
 await page.goto("/projects/"+projectId);await showChat(page);
 await expect(page.locator(".turn-notice")).toHaveCount(1);
 await expect(page.locator(".turn-notice")).toContainText("限流");
});

test("only an explicit definitely-unsent retry sends one new command",async({page})=>{
 const {projectId,taskA}=await setup(page,"real-retry");
 const sent:any[]=[];
 fixtures.get(page)!.onCommand=frame=>{if(frame.method==="commands")sent.push(frame.body)};
 transform(page,body=>{failConversation(body,taskA,"failed",SESSION_ERROR,"请继续完善");conversationOf(body,taskA).messages[0].delivery="not_sent";});
 await openTask(page,projectId,taskA,"执行失败任务");
 await expect(page.locator(".turn-notice").getByRole("button",{name:"重试",exact:true})).toBeVisible();
 expect(sent.filter(c=>c.type==="send_message")).toHaveLength(0);
 await page.locator(".turn-notice").getByRole("button",{name:"重试",exact:true}).click();
 await expect.poll(()=>sent.filter(c=>c.type==="send_message").length).toBe(1);
 expect(sent.find(c=>c.type==="send_message").text).toBe("请继续完善");
 await expect(page.locator("#send")).toBeDisabled();
 expect(sent.filter(c=>c.type==="send_message")).toHaveLength(1);
});

test("details stay open through unrelated snapshot updates",async({page})=>{
 const {projectId,taskA}=await setup(page,"dialog-snapshot");
 let snapshots=0;
 transform(page,body=>{snapshots++;failConversation(body,taskA,"failed",PROVIDER_ERROR)});
 await openTask(page,projectId,taskA,"执行失败任务");
 await page.locator(".turn-notice").getByRole("button",{name:"详情",exact:true}).click();
 const dialog=page.getByRole("dialog",{name:"本轮详情"});await expect(dialog).toBeVisible();
 const before=snapshots;
 fixtures.get(page)!.publish!();
 await expect.poll(()=>snapshots).toBeGreaterThan(before);
 await expect(dialog).toBeVisible();
 await page.keyboard.press("Escape");await expect(dialog).not.toBeVisible();
 await expect(page.getByRole("dialog",{name:"聊天面板",exact:true})).toBeVisible();
});

test("light theme and narrow details remain readable",async({page})=>{
 await page.emulateMedia({colorScheme:"light",reducedMotion:"reduce"});
 const {projectId,taskA}=await setup(page,"light");
 transform(page,body=>failConversation(body,taskA,"failed",PROVIDER_ERROR));
 await openTask(page,projectId,taskA,"执行失败任务");
 await page.setViewportSize({width:390,height:844});
 await page.locator(".turn-notice").getByRole("button",{name:"详情",exact:true}).click();
 const dialog=page.getByRole("dialog",{name:"本轮详情"});await expect(dialog).toBeVisible();
 const box=await dialog.boundingBox();expect(box!.x).toBeGreaterThanOrEqual(0);expect(box!.x+box!.width).toBeLessThanOrEqual(391);
 await page.screenshot({path:ARTIFACTS+"/actual-detail-light-390.png"});
 await dialog.getByRole("button",{name:"关闭详情",exact:true}).click();await expect(dialog).not.toBeVisible();
 await expect(page.getByRole("dialog",{name:"聊天面板",exact:true})).toBeVisible();
 await page.screenshot({path:ARTIFACTS+"/actual-failed-light-390.png"});
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

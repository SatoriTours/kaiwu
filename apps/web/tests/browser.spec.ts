import { test, expect } from "@playwright/test";
import type { Page, BrowserContext, Cookie } from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
// Intercept real WebSocket frames; all business requests still reach the real Go server.
const socketProbes = new WeakMap<Page, {calls:any[];received:any[];held:(()=>void)[];holdMethod:string;holdType:string;dropMethod:string;modelMode:string;transform?:(v:any)=>void;deliver?:(v:any)=>void}>();
test.beforeEach(async({page})=>{
 const probe={calls:[] as any[],received:[] as any[],held:[] as (()=>void)[],holdMethod:'',holdType:'',dropMethod:'',modelMode:''};socketProbes.set(page,probe);
 await page.routeWebSocket('**/api/socket',client=>{
  const server=client.connectToServer(),methods=new Map<string,string>();
  (probe as {deliver?:(v:any)=>void}).deliver = frame => client.send(JSON.stringify(frame));
  client.onMessage(raw=>{const v=JSON.parse(String(raw));probe.calls.push(v);methods.set(v.id,v.method);server.send(raw)});
  server.onMessage(raw=>{
   const v=JSON.parse(String(raw)),method=methods.get(v.id);probe.received.push(v);
   const transform=(probe as {transform?:(v:any)=>void}).transform;
   if(v.type==='snapshot'&&transform){transform(v);client.send(JSON.stringify(v));return}
   if(v.type==='reply'&&method===probe.dropMethod){probe.dropMethod='';void client.close({code:1012,reason:'fixture response lost'});return}
   if((v.type==='reply'&&method===probe.holdMethod)||(v.type===probe.holdType)){probe.held.push(()=>{try{client.send(raw)}catch{}});return}
   if(v.type==='reply'&&method?.startsWith('models')&&probe.modelMode){v.status=probe.modelMode==='error'?503:200;v.data=probe.modelMode==='error'?{error:{code:'models_unavailable',message:'pi 检测暂时失败'}}:{models:[]};client.send(JSON.stringify(v));return}
   client.send(raw);
  });
 });
});
async function chooseMenuProject(page: Page, target: { label: string } | { id: string }) {
  await page.getByRole('button', { name: '当前项目', exact: true }).click();
  const menu = page.getByRole('menu', { name: '项目菜单' });
  const item = 'label' in target
    ? menu.getByRole('menuitemradio', { name: target.label, exact: true })
    : menu.locator('[role=menuitemradio][data-project-id="' + target.id + '"]');
  await item.click();
}
async function openAddProject(page: Page) {
  await page.getByRole('button', { name: '当前项目', exact: true }).click();
  await page.getByRole('menuitem', { name: '添加项目', exact: true }).click();
}
function probeFor(page:Page){return socketProbes.get(page)!}
function releaseFrames(page:Page){const p=probeFor(page);p.holdMethod='';p.holdType='';for(const release of p.held.splice(0))release()}
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
  await page.getByRole('button', { name: '当前项目', exact: true }).click();
  const emptyMenu = page.getByRole('menu', { name: '项目菜单' });
  await expect(emptyMenu).toContainText('还没有项目');
  await expect(emptyMenu.getByRole('menuitemradio')).toHaveCount(0);
  await expect(emptyMenu.getByRole('menuitem', { name: 'Git 设置', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
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
  const probe=probeFor(page);
  await expect.poll(()=>probe.received.some(v=>v.type==='reply'&&probe.calls.some(c=>c.id===v.id&&c.method==='sync'))).toBe(true);
  probe.holdType='patch';
  const st=(await (await page.request.get('/api/snapshot')).json()).state;
  const projectId=Object.keys(st.projects)[0];
  await page.request.post('/api/commands',{data:{operationId:'held-update',type:'add_topic',revision:st.revision,structureRevision:st.structureRevision,projectId,name:'晚到更新'},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});
  await expect.poll(()=>probe.held.length).toBeGreaterThan(0);
  await page.getByRole('button',{name:'退出',exact:true}).click();
  await expect(page.getByLabel('本机访问令牌')).toBeVisible();
  releaseFrames(page);
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
  const probe=probeFor(page);probe.dropMethod='commands';
  await page.locator("#draft").fill("回执丢失仍只发送一次");
  await page.locator("#send").click();
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：回执丢失仍只发送一次",
  );
  await expect(page.locator("#draft")).toHaveValue("");
  expect(probe.calls.filter(r=>r.method==='commands')).toHaveLength(1);
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
  const probe=probeFor(page);probe.holdMethod='commands';
  await page.locator("#draft").fill("旧身份的消息");
  await page.locator("#send").click();
  await expect.poll(()=>probe.held.length).toBeGreaterThan(0);
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  await page.locator("#draft").fill("新身份的新草稿");
  releaseFrames(page);
  await expect(page.locator("#draft")).toHaveValue("新身份的新草稿");
});

test('opening conversations shows latest messages without interrupting manual reading', async ({ page }) => {
  await authenticateResultTest(page);
  const headers = { Origin: 'http://127.0.0.1:17331', 'X-Kaiwu-Client': 'web' };
  const command = async (body: any) => {
    const state = (await (await page.request.get('/api/snapshot')).json()).state;
    const response = await page.request.post('/api/commands', { headers, data: { ...body, revision: state.revision, operationId: crypto.randomUUID() } });
    expect(response.ok(), await response.text()).toBe(true);
    return response.json();
  };
  const seed = readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8');
  const existing = (await (await page.request.get('/api/snapshot')).json()).state;
  const existingTask = Object.values(existing.tasks).find((t: any) => t.title === '任务 A') as any;
  // Reuse the suite's conversation so a view-only test does not add projects that
  // later tests may accidentally choose as the default and occupy extra sessions.
  const projectId = existingTask?.projectId ?? (await command({ type: 'add_project', name: '滚动验证', path: seed + '-scroll' })).createdId;
  const taskId = existingTask?.id ?? (await command({ type: 'add_task', projectId, title: '滚动验证任务', request: '仅使用合成消息，不启动模型' })).createdId;
  const state = (await (await page.request.get('/api/snapshot')).json()).state;
  const taskConversationId = state.tasks[taskId].conversationId;
  const conversationIds = [state.projects[projectId].conversationId, taskConversationId];
  const history = Array.from({ length: 80 }, (_, i) => ({ id: 'scroll-' + i, role: 'assistant', delivery: 'complete', text: ('历史记录 ' + i + '\n\n').repeat(5) }));
  let streamId = '', revision = 0;
  const transform = (data: any) => {
    for (const id of conversationIds) data.state.conversations[id].messages = history;
    if (data.streamId) { streamId = data.streamId; revision = data.state.revision; }
  };
  probeFor(page).transform = transform;
  // This view-only fixture owns subsequent message events; the server state is not changed.
  probeFor(page).holdType = 'patch';
  await page.route('**/api/snapshot', async route => {
    const response = await route.fetch(), data = await response.json();
    transform(data);
    await route.fulfill({ response, json: data });
  });
  const patch = (ops: any[]) => {
    const after = revision++;
    probeFor(page).deliver!({ type: 'patch', streamId, after, revision, ops: [...ops, { op: 'replace', path: ['revision'], value: revision }] });
  };
  const messages = page.locator('#messages');
  const atBottom = () => messages.evaluate(el => el.scrollHeight - el.clientHeight - el.scrollTop);
  const expectLatest = async () => {
    await expect(messages).toBeVisible();
    expect(await messages.evaluate(el => el.scrollHeight - el.clientHeight)).toBeGreaterThan(1000);
    await expect.poll(atBottom).toBeLessThan(2);
  };
  const readEarlier = () => messages.evaluate(el => { el.scrollTop = 300; el.dispatchEvent(new Event('scroll')); });
  const path = '/projects/' + projectId + '/tasks/' + taskId;
  await page.goto(path);
  await expect(messages).toContainText('历史记录 79');
  await expectLatest(); // Initial mount with already-loaded history, not an empty chat.
  await expect.poll(() => !!streamId && !!probeFor(page).deliver).toBe(true);
  const slightlyAboveBottom = await messages.evaluate(el => { el.scrollTop -= 30; return el.scrollTop; });
  patch([{ op: 'add', path: ['conversations', taskConversationId, 'messages', '80'], value: { id: 'scroll-new', role: 'assistant', delivery: 'complete', text: '新的回复' } }]);
  await expect(messages.locator('.message').last()).toContainText('新的回复');
  await expect.poll(() => messages.evaluate(el => el.scrollTop)).toBe(slightlyAboveBottom);
  await readEarlier();
  patch([{ op: 'append', path: ['conversations', taskConversationId, 'messages', '80', 'text'], value: '\n\n' + '上翻时仍在回复\n\n'.repeat(20) }]);
  await expect(messages.locator('.message').last()).toContainText('上翻时仍在回复');
  await expect.poll(() => messages.evaluate(el => el.scrollTop)).toBe(300);
  await messages.evaluate(el => { el.scrollTop = el.scrollHeight; });
  patch([{ op: 'append', path: ['conversations', taskConversationId, 'messages', '80', 'text'], value: '\n\n' + '流式增加内容\n\n'.repeat(20) }]);
  await expect(messages.locator('.message').last()).toContainText('流式增加内容');
  await expectLatest(); // Returning to the bottom enables following again.
  await readEarlier();
  await page.getByRole('button', { name: '← 返回项目对话' }).click();
  await expectLatest();
  await page.getByRole('button', { name: '选择任务 ' + state.tasks[taskId].title, exact: true }).click();
  await expectLatest();
  await readEarlier();
  await page.reload();
  await expectLatest();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: '◌ 聊天', exact: true }).click();
  await expectLatest();
  await readEarlier();
  await page.getByRole('button', { name: '▦ 看板', exact: true }).click();
  patch([{ op: 'add', path: ['conversations', taskConversationId, 'messages', '80'], value: { id: 'scroll-hidden', role: 'assistant', delivery: 'complete', text: '聊天隐藏期间的新回复' } }]);
  await page.getByRole('button', { name: '◌ 聊天', exact: true }).click();
  await expect(messages.locator('.message').last()).toContainText('聊天隐藏期间的新回复');
  await expectLatest();
  await page.reload();
  await page.getByRole('button', { name: '◌ 聊天', exact: true }).click();
  await expectLatest();
});

test("Enter sends once, Shift+Enter adds a newline, and IME confirmation preserves the draft", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  await expect(page.locator("#chat-title")).toBeVisible();
  const count=()=>probeFor(page).calls.filter(r=>r.method==='commands'&&r.body.type==='send_message').length;
  const draft = page.locator("#draft");
  await draft.fill("组合输入");
  await draft.dispatchEvent("keydown", {
    key: "Enter",
    ctrlKey: true,
    isComposing: true,
  });
  await draft.dispatchEvent("keydown", {key:"Enter",isComposing:true});
  await draft.dispatchEvent("keydown", {key:"Enter",keyCode:229});
  expect(count()).toBe(0);
  await expect(draft).toHaveValue("组合输入");
  await draft.press("Shift+Enter");
  await expect(draft).toHaveValue("组合输入\n");
  expect(count()).toBe(0);
  await draft.press("Enter");
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：组合输入",
  );
  expect(count()).toBe(1);
});

test("consecutive progress updates show one useful summary and expand without losing history", async ({page,context})=>{
 await enterStart(page,context);
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const project=Object.values(state.projects)[0] as any;
 const lastReply='## 接口已完成\n\n- 返回 hello world\n- 已核对局域网访问\n\n访问地址：http://192.168.2.122:8000/hello\n\n'+('详细记录：接口返回 hello world。\n'.repeat(80));
 const transform=(data:any)=>{const c=data.state?.conversations[project.conversationId];if(!c)return;c.runState='idle';c.rounds=[];c.messages=[
  {id:'summary-user',role:'user',text:'开发一个 hello world 接口',delivery:'complete'},
  {id:'summary-progress-1',role:'assistant',kind:'progress',text:'先检查当前服务的启动方式。',delivery:'complete'},
  {id:'summary-progress-2',role:'assistant',kind:'progress',text:'已添加接口，准备验证响应。',delivery:'complete'},
  {id:'summary-progress-3',role:'assistant',kind:'progress',text:'<kaiwu-report>'+JSON.stringify({summary:'正在核对 /hello 接口的返回内容。',stage:'verifying',plan:[],results:[],checks:[],blockers:[],next:[],question:''})+'</kaiwu-report>',displayText:'',delivery:'complete'},
  {id:'summary-reply',role:'assistant',text:lastReply,delivery:'complete'},
  {id:'summary-user-2',role:'user',text:'继续检查边界',delivery:'complete'},
  {id:'summary-progress-4',role:'assistant',kind:'progress',text:'<kaiwu-report>'+JSON.stringify({version:1,summary:'正在检查空路径和错误请求。',stage:'developing',plan:[],results:[],checks:[],blockers:[],next:[],question:''})+'</kaiwu-report>',displayText:'',delivery:'complete'}
 ]};
 probeFor(page).transform=transform;
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch(),data=await response.json();transform(data);await route.fulfill({response,json:data})});
 await page.goto('/projects/'+project.id);
 const replies=page.locator('.message.assistant');
 await expect(replies).toHaveCount(3);
 const progress=replies.first();
 await expect(progress.locator('.assistant-summary')).toHaveText('正在核对 /hello 接口的返回内容。');
 await expect(progress).toContainText('3 条更新');
 await expect(progress.getByText('先检查当前服务的启动方式。',{exact:true})).toBeHidden();
 await progress.locator('summary').first().click();
 await expect(progress.getByText('先检查当前服务的启动方式。',{exact:true})).toBeVisible();
 await expect(progress.getByText('已添加接口，准备验证响应。',{exact:true})).toBeVisible();
 const reply=replies.nth(1);
 await expect(reply.locator('.assistant-summary')).toContainText('接口已完成');
 await expect(reply.locator('.assistant-summary li')).toHaveCount(2);
 await expect(reply.getByRole('link',{name:'http://192.168.2.122:8000/hello',exact:true}).first()).toHaveAttribute('href','http://192.168.2.122:8000/hello');
 await expect(reply.getByRole('link',{name:'http://192.168.2.122:8000/hello',exact:true}).first()).toHaveAttribute('target','_blank');
 await expect(reply.locator('.assistant-detail > .markdown-content')).toBeHidden();
 await reply.locator('summary').first().click();
 await expect(reply.locator('.assistant-detail > .markdown-content')).toContainText('详细记录');
 await expect(replies.last().locator('.assistant-summary')).toHaveText('正在检查空路径和错误请求。');
 await progress.locator('summary').first().click();
 await reply.locator('summary').first().click();
 await page.setViewportSize({width:390,height:844});
 await page.getByRole('button',{name:'◌ 聊天',exact:true}).click();
 await expect(replies.last().locator('.assistant-summary')).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
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
    await openAddProject(page);
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
    await expect(page.getByRole('button', { name: '当前项目', exact: true })).toHaveText(
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
  await chooseMenuProject(page, { label: '开悟验收项目' });
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
  await expect(page.locator(".progress-state")).toContainText("未开始");
  await page.locator("#draft").fill("保留这份草稿");
  await card.getByRole("button", { name: "开始开发", exact: true }).click();
  await expect(card).toContainText("待验收");
  await expect(page.locator("#draft")).toHaveValue("保留这份草稿");
  if(await page.getByRole("button",{name:"收起或展开进展卡"}).getAttribute("aria-expanded")==="false")await page.getByRole("button",{name:"收起或展开进展卡"}).click();
  await page.getByRole("tab",{name:"验证",exact:true}).click();
  await expect(page.locator(".progress-card")).toContainText("尚未提供可核对的验证报告");
  expect(probeFor(page).calls.filter(r=>r.method==='commands'&&r.body.type==='start_task')).toHaveLength(1);
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
  await expect(card).toContainText("待验收");
  await expect(page.locator("#draft")).toHaveValue("");
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  await expect(page.locator("#draft")).toHaveValue("开始开发");
});

test("start without a model opens selection and preserves the draft", async ({
  page,
  context,
}) => {
  const transform=(data:any)=>{
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
  };
  probeFor(page).transform=transform;
  await page.route("**/api/snapshot",async route=>{const response=await route.fetch(),data=await response.json();if(data.state)transform(data);await route.fulfill({response,json:data})});
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
  const projectID = (await page.getByRole('button', { name: '当前项目', exact: true }).getAttribute('data-project-id'))!;
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
  await openAddProject(page);
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
  await expect(card).toContainText("待验收");
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
  await chooseMenuProject(page, { label: '合并验收' });
  const card = page
    .locator(".task-card")
    .filter({
      has: page.getByRole("heading", { name: "集成功能", exact: true }),
    });
  await card
    .getByRole("button", { name: "选择任务 集成功能", exact: true })
    .click();
  const probe=probeFor(page);probe.holdMethod='git/preview';
  await card.getByRole("button", { name: "合并到主分支", exact: true }).click();
  await expect.poll(() => probe.held.length).toBeGreaterThan(0);
  await page.getByRole("button", { name: "← 返回项目对话" }).click();
  releaseFrames(page);
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
  await chooseMenuProject(page, { label: '合并验收' });
  const card = page
    .locator(".task-card")
    .filter({
      has: page.getByRole("heading", { name: "集成功能", exact: true }),
    });
  await card.getByRole("button", { name: "合并到主分支", exact: true }).click();
  const probe=probeFor(page);probe.dropMethod='git/merge';
  await page.getByRole("button", { name: "提交并合并", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText("回执");
  await expect(page.locator('.connection')).toContainText('已连接');
  await page.getByRole("button", { name: "提交并合并", exact: true }).click();
  await expect.poll(()=>probe.calls.filter(r=>r.method==='git/merge').length).toBe(2);
  const ids=probe.calls.filter(r=>r.method==='git/merge').map(r=>r.body.operationId);expect(ids[1]).toBe(ids[0]);
  await expect(page.getByRole("dialog")).toContainText("合并完成");
});

test("pi models: discover, choose an empty conversation and expose next-round selection", async ({ page, context }) => {
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
  await chooseMenuProject(page, { id: created });
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
  await expect(page.getByRole('button', { name: '选择会话模型', exact: true })).toBeEnabled();
  await page.getByRole('button', { name: '选择会话模型', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('下一轮');
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click();
});

test("pi models: errors, empty catalog and mobile selection retain draft", async ({ page, context }) => {
  await enterStart(page, context);
  await page.setViewportSize({ width: 360, height: 900 });
  probeFor(page).modelMode='error';
  await page.getByRole('button', { name: '默认模型', exact: true }).click();
  const picker = page.getByRole('dialog', { name: '默认模型', exact: true });
  await expect(picker).toContainText('pi 检测暂时失败');
  await expect(picker.getByRole('button', { name: '保存模型' })).toBeDisabled();
  probeFor(page).modelMode='empty';
  await picker.getByRole('button', { name: '重新检测' }).click();
  await expect(picker).toContainText('pi 尚无可用模型');
  probeFor(page).modelMode='';
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

test('structured progress uses bidirectional socket and keeps verification evidence separate', async ({page,context})=>{
 const fixturePath=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const cookieFile=resolve(fixturePath,'..','browser-session.json');
 if(existsSync(cookieFile))await context.addCookies(JSON.parse(readFileSync(cookieFile,'utf8')));
 else await authenticateResultTest(page);
 const snapshot=(await (await page.request.get('/api/snapshot')).json()).state;
 if(!Object.keys(snapshot.projects).length){
  const path=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
  await page.request.post('/api/commands',{data:{type:'add_project',operationId:'progress-fixture-project',revision:snapshot.revision,name:'进度测试',path},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});
 }
 await page.goto('/');await expect(page.locator('#chat-title')).toBeVisible();
 const requests:string[]=[];page.on('request',r=>{if(r.url().includes('/api/commands'))requests.push(r.url())});
 await page.locator('[data-action=new-task]').click();
 await page.locator('input[name=title]').fill('结构化进展验收');
 await page.locator('textarea[name=request]').fill('结构化登录恢复');
 await page.getByRole('button',{name:'保存',exact:true}).click();
 const card=page.locator('.task-card').filter({has:page.getByRole('heading',{name:'结构化进展验收',exact:true})});
 await card.getByRole('button',{name:'开始开发',exact:true}).click();
 await expect(page.getByRole('tab',{name:/成果/})).toBeVisible();
 await expect(page.locator('.progress-card')).toContainText('刷新自动恢复');
 await page.getByRole('tab',{name:'验证',exact:true}).click();
 await expect(page.locator('.progress-card')).toContainText('AI 报告通过');
 await expect(page.locator('.progress-card')).toContainText('命令完成');
 await page.getByRole('tab',{name:'记录',exact:true}).click();
 await expect(page.locator('.progress-card')).toContainText('bash');
 await page.getByRole('tab',{name:/成果/}).click();
 await expect(page.getByRole('button',{name:'确认完成',exact:true})).toBeVisible();
 await page.setViewportSize({width:1440,height:900});
 await page.screenshot({path:resolve('../../docs/design/web-service/progress-desktop.png')});
 await page.setViewportSize({width:390,height:844});
 await page.getByRole('button',{name:'◌ 聊天',exact:true}).click();
 await expect(page.locator('#draft')).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.screenshot({path:resolve('../../docs/design/web-service/progress-mobile.png')});
 await page.setViewportSize({width:1440,height:900});
 const before=(await (await page.request.get('/api/snapshot')).json()).state;
 const t=Object.values(before.tasks).find((t:any)=>t.title==='结构化进展验收') as any;
 await page.getByRole('button',{name:'确认完成',exact:true}).click();
 await expect(page.locator('.progress-card')).toContainText('已完成');
 const after=(await (await page.request.get('/api/snapshot')).json()).state;
 expect(after.tasks[t.id].status).toBe('completed');
 expect(after.taskGit?.[t.id]?.state).toBe(before.taskGit?.[t.id]?.state);
 await page.locator('#draft').fill('继续改进结构化登录恢复');await page.locator('#send').click();
 await expect(page.getByLabel('查看历史轮次')).toBeVisible();
 await expect(page.getByRole('button',{name:'确认完成',exact:true})).toBeVisible();
 await page.getByLabel('查看历史轮次').selectOption(before.conversations[t.conversationId].rounds[0].id);
 await expect(page.locator('.progress-card')).toContainText('历史轮次');
 await expect(page.getByRole('button',{name:'确认完成',exact:true})).toHaveCount(0);
 expect(requests).toEqual([]);
});

test('new messages and continue icon restore interrupted task automatically without a resume command',async({page,context})=>{
 const fixturePath=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const cookieFile=resolve(fixturePath,'..','browser-session.json');
 if(existsSync(cookieFile))await context.addCookies(JSON.parse(readFileSync(cookieFile,'utf8')));
 else await authenticateResultTest(page);
 const fixtureCommand=async(body:any)=>{const st=(await (await page.request.get('/api/snapshot')).json()).state;const response=await page.request.post('/api/commands',{data:{...body,revision:st.revision},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});expect(response.ok()).toBe(true);return response.json()};
 let st=(await (await page.request.get('/api/snapshot')).json()).state;
 if(!Object.keys(st.projects).length)await fixtureCommand({operationId:'auto-ui-project',type:'add_project',name:'自动接回验证',path:fixturePath});
 st=(await (await page.request.get('/api/snapshot')).json()).state;const projectId=Object.keys(st.projects)[0];
 const taskID=(await fixtureCommand({operationId:'auto-ui-task',type:'add_task',projectId,title:'自动接回会话',request:'固定协议测试文件'})).createdId;
 await page.goto('/');await chooseMenuProject(page, { id: projectId });
 const card=page.locator('.task-card[data-taskid="'+taskID+'"]');await card.getByRole('button',{name:'开始开发',exact:true}).click();await expect(card).toContainText('待验收');
 await page.locator('#draft').fill('__DISCONNECT_FIXTURE__');await page.locator('#send').click();await expect(card).toContainText('已中断');
 const before=(await (await page.request.get('/api/snapshot')).json()).state,t=before.tasks[taskID],c=before.conversations[t.conversationId],x=before.executions[t.executionId];
 await expect(page.getByRole('button',{name:'恢复原会话',exact:true})).toHaveCount(0);
 await page.locator('#draft').fill('接着执行新的明确要求');await expect(page.locator('#send')).toBeEnabled();await page.locator('#send').click();
 await expect(page.locator('#messages')).toContainText('[隔离协议测试] 已收到：接着执行新的明确要求');await expect(card).toContainText('待验收');
 let after=(await (await page.request.get('/api/snapshot')).json()).state;
 expect(after.tasks[taskID].executionId).toBe(t.executionId);expect(after.executions[t.executionId].workspace).toBe(x.workspace);expect(after.conversations[c.id].sessionFile).toBe(c.sessionFile);expect(after.conversations[c.id].sessionId).toBe(c.sessionId);
 expect(after.conversations[c.id].messages.filter((m:any)=>m.role==='user'&&m.text==='接着执行新的明确要求')).toHaveLength(1);
 await page.locator('#draft').fill('__DISCONNECT_FIXTURE__');await page.locator('#send').click();await expect(card).toContainText('已中断');
 await card.getByRole('button',{name:'继续开发',exact:true}).click();await expect(card).toContainText('待验收');
 after=(await (await page.request.get('/api/snapshot')).json()).state;
 expect(after.tasks[taskID].executionId).toBe(t.executionId);expect(after.conversations[c.id].messages.filter((m:any)=>m.role==='user'&&m.text==='__DISCONNECT_FIXTURE__')).toHaveLength(2);
 expect(probeFor(page).calls.filter(r=>r.method==='commands'&&r.body.type==='resume')).toHaveLength(0);
});

test('legacy interrupted tasks explain automatic recovery and preserve useful mobile facts',async({page,context})=>{
 const fixturePath=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const cookieFile=resolve(fixturePath,'..','browser-session.json');
 if(existsSync(cookieFile))await context.addCookies(JSON.parse(readFileSync(cookieFile,'utf8')));
 else await authenticateResultTest(page);
 const fixtureCommand=async(body:any)=>{
  const st=(await (await page.request.get('/api/snapshot')).json()).state;
  const response=await page.request.post('/api/commands',{data:{...body,revision:st.revision},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});
  expect(response.ok()).toBe(true);return response.json();
 };
 let st=(await (await page.request.get('/api/snapshot')).json()).state;
 if(!Object.keys(st.projects).length)await fixtureCommand({operationId:'clarity-project',type:'add_project',name:'界面核对',path:fixturePath});
 st=(await (await page.request.get('/api/snapshot')).json()).state;const projectId=Object.keys(st.projects)[0];
 const receipt=await fixtureCommand({operationId:'clarity-task',type:'add_task',projectId,title:'中断任务核对',request:'实现 /hello 接口，返回 hello world'});
 const id=receipt.createdId;
 const transform=(data:any)=>{
  const t=data.state.tasks[id];if(!t)return;t.status='needs_confirmation';t.executionId='legacy-clarity';
  const c=data.state.conversations[t.conversationId];c.runState='interrupted';c.error='pi 会话已中断，请明确恢复；历史已保留';c.rounds=[];
  c.messages=[{id:'legacy-user',role:'user',text:'开始开发',delivery:'uncertain'},{id:'legacy-assistant',role:'assistant',text:'已创建 /hello 接口，正在验证启动，尚未收到完整结果。',delivery:'uncertain'}];
  data.state.executions??={};data.state.executions[t.executionId]={id:t.executionId,phase:'awaiting_review',tools:[{id:'write',name:'write',result:'saved'},{id:'bash',name:'bash'}]};
  data.state.taskGit??={};data.state.taskGit[id]={taskId:id,state:'uncommitted',mainBranch:'main',dirtyFiles:[{path:'main.go',status:'M'}],canMerge:false};
 };
 probeFor(page).transform=transform;
 // This view-only legacy fixture stays stable while the real server observes its own Git state.
 probeFor(page).holdType='patch';
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch(),data=await response.json();if(data.state)transform(data);await route.fulfill({response,json:data})});
 await page.setViewportSize({width:390,height:844});await page.goto('/?project='+projectId);
 await chooseMenuProject(page, { id: projectId });
 // The fixture task may be in the protected topic on a narrow screen.
 const fresh=(await (await page.request.get('/api/snapshot')).json()).state;
 await page.getByLabel('当前看板专题').selectOption(fresh.tasks[id].topicId);
 const card=page.locator('.task-card[data-taskid="'+id+'"]');await expect(card).toContainText('已中断');
 await card.getByRole('button',{name:'选择任务 中断任务核对',exact:true}).click();
 await expect(page.getByRole('button',{name:'恢复原会话',exact:true})).toHaveCount(0);
 await expect(page.locator('.auto-recovery')).toContainText('尝试接回原会话');
 await expect(page.getByRole('button',{name:'确认完成',exact:true})).toHaveCount(0);
 await expect(page.locator('.chat-head').getByRole('button',{name:'继续开发',exact:true})).toBeVisible();
 await expect(page.locator('.progress-panel')).toContainText('实现 /hello 接口');
 await expect(page.locator('.progress-panel')).toContainText('已创建 /hello 接口');
 await expect(page.locator('.progress-panel')).toContainText('1 条已返回');
 await expect(page.locator('.progress-panel')).toContainText('1 条未结束');
 await expect(page.locator('.progress-panel')).toContainText('1 个文件待提交');
 await expect(page.locator('.progress-panel')).not.toContainText('开始任务后');
 expect(await page.locator('#messages').evaluate(el=>el.clientHeight)).toBeGreaterThanOrEqual(140);
 await page.locator('#draft').fill('继续处理新的要求');await expect(page.locator('#send')).toBeEnabled();
 await page.screenshot({path:resolve('../../docs/design/web-service/task-recovery-mobile.png')});
 await page.getByRole('button',{name:'收起或展开进展卡'}).click();
 await page.getByRole('tab',{name:'记录',exact:true}).click();
 await expect(page.locator('.progress-card')).toContainText('write');
 expect(probeFor(page).calls.filter(r=>r.method==='commands')).toHaveLength(0);
});

test('result review opens a result view and completion remains visible after refresh',async({page})=>{
 const headers={Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'};
 await authenticateResultTest(page);
 const command=async(body:any)=>{const state=(await(await page.request.get('/api/snapshot')).json()).state;const response=await page.request.post('/api/commands',{headers,data:{...body,revision:state.revision}});expect(response.ok()).toBe(true);return response.json()};
 let state=(await(await page.request.get('/api/snapshot')).json()).state;
 let projectId=Object.keys(state.projects)[0];
 if(!projectId){projectId=(await command({type:'add_project',operationId:'review-project',name:'成果验收',path:readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8')})).createdId}
 const taskId=(await command({type:'add_task',operationId:'review-task',projectId,title:'成果预览验收',request:'结构化成果预览验收'})).createdId;
 await command({type:'start_task',operationId:'review-start',projectId,taskId});
 await page.goto('/projects/'+projectId+'/tasks/'+taskId);
 await expect(page.getByRole('button',{name:'查看效果',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'查看效果',exact:true}).click();
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toBeVisible();
 await page.getByRole('button',{name:'确认完成',exact:true}).click();
 await expect(page.locator('.task-completed')).toContainText('你已确认本轮结果');
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toBeVisible();
 await expect(page.getByRole('button',{name:'确认完成',exact:true})).toHaveCount(0);
 const disconnected=(data:any)=>{const t=data.state?.tasks[taskId];if(t){const c=data.state.conversations[t.conversationId];c.runState='interrupted';c.error='pi 会话已中断，发送下一条消息时自动接回；历史已保留'}};
 probeFor(page).transform=disconnected;
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch(),data=await response.json();disconnected(data);await route.fulfill({response,json:data})});
 await page.reload();
 await expect(page.locator('.task-completed')).toBeVisible();
 await expect(page.locator('.auto-recovery')).toHaveCount(0);
 await expect(page.locator('.task-completed')).toContainText('合并到主分支');
 await page.getByRole('button',{name:'继续修改',exact:true}).click();
 await expect(page.locator('#draft')).toBeFocused();
 await page.setViewportSize({width:390,height:844});
 await page.getByRole('button',{name:'◌ 聊天',exact:true}).click();
 await page.getByRole('button',{name:'查看效果',exact:true}).click();
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toBeVisible();
 await page.getByRole('button',{name:'返回任务对话',exact:true}).click();
 await expect(page.locator('#draft')).toBeVisible();
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});


async function resultFixture(page:Page, label:string, webpage=false, run=true){
 const headers={Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'};
 const base=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const path=resolve(base,'..','preview-'+label);
 mkdirSync(path,{recursive:true});
 if(webpage){
  writeFileSync(resolve(path,'kaiwu-preview.json'),JSON.stringify({version:1,command:['python3','preview.py','--port','{port}'],path:'/'}));
  writeFileSync(resolve(path,'preview.py'),`
import argparse
from http.server import BaseHTTPRequestHandler, HTTPServer
p=argparse.ArgumentParser();p.add_argument('--port',type=int);a=p.parse_args()
class Handler(BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path.startswith('/api/hello'):
   body=b'API OK';content='text/plain'
  else:
   body="""<!doctype html><meta charset=utf-8><h1>真实任务预览</h1><button id=ping>测试接口</button><p id=result></p><script>document.getElementById('ping').onclick=async()=>{document.getElementById('result').textContent=await(await fetch('api/hello')).text()};try{parent.document.body;document.body.dataset.parentAccess='allowed'}catch{document.body.dataset.parentAccess='blocked'}</script>""".encode();content='text/html; charset=utf-8'
  self.send_response(200);self.send_header('Content-Type',content);self.end_headers();self.wfile.write(body)
HTTPServer(('127.0.0.1',a.port),Handler).serve_forever()
`);
 }
 const command=async(body:any)=>{const st=(await(await page.request.get('/api/snapshot')).json()).state;const response=await page.request.post('/api/commands',{headers,data:{...body,revision:st.revision}});expect(response.ok()).toBe(true);return response.json()};
 const projectId=(await command({type:'add_project',operationId:'preview-project-'+label,name:'预览项目 '+label,path})).createdId;
 const taskId=(await command({type:'add_task',operationId:'preview-task-'+label,projectId,title:'预览任务 '+label,request:'结构化效果预览验收'})).createdId;
 if(run)await command({type:'start_task',operationId:'preview-run-'+label,projectId,taskId});
 return {projectId,taskId};
}

test('a real task HTTP preview is isolated, remains after acceptance, and stops explicitly',async({page})=>{
 await authenticateResultTest(page);
 const {projectId,taskId}=await resultFixture(page,'http',true);
 await page.goto('/projects/'+projectId+'/tasks/'+taskId);
 await page.getByRole('button',{name:'查看效果',exact:true}).click();
 const pane=page.getByRole('region',{name:'任务成果与效果'});
 await expect(pane.getByRole('button',{name:'启动预览',exact:true})).toBeVisible();
 await expect(pane).toContainText('preview.py');
 // External edits after display must not turn this click into approval of a
 // different command. The changed command has a visible filesystem side effect.
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const tree=state.executions[state.tasks[taskId].executionId].workspace;
 const manifestFile=resolve(tree,'kaiwu-preview.json'),shown=readFileSync(manifestFile,'utf8');
 writeFileSync(resolve(tree,'unapproved.py'),"from pathlib import Path\nPath('preview-unapproved').touch()\n");
 writeFileSync(manifestFile,JSON.stringify({version:1,command:['python3','unapproved.py','{port}'],path:'/'}));
 await pane.getByRole('button',{name:'启动预览',exact:true}).click();
 await expect(pane.getByRole('alert')).toContainText(/配置|指纹|刷新/);
 expect(existsSync(resolve(tree,'preview-unapproved'))).toBe(false);
 writeFileSync(manifestFile,shown);
 await pane.getByRole('button',{name:'刷新状态',exact:true}).click();
 await pane.getByRole('button',{name:'启动预览',exact:true}).click();
 const frame=page.frameLocator('iframe[title="任务效果"]');
 await expect(frame.getByRole('heading',{name:'真实任务预览'})).toBeVisible();
 await expect(page.locator('iframe[title="任务效果"]')).toHaveAttribute('sandbox','allow-scripts');
 expect(await frame.locator('body').getAttribute('data-parent-access')).toBe('blocked');
 await frame.getByRole('button',{name:'测试接口'}).click();
 await expect(frame.locator('#result')).toHaveText('API OK');
 await page.getByRole('button',{name:'确认完成',exact:true}).click();
 await expect(page.locator('.task-completed')).toBeVisible();
 await expect(frame.getByRole('heading',{name:'真实任务预览'})).toBeVisible();
 await page.screenshot({path:resolve('../../docs/design/web-service/task-result-desktop.png')});
 await page.setViewportSize({width:390,height:844});
 await expect(pane).toBeVisible();
 await pane.getByRole('button',{name:'返回任务对话'}).click();
 await expect(page.locator('#draft')).toBeVisible();
 await page.screenshot({path:resolve('../../docs/design/web-service/task-result-mobile.png')});
 await page.getByRole('button',{name:'查看效果',exact:true}).click();
 await pane.getByRole('button',{name:'停止预览',exact:true}).click();
 await expect(pane).toContainText('服务已停止');
 await expect(page.locator('iframe[title="任务效果"]')).toHaveCount(0);
 expect(probeFor(page).calls.filter(r=>r.method==='previews/start')).toHaveLength(2);
});

test('late preview responses stay with their original task and absent previews do not start a model',async({page})=>{
 await authenticateResultTest(page);
 const a=await resultFixture(page,'late-a');const b=await resultFixture(page,'late-b',false,false);
 await page.goto('/projects/'+a.projectId+'/tasks/'+a.taskId);
 await expect(page.getByRole('button',{name:'查看效果',exact:true})).toBeVisible();
 probeFor(page).holdMethod='previews/status';
 await page.getByRole('button',{name:'查看效果',exact:true}).click();
 await expect.poll(()=>probeFor(page).held.length).toBeGreaterThan(0);
 await chooseMenuProject(page, { id: b.projectId });
 await page.locator('.task-card[data-taskid="'+b.taskId+'"]').click();
 releaseFrames(page);
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toHaveCount(0);
 await expect(page.locator('#chat-title')).toContainText('预览任务 late-b');
 await chooseMenuProject(page, { id: a.projectId });
 await page.locator('.task-card[data-taskid="'+a.taskId+'"]').click();
 await page.getByRole('button',{name:'查看效果',exact:true}).click();
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toContainText('暂无网页预览');
 const sendsBefore=probeFor(page).calls.filter(r=>r.method==='commands'&&r.body?.type==='send_message').length;
 await page.getByRole('button',{name:'补充预览说明',exact:true}).click();
 await expect(page.locator('#draft')).toHaveValue(/kaiwu-preview.json/);
 expect(probeFor(page).calls.filter(r=>r.method==='previews/start')).toHaveLength(0);
 expect(probeFor(page).calls.filter(r=>r.method==='commands'&&r.body?.type==='send_message')).toHaveLength(sendsBefore);
});



async function projectMenuFixture(page: Page) {
  await authenticateResultTest(page);
  const seed = readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8');
  const ids: string[] = [];
  for (const name of ['abc', '这是一个名称很长的项目用于验证窄屏省略与菜单完整名称']) {
    const state = (await (await page.request.get('/api/snapshot')).json()).state;
    const existing = Object.values(state.projects).find((p: any) => p.name === name) as any;
    if (existing) { ids.push(existing.id); continue; }
    const response = await page.request.post('/api/commands', {
      data: { type: 'add_project', operationId: crypto.randomUUID(), revision: state.revision, name, path: seed + '-menu-' + crypto.randomUUID() },
      headers: { Origin: 'http://127.0.0.1:17331', 'X-Kaiwu-Client': 'web' },
    });
    expect(response.ok(), await response.text()).toBe(true);
    ids.push((await response.json()).createdId);
  }
  await page.goto('/projects/' + ids[0]);
  return ids;
}

test('project menu keeps project actions together and opens the existing dialogs', async ({ page }) => {
  await projectMenuFixture(page);
  const trigger = page.getByRole('button', { name: '当前项目', exact: true });
  await expect(trigger).toContainText('abc');
  await expect(trigger).toHaveAttribute('aria-expanded', 'false');
  await trigger.click();
  const menu = page.getByRole('menu', { name: '项目菜单' });
  await expect(menu.getByRole('menuitemradio', { name: 'abc', exact: true })).toHaveAttribute('aria-checked', 'true');
  await menu.getByRole('menuitem', { name: 'Git 设置', exact: true }).click();
  await expect(menu).toHaveCount(0);
  await expect(page.getByRole('dialog')).toContainText('主分支');
  await page.getByRole('button', { name: '取消', exact: true }).click();
  await trigger.click();
  const refreshCount = probeFor(page).calls.filter(c => c.method === 'git/refresh').length;
  await menu.getByRole('menuitem', { name: '刷新 Git 状态', exact: true }).click();
  await expect(menu).toHaveCount(0);
  await expect.poll(() => probeFor(page).calls.filter(c => c.method === 'git/refresh').length).toBeGreaterThan(refreshCount);
  await trigger.click();
  await menu.getByRole('menuitem', { name: '添加项目', exact: true }).click();
  await expect(menu).toHaveCount(0);
  await expect(page.getByRole('dialog')).toContainText('项目绝对路径');
});

test('project menu keyboard selection preserves scope drafts and dismisses predictably', async ({ page }) => {
  const ids = await projectMenuFixture(page);
  const trigger = page.getByRole('button', { name: '当前项目', exact: true });
  const menu = page.getByRole('menu', { name: '项目菜单' });
  await page.locator('#draft').fill('第一个项目的草稿');
  await trigger.focus();
  await trigger.press('ArrowDown');
  await expect(menu.getByRole('menuitemradio', { name: 'abc', exact: true })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await trigger.press('Enter');
  await expect(menu).toBeVisible();
  const options = menu.getByRole('menuitemradio');
  const targetIndex = await options.evaluateAll((els, id) => els.findIndex(el => el.getAttribute('data-project-id') === id), ids[1]);
  await page.keyboard.press('Home');
  for (let i = 0; i < targetIndex; i++) await page.keyboard.press('ArrowDown');
  await expect(options.nth(targetIndex)).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(trigger).toHaveAttribute('data-project-id', ids[1]);
  await expect(page.locator('#draft')).toHaveValue('');
  await trigger.click();
  await menu.getByRole('menuitemradio', { name: 'abc', exact: true }).click();
  await expect(page.locator('#draft')).toHaveValue('第一个项目的草稿');
  await trigger.click();
  await page.locator('.toolbar h1').click();
  await expect(menu).toHaveCount(0);
  await trigger.focus();
  await trigger.press('ArrowUp');
  await expect(menu.getByRole('menuitem', { name: '添加项目', exact: true })).toBeFocused();
  await page.keyboard.press('Home');
  await expect(menu.getByRole('menuitemradio').first()).toBeFocused();
  await page.keyboard.press('End');
  await expect(menu.getByRole('menuitem', { name: '添加项目', exact: true })).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(menu).toHaveCount(0);
});

test('project menu reselecting the current project retains its task and draft', async ({ page }) => {
  const ids = await projectMenuFixture(page);
  const state = (await (await page.request.get('/api/snapshot')).json()).state;
  const response = await page.request.post('/api/commands', {
    data: { type: 'add_task', projectId: ids[0], title: '菜单任务上下文', request: '不执行模型', operationId: crypto.randomUUID(), revision: state.revision },
    headers: { Origin: 'http://127.0.0.1:17331', 'X-Kaiwu-Client': 'web' },
  });
  expect(response.ok(), await response.text()).toBe(true);
  const taskId = (await response.json()).createdId;
  await page.goto('/projects/' + ids[0] + '/tasks/' + taskId);
  await page.locator('#draft').fill('保留当前任务的草稿');
  await chooseMenuProject(page, { id: ids[0]! });
  await expect(page).toHaveURL(new RegExp('/tasks/' + taskId + '$'));
  await expect(page.locator('#chat-title')).toContainText('菜单任务上下文');
  await expect(page.locator('#draft')).toHaveValue('保留当前任务的草稿');
});

test('project menu stays usable with long names on desktop and mobile', async ({ page }) => {
  const ids = await projectMenuFixture(page);
  await page.goto('/projects/' + ids[1]);
  const trigger = page.getByRole('button', { name: '当前项目', exact: true });
  for (const width of [1440, 768, 390, 360, 320]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(trigger).toHaveAttribute('data-project-id', ids[1]);
    const header = await page.locator('.top').boundingBox();
    const model = await page.getByRole('button', { name: '默认模型', exact: true }).boundingBox();
    expect(model!.x).toBeGreaterThan(width / 2);
    expect(header!.height).toBeLessThanOrEqual(52);
    await trigger.click();
    const menu = page.getByRole('menu', { name: '项目菜单' });
    await expect(menu).toBeVisible();
    const box = await menu.boundingBox();
    expect(box!.x).toBeGreaterThanOrEqual(8);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width - 8);
    await expect(menu.getByRole('menuitem', { name: '添加项目', exact: true })).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width === 1440 || width === 390) await page.screenshot({ path: resolve('../../docs/design/web-service/project-menu-' + (width === 1440 ? 'desktop' : 'mobile') + '.png') });
    await page.keyboard.press('Escape');
  }
});

test('revoked login during socket disconnection returns to login after failed upgrade',async({browser})=>{
 const fixturePath=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const context=await browser.newContext({storageState:{cookies:JSON.parse(readFileSync(resolve(fixturePath,'..','browser-session.json'),'utf8')),origins:[]}});
 await context.addInitScript(()=>{
  const Native=window.WebSocket;
  (window as any).kaiwuSockets=[];
  window.WebSocket=class extends Native{constructor(url:string|URL,protocols?:string|string[]){super(url,protocols);(window as any).kaiwuSockets.push(this)}};
 });
 const page=await context.newPage();await page.goto('http://127.0.0.1:17331/');await expect(page.locator('#chat-title')).toBeVisible();
 await expect.poll(()=>page.evaluate(()=>(window as any).kaiwuSockets.some((s:WebSocket)=>s.readyState===1))).toBe(true);
 // Close without the server's 1008 frame, after another client revokes the cookie.
 await page.request.post('/api/logout',{data:{},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});
 await page.evaluate(()=>(window as any).kaiwuSockets.forEach((s:WebSocket)=>s.close()));
 await expect(page.getByLabel('本机访问令牌')).toBeVisible();
 await expect(page.getByText('登录已失效，请重新输入令牌')).toBeVisible();
 await context.close();
});

async function authenticateResultTest(page:Page){
 const fixture=readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'),'utf8');
 const cookies=resolve(fixture,'..','browser-session.json');
 if(existsSync(cookies))await page.context().addCookies(JSON.parse(readFileSync(cookies,'utf8')));
 if(!(await page.request.get('/api/session')).ok()){
  const login=await page.request.post('/api/login',{data:{token:'kaiwu-browser-test-token-00000000'},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});
  expect(login.ok(),await login.text()).toBe(true);
 }
}

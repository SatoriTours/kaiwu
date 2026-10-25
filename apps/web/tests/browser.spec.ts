import { clickInView, closeChat, fillInChat, openChat } from './view-navigation';
import { test, expect } from "@playwright/test";
import type { Page, BrowserContext, Cookie } from "@playwright/test";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";
// Intercept real WebSocket frames; all business requests still reach the real Go server.
const socketProbes = new WeakMap<Page, {calls:any[];received:any[];held:(()=>void)[];holdMethod:string;holdType:string;dropMethod:string;modelMode:string;missingReceipt?:boolean;transform?:(v:any)=>void;deliver?:(v:any)=>void}>();
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
   if(v.type==='reply'&&method?.startsWith('git/operations/')&&(probe as {missingReceipt?:boolean}).missingReceipt){v.status=404;v.data={error:{code:'not_found',message:'fixture receipt temporarily unavailable'}};client.send(JSON.stringify(v));return}
   client.send(raw);
  });
 });
});
async function chooseMenuProject(page: Page, target: { label: string } | { id: string }) {
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '当前项目', exact: true }));
  const menu = page.getByRole('menu', { name: '项目菜单' });
  const item = 'label' in target
    ? menu.getByRole('menuitemradio', { name: target.label, exact: true })
    : menu.locator('[role=menuitemradio][data-project-id="' + target.id + '"]');
  await clickInView(page, item);
}
async function openAddProject(page: Page) {
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '当前项目', exact: true }));
  await clickInView(page, page.getByRole('menuitem', { includeHidden: true, name: '添加项目', exact: true }));
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '当前项目', exact: true }));
  const emptyMenu = page.getByRole('menu', { name: '项目菜单' });
  await expect(emptyMenu).toContainText('还没有项目');
  await expect(emptyMenu.getByRole('menuitemradio')).toHaveCount(0);
  await expect(emptyMenu.getByRole('menuitem', { name: 'Git 设置', exact: true })).toHaveCount(0);
  await page.keyboard.press('Escape');
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "＋ 添加项目", exact: true }));
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
  await expect(page.locator("#chat-title")).toContainText("项目对话");
  await clickInView(page, page.locator("[data-action=new-topic]"));
  await page.locator("input[name=name]").fill("功能开发");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
  await expect(page.locator(".column h2").first()).toHaveText("功能开发");
  for (const title of ["任务 A", "任务 B"]) {
    await clickInView(page, page
      .getByRole("button", { includeHidden: true, name: "在 功能开发 新建任务", exact: true }));
    await page.locator("input[name=title]").fill(title);
    await page.locator("textarea[name=request]").fill("保留独立上下文");
    await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
    await expect(page.locator("#chat-title")).toContainText(title);
  }
  await clickInView(page, page
    .locator(".task-card")
    .filter({ has: page.getByRole('button', { includeHidden: true, name: '选择任务 任务 A', exact: true }) }));
  await fillInChat(page, page.locator('#draft'), "只给 A 的消息");
  await clickInView(page, page.locator("#send"));
  await clickInView(page, page
    .locator(".task-card")
    .filter({ has: page.getByRole('button', { includeHidden: true, name: '选择任务 任务 B', exact: true }) }));
  await fillInChat(page, page.locator('#draft'), "B 独立草稿");
  await expect(page.locator("#messages")).not.toContainText("只给 A 的消息");
  await expect(page.locator("#draft")).toHaveValue("B 独立草稿");
  await clickInView(page, page
    .locator(".task-card")
    .filter({ has: page.getByRole('button', { includeHidden: true, name: '选择任务 任务 A', exact: true }) }));
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：只给 A 的消息",
  );
  await expect(page.locator("#draft")).toHaveValue("");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "← 返回项目对话" }));
  await expect(page.locator("#messages")).not.toContainText("只给 A 的消息");
  await fillInChat(page, page.locator('#draft'), "项目层面的讨论");
  await clickInView(page, page.locator("#send"));
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：项目层面的讨论",
  );
  page.on("dialog", (dialog) => dialog.accept());
  await page.reload();
  await expect(page.locator(".task-card")).toHaveCount(2);
  await clickInView(page, page
    .locator(".task-card")
    .filter({ has: page.getByRole('button', { includeHidden: true, name: '选择任务 任务 A', exact: true }) }));
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
  await clickInView(page, page
    .getByRole("button", { includeHidden: true, name: "删除专题 功能开发", exact: true }));
  await clickInView(page, page
    .getByRole("button", { includeHidden: true, name: "删除专题，保留任务", exact: true }));
  await expect(
    page
      .getByRole("region", { name: "其他", exact: true })
      .locator(".task-card"),
  ).toHaveCount(2);
  await expect(page.locator("#messages")).toContainText("只给 A 的消息");
  await page.setViewportSize({ width: 360, height: 900 });
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "◌ 聊天", exact: true }));
  await expect(page.locator("#draft")).toBeVisible();
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "▦ 看板", exact: true }));
  await expect(page.locator("#board")).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 1100 });
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "← 返回项目对话" }));
  for (const [topic, title] of [
    ["界面体验", "打磨看板与对话切换"],
    ["服务接入", "验证独立 pi 会话"],
  ]) {
    await clickInView(page, page.locator("[data-action=new-topic]"));
    await page.locator("input[name=name]").fill(topic!);
    await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
    await clickInView(page, page
      .getByRole("button", { includeHidden: true, name: "在 " + topic + " 新建任务", exact: true }));
    await page.locator("input[name=title]").fill(title!);
    await page.locator("textarea[name=request]").fill("浏览器隔离验收示例");
    await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
    // Creating a task opens its drawer only after the real command receipt.
    await expect(page.locator("#chat-title")).toContainText(title!);
  }
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "← 返回项目对话" }));
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await openChat(page);
  await expect(page.locator("#chat-title")).toBeVisible();
  const probe=probeFor(page);
  await expect.poll(()=>probe.received.some(v=>v.type==='reply'&&probe.calls.some(c=>c.id===v.id&&c.method==='sync'))).toBe(true);
  probe.holdType='patch';
  const st=(await (await page.request.get('/api/snapshot')).json()).state;
  const projectId=Object.keys(st.projects)[0];
  await page.request.post('/api/commands',{data:{operationId:'held-update',type:'add_topic',revision:st.revision,structureRevision:st.structureRevision,projectId,name:'晚到更新'},headers:{Origin:'http://127.0.0.1:17331','X-Kaiwu-Client':'web'}});
  await expect.poll(()=>probe.held.length).toBeGreaterThan(0);
  await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'退出',exact:true}));
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await openChat(page);
  await expect(page.locator("#chat-title")).toBeVisible();
  const probe=probeFor(page);probe.dropMethod='commands';
  await fillInChat(page, page.locator('#draft'), "回执丢失仍只发送一次");
  await clickInView(page, page.locator("#send"));
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：回执丢失仍只发送一次",
  );
  await expect(page.locator("#draft")).toHaveValue("");
  expect(probe.calls.filter(r=>r.method==='commands')).toHaveLength(1);
  await fillInChat(page, page.locator('#draft'), "确认后可以新发");
  await clickInView(page, page.locator("#send"));
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await openChat(page);
  await expect(page.locator("#chat-title")).toBeVisible();
  const probe=probeFor(page);probe.holdMethod='commands';
  await fillInChat(page, page.locator('#draft'), "旧身份的消息");
  await clickInView(page, page.locator("#send"));
  await expect.poll(()=>probe.held.length).toBeGreaterThan(0);
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "退出", exact: true }));
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await openChat(page);
  await expect(page.locator("#chat-title")).toBeVisible();
  await fillInChat(page, page.locator('#draft'), "新身份的新草稿");
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
  const history = Array.from({ length: 80 }, (_, i) => [{ id: 'scroll-user-' + i, role: 'user', delivery: 'complete', text: '问题 ' + i }, { id: 'scroll-' + i, role: 'assistant', delivery: 'complete', text: ('历史记录 ' + i + '\n\n').repeat(5) }]).flat();
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
  patch([{ op: 'add', path: ['conversations', taskConversationId, 'messages', String(history.length)], value: { id: 'scroll-new', role: 'assistant', delivery: 'complete', text: history.at(-1)!.text + '\n\n新的回复' } }]);
  await expect(messages.locator('.message').last()).toContainText('新的回复');
  await expect.poll(() => messages.evaluate(el => el.scrollTop)).toBe(slightlyAboveBottom);
  await readEarlier();
  patch([{ op: 'append', path: ['conversations', taskConversationId, 'messages', String(history.length), 'text'], value: '\n\n' + '上翻时仍在回复\n\n'.repeat(20) }]);
  await expect(messages.locator('.message').last()).toContainText('上翻时仍在回复');
  await expect.poll(() => messages.evaluate(el => el.scrollTop)).toBe(300);
  await messages.evaluate(el => { el.scrollTop = el.scrollHeight; });
  patch([{ op: 'append', path: ['conversations', taskConversationId, 'messages', String(history.length), 'text'], value: '\n\n' + '流式增加内容\n\n'.repeat(20) }]);
  await expect(messages.locator('.message').last()).toContainText('流式增加内容');
  await expectLatest(); // Returning to the bottom enables following again.
  await readEarlier();
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '← 返回项目对话' }));
  await expectLatest();
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '选择任务 ' + state.tasks[taskId].title, exact: true }));
  await expectLatest();
  await readEarlier();
  await page.reload();
  await expectLatest();
  await page.setViewportSize({ width: 390, height: 844 });
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '◌ 聊天', exact: true }));
  await expectLatest();
  await readEarlier();
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '▦ 看板', exact: true }));
  patch([{ op: 'add', path: ['conversations', taskConversationId, 'messages', String(history.length)], value: { id: 'scroll-hidden', role: 'assistant', delivery: 'complete', text: '聊天隐藏期间的新回复' } }]);
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '◌ 聊天', exact: true }));
  await expect(messages.locator('.message').last()).toContainText('聊天隐藏期间的新回复');
  await expectLatest();
  await page.reload();
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '◌ 聊天', exact: true }));
  await expectLatest();
});

test("Enter sends once, Shift+Enter adds a newline, and IME confirmation preserves the draft", async ({
  page,
}) => {
  await page.goto("/");
  await page
    .getByLabel("本机访问令牌")
    .fill("kaiwu-browser-test-token-00000000");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await openChat(page);
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

test("consecutive progress updates merge by user turn and keep full prose with expandable records", async ({page,context})=>{
 await enterStart(page,context);
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const project=Object.values(state.projects).find((p:any)=>p.name==='开悟验收项目') as any;
 const lastReply='## 接口已完成\n\n- 返回 hello world\n- 已核对局域网访问\n\n访问地址：http://192.168.2.122:8000/hello\n\n'+('详细记录：接口返回 hello world。\n'.repeat(80));
 const transform=(data:any)=>{const c=data.state?.conversations[project.conversationId];if(!c)return;c.runState='idle';c.rounds=[];c.messages=[
  {id:'summary-user',role:'user',text:'开发一个 hello world 接口',delivery:'complete'},
  {id:'summary-progress-1',role:'assistant',kind:'progress',text:'先检查当前服务的启动方式。',delivery:'complete'},
  {id:'summary-progress-2',role:'assistant',kind:'progress',text:'已添加接口，准备验证响应。',delivery:'complete'},
  {id:'summary-reply',role:'assistant',text:lastReply,delivery:'complete'},
  {id:'summary-user-2',role:'user',text:'继续检查边界',delivery:'complete'},
  {id:'summary-progress-4',role:'assistant',kind:'progress',text:'正在检查空路径和错误请求。',delivery:'complete'}
 ]};
 probeFor(page).transform=transform;
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch(),data=await response.json();transform(data);await route.fulfill({response,json:data})});
 await page.goto('/projects/'+project.id);
 await openChat(page);
 const replies=page.locator('.message.assistant');
 await expect(replies).toHaveCount(2);
 const reply=replies.first(), body=reply.locator('.assistant-summary-markdown');
 await expect(body.getByRole('heading',{name:'接口已完成'})).toBeVisible();
 await expect(body.locator('li')).toHaveCount(2);
 await expect(body).toContainText('详细记录');
 expect((await body.textContent())?.match(/详细记录/g)).toHaveLength(80);
 await expect(body.getByRole('link')).toHaveAttribute('href','http://192.168.2.122:8000/hello');
 await expect(body.getByRole('link')).toHaveAttribute('target','_blank');
 await expect(reply.getByText('先检查当前服务的启动方式。',{exact:true})).toBeHidden();
 await clickInView(page, reply.getByLabel('执行详情',{exact:true}));
 await expect(reply.getByText('先检查当前服务的启动方式。',{exact:true})).toBeVisible();
 await expect(reply.getByText('已添加接口，准备验证响应。',{exact:true})).toBeVisible();
 await clickInView(page, reply.getByLabel('执行详情',{exact:true}));
 await expect(body).toBeVisible();
 await page.setViewportSize({width:390,height:844});
 await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'◌ 聊天',exact:true}));
 await expect(replies.last().locator('.assistant-summary-markdown')).toHaveText('正在检查空路径和错误请求。');
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  for (const remote of ["", "git@example.com:team/project.git"]) {
    await openAddProject(page);
    const dir = seed + (remote ? "-with-origin" : "-without-origin");
    await page
      .getByLabel("项目名称", { exact: true })
      .fill(remote ? "带远程地址" : "从零开始");
    await page.getByLabel("项目绝对路径", { exact: true }).fill(dir);
    await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText("会自动初始化");
    await page.getByLabel("Git 远程地址（可选）", { exact: true }).fill(remote);
    await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
    await expect(page.locator('dialog:not(.chat-drawer)[open]')).toHaveCount(0);
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
  await openChat(page);
  await expect(page.locator("#chat-title")).toBeVisible();
  await page.reload();
  await openChat(page);
  await expect(page.locator("#chat-title")).toBeVisible();
  const saved = await context.storageState();
  const cookie = saved.cookies.find((c) => c.name === "kaiwu_session")!;
  expect(cookie.expires).toBeGreaterThan(Date.now() / 1000 + 29 * 24 * 3600);
  const reopened = await browser.newContext({ storageState: saved });
  try {
    const restored = await reopened.newPage();
    await restored.goto("http://127.0.0.1:17331/");
    await openChat(restored);
  await expect(restored.locator("#chat-title")).toBeVisible();
    await clickInView(restored, restored.getByRole("button", { includeHidden: true, name: "退出", exact: true }));
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
    await clickInView(page, page.getByRole("button", { includeHidden: true, name: "进入工作台", exact: true }));
    await openChat(page);
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
    has: page.getByRole('button', { name: '选择任务 任务 A', exact: true }),
  });
  await clickInView(page, card
    .getByRole("button", { includeHidden: true, name: "选择任务 任务 A", exact: true }));
  await fillInChat(page, page.locator('#draft'), "不要开始开发");
  await clickInView(page, page.locator("#send"));
  await expect(page.locator("#messages")).toContainText(
    "[隔离协议测试] 已收到：不要开始开发",
  );
  await expect(card).toContainText("未开始");
  await expect(page.locator(".confirm-btn")).toHaveCount(0);
  await fillInChat(page, page.locator('#draft'), "保留这份草稿");
  await clickInView(page, card.getByRole("button", { includeHidden: true, name: "开始开发", exact: true }));
  await expect(card).toContainText("待验收");
  await expect(page.locator("#draft")).toHaveValue("保留这份草稿");
  const reply=page.locator('.message.assistant').last();
  await clickInView(page, reply.getByLabel('执行详情',{exact:true}));
  await expect(reply.locator('.tools-details')).toContainText('write · 已返回');
  await expect(reply.locator('.confirm-btn')).toBeVisible();
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "◌ 聊天", exact: true }));
  await openChat(page);
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
  await fillInChat(page, page.locator('#draft'), "开始开发");
  await clickInView(page, page.locator("#send"));
  await expect(page.getByRole("alert")).toContainText("请先选择");
  await expect(page.locator("#draft")).toHaveValue("开始开发");
  const card = page.locator(".task-card").filter({
    has: page.getByRole('button', { name: '选择任务 任务 B', exact: true }),
  });
  await clickInView(page, card
    .getByRole("button", { includeHidden: true, name: "选择任务 任务 B", exact: true }));
  await fillInChat(page, page.locator('#draft'), " 开始开发！ ");
  await clickInView(page, page.locator("#send"));
  await expect(card).toContainText("待验收");
  await expect(page.locator("#draft")).toHaveValue("");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "← 返回项目对话" }));
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
  await clickInView(page, card.getByRole("button", { includeHidden: true, name: "开始开发", exact: true }));
  const picker = page.getByRole('dialog', { name: '选择会话模型', exact: true });
  await expect(picker).toBeVisible();
  await clickInView(page, picker.getByRole('button', { includeHidden: true, name: '取消', exact: true }));
  await fillInChat(page, page.locator('#draft'), "开始开发");
  await expect(page.locator("#send")).toBeEnabled();
  await clickInView(page, page.locator("#send"));
  await expect(picker).toBeVisible();
  await clickInView(page, picker.getByRole('button', { includeHidden: true, name: '取消', exact: true }));
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
  await fillInChat(page, page.locator('#draft'), content);
  await clickInView(page, page.locator("#send"));
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
  await clickInView(page, reply
    .getByRole("button", { includeHidden: true, name: "复制代码", exact: true })
    .first());
  await expect
    .poll(() =>
      page.evaluate(() => (window as any).fixtureClipboard.readText()),
    )
    .toBe(source);
  await expect(reply.getByRole("status")).toContainText("已复制");
  await page.screenshot({ path: "/tmp/kaiwu-markdown-desktop.png" });
  await page.reload();
  await openChat(page);
  await expect(
    page
      .locator(".message.assistant")
      .last()
      .getByRole("heading", { name: "格式验收" }),
  ).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "◌ 聊天", exact: true }));
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
  await fillInChat(page, page.locator('#draft'), source);
  await clickInView(page, page
    .getByRole("button", { includeHidden: true, name: "预览 Markdown", exact: true }));
  const preview = page.getByLabel("当前草稿预览", { exact: true });
  await expect(
    preview.getByRole("heading", { name: "项目草稿" }),
  ).toBeVisible();
  await expect(preview.locator("code .hljs-attr").first()).toBeVisible();
  await expect(page.locator("#draft")).toBeHidden();
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "返回编辑", exact: true }));
  await expect(page.locator("#draft")).toHaveValue(source);
  await expect(page.locator("#draft")).toBeFocused();
  await clickInView(page, page
    .getByRole("button", { includeHidden: true, name: "预览 Markdown", exact: true }));
  await clickInView(page, page
    .getByRole("button", { includeHidden: true, name: "选择任务 任务 A", exact: true }));
  await expect(page.getByLabel("当前草稿预览", { exact: true })).toHaveCount(0);
  await fillInChat(page, page.locator('#draft'), "任务草稿");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "← 返回项目对话" }));
  await expect(page.locator("#draft")).toHaveValue(source);
  await clickInView(page, page
    .getByRole("button", { includeHidden: true, name: "预览 Markdown", exact: true }));
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
  await fillInChat(page, page.locator('#draft'), "__MARKDOWN_STREAM_FIXTURE__");
  await clickInView(page, page.locator("#send"));
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
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toHaveCount(0);
  await clickInView(page, page.locator("[data-action=new-task]"));
  await page.locator("input[name=title]").fill("集成功能");
  await page.locator("textarea[name=request]").fill("隔离开发并合并");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "保存", exact: true }));
  const card = page.locator(".task-card").filter({
    has: page.getByRole('button', { name: '选择任务 集成功能', exact: true }),
  });
  await clickInView(page, card.getByRole("button", { includeHidden: true, name: "开始开发", exact: true }));
  await expect(card).toContainText("待验收");
  await expect(card).toContainText("待提交");
  await clickInView(page, card.getByRole("button", { includeHidden: true, name: "合并到主分支", exact: true }));
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText("Kaiwu");
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText(
    "kaiwu-fixture-result.txt",
  );
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "取消", exact: true }));
  expect(existsSync(resolve(dir, "kaiwu-fixture-result.txt"))).toBe(false);
  await fillInChat(page, page.locator('#draft'), "合并到主分支");
  await clickInView(page, page.locator("#send"));
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toBeVisible();
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "提交并合并到主分支", exact: true }));
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText("操作完成");
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText("本地合并：主分支已合并");
  await expect(card).toContainText("已合并");
  expect(
    readFileSync(resolve(dir, "kaiwu-fixture-result.txt"), "utf8"),
  ).toContain("隔离开发并合并");
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "取消", exact: true }));
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
      has: page.getByRole('button', { name: '选择任务 集成功能', exact: true }),
    });
  await clickInView(page, card
    .getByRole("button", { includeHidden: true, name: "选择任务 集成功能", exact: true }));
  const probe=probeFor(page);probe.holdMethod='git/preview';
  await clickInView(page, card.getByRole("button", { includeHidden: true, name: "合并到主分支", exact: true }));
  await expect.poll(() => probe.held.length).toBeGreaterThan(0);
  await clickInView(page, page.getByRole("button", { includeHidden: true, name: "← 返回项目对话" }));
  releaseFrames(page);
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toHaveCount(0);
  await expect(page.locator("#chat-title")).toContainText("项目对话");
});

test("lost merge response and missing receipt reconcile read-only without resending", async ({page,context})=>{
 await enterStart(page,context);await chooseMenuProject(page,{label:'合并验收'});
 const card=page.locator('.task-card').filter({has:page.getByRole('heading',{name:'集成功能',exact:true})});
 await clickInView(page, card.getByRole('button',{ includeHidden: true,name:'合并到主分支',exact:true}));
 const probe=probeFor(page);probe.dropMethod='git/merge';
 await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'提交并合并到主分支',exact:true}));
 await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText('回执');
 await expect(page.locator('.connection')).toContainText('已连接');
 const sent=()=>probe.calls.filter(r=>r.method==='git/merge');
 expect(sent()).toHaveLength(1);
 const operationId=sent()[0].body.operationId;
 // A missing receipt is not proof that the original write never happened.
 probe.missingReceipt=true;
 await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'重新检查',exact:true}));
 await expect(page.getByRole('alert')).toContainText('不会重新发送操作');
 expect(sent()).toHaveLength(1);
 const snapshot=(await(await page.request.get('/api/snapshot')).json()).state;
 const task=Object.values(snapshot.tasks).find((t:any)=>t.title==='集成功能') as any;
 const project=snapshot.projects[task.projectId];
 const head=execFileSync('git',['-C',project.path,'rev-parse','HEAD'],{encoding:'utf8'}).trim();
 probe.missingReceipt=false;
 await clickInView(page, card.getByRole('button',{ includeHidden: true,name:'合并到主分支',exact:true}));
 await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText('操作完成');
 await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText('本地合并：主分支已合并');
 expect(sent()).toHaveLength(1);
 expect(probe.calls.filter(r=>r.method==='git/operations/'+operationId).length).toBeGreaterThanOrEqual(2);
 expect(execFileSync('git',['-C',project.path,'rev-parse','HEAD'],{encoding:'utf8'}).trim()).toBe(head);
});


test("pi models: discover, choose an empty conversation and expose next-round selection", async ({ page, context }) => {
  await enterStart(page, context);
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '默认模型', exact: true }));
  const picker = page.getByRole('dialog', { name: '默认模型', exact: true });
  await expect(picker).toContainText('检测到 3 个可用模型');
  await clickInView(page, picker.getByLabel('提供商', { exact: true }));
  await clickInView(page, picker.locator('[role="option"]:has-text("fixture")'));
  await picker.getByLabel('搜索模型', { exact: true }).fill('another');
  await clickInView(page, picker.getByLabel('模型', { exact: true }));
  await expect(picker.locator('[role="option"]')).toHaveCount(1);
  await clickInView(page, picker.locator('[role="option"]:has-text("another-test")'));
  await expect(picker.getByLabel('思考级别', { exact: true })).toHaveCount(0);
  await clickInView(page, picker.getByRole('button', { includeHidden: true, name: '保存模型' }));
  await expect(picker).toHaveCount(0);
  const modelProjectPath = resolve(readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8'), '..', 'model-picker-project');
  const created = await page.evaluate(async (path) => {
    const snap = await (await fetch('/api/snapshot')).json();
    const result = await (await fetch('/api/commands', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Kaiwu-Client': 'web' }, body: JSON.stringify({ type: 'add_project', operationId: crypto.randomUUID(), revision: snap.state.revision, name: '模型选择验证', path }) })).json();
    return result.createdId as string;
  }, modelProjectPath);
  await chooseMenuProject(page, { id: created });
  await fillInChat(page, page.locator('#draft'), '选择模型时保留的草稿');
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '选择会话模型', exact: true }));
  const scoped = page.getByRole('dialog', { name: '选择会话模型', exact: true });
  await expect(scoped.getByLabel('模型', { exact: true })).toContainText('another-test');
  await clickInView(page, scoped.getByLabel('提供商', { exact: true }));
  await clickInView(page, scoped.locator('[role="option"]:has-text("second")'));
  await clickInView(page, scoped.getByLabel('模型', { exact: true }));
  await clickInView(page, scoped.locator('[role="option"]:has-text("isolated-test")'));
  await clickInView(page, scoped.getByRole('button', { includeHidden: true, name: '保存模型' }));
  await expect(scoped).toHaveCount(0);
  await expect(page.locator('#draft')).toHaveValue('选择模型时保留的草稿');
  const selected = await page.evaluate(async () => (await (await fetch('/api/snapshot')).json()).state);
  expect(selected.profile.model).toBe('another-test');
  const c = selected.conversations[selected.projects[created].conversationId];
  expect(c.profile.provider).toBe('second');
  expect(c.profile.model).toBe('isolated-test');
  expect(c.messages).toHaveLength(0);
  expect(JSON.stringify(selected)).not.toContain('must-not-reach-browser');
  await clickInView(page, page.locator('#send'));
  await expect(page.locator('#messages')).toContainText('[隔离协议测试] 已收到：选择模型时保留的草稿');
  await expect(page.getByRole('button', { name: '选择会话模型', exact: true })).toBeEnabled();
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '选择会话模型', exact: true }));
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText('下一轮');
  await clickInView(page, page.locator('dialog:not(.chat-drawer)[open]').getByRole('button', { includeHidden: true, name: '取消', exact: true }));
});

test("pi models: errors, empty catalog and mobile selection retain draft", async ({ page, context }) => {
  await enterStart(page, context);
  await page.setViewportSize({ width: 360, height: 900 });
  probeFor(page).modelMode='error';
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '默认模型', exact: true }));
  const picker = page.getByRole('dialog', { name: '默认模型', exact: true });
  await expect(picker).toContainText('pi 检测暂时失败');
  await expect(picker.getByRole('button', { name: '保存模型' })).toBeDisabled();
  probeFor(page).modelMode='empty';
  await clickInView(page, picker.getByRole('button', { includeHidden: true, name: '重新检测' }));
  await expect(picker).toContainText('pi 尚无可用模型');
  probeFor(page).modelMode='';
  await clickInView(page, picker.getByRole('button', { includeHidden: true, name: '重新检测' }));
  await expect(picker).toContainText('检测到 3 个可用模型');
  await clickInView(page, picker.getByLabel('提供商', { exact: true }));
  await clickInView(page, picker.locator('[role="option"]:has-text("fixture")'));
  await clickInView(page, picker.getByLabel('模型', { exact: true }));
  await clickInView(page, picker.locator('[role="option"]:has-text("isolated-test")'));
  await clickInView(page, picker.getByLabel('思考级别', { exact: true }));
  await clickInView(page, picker.locator('[role="option"]:has-text("中")'));
  expect(await picker.evaluate(el => el.getBoundingClientRect().width)).toBeLessThanOrEqual(360);
  await clickInView(page, picker.getByRole('button', { includeHidden: true, name: '保存模型' }));
  await expect(picker).toHaveCount(0);
  probeFor(page).modelMode='';
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
 await page.goto('/');await openChat(page);
  await expect(page.locator('#chat-title')).toBeVisible();
 const requests:string[]=[];page.on('request',r=>{if(r.url().includes('/api/commands'))requests.push(r.url())});
 await clickInView(page, page.locator('[data-action=new-task]'));
 await page.locator('input[name=title]').fill('结构化进展验收');
 await page.locator('textarea[name=request]').fill('结构化登录恢复');
 await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'保存',exact:true}));
 const card=page.locator('.task-card').filter({has:page.getByRole('heading',{name:'结构化进展验收',exact:true})});
 await clickInView(page, card.getByRole('button',{ includeHidden: true,name:'开始开发',exact:true}));
  const reply=page.locator('.message.assistant').last();
  await expect(reply.locator('.assistant-summary-markdown')).toContainText('登录恢复');
  await expect(reply.locator('.verification-brief')).toContainText('1 项有执行依据');
  await expect(reply.locator('.confirm-btn')).toBeVisible();
  await clickInView(page, reply.getByLabel('执行详情',{exact:true}));
  const check=reply.locator('.check-row');
  await expect(check).toHaveCount(1);
  await expect(reply.locator('.round-report-section')).toContainText('刷新自动恢复');
  await expect(check).toContainText('AI 报告通过');
  await expect(check).toContainText('命令完成');
  await clickInView(page, check.locator('summary'));
  await expect(check).toContainText('PASS fixture');
  const state=(await(await page.request.get('/api/snapshot')).json()).state;
  const task=Object.values(state.tasks).find((t:any)=>t.title==='结构化进展验收') as any;
  const firstRound=state.conversations[task.conversationId].rounds[0];
  const priorGit=state.taskGit[task.id].state;
  await clickInView(page, reply.locator('.confirm-btn'));
  await expect(card).toContainText('已完成');
  await expect(reply.locator('.message-status')).toHaveText('结果已确认');
  await fillInChat(page, page.locator('#draft'), '结构化登录恢复：补充边界检查');
  await clickInView(page, page.locator('#send'));
  await expect(page.locator('.confirm-btn')).toHaveCount(1);
  const history=page.locator('.assistant-turn-container.historical');
  await expect(history).toHaveCount(1);
  await expect(history).not.toHaveAttribute('open','');
  await clickInView(page, history.locator('> summary'));
  await expect(history.locator('.confirm-btn')).toHaveCount(0);
  await clickInView(page, history.getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
  await expect(history.locator('.historical-result')).toContainText('历史网页效果未存档');
  const after=(await(await page.request.get('/api/snapshot')).json()).state;
  expect(after.conversations[task.conversationId].rounds[0].id).toBe(firstRound.id);
  expect(after.conversations[task.conversationId].rounds[0].phase).toBe('confirmed');
  expect(after.taskGit[task.id].state).toBe(priorGit);
  await page.screenshot({path:'../../docs/design/web-service/progress-desktop.png',fullPage:true});
  await page.setViewportSize({width:390,height:844});
  await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'◌ 聊天',exact:true}));
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.screenshot({path:'../../docs/design/web-service/progress-mobile.png',fullPage:true});
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
 const card=page.locator('.task-card[data-taskid="'+taskID+'"]');await clickInView(page, card.getByRole('button',{ includeHidden: true,name:'开始开发',exact:true}));await expect(card).toContainText('待验收');
 await fillInChat(page, page.locator('#draft'), '__DISCONNECT_FIXTURE__');await clickInView(page, page.locator('#send'));await expect(card).toContainText('已中断');
 const before=(await (await page.request.get('/api/snapshot')).json()).state,t=before.tasks[taskID],c=before.conversations[t.conversationId],x=before.executions[t.executionId];
 await expect(page.getByRole('button',{name:'恢复原会话',exact:true})).toHaveCount(0);
 await fillInChat(page, page.locator('#draft'), '接着执行新的明确要求');await expect(page.locator('#send')).toBeEnabled();await clickInView(page, page.locator('#send'));
 await expect(page.locator('#messages')).toContainText('[隔离协议测试] 已收到：接着执行新的明确要求');await expect(card).toContainText('待验收');
 let after=(await (await page.request.get('/api/snapshot')).json()).state;
 expect(after.tasks[taskID].executionId).toBe(t.executionId);expect(after.executions[t.executionId].workspace).toBe(x.workspace);expect(after.conversations[c.id].sessionFile).toBe(c.sessionFile);expect(after.conversations[c.id].sessionId).toBe(c.sessionId);
 expect(after.conversations[c.id].messages.filter((m:any)=>m.role==='user'&&m.text==='接着执行新的明确要求')).toHaveLength(1);
 await fillInChat(page, page.locator('#draft'), '__DISCONNECT_FIXTURE__');await clickInView(page, page.locator('#send'));await expect(card).toContainText('已中断');
 await clickInView(page, card.getByRole('button',{ includeHidden: true,name:'继续开发',exact:true}));await expect(card).toContainText('待验收');
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
 await clickInView(page, page.getByLabel('当前看板专题'));
 await clickInView(page, page.locator(`[role="option"][data-value="${fresh.tasks[id].topicId}"]`));
 const card=page.locator('.task-card[data-taskid="'+id+'"]');await expect(card).toContainText('已中断');
 await clickInView(page, card.getByRole('button',{ includeHidden: true,name:'选择任务 中断任务核对',exact:true}));
 await expect(page.getByRole('button',{name:'恢复原会话',exact:true})).toHaveCount(0);
 await expect(page.locator('.chat-failure')).toContainText('自动接回原会话');
 await expect(page.getByRole('button',{name:'确认完成',exact:true})).toHaveCount(0);
 await expect(page.locator('.chat-head').getByRole('button',{name:'继续开发',exact:true})).toBeVisible();
 await expect(page.locator('.message.assistant .assistant-summary-markdown')).toContainText('已创建 /hello 接口');
 await expect(page.locator('.assistant-turn-details')).not.toHaveAttribute('open','');
 await clickInView(page, page.getByLabel('执行详情',{exact:true}));
 const tools=page.locator('.tools-details');
 await clickInView(page, tools.locator('> summary'));
 await expect(tools).toContainText('write · 已返回');
 await expect(tools).toContainText('bash · 未收到结束回执');
 // The legacy execution has no verifiable workspace: never invent Git completion.
 await expect(card).not.toContainText('已合并');
 await expect(page.locator('.chat-failure')).not.toContainText('需要确认');
 expect(await page.locator('#messages').evaluate(el=>el.clientHeight)).toBeGreaterThanOrEqual(140);
 await fillInChat(page, page.locator('#draft'), '继续处理新的要求');await expect(page.locator('#send')).toBeEnabled();
 await page.screenshot({path:resolve('../../docs/design/web-service/task-recovery-mobile.png')});
 expect(probeFor(page).calls.filter(r=>r.method==='commands')).toHaveLength(0);
 probeFor(page).transform=null;probeFor(page).holdType=null;
 await page.unroute('**/api/snapshot');
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
 const reply=page.locator('.message.assistant').last();
 await expect(reply.locator('.confirm-btn')).toBeVisible();
 await clickInView(page, reply.getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 const pane=page.locator('.result-pane');
 await expect(pane).toBeVisible();
 await clickInView(page, reply.locator('.confirm-btn'));
 await expect(reply.locator('.completed-compact-bar')).toBeVisible();
 await expect(reply.locator('.completed-compact-bar')).not.toContainText('你已确认本轮结果');
 await expect(reply.locator('.completed-compact-bar').getByRole('button',{name:'查看结果',exact:true})).toBeVisible();
 await expect(pane).toBeVisible();
 await expect(reply.locator('.confirm-btn')).toHaveCount(0);
 const disconnected=(data:any)=>{const t=data.state?.tasks[taskId];if(t){const c=data.state.conversations[t.conversationId];c.runState='interrupted';c.error='pi 会话已中断，发送下一条消息时自动接回；历史已保留'}};
 probeFor(page).transform=disconnected;
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch(),data=await response.json();disconnected(data);await route.fulfill({response,json:data})});
 await page.reload();
 await expect(reply.locator('.completed-compact-bar')).toBeVisible();
 await expect(reply.locator('.confirm-btn')).toHaveCount(0);
 const mergedSnapshot=(data:any)=>{if(data.state?.taskGit?.[taskId])Object.assign(data.state.taskGit[taskId],{state:'merged',canMerge:false,mainBranch:'main'})};
 probeFor(page).transform=mergedSnapshot;
 await page.unroute('**/api/snapshot');
 await page.route('**/api/snapshot',async route=>{const response=await route.fetch(),data=await response.json();mergedSnapshot(data);await route.fulfill({response,json:data})});
 await page.reload();
 await expect(reply.locator('.completed-compact-bar')).toContainText('已合并 · main');
 await expect(reply.getByRole('button',{name:'合并到主分支'})).toHaveCount(0);
 await expect(reply.locator('.completed-compact-bar')).not.toContainText('先审核具体改动再合并');
 // Continuing uses the same composer; V5 no longer adds a duplicate Continue button.
 await fillInChat(page, page.locator('#draft'), '继续修改，先讨论需求');
 await page.locator('#draft').focus();
 await expect(page.locator('#draft')).toBeFocused();
 await page.setViewportSize({width:390,height:844});
 await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'◌ 聊天',exact:true}));
 await clickInView(page, reply.getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 await expect(pane).toBeVisible();
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'返回任务对话',exact:true}));
 await expect(page.locator('#draft')).toHaveValue('继续修改，先讨论需求');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 probeFor(page).transform=null;
 await page.unroute('**/api/snapshot');
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
 await clickInView(page, page.locator('.message.assistant').last().getByRole('button',{ includeHidden: true,name:/查看结果|打开网页/,exact:true}));
 const pane=page.getByRole('region',{name:'任务成果与效果'});
 await expect(pane.getByRole('button',{name:'启动预览并在新标签页打开',exact:true})).toBeVisible();
 await expect(pane).toContainText('preview.py');
 // External edits after display must not turn this click into approval of a
 // different command. The changed command has a visible filesystem side effect.
 const state=(await(await page.request.get('/api/snapshot')).json()).state;
 const tree=state.executions[state.tasks[taskId].executionId].workspace;
 const manifestFile=resolve(tree,'kaiwu-preview.json'),shown=readFileSync(manifestFile,'utf8');
 writeFileSync(resolve(tree,'unapproved.py'),"from pathlib import Path\nPath('preview-unapproved').touch()\n");
 writeFileSync(manifestFile,JSON.stringify({version:1,command:['python3','unapproved.py','{port}'],path:'/'}));
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'启动预览并在新标签页打开',exact:true}));
 await expect(pane.getByRole('alert')).toContainText(/配置|指纹|刷新/);
 expect(existsSync(resolve(tree,'preview-unapproved'))).toBe(false);
 writeFileSync(manifestFile,shown);
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'刷新状态',exact:true}));
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'启动预览并在新标签页打开',exact:true}));
 await expect(pane.getByRole('alert')).toContainText('上次启动结果未知');
 expect(probeFor(page).calls.filter(r=>r.method==='previews/start')).toHaveLength(1);
 // An unknown receipt stays fail-closed for that round. A genuinely new user
 // development round owns a new preview intent; never clear the old receipt.
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'← 返回看板',exact:true}));
 await fillInChat(page, page.locator('#draft'), '继续开发：保留已核对的原预览命令');
 await clickInView(page, page.locator('#send'));
 await expect(page.locator('.message.assistant')).toHaveCount(2);
 await expect(page.locator('.message.assistant').last().locator('.confirm-btn')).toBeVisible();
 await clickInView(page, page.locator('.message.assistant').last().getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'启动预览并在新标签页打开',exact:true}));
 const frame=page.frameLocator('iframe[title="任务效果"]');
 await expect(frame.getByRole('heading',{name:'真实任务预览'})).toBeVisible();
 await expect(page.locator('iframe[title="任务效果"]')).toHaveAttribute('sandbox','allow-scripts');
 expect(await frame.locator('body').getAttribute('data-parent-access')).toBe('blocked');
 await clickInView(page, frame.getByRole('button',{ includeHidden: true,name:'测试接口'}));
 await expect(frame.locator('#result')).toHaveText('API OK');
 await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'确认完成',exact:true}));
 await expect(page.locator('.completed-compact-bar')).toBeVisible();
 await expect(frame.getByRole('heading',{name:'真实任务预览'})).toBeVisible();
 await page.screenshot({path:resolve('../../docs/design/web-service/task-result-desktop.png')});
 await page.setViewportSize({width:390,height:844});
 await expect(pane).toBeVisible();
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'返回任务对话'}));
 await expect(page.locator('#draft')).toBeVisible();
 await page.screenshot({path:resolve('../../docs/design/web-service/task-result-mobile.png')});
 await clickInView(page, page.locator('.mobile-switch button').first());
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'停止预览',exact:true}));
 await expect(pane).toContainText('服务已停止');
 await expect(page.locator('iframe[title="任务效果"]')).toHaveCount(0);
 expect(probeFor(page).calls.filter(r=>r.method==='previews/start')).toHaveLength(2);
});

test('late preview responses stay with their original task and absent previews do not start a model',async({page})=>{
 await authenticateResultTest(page);
 const a=await resultFixture(page,'late-a');const b=await resultFixture(page,'late-b',false,false);
 await page.goto('/projects/'+a.projectId+'/tasks/'+a.taskId);
 await expect(page.locator('.message.assistant').last().getByRole('button',{name:'查看结果',exact:true})).toBeVisible();
 probeFor(page).holdMethod='previews/status';
 await clickInView(page, page.locator('.message.assistant').last().getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 await expect.poll(()=>probeFor(page).held.length).toBeGreaterThan(0);
 await chooseMenuProject(page, { id: b.projectId });
 await clickInView(page, page.locator('.task-card[data-taskid="'+b.taskId+'"]'));
 releaseFrames(page);
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toHaveCount(0);
 await expect(page.locator('#chat-title')).toContainText('预览任务 late-b');
 await chooseMenuProject(page, { id: a.projectId });
 await clickInView(page, page.locator('.task-card[data-taskid="'+a.taskId+'"]'));
 const sendsBefore=probeFor(page).calls.filter(r=>r.method==='commands'&&r.body?.type==='send_message').length;
 const draftBefore=await page.locator('#draft').inputValue();
 await clickInView(page, page.locator('.message.assistant').last().getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 await expect(page.getByRole('region',{name:'任务成果与效果'})).toContainText('暂无网页效果：任务工作区没有可确定的 HTML 入口');
 await expect(page.locator('#draft')).toHaveValue(draftBefore);
 await expect(page.getByRole('region',{name:'任务成果与效果'}).getByRole('button',{name:/启动预览/})).toHaveCount(0);
 expect(probeFor(page).calls.filter(r=>r.method==='previews/start')).toHaveLength(0);
 expect(probeFor(page).calls.filter(r=>r.method==='commands'&&r.body?.type==='send_message')).toHaveLength(sendsBefore);
 probeFor(page).holdMethod=null;
 releaseFrames(page);
});

test('result history UI: compact completed bar, history toggle, request summary, heading without 第N轮, and mobile layout', async ({ page }) => {
 await authenticateResultTest(page);
 const fixturePath = readFileSync(resolve('../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt'), 'utf8');
 let snapshot = (await (await page.request.get('/api/snapshot')).json()).state;
 let targetProject = Object.values(snapshot.projects).find((p: any) => p.name === '开悟验收项目' || p.path === fixturePath) as any;
 if (!targetProject) {
  const resp = await page.request.post('/api/commands', {
   data: { type: 'add_project', operationId: 'history-ui-project', revision: snapshot.revision, name: '历史UI测试', path: fixturePath },
   headers: { Origin: 'http://127.0.0.1:17331', 'X-Kaiwu-Client': 'web' },
  });
  expect(resp.ok()).toBe(true);
  targetProject = { id: (await resp.json()).createdId, name: '历史UI测试' };
 }
 await page.goto('/projects/' + targetProject.id);
 await chooseMenuProject(page, { id: targetProject.id });
 await openChat(page);
  await expect(page.locator('#chat-title')).toBeVisible();

 await clickInView(page, page.locator('[data-action=new-task]'));
 await page.locator('input[name=title]').fill('历史UI测试任务');
 await page.locator('textarea[name=request]').fill('结构化登录恢复');
 await clickInView(page, page.getByRole('button', { includeHidden: true, name: '保存', exact: true }));

 await expect(page.locator('#chat-title')).toContainText('历史UI测试任务');
 const card = page.locator('.task-card').filter({ has: page.getByRole('button', { includeHidden: true, name: '选择任务 历史UI测试任务', exact: true }) });
 await clickInView(page, card.getByRole('button', { includeHidden: true, name: '开始开发', exact: true }));
 await expect(page.getByRole('button', { name: '确认完成', exact: true })).toBeVisible();

 // Confirmation changes task/result state, never Git or history ownership.
 const before=(await(await page.request.get('/api/snapshot')).json()).state;
 const task=Object.values(before.tasks).find((t:any)=>t.title==='历史UI测试任务') as any;
 const first=before.conversations[task.conversationId].rounds[0];
 const latest=page.locator('.message.assistant').last();
 await clickInView(page, latest.locator('.confirm-btn'));
 await expect(card).toContainText('已完成');
 await expect(latest.locator('.message-status')).toContainText('结果已确认');
 await expect(latest.locator('.completed-compact-bar')).toBeVisible();
 await expect(latest.locator('.confirm-btn')).toHaveCount(0);
 await clickInView(page, latest.getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 const pane=page.getByRole('region',{name:'任务成果与效果'});
 await expect(pane).toBeVisible();
 await expect(pane.locator('.result-pane-heading')).toContainText('你已确认');
 await clickInView(page, pane.getByRole('button',{ includeHidden: true,name:'← 返回看板',exact:true}));
 await fillInChat(page, page.locator('#draft'), '结构化登录恢复 第二轮改进请求内容');
 await clickInView(page, page.locator('#send'));
 await expect(page.locator('.confirm-btn')).toHaveCount(1);
 const history=page.locator('.assistant-turn-container.historical');
 await expect(history).toHaveCount(1);
 await expect(history).not.toHaveAttribute('open','');
 const disclosure=history.locator('> summary');
 await expect(disclosure).toContainText('上一轮结果');
 expect(await disclosure.textContent()).not.toMatch(/第\s*\d+\s*轮/);
 await disclosure.focus();await page.keyboard.press('Enter');
 await expect(history).toHaveAttribute('open','');
 await expect(history.locator('.confirm-btn')).toHaveCount(0);
 const previewCalls=()=>probeFor(page).calls.filter(c=>c.method?.startsWith('previews/')).length;
 const calls=previewCalls();
 await clickInView(page, history.getByRole('button',{ includeHidden: true,name:'查看结果',exact:true}));
 await expect(history.locator('.historical-result')).toContainText('历史网页效果未存档');
 expect(previewCalls()).toBe(calls);
 await expect(page.locator('.message.assistant').last().locator('.confirm-btn')).toBeVisible();
 const after=(await(await page.request.get('/api/snapshot')).json()).state;
 expect(after.conversations[task.conversationId].rounds[0].id).toBe(first.id);
 expect(after.conversations[task.conversationId].rounds[0].phase).toBe('confirmed');
 expect(after.conversations[task.conversationId].rounds.at(-1).phase).toBe('finished');
 expect(after.taskGit[task.id].state).toBe(before.taskGit[task.id].state);
 await page.reload();
 await expect(page.locator('.assistant-turn-container.historical')).toHaveCount(1);
 await expect(page.locator('.confirm-btn')).toHaveCount(1);
 for(const [width,height] of [[390,844],[360,800]]){
  await page.setViewportSize({width,height});
  await clickInView(page, page.getByRole('button',{ includeHidden: true,name:'◌ 聊天',exact:true}));
  await expect(page.locator('#draft')).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 }
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
  await clickInView(page, trigger);
  const menu = page.getByRole('menu', { name: '项目菜单' });
  await expect(menu.getByRole('menuitemradio', { name: 'abc', exact: true })).toHaveAttribute('aria-checked', 'true');
  await clickInView(page, menu.getByRole('menuitem', { includeHidden: true, name: 'Git 设置', exact: true }));
  await expect(menu).toHaveCount(0);
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText('主分支');
  await clickInView(page, page.getByRole('button', { includeHidden: true, name: '取消', exact: true }));
  await clickInView(page, trigger);
  const refreshCount = probeFor(page).calls.filter(c => c.method === 'git/refresh').length;
  await clickInView(page, menu.getByRole('menuitem', { includeHidden: true, name: '刷新 Git 状态', exact: true }));
  await expect(menu).toHaveCount(0);
  await expect.poll(() => probeFor(page).calls.filter(c => c.method === 'git/refresh').length).toBeGreaterThan(refreshCount);
  await clickInView(page, trigger);
  await clickInView(page, menu.getByRole('menuitem', { includeHidden: true, name: '添加项目', exact: true }));
  await expect(menu).toHaveCount(0);
  await expect(page.locator('dialog:not(.chat-drawer)[open]')).toContainText('项目绝对路径');
});

test('project menu keyboard selection preserves scope drafts and dismisses predictably', async ({ page }) => {
  const ids = await projectMenuFixture(page);
  const trigger = page.getByRole('button', { name: '当前项目', exact: true });
  const menu = page.getByRole('menu', { name: '项目菜单' });
  await fillInChat(page, page.locator('#draft'), '第一个项目的草稿');
  await closeChat(page);
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
  await clickInView(page, trigger);
  await clickInView(page, menu.getByRole('menuitemradio', { includeHidden: true, name: 'abc', exact: true }));
  await expect(page.locator('#draft')).toHaveValue('第一个项目的草稿');
  await clickInView(page, trigger);
  await clickInView(page, page.locator('.toolbar h1'));
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
  await fillInChat(page, page.locator('#draft'), '保留当前任务的草稿');
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
    await clickInView(page, trigger);
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
 const page=await context.newPage();await page.goto('http://127.0.0.1:17331/');await openChat(page);
  await expect(page.locator('#chat-title')).toBeVisible();
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

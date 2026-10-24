import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createLocalState,
  selectConversation,
  setDraft,
  acceptSent,
  mergeSnapshot,
  createRefreshQueue,
  createOperationID,
  isStartRequest,
} from "../src/workbench.ts";
import { safePreviewURL, isWebPreviewable, isManifestApproved, recordManifestApproval, clearManifestApprovals } from "../src/preview.ts";

test("preview helper safePreviewURL, clearManifestApprovals, and static/web detection", () => {
  // safePreviewURL rejects traversal, hashes, and non-capability prefixes
  assert.equal(safePreviewURL("/preview/" + "a".repeat(64) + "/index.html"), "/preview/" + "a".repeat(64) + "/index.html");
  assert.equal(safePreviewURL("/preview/" + "a".repeat(64) + "/sub/page.html?v=1"), "/preview/" + "a".repeat(64) + "/sub/page.html?v=1");
  assert.equal(safePreviewURL("/preview/" + "a".repeat(64) + "/../secret"), undefined);
  assert.equal(safePreviewURL("http://evil.com/page"), undefined);

  // isWebPreviewable helper: checks if a preview is available as static or runnable command web preview
  assert.equal(isWebPreviewable({ state: 'stopped', kind: 'static', entry: 'index.html', message: 'ok', taskId: 't1', roundId: 'r1' }), true);
  assert.equal(isWebPreviewable({ state: 'running', url: '/preview/' + 'a'.repeat(64) + '/', message: 'ok', taskId: 't1', roundId: 'r1' }), true);
  assert.equal(isWebPreviewable({ state: 'stopped', command: ['npm', 'start'], message: 'ok', taskId: 't1', roundId: 'r1' }), true);
  assert.equal(isWebPreviewable({ state: 'unavailable', message: '暂无网页', taskId: 't1', roundId: 'r1' }), false);
  assert.equal(isWebPreviewable({ state: 'stopped', message: '纯后台工具', taskId: 't1', roundId: 'r1' }), false);
  assert.equal(isWebPreviewable(undefined), false);

  // In-memory manifest approval check & clearManifestApprovals
  assert.equal(isManifestApproved('t1', 'r1', 'm1'), false);
  recordManifestApproval('t1', 'r1', 'm1');
  assert.equal(isManifestApproved('t1', 'r1', 'm1'), true);
  // Different round or task does not share approval
  assert.equal(isManifestApproved('t1', 'r2', 'm1'), false);
  assert.equal(isManifestApproved('t2', 'r1', 'm1'), false);
  // clearManifestApprovals wipes in-memory authorization across all tasks/rounds
  clearManifestApprovals();
  assert.equal(isManifestApproved('t1', 'r1', 'm1'), false);
});

test("only an explicit start phrase starts development", () => {
  for (const s of ["开始开发", " 开始开发！ ", "开始开发。"])
    assert.equal(isStartRequest(s), true);
  for (const s of [
    "不要开始开发",
    "什么时候开始开发？",
    "开始开发这个功能",
    "",
  ])
    assert.equal(isStartRequest(s), false);
});
test("operation IDs work without secure-context randomUUID", () => {
  assert.equal(
    createOperationID((bytes) => {
      bytes.fill(0xab);
      return bytes;
    }),
    "ab".repeat(16),
  );
});
test("project/A/B drafts and late acceptance stay in their captured scope", () => {
  const local = createLocalState();
  selectConversation(local, "project");
  setDraft(local, "project draft");
  selectConversation(local, "A");
  setDraft(local, "old A");
  const revision = local.drafts.A!.revision;
  selectConversation(local, "B");
  setDraft(local, "new B");
  acceptSent(local, "A", revision);
  assert.equal(local.current, "B");
  assert.equal(local.drafts.B!.text, "new B");
  assert.equal(local.drafts.project!.text, "project draft");
  selectConversation(local, "A");
  setDraft(local, "new A");
  acceptSent(local, "A", revision);
  assert.equal(local.drafts.A!.text, "new A");
  selectConversation(local, "A");
  assert.equal(local.drafts.A!.text, "new A");
});
test("older snapshot cannot replace newer data", () => {
  const local = createLocalState();
  mergeSnapshot(local, { revision: 5 } as any);
  mergeSnapshot(local, { revision: 3 } as any);
  assert.equal(local.snapshot?.revision, 5);
});
test("overlapping refresh callers await the queued snapshot", async () => {
  let unblock!: () => void;
  let count = 0;
  const gate = new Promise<void>((resolve) => (unblock = resolve));
  const refresh = createRefreshQueue(async () => {
    count++;
    if (count === 1) await gate;
  });
  const first = refresh();
  let finished = false;
  const second = refresh().then(() => (finished = true));
  await Promise.resolve();
  assert.equal(finished, false);
  unblock();
  await Promise.all([first, second]);
  assert.equal(count, 2);
  assert.equal(finished, true);
});

test("only explicit merge phrases open an approval", async () => {
  const { isMergeRequest, gitStatusLabel } = await import("../src/git.ts");
  for (const text of [
    "合并到主分支",
    "请提交并合并到主分支。",
    " 合并到主分支！ ",
  ])
    assert.equal(isMergeRequest(text), true);
  for (const text of [
    "不要合并到主分支",
    "合并到主分支？",
    "合并到主分支\n",
    '"合并到主分支"',
    "请帮我看看是否合并到主分支",
  ])
    assert.equal(isMergeRequest(text), false);
  assert.equal(gitStatusLabel({ state: "merged" } as any), "已合并");
  assert.notEqual(gitStatusLabel(), "已合并");
});

test('socket patches append text without mutating prior snapshot and reject gaps', async () => {
  const { applyPatch } = await import('../src/sync.ts');
  const old:any={revision:2,conversations:{c:{messages:[{text:'hello'}]}}};
  const next=applyPatch(old,{after:2,revision:3,ops:[{op:'append',path:['conversations','c','messages','0','text'],value:' world'},{op:'replace',path:['revision'],value:3}]});
  assert.equal(next.conversations.c.messages[0].text,'hello world');
  assert.equal(old.conversations.c.messages[0].text,'hello');
  assert.throws(()=>applyPatch(old,{after:1,revision:3,ops:[]}));
  assert.throws(()=>applyPatch(old,{after:2,revision:3,ops:[{op:'replace',path:['__proto__','polluted'],value:true}]}));
});

test('snapshot chunks preserve Unicode and reject missing or interleaved parts',async()=>{
 const {SnapshotAssembler}=await import('../src/snapshot.ts');const a=new SnapshotAssembler();
 const original={type:'snapshot',state:{text:'你好\"\n'}};const raw=JSON.stringify(original);
 assert.equal(a.accept({type:'snapshot_start',parts:2}),null);
 assert.equal(a.accept({type:'snapshot_chunk',index:0,data:raw.slice(0,18)}),null);
 assert.equal(a.accept({type:'snapshot_chunk',index:1,data:raw.slice(18)}),null);
 assert.deepEqual(a.accept({type:'snapshot_end'}),original);
 a.accept({type:'snapshot_start',parts:2});assert.throws(()=>a.accept({type:'snapshot_chunk',index:1,data:'bad'}));
 const b=new SnapshotAssembler();b.accept({type:'snapshot_start',parts:1});assert.throws(()=>b.accept({type:'patch'}));
});

test('interrupted legacy tasks show recovery and real progress instead of result acceptance',async()=>{
 const {taskState}=await import('../src/task-state.ts');
 const t:any={status:'needs_confirmation',executionId:'x',request:'实现 /hello'};
 const c:any={runState:'interrupted',error:'进程中断',messages:[{role:'assistant',text:'已创建 /hello 路由，验证尚未结束。',delivery:'uncertain'}]};
 const x:any={id:'x',phase:'interrupted',tools:[{id:'w',name:'write',result:'saved'},{id:'test',name:'bash'}]};
 const v=taskState(t,c,x);
 assert.equal(v.status,'interrupted');assert.equal(v.label,'已中断');assert.equal(v.needsResume,true);assert.equal(v.confirmRoundId,undefined);
 assert.equal(v.returnedTools,1);assert.equal(v.unfinishedTools,1);assert.match(v.summary,/\/hello/);
 c.runState='idle';const restored=taskState(t,c,x);assert.equal(restored.label,'待继续');assert.equal(restored.needsResume,false);assert.equal(restored.confirmRoundId,undefined);
});
test('finished results remain confirmable when only the CLI session is offline',async()=>{
 const {taskState}=await import('../src/task-state.ts');const t:any={status:'needs_confirmation',executionId:'x'};
 const c:any={runState:'interrupted',messages:[],rounds:[{id:'r',phase:'finished',tools:[]}]};const x:any={id:'x',phase:'awaiting_review',finishedAt:'finished',tools:[]};
 assert.equal(taskState(t,c,x).label,'待验收');assert.equal(taskState(t,c,x).confirmRoundId,'r');
 c.rounds=[];assert.equal(taskState(t,c,x).confirmRoundId,'legacy:x:finished');
 x.phase='interrupted';assert.equal(taskState(t,c,x).confirmRoundId,undefined);
});

test('historical progress and reply navigation stop at the next round',async()=>{
 const {taskState}=await import('../src/task-state.ts');const t:any={status:'needs_confirmation'};
 const c:any={runState:'idle',rounds:[{id:'one',phase:'finished',tools:[]},{id:'two',phase:'finished',tools:[]}],messages:[{id:'one',role:'user',text:'old'},{id:'first',role:'assistant',text:'previous result'},{id:'two',role:'user',text:'new'},{id:'second',role:'assistant',text:'new result'}]};
 const selected=taskState(t,c,undefined,'one');assert.equal(selected.summary,'previous result');assert.equal(selected.replyId,'first');
});


test('finished and confirmed rounds retain a stable result target without reopening acceptance', async()=>{
 const {taskState}=await import('../src/task-state.ts');
 const t:any={status:'needs_confirmation',executionId:'x'};
 const c:any={runState:'idle',messages:[],rounds:[{id:'r',phase:'finished',tools:[]}]};
 const x:any={id:'x',phase:'awaiting_review',finishedAt:'2026-10-17T00:00:00Z',tools:[]};
 assert.equal(taskState(t,c,x).resultRoundId,'r');
 t.status='completed';x.phase='confirmed';c.rounds[0].phase='confirmed';
 const completed=taskState(t,c,x);
 assert.equal(completed.resultRoundId,'r');assert.equal(completed.confirmRoundId,undefined);assert.equal(completed.resultConfirmed,true);
 c.rounds.push({id:'new',phase:'running',tools:[]});c.runState='running';t.status='running';
 assert.equal(taskState(t,c,x).resultRoundId,undefined);
 assert.equal(taskState(t,c,x,'r').resultRoundId,'r');
});


test('only the scoped preview gateway can become an iframe target',async()=>{
 const {safePreviewURL}=await import('../src/preview.ts');
 const valid='/preview/'+'a'.repeat(64)+'/hello?name=world';
 assert.equal(safePreviewURL(valid),valid);
 for(const value of ['https://example.com','//example.com','/api/snapshot','/preview/short/','/preview/'+'a'.repeat(64)+'/../../api/snapshot','/preview/'+'a'.repeat(64)+'/%2e%2e/api','/preview/'+'a'.repeat(64)+'/a%2fb',undefined])assert.equal(safePreviewURL(value),undefined);
});

import {groupChatMessages,replySummary,messageText,replyLinks,filterChatEntries,turnSummary} from '../src/chat-messages.ts';
test('chat grouping merges all progress and final messages per user turn into one AI bubble', () => {
 const messages = [
  { id: 'u1', role: 'user', text: '开发接口', delivery: 'complete' },
  { id: 'p1', role: 'assistant', kind: 'progress', text: '检查项目', delivery: 'complete' },
  { id: 'p2', role: 'assistant', kind: 'progress', text: '验证接口', delivery: 'complete' },
  { id: 'a1', role: 'assistant', kind: 'summary', text: '接口已完成', delivery: 'complete' },
  { id: 'u2', role: 'user', text: '继续修改', delivery: 'complete' },
  { id: 'p3', role: 'assistant', kind: 'progress', text: '检查新要求', delivery: 'complete' },
 ];
 const original = JSON.stringify(messages);
 const entries = groupChatMessages(messages);
 // Per-turn grouping:
 // u1 -> single assistant bubble combining [p1, p2, a1]
 // u2 -> assistant bubble containing [p3]
 assert.deepEqual(entries.map(e => e.updates?.map(m => m.id) ?? [e.message.id]), [['u1'], ['p1', 'p2', 'a1'], ['u2'], ['p3']]);
 assert.equal(entries[1]!.message.id, 'a1'); // latest message represents the bubble
 assert.equal(entries[1]!.userMessage?.id, 'u1');
 assert.equal(JSON.stringify(messages), original);
});

test('chat grouping creates placeholder for active running/starting turns without assistant message, and stops loading on interrupt/fail/waiting', () => {
 const messages = [
  { id: 'u1', role: 'user', text: '启动分析', delivery: 'accepted' },
 ];
 // Starting with no assistant reply yet:
 const starting = groupChatMessages(messages, { runState: 'starting' });
 assert.equal(starting.length, 2);
 assert.equal(starting[1]!.role, 'assistant');
 assert.equal(starting[1]!.isPlaceholder, true);
 assert.equal(starting[1]!.loading, true);
 assert.equal(starting[1]!.userMessage?.id, 'u1');

 // Running with no assistant reply yet:
 const running = groupChatMessages(messages, { runState: 'running' });
 assert.equal(running[1]!.loading, true);

 // Interrupted or failed: placeholder exists, loading stopped:
 const interrupted = groupChatMessages(messages, { runState: 'interrupted' });
 assert.equal(interrupted[1]!.loading, false);
 assert.equal(interrupted[1]!.isPlaceholder, true);

 const failed = groupChatMessages(messages, { runState: 'failed' });
 assert.equal(failed[1]!.loading, false);

 const waiting = groupChatMessages(messages, { runState: 'running', taskStatus: 'waiting_user' });
 assert.equal(waiting[1]!.loading, false);
});

test('turnSummary uses report.summary when present, or 1-2 sentence status/reply without code blocks, without fake success', () => {
 // Case 1: Structured report.summary
 const entryWithReport: any = {
  id: 'a1',
  role: 'assistant',
  message: { id: 'm1', role: 'assistant', text: '详细技术日志\n```go\ncode\n```', delivery: 'complete' },
  round: { id: 'u1', report: { summary: '已完成接口开发，端口可通过本地访问。' } },
 };
 assert.equal(turnSummary(entryWithReport), '已完成接口开发，端口可通过本地访问。');

 // Case 2: Running with progress message (currently doing, not implementation detail)
 const entryRunning: any = {
  id: 'a2',
  role: 'assistant',
  loading: true,
  message: { id: 'p1', role: 'assistant', kind: 'progress', text: '正在验证接口路由与数据库连接。\n\n具体执行：npm test -- --grep route', delivery: 'complete' },
 };
 assert.equal(turnSummary(entryRunning), '正在验证接口路由与数据库连接。');

 // Case 3: Placeholder running/starting
 const entryPlaceholder: any = {
  id: 'ph',
  role: 'assistant',
  isPlaceholder: true,
  loading: true,
  runState: 'starting',
 };
 assert.equal(turnSummary(entryPlaceholder), '正在准备启动…');

 // Case 4: Raw text with code blocks stripped to 1-2 sentences
 const entryCode: any = {
  id: 'a3',
  role: 'assistant',
  message: { id: 'm3', role: 'assistant', text: '服务已正常运行。请在浏览器测试。\n```bash\ncurl http://localhost:8000\n```\n更多详情见日志。', delivery: 'complete' },
 };
 assert.equal(turnSummary(entryCode), '服务已正常运行。请在浏览器测试。');
});

test('turnSummary constraints: limits to 2 sentences, strips commands and implementation lists, and falls back cleanly without fake success', () => {
 // Length > 200 chars and many sentences
 const longSummary = '第一句话关于总体进展。第二句话关于测试情况。第三句话说明更多细节。第四句话关于后续规划。';
 const entryLong: any = {
  id: 'a-long',
  role: 'assistant',
  round: { id: 'u1', report: { summary: longSummary } },
 };
 const sLong = turnSummary(entryLong);
 assert.equal(sLong, '第一句话关于总体进展。第二句话关于测试情况。');

 // Non-fence shell commands and implementation task list
 const commandSummary = `
已完成当前模块修复。
$ go test ./...
> npm run build
- 修复了模型参数转换
* 完善了单元测试覆盖
1. 第一项具体实现
展开查看完整日志。
`;
 const entryCmd: any = {
  id: 'a-cmd',
  role: 'assistant',
  message: { id: 'm-cmd', role: 'assistant', text: commandSummary, delivery: 'complete' },
 };
 const sCmd = turnSummary(entryCmd);
 assert.equal(sCmd, '已完成当前模块修复。');
 assert.ok(!sCmd.includes('go test'));
 assert.ok(!sCmd.includes('npm run build'));
 assert.ok(!sCmd.includes('修复了模型参数转换'));

 // Empty text fallback
 const entryEmpty: any = {
  id: 'a-empty',
  role: 'assistant',
  message: { id: 'm-empty', role: 'assistant', text: '', delivery: 'complete' },
 };
 assert.equal(turnSummary(entryEmpty), '本条更新没有文字说明，展开查看记录。');
});

test('taskState correctly tracks confirmRoundId and resultRoundId across finished, confirmed, interrupted, and awaiting_user rounds with conversation', async () => {
 const { taskState } = await import('../src/task-state.ts');
 const task: any = { id: 't1', status: 'needs_confirmation', executionId: 'e1' };
 const execution: any = { id: 'e1', phase: 'awaiting_review' };
 const conversation: any = {
  id: 'c1',
  runState: 'idle',
  rounds: [
   { id: 'r1', phase: 'finished', report: { summary: '完成接口开发' } },
  ],
  messages: [{ id: 'r1', role: 'user', text: '开发接口' }],
 };

 // Case 1: Finished round with conversation -> confirmRoundId present
 const stateFinished = taskState(task, conversation, execution);
 assert.equal(stateFinished.status, 'needs_confirmation');
 assert.equal(stateFinished.confirmRoundId, 'r1');

 // Case 2: Confirmed round -> resultRoundId present and resultConfirmed true
 const completedTask: any = { id: 't1', status: 'completed', executionId: 'e1' };
 const confirmedConv: any = {
  id: 'c1',
  runState: 'idle',
  rounds: [
   { id: 'r1', phase: 'confirmed', report: { summary: '完成并已验收' } },
  ],
  messages: [{ id: 'r1', role: 'user', text: '开发接口' }],
 };
 const stateConfirmed = taskState(completedTask, confirmedConv, execution);
 assert.equal(stateConfirmed.status, 'completed');
 assert.equal(stateConfirmed.resultConfirmed, true);
 assert.equal(stateConfirmed.resultRoundId, 'r1');

 // Case 3: Interrupted after finish -> preserves result / confirmRoundId
 const interruptedConv: any = {
  ...conversation,
  runState: 'interrupted',
 };
 const stateInterrupted = taskState(task, interruptedConv, execution);
 assert.equal(stateInterrupted.confirmRoundId, 'r1');

 // Case 4: Awaiting user / blocking interaction -> waiting_user, no confirmRoundId
 const waitingConv: any = {
  ...conversation,
  interactions: [
   { id: 'ix1', status: 'pending', blocking: true, expiresAt: new Date(Date.now() + 60000).toISOString() },
  ],
 };
 const stateWaiting = taskState(task, waitingConv, execution);
 assert.equal(stateWaiting.status, 'waiting_user');
 assert.equal(stateWaiting.confirmRoundId, undefined);
});

test('filterChatEntries purely filters questions without mutating messages or losing data', () => {
 const entries: any[] = [
  { id: 'u1', role: 'user', message: { id: 'u1', role: 'user', text: '问题一' } },
  { id: 'a1', role: 'assistant', message: { id: 'a1', role: 'assistant', text: '回答一' } },
  { id: 'u2', role: 'user', message: { id: 'u2', role: 'user', text: '问题二' } },
  { id: 'a2', role: 'assistant', message: { id: 'a2', role: 'assistant', text: '回答二' } },
 ];
 const all = filterChatEntries(entries, 'all');
 assert.equal(all.length, 4);

 const questions = filterChatEntries(entries, 'questions');
 assert.equal(questions.length, 2);
 assert.deepEqual(questions.map(q => q.id), ['u1', 'u2']);
 assert.equal(entries.length, 4); // pure
});
test('reply summaries use the actual report when visible prose is empty and retain malformed reports in details',()=>{
 const report={id:'report',role:'assistant',delivery:'complete',displayText:'',text:'<kaiwu-report>{"summary":"已实现 hello 接口，正在核对浏览器访问。"}</kaiwu-report>'};
 assert.equal(replySummary(report),'已实现 hello 接口，正在核对浏览器访问。');
 const malformed={id:'bad',role:'assistant',delivery:'complete',text:'执行已返回。<kaiwu-report>bad JSON</kaiwu-report>'};
 assert.equal(messageText(malformed),malformed.text);
 assert.equal(replySummary(malformed),'执行已返回。');
 assert.ok(replySummary({id:'path',role:'assistant',delivery:'complete',text:'正在检查 `/hello_world` 接口。'}).includes('/hello_world'));
});

test('reply links preserve complete HTTP targets outside the short summary and ignore unsafe or credential URLs',()=>{
 const message={id:'urls',role:'assistant',delivery:'complete',text:'## 完成\n\n- 接口已验证\n- 局域网可访问\n\n更多说明。\n\n访问 http://192.168.2.122:8000/hello?name=world。还有 [JSON](http://192.168.2.122:8000/hello-json)。重复 http://192.168.2.122:8000/hello?name=world 和 javascript:alert(1) 以及 https://user:secret@example.com/。'};
 assert.deepEqual(replyLinks(message),['http://192.168.2.122:8000/hello?name=world','http://192.168.2.122:8000/hello-json']);
 assert.ok(replySummary(message).includes('\n- 接口已验证'));
});

test('deliveryView pure projection maps phases, repairs, block reasons, and safe action links', async () => {
 const { deliveryView, canAcceptDelivery } = await import('../src/delivery.ts');
 assert.equal(deliveryView(undefined).active, false);
 assert.equal(deliveryView(undefined).label, '未启用');

 const summary: any = {
  id: 'del_1',
  projectId: 'p1',
  taskId: 't1',
  phase: 'task_ci',
  candidateSHA: '11223344556677889900aabbccddeeff11223344',
  requireAcceptance: true,
  repairsUsed: 1,
  repairsLimit: 3,
  steps: [{ sequence: 1, kind: 'check', summary: 'local passed' }],
  ci: [{ stage: 'task', status: 'completed', conclusion: 'failure', url: 'https://github.com/org/repo/actions/runs/123' }],
 };
 const view = deliveryView(summary);
 assert.equal(view.active, true);
 assert.equal(view.phase, 'task_ci');
 assert.equal(view.label, '任务 CI 中');
 assert.equal(view.repairsText, '1/3 次修复');
 assert.equal(view.ciFailed, true);
 assert.equal(canAcceptDelivery(summary), false);
 assert.equal(view.ciRuns[0].url, 'https://github.com/org/repo/actions/runs/123');
});

test('snapshot handles optional deliveries and deliveryPolicies without breaking legacy projects', () => {
 const local = createLocalState();
 const legacySnapshot: any = {
  revision: 1,
  projects: {},
  topics: [],
  tasks: {},
  conversations: {},
  profile: { provider: '', model: '', thinking: '' },
 };
 mergeSnapshot(local, legacySnapshot);
 assert.equal(local.snapshot?.deliveries, undefined);
 assert.equal(local.snapshot?.deliveryPolicies, undefined);

 const deliverySnapshot: any = {
  revision: 2,
  projects: {},
  topics: [],
  tasks: {},
  conversations: {},
  profile: { provider: '', model: '', thinking: '' },
  deliveries: { t1: { id: 'd1', phase: 'local_checking', repairsUsed: 0, repairsLimit: 3 } },
  deliveryPolicies: { p1: { version: 1, enabled: true, requireAcceptance: true } },
 };
 mergeSnapshot(local, deliverySnapshot);
 assert.equal(local.snapshot?.deliveries?.t1.phase, 'local_checking');
 assert.equal(local.snapshot?.deliveryPolicies?.p1.enabled, true);
});

test('CI red light is not masked by task.status=completed and human acceptance is independent', async () => {
 const { deliveryView, canAcceptDelivery } = await import('../src/delivery.ts');
 const summary: any = {
  id: 'del_fail',
  phase: 'blocked',
  blockCode: 'main_ci_failed',
  blockReason: '主分支 CI 门禁失败',
  candidateSHA: 'abc',
  localMainSHA: 'abc',
  requireAcceptance: false,
  repairsUsed: 3,
  repairsLimit: 3,
  ci: [{ stage: 'main', status: 'completed', conclusion: 'failure', url: 'https://github.com/org/repo/actions/runs/456' }],
 };
 const view = deliveryView(summary);
 assert.equal(view.active, true);
 assert.equal(view.blocked, true);
 assert.equal(view.ciFailed, true);
 assert.equal(view.blockReason, '主分支 CI 门禁失败');
 assert.equal(canAcceptDelivery(summary), false);
});

test('exact accept condition requires awaiting_acceptance and valid token', async () => {
 const { canAcceptDelivery, deliveryView } = await import('../src/delivery.ts');
 const waiting: any = {
  id: 'del_wait',
  version: 3,
  phase: 'awaiting_acceptance',
  requireAcceptance: true,
  acceptanceToken: 'tok_secret_123',
  candidateSHA: '11223344556677889900aabbccddeeff11223344',
  repairsUsed: 0,
  repairsLimit: 3,
 };
 assert.equal(canAcceptDelivery(waiting), true);
 assert.equal(deliveryView(waiting).canAccept, true);

 const noToken = { ...waiting, acceptanceToken: '' };
 assert.equal(canAcceptDelivery(noToken), false);

 const falseMode = { ...waiting, requireAcceptance: false };
 assert.equal(canAcceptDelivery(falseMode), false);
});

test('safe Actions links filter unsafe or credentialed URLs and budget uses repairsLimit', async () => {
 const { deliveryView } = await import('../src/delivery.ts');
 const summary: any = {
  id: 'del_safe',
  phase: 'succeeded',
  repairsUsed: 2,
  repairsLimit: 5,
  ci: [
   { stage: 'task', url: 'https://github.com/foo/bar/actions/runs/999' },
   { stage: 'task', url: 'javascript:alert(1)' },
   { stage: 'main', url: 'https://user:pass@github.com/foo/bar/actions/runs/888' },
   { stage: 'main', url: 'http://insecure.example.com' },
  ],
 };
 const view = deliveryView(summary);
 assert.equal(view.repairsText, '2/5 次修复');
 assert.equal(view.ciRuns.length, 1);
 assert.equal(view.ciRuns[0].url, 'https://github.com/foo/bar/actions/runs/999');
});

test('validatePolicyDraft checks repository, checks, workflows and credentials without sending token', async () => {
 const { validatePolicyDraft, defaultPolicyDraft } = await import('../src/delivery.ts');
 const d = defaultPolicyDraft('proj_1');
 assert.equal(d.enabled, false);
 assert.equal(d.maxRepairs, 3);
 assert.equal(d.requireAcceptance, true);

 // Disabled draft can be saved even if incomplete
 const errDisabled = validatePolicyDraft(d);
 assert.equal(errDisabled, null);

 // Enabled draft requires full configuration
 d.enabled = true;
 assert.match(validatePolicyDraft(d) ?? '', /授权/);

 d.authorized = true;
 assert.match(validatePolicyDraft(d) ?? '', /仓库地址/);

 d.repository.url = 'https://github.com/org/repo.git';
 d.repository.owner = 'org';
 d.repository.name = 'repo';
 d.repository.id = 12345;
 d.repository.mainBranch = 'main';
 assert.match(validatePolicyDraft(d) ?? '', /本地检查/);

 d.checks = [{ id: 'test', argv: ['npm', 'test'], dir: '.', timeoutSeconds: 300 }];
 assert.match(validatePolicyDraft(d) ?? '', /workflow/);

 d.taskWorkflows = [{ id: 101, jobs: ['build'] }];
 d.mainWorkflows = [{ id: 102, jobs: ['deploy'] }];
 assert.match(validatePolicyDraft(d) ?? '', /凭据引用/);

 d.gitCredentialRef = 'server_github_https';
 d.apiCredentialRef = 'server_github_api';
 assert.equal(validatePolicyDraft(d), null);
});

test('validatePolicyDraft rejects non-array argv, null inputs, and credentialed URLs even when disabled', async () => {
 const { validatePolicyDraft, defaultPolicyDraft } = await import('../src/delivery.ts');
 const d = defaultPolicyDraft('proj_1');

 // Credentialed URL must be rejected even when disabled (no leaking token in disabled draft)
 d.repository.url = 'https://user:pass@github.com/org/repo.git';
 assert.match(validatePolicyDraft(d) ?? '', /凭据/);

 d.repository.url = 'https://github.com/org/repo.git';
 d.enabled = true;
 d.authorized = true;
 d.repository.owner = 'org';
 d.repository.name = 'repo';
 d.repository.id = 123;
 d.repository.mainBranch = 'main';
 d.gitCredentialRef = 'server_github_https';
 d.apiCredentialRef = 'server_github_api';
 d.taskWorkflows = [{ id: 1, jobs: [] }];
 d.mainWorkflows = [{ id: 2, jobs: [] }];

 // Malformed checks: non-array, null, or argv elements not string
 d.checks = null as any;
 assert.match(validatePolicyDraft(d) ?? '', /本地检查/);

 d.checks = [{ id: 'test', argv: 'npm test' as any, dir: '.', timeoutSeconds: 300 }];
 assert.match(validatePolicyDraft(d) ?? '', /数组/);

 d.checks = [{ id: 'test', argv: [123 as any], dir: '.', timeoutSeconds: 300 }];
 assert.match(validatePolicyDraft(d) ?? '', /字符串/);

 d.checks = [{ id: 'test', argv: ['npm', 'test'], dir: '../escape', timeoutSeconds: 300 }];
 assert.match(validatePolicyDraft(d) ?? '', /工作区内/);

 d.checks = [{ id: 'test', argv: ['npm', 'test'], dir: '.', timeoutSeconds: 300 }];
 assert.equal(validatePolicyDraft(d), null);
});

test('buildSavePolicyPayload packages operationId, expectedVersion and policy object', async () => {
 const { buildSavePolicyPayload, defaultPolicyDraft } = await import('../src/delivery.ts');
 const draft = defaultPolicyDraft('proj_x');
 draft.version = 4;
 const payload = buildSavePolicyPayload('op_save_1', draft);
 assert.equal(payload.operationId, 'op_save_1');
 assert.equal(payload.expectedVersion, 4);
 assert.equal(payload.policy.projectId, 'proj_x');
 assert.equal(payload.policy.version, 4);
});

test('isZeroTime checks Go zero time string and empty states correctly', async () => {
 const { isZeroTime } = await import('../src/delivery.ts');
 assert.equal(isZeroTime('0001-01-01T00:00:00Z'), true);
 assert.equal(isZeroTime('0001-01-01T00:00:00.000Z'), true);
 assert.equal(isZeroTime(''), true);
 assert.equal(isZeroTime(undefined), true);
 assert.equal(isZeroTime('2026-10-18T12:00:00Z'), false);
});

test('canPauseDelivery checks phase validity according to backend rules', async () => {
 const { canPauseDelivery } = await import('../src/delivery.ts');
 assert.equal(canPauseDelivery('task_ci'), true);
 assert.equal(canPauseDelivery('local_checking'), true);
 assert.equal(canPauseDelivery('awaiting_acceptance'), true);
 assert.equal(canPauseDelivery('blocked'), false);
 assert.equal(canPauseDelivery('paused'), false);
 assert.equal(canPauseDelivery('succeeded'), false);
 assert.equal(canPauseDelivery('cancelled'), false);
});

test('DeliveryStep history merging sorts and deduplicates by id', () => {
 const recentSteps = [
  { id: 'step-20', sequence: 20, kind: 'budget', summary: 'step 20', startedAt: '2026-10-18T10:00:00Z', exitKnown: true, exitCode: 0 },
  { id: 'step-21', sequence: 21, kind: 'ci', summary: 'step 21', startedAt: '2026-10-18T10:05:00Z', exitKnown: true, exitCode: 0 },
 ];
 const pagedSteps = [
  { id: 'step-1', sequence: 1, kind: 'local', summary: 'step 1', startedAt: '2026-10-18T09:00:00Z', exitKnown: true, exitCode: 0 },
  { id: 'step-20', sequence: 20, kind: 'budget', summary: 'step 20 updated', startedAt: '2026-10-18T10:00:00Z', exitKnown: true, exitCode: 0 },
 ];

 const map = new Map<string, any>();
 for (const s of pagedSteps) map.set(s.id, s);
 for (const s of recentSteps) map.set(s.id, s);
 const all = Array.from(map.values()).sort((a, b) => a.sequence - b.sequence);

 assert.equal(all.length, 3);
 assert.equal(all[0].id, 'step-1');
 assert.equal(all[1].id, 'step-20');
 assert.equal(all[1].summary, 'step 20'); // recent overrides paged
 assert.equal(all[2].id, 'step-21');
});

test('parseArgsString and formatArgsString handle spaces, quotes, tabs and roundtrip without loss', async () => {
 const { parseArgsString, formatArgsString, checksToUI, uiToChecks, parseGitHubRepo, generateNextCheckId, isEquivalentRepo } = await import('../src/delivery-settings.ts');

 assert.deepEqual(parseArgsString('test ./...'), { args: ['test', './...'] });
 assert.deepEqual(parseArgsString('run --name "hello world" --flag'), { args: ['run', '--name', 'hello world', '--flag'] });
 assert.deepEqual(parseArgsString("run 'single quote with space'"), { args: ['run', 'single quote with space'] });
 assert.deepEqual(parseArgsString(''), { args: [] });

 // 严格检验未闭合错误与转义错误
 assert.match(parseArgsString('run "unclosed').error || '', /未闭合的双引号/);
 assert.match(parseArgsString("run 'unclosed").error || '', /未闭合的单引号/);
 assert.match(parseArgsString('run trailing\\').error || '', /未完成的反斜杠转义/);

 // 无损 roundtrip 集合（含引号、tab、空串、反斜杠）
 const cases = [
  ['test', './...'],
  ['run', '--name', 'hello world'],
  ['echo', 'a"b'],
  ['echo', "it's"],
  ['run', '', 'x'],
  ['echo', 'a\tb'],
  ['echo', 'a\\b'],
  ['echo', 'a\\"b'],
  ['go', 'test', '-run', 'Test"X'],
  ['echo', '"leading'],
  ['printf', 'a b\tc'],
 ];
 for (const c of cases) {
  const formatted = formatArgsString(c);
  const parsed = parseArgsString(formatted);
  assert.equal(parsed.error, undefined);
  assert.deepEqual(parsed.args, c);
 }

 const checks = [{ id: 'test', argv: ['go', 'test', '-run', 'Test With Space'], dir: '.', timeoutSeconds: 300 }];
 const ui = checksToUI(checks);
 assert.equal(ui.length, 1);
 assert.equal(ui[0].program, 'go');
 assert.deepEqual(ui[0].args, ['test', '-run', 'Test With Space']);

 const convertedBack = uiToChecks(ui);
 assert.deepEqual(convertedBack[0].argv, ['go', 'test', '-run', 'Test With Space']);

 // 唯一 ID 生成
 assert.equal(generateNextCheckId([]), 'check-1');
 assert.equal(generateNextCheckId([{ id: 'check-1' }, { id: 'check-2' }]), 'check-3');
 assert.equal(generateNextCheckId([{ id: 'check-2' }]), 'check-3'); // 删去 check-1 后新增应为 check-3，绝不重复 check-2
 assert.equal(generateNextCheckId([{ id: 'check-1' }, { id: 'custom' }]), 'check-2');

 // 仓库解析与等价判定
 assert.deepEqual(parseGitHubRepo('https://github.com/acme/sample.git'), { owner: 'acme', name: 'sample' });
 assert.deepEqual(parseGitHubRepo('git@github.com:foo/bar.git'), { owner: 'foo', name: 'bar' });
 assert.deepEqual(parseGitHubRepo('ssh://git@github.com/foo/bar.git'), { owner: 'foo', name: 'bar' });
 assert.deepEqual(parseGitHubRepo('https://github.com/foo/bar'), { owner: 'foo', name: 'bar' });
 assert.deepEqual(parseGitHubRepo('invalid-url'), { owner: '', name: '' });

 assert.equal(isEquivalentRepo('https://github.com/foo/bar.git', 'https://github.com/foo/bar'), true);
 assert.equal(isEquivalentRepo('https://github.com/foo/bar.git', 'git@github.com:foo/bar.git'), true);
 assert.equal(isEquivalentRepo('https://github.com/foo/bar.git', 'ssh://git@github.com/foo/bar.git'), true);
 assert.equal(isEquivalentRepo('https://github.com/foo/bar.git', 'https://github.com/foo/other.git'), false);
 assert.equal(isEquivalentRepo('https://github.com/foo/bar.git', ''), false);
});

test('result-history pure functions handle time, displayText, multiline stripping, honest fallback without 第N轮', async () => {
 const {
  formatRoundTime,
  cleanTextSummary,
  getRoundRequestSummary,
  getRoundOptions,
  formatResultHeading,
 } = await import('../src/result-history.ts');

 // 1. formatRoundTime
 const formattedTime = formatRoundTime('2026-10-18T10:30:00Z');
 assert.match(formattedTime, /^10\/18 \d\d:\d\d$/);
 assert.equal(formatRoundTime(''), '');
 assert.equal(formatRoundTime('invalid-date'), '');

 // 2. cleanTextSummary: 多行压缩、空白折叠、超长省略、JSON unescape
 const multiline = '第一行需求\n  第二行说明\t\t更多细节  \n 结束';
 assert.equal(cleanTextSummary(multiline, 20), '第一行需求 第二行说明 更多细节 结束');
 const jsonSummary = '{"request":"从 JSON 解析的请求内容"}';
 assert.equal(cleanTextSummary(jsonSummary, 30), '从 JSON 解析的请求内容');
 const longText = '这是一个超过二十个字符非常非常非常长的需求摘要文本用来测试截断';
 assert.equal(cleanTextSummary(longText, 15), '这是一个超过二十个字符非常非常…');

 // 3. getRoundRequestSummary: 优先 displayText -> text -> report.summary -> 请求记录不可用
 const mockRound: any = {
  id: 'round-1',
  userMessageId: 'msg-1',
  createdAt: '2026-10-18T10:30:00Z',
  report: { summary: '报告备选摘要' },
 };
 const convWithDisplayText: any = {
  messages: [
   { id: 'round-1', role: 'user', displayText: '优先展示的显示文本\n带换行', text: 'raw prompt text' },
  ],
 };
 assert.equal(getRoundRequestSummary(mockRound, convWithDisplayText), '优先展示的显示文本 带换行');

 const convWithTextOnly: any = {
  messages: [
   { id: 'msg-1', role: 'user', text: '退回的原始文本' },
  ],
 };
 assert.equal(getRoundRequestSummary(mockRound, convWithTextOnly), '退回的原始文本');

 const convEmpty: any = { messages: [] };
 assert.equal(getRoundRequestSummary(mockRound, convEmpty), '报告备选摘要');

 const roundNoReport: any = { id: 'round-x' };
 assert.equal(getRoundRequestSummary(roundNoReport, convEmpty), '请求记录不可用');

 // 4. getRoundOptions: 不含“第N轮”，输出格式为 "MM/DD HH:mm · 摘要"
 const rounds = [mockRound];
 const options = getRoundOptions(rounds, convWithDisplayText);
 assert.equal(options.length, 1);
 assert.equal(options[0].id, 'round-1');
 assert.match(options[0].label, /^10\/18 \d\d:\d\d · 优先展示的显示文本 带换行$/);
 assert.ok(!options[0].label.includes('第 1 轮'));
 assert.ok(!options[0].label.includes('第1轮'));

 // 5. formatResultHeading: 最新/历史结果 · 时间 · 需求摘要，不含“第N轮”
 const latestHeading = formatResultHeading(mockRound, true, convWithDisplayText);
 assert.match(latestHeading, /^最新结果 · 10\/18 \d\d:\d\d · 优先展示的显示文本 带换行$/);
 assert.ok(!latestHeading.includes('第 1 轮'));

 const historyHeading = formatResultHeading(mockRound, false, convWithDisplayText);
 assert.match(historyHeading, /^历史结果 · 10\/18 \d\d:\d\d · 优先展示的显示文本 带换行$/);
 assert.ok(!historyHeading.includes('第 1 轮'));
});
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

import {groupChatMessages,replySummary,messageText,replyLinks} from '../src/chat-messages.ts';
test('chat grouping preserves every message and separates progress across user turns and final replies',()=>{
 const messages=[
  {id:'u1',role:'user',text:'开发接口',delivery:'complete'},
  {id:'p1',role:'assistant',kind:'progress',text:'检查项目',delivery:'complete'},
  {id:'p2',role:'assistant',kind:'progress',text:'验证接口',delivery:'complete'},
  {id:'a1',role:'assistant',kind:'summary',text:'接口已完成',delivery:'complete'},
  {id:'u2',role:'user',text:'继续修改',delivery:'complete'},
  {id:'p3',role:'assistant',kind:'progress',text:'检查新要求',delivery:'complete'},
 ];
 const original=JSON.stringify(messages),entries=groupChatMessages(messages);
 assert.deepEqual(entries.map(e=>e.updates?.map(m=>m.id)??[e.message.id]),[['u1'],['p1','p2'],['a1'],['u2'],['p3']]);
 assert.equal(entries[1]!.message.id,'p2');
 assert.equal(JSON.stringify(messages),original);
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

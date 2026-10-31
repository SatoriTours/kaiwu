import { test } from "node:test";
import assert from "node:assert/strict";
import { failureView } from "../src/failure.ts";
import { groupChatMessages } from "../src/chat-messages.ts";
import type { Conversation, Round } from "../src/types.ts";

const ERROR="模型调用失败；已有内容保留：429 已达到 Token Plan 速率限制，请稍后重试。";
const SESSION="pi 会话已中断，发送下一条消息时自动接回；历史已保留";
function conv(phase="interrupted", error=SESSION): Conversation {
 return {id:"c",projectId:"p",sessionId:"s",profile:{provider:"fake",model:"fake",thinking:""},runState:phase,error,
 messages:[{id:"u",role:"user",text:"继续",delivery:"complete"},{id:"a",role:"assistant",text:"已生成内容",delivery:"complete"}],rounds:[]};
}
function round(phase:string):Round{return {id:"u",phase,startedAt:"2026-10-31T10:00:00Z",finishedAt:"2026-10-31T10:01:00Z",tools:[]}}
function view(c:Conversation){const entries=groupChatMessages(c.messages,{runState:c.runState,rounds:c.rounds});return failureView(c,undefined,entries.at(-1));}

test("idle disconnection after a finished answer is silent",()=>{
 const c=conv();c.rounds=[round("answered")];assert.equal(view(c),undefined);
});
test("a completed development result is not a connection failure",()=>{
 const c=conv();c.rounds=[round("confirmed")];assert.equal(view(c),undefined);
});
test("legacy completed messages stay silent on disconnect",()=>{
 assert.equal(view(conv()),undefined);
 const c=conv();c.messages.pop();
 assert.equal(groupChatMessages(c.messages,{runState:c.runState}).filter(e=>e.isPlaceholder).length,0);
});
test("a real interrupted round gets one neutral line, not an invented cause",()=>{
 const c=conv();c.rounds=[round("interrupted")];
 const v=view(c);assert.ok(v);assert.equal(v.text,"回复已中断");assert.equal(v.detail,"");assert.equal(v.kind,"session");
});
test("an unfinished assistant message is enough evidence for legacy interruption",()=>{
 const c=conv();c.messages[1]!.delivery="uncertain";assert.equal(view(c)?.text,"回复已中断");
});
test("a provider failure is concise, with the original reason only in details",()=>{
 const c=conv("failed",ERROR);c.rounds=[round("failed")];
 const v=view(c);assert.ok(v);assert.equal(v.text,"本次请求受到限流，请稍后再试。");assert.match(v.detail,/429.*Token Plan/);assert.equal(v.placement,"assistant");
});
test("unknown failure has no empty reason or fixed-message details",()=>{
 const c=conv("failed","模型调用失败或被中断；已有内容保留");c.rounds=[round("failed")];
 const v=view(c);assert.ok(v);assert.equal(v.text,"本轮未完成");assert.equal(v.detail,"");
});
test("a persisted historical failure stays with its round during a new request",()=>{
 const c=conv("running","");c.rounds=[{...round("failed"),failure:{kind:"execution",detail:ERROR}}];
 c.messages.push({id:"u2",role:"user",text:"下一轮",delivery:"accepted"});c.rounds.push({...round("running"),id:"u2",finishedAt:undefined});
 const entries=groupChatMessages(c.messages,{runState:c.runState,rounds:c.rounds});
 assert.match(failureView(c,undefined,entries[1])?.detail??"",/429/);
 assert.equal(failureView(c,undefined,entries.at(-1)),undefined);
});
test("global error never contaminates an older round",()=>{
 const c=conv("failed",ERROR);c.rounds=[round("answered"),{...round("failed"),id:"u2"}];c.messages.push({id:"u2",role:"user",text:"新问题",delivery:"failed"});
 const entries=groupChatMessages(c.messages,{runState:c.runState,rounds:c.rounds});
 assert.equal(failureView(c,undefined,entries[1]),undefined);
});
test("unknown send delivery does not offer automatic retry",()=>{
 const c=conv("interrupted","消息未能确认完成；请检查后明确恢复，不会自动重发");
 c.messages=[{id:"u",role:"user",text:"修改",delivery:"uncertain"}];c.rounds=[round("interrupted")];
 const v=view(c);assert.ok(v);assert.equal(v.text,"发送状态待确认");assert.equal(v.placement,"user");assert.equal(v.retry,false);
});
test("definitely unsent message can be explicitly retried",()=>{
 const c=conv("failed","pi 启动或协议握手失败，请检查配置后恢复");
 c.messages=[{id:"u",role:"user",text:"修改",delivery:"not_sent"}];c.rounds=[round("failed")];
 const v=view(c);assert.ok(v);assert.equal(v.text,"未发送");assert.equal(v.retry,true);assert.equal(v.placement,"user");
});
test("running and starting round never shows a stale error",()=>{
 for(const state of ["running","starting"]){const c=conv(state,ERROR);c.rounds=[round("running")];assert.equal(view(c),undefined)}
});
test("long reasons are bounded inline but retain the actionable tail in details",()=>{
 const c=conv("failed","模型调用失败；已有内容保留："+"服务异常说明".repeat(35)+"请检查模型配置");
 c.rounds=[round("failed")];const v=view(c);assert.ok(v);assert.ok(v.text.length<=100);assert.match(v.detail,/请检查模型配置$/);
});
test("a failed new round can show its reason without invalidating an older result",()=>{
 const c=conv("failed",ERROR);c.rounds=[round("failed")];
 const entries=groupChatMessages(c.messages,{runState:c.runState,rounds:c.rounds});
 const v=failureView(c,{phase:"awaiting_review"},entries.at(-1));assert.ok(v);assert.match(v.detail,/429/);
});

test("a historical failed turn with no assistant reply keeps its own notice",()=>{
 const c=conv("running","");
 c.messages=[{id:"u",role:"user",text:"旧问题",delivery:"failed"},{id:"next",role:"user",text:"新问题",delivery:"accepted"}];
 c.rounds=[{...round("failed"),failure:{kind:"execution",detail:ERROR}},{...round("running"),id:"next",finishedAt:undefined}];
 const entries=groupChatMessages(c.messages,{runState:c.runState,rounds:c.rounds});
 const old=entries.find(e=>e.role==="assistant"&&e.userMessage?.id==="u");
 assert.ok(old,"failed turn needs a stable reply slot even after another user turn");
 assert.match(failureView(c,undefined,old)?.detail??"",/429/);
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createCompletionIntent } from '../src/task-completion.ts';
import type { Task, Conversation, Execution, DeliverySummary } from '../src/types.ts';
function fixture() {
 const profile={provider:'fixture',model:'isolated',thinking:'off'};
 const task:Task={id:'a',projectId:'p',topicId:'topic',title:'任务A',request:'开发',status:'needs_confirmation',conversationId:'ca',executionId:'ea',profile};
 const conversation:Conversation={id:'ca',projectId:'p',taskId:'a',sessionId:'sa',runState:'idle',profile,messages:[],rounds:[{id:'r1',startedAt:'2026-10-28T05:59:00Z',phase:'finished',tools:[],report:{summary:'完成说明',stage:'awaiting_review',plan:[],results:[],blockers:[],next:[],question:'',checks:[{title:'无依据检查',claim:'passed'}]}}]};
 const execution:Execution={id:'ea',phase:'awaiting_review',finishedAt:'2026-10-28T06:00:00Z',tools:[]};
 return {task,conversation,execution,online:true,pending:false};
}
test('completion freezes the latest finished task and round, including missing evidence',()=>{
 const ctx=fixture();const intent=createCompletionIntent(ctx)!;
 assert.deepEqual(intent,{taskId:'a',projectId:'p',conversationId:'ca',roundId:'r1',title:'任务A',missingChecks:1});
 ctx.task.id='b';ctx.conversation.rounds!.push({id:'r2',startedAt:'2026-10-28T06:01:00Z',phase:'finished',tools:[]});
 assert.equal(intent.taskId,'a');assert.equal(intent.roundId,'r1');assert(Object.isFrozen(intent));
});
test('completion is absent while offline, busy, failed, interrupted, blocked or delivering',()=>{
 for(const state of ['running','starting']){const ctx=fixture();ctx.conversation.runState=state;assert.equal(createCompletionIntent(ctx),undefined)}
 for(const phase of ['failed','interrupted','running','confirmed']){const ctx=fixture();ctx.conversation.rounds![0]!.phase=phase;assert.equal(createCompletionIntent(ctx),undefined)}
 const offline=fixture();offline.online=false;assert.equal(createCompletionIntent(offline),undefined);
 const pending=fixture();pending.pending=true;assert.equal(createCompletionIntent(pending),undefined);
 const blocked=fixture();blocked.conversation.interactions=[{schemaVersion:1,id:'ix',version:1,projectId:'p',allowCancel:true,fields:[],source:'agent_ui',status:'pending',blocking:true,kind:'confirm',conversationId:'ca',roundId:'r1',title:'确认',createdAt:'2026-10-28T06:00:00Z'}] as Conversation['interactions'];assert.equal(createCompletionIntent(blocked),undefined);
 assert.equal(createCompletionIntent({...fixture(),delivery:{phase:'developing'} as DeliverySummary}),undefined);
});
test('completion rejects mismatched scope, retains legacy proof and does not invent evidence',()=>{
 const wrong=fixture();wrong.conversation.id='cb';assert.equal(createCompletionIntent(wrong),undefined);
 const wrongExecution=fixture();wrongExecution.execution.id='eb';assert.equal(createCompletionIntent(wrongExecution),undefined);
 const legacy=fixture();legacy.conversation.rounds=[];assert.equal(createCompletionIntent(legacy)?.roundId,'legacy:ea:2026-10-28T06:00:00Z');
 const noProof=fixture();noProof.conversation.rounds=[];delete noProof.execution.finishedAt;assert.equal(createCompletionIntent(noProof),undefined);
 const failedTool=fixture();failedTool.conversation.rounds![0]!.tools=[{id:'t',name:'bash',result:'FAIL',failed:true}];failedTool.conversation.rounds![0]!.report!.checks![0]!.toolId='t';assert.equal(createCompletionIntent(failedTool)?.missingChecks,1);
});

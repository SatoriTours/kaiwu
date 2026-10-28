import { taskState } from './task-state.ts';
import { isDeliveryActive } from './delivery.ts';
import type { Task, Conversation, Execution, DeliverySummary } from './types.ts';
export interface CompletionIntent { readonly taskId:string; readonly projectId:string; readonly conversationId:string; readonly roundId:string; readonly title:string; readonly missingChecks:number }
export interface CompletionContext { task:Task; conversation?:Conversation; execution?:Execution; delivery?:DeliverySummary; online:boolean; pending?:boolean }
// Freeze primitive identity only. Opening this dialog is not approval of any later round.
export function createCompletionIntent({task,conversation,execution,delivery,online,pending}:CompletionContext):Readonly<CompletionIntent>|undefined {
 if(!online||pending||!conversation||!execution||isDeliveryActive(delivery?.phase))return;
 if(conversation.id!==task.conversationId||conversation.projectId!==task.projectId||conversation.taskId!==task.id||execution.id!==task.executionId)return;
 const roundId=taskState(task,conversation,execution).confirmRoundId;if(!roundId)return;
 const round=conversation.rounds?.find(r=>r.id===roundId);
 const missingChecks=(round?.report?.checks??[]).filter(c=>c.claim!=='not_run'&&!round?.tools.some(t=>t.id===c.toolId&&!!t.result&&!t.failed)).length;
 return Object.freeze({taskId:task.id,projectId:task.projectId,conversationId:task.conversationId,roundId,title:task.title,missingChecks});
}

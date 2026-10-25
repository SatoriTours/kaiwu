import type {Task,Conversation,Execution} from './types.ts';
import {getPendingInteractions, interactionPresentation} from './interaction.ts';

// One interpretation for the board and detail view. CLI connectivity and a
// persisted, finished development result are independent facts.
export function taskState(task:Task, conversation?:Conversation, execution?:Execution, selectedRoundId?:string, now = Date.now()) {
 const rounds=conversation?.rounds??[];
 const latest=[...rounds].reverse().find(r=>r.phase!=='answered');
 const round=selectedRoundId?rounds.find(r=>r.id===selectedRoundId):latest;
 const busy=['starting','running'].includes(conversation?.runState??'');
 const pendingInteractions=getPendingInteractions(conversation?.interactions, now).filter(ix=>ix.blocking);
 const hasBlocking=pendingInteractions.length>0;
 const interactionView=hasBlocking?interactionPresentation(pendingInteractions[0]!):undefined;
 // Unknown delivery is a warning for the original live waiter, not a permanent
 // task status: answered native records intentionally survive process shutdown.
 const activeRound=rounds.at(-1);
 const unresolvedNative=busy?conversation?.interactions?.find(ix=>
  ix.source==='agent_ui'&&['answered','cancelled'].includes(ix.status)&&ix.response&&ix.response.delivery!=='accepted'&&
  (!activeRound||(ix.roundId===activeRound.id&&!activeRound.finishedAt&&!['finished','confirmed','answered','failed','interrupted'].includes(activeRound.phase)))
 ):undefined;
 const waitingUser=hasBlocking||!!unresolvedNative||(!busy&&(task.status==='waiting_user'||execution?.phase==='awaiting_user'||latest?.phase==='awaiting_user'));
 let confirmRoundId:string|undefined;
 if(!waitingUser&&task.status==='needs_confirmation'&&execution?.phase==='awaiting_review'&&!busy&&['idle','interrupted','failed'].includes(conversation?.runState??'')){
  if(latest?.phase==='finished')confirmRoundId=latest.id;
  else if(!latest&&execution.finishedAt)confirmRoundId=`legacy:${execution.id}:${execution.finishedAt}`;
 }
 let status=task.status;
 if(hasBlocking||unresolvedNative)status='waiting_user';
 else if(busy&&task.executionId)status='running';
 else if(waitingUser)status='waiting_user';
 else if(confirmRoundId)status='needs_confirmation';
 else if(task.status!=='completed'){
  if(conversation?.runState==='failed'||execution?.phase==='failed')status='failed';
  else if(conversation?.runState==='interrupted'||execution?.phase==='interrupted'||latest?.phase==='interrupted')status='interrupted';
  else if(task.status==='needs_confirmation')status='interrupted';
 }
 const needsResume=!busy&&['interrupted','failed'].includes(conversation?.runState??'');
 const deliveryAttention=unresolvedNative?.response?.delivery==='uncertain'?'送达待核查':unresolvedNative?.response?.delivery==='dispatching'?'回答发送中':'回答待发送';
 const label=status==='waiting_user'&&interactionView?interactionView.label:status==='waiting_user'&&unresolvedNative?deliveryAttention:status==='interrupted'&&!needsResume?'待继续':({idle:'未开始',running:'执行中',waiting_user:'待你处理',needs_confirmation:'待验收',completed:'已完成',interrupted:'已中断',failed:'执行失败'}[status]??'待核查');
 const legacyRound=round?.id.startsWith('legacy:');
 const nextRound=round?rounds[rounds.indexOf(round)+1]:undefined;
 const tools=legacyRound?(round?.tools?.length?round.tools:!nextRound?execution?.tools??[]:[]):round?.tools??execution?.tools??[];
 const messages=conversation?.messages??[];
 const roundStart=round?messages.findIndex(m=>m.id===round.id):-1;
 const end=nextRound?messages.findIndex(m=>m.id===nextRound.id):messages.length;
 const candidates=legacyRound?(end>=0?messages.slice(0,end):[]):round?roundStart>=0&&end>=0?messages.slice(roundStart+1,end):[]:messages;
 const reply=candidates.filter(m=>m.role==='assistant').at(-1);
 const raw=round?.report?.summary??reply?.displayText??reply?.text??(!round||(legacyRound&&!nextRound)?execution?.summary:'')??'';
 const marker=raw.indexOf('<kaiwu-report>');
 const visible=(marker<0?raw:raw.slice(0,marker)).trim();
 const summary=visible.length>240?visible.slice(0,240)+'…':visible;
 const summarySource=round?.report?'AI 进展摘要':reply||execution?.summary?'最近回复':'执行情况';
 const returnedTools=tools.filter(t=>!!t.result).length;
 const unfinishedTools=tools.filter(t=>!t.result).length;
 const closed=round&&['finished','confirmed'].includes(round.phase);
 const resultRoundId=closed?round.id:!round&&execution?.finishedAt&&['awaiting_review','confirmed'].includes(execution.phase)?`legacy:${execution.id}:${execution.finishedAt}`:undefined;
 const resultConfirmed=round?.phase==='confirmed'||(!round&&status==='completed'&&execution?.phase==='confirmed');
 return {status,label,interactionKind:interactionView?.kind,pendingInteractionCount:pendingInteractions.length,needsResume,confirmRoundId,resultRoundId,resultConfirmed,summary,summarySource,returnedTools,unfinishedTools,replyId:reply?.id};
}

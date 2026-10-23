<script setup lang="ts">
import {computed,ref,watch,onMounted,onBeforeUnmount} from 'vue';
import type {Conversation,Task,Execution,GitStatus,ReportCheck,Round,DeliverySummary} from './types.ts';
import MarkdownContent from './MarkdownContent.vue';
import GitStatusView from './GitStatus.vue';
import {taskState} from './task-state.ts';
import {gitStatusLabel} from './git.ts';
import {isDeliveryActive} from './delivery.ts';
import {getRoundOptions} from './result-history.ts';
import CustomSelect from './CustomSelect.vue';
const props=defineProps<{conversation:Conversation;task?:Task;tasks:Task[];execution?:Execution;git?:GitStatus;delivery?:DeliverySummary;online:boolean;pending:boolean;agentAvailable:boolean}>();
const emit=defineEmits<{confirm:[string];revise:[];merge:[];resume:[];reply:[string];preview:[string]}>();
const media=window.matchMedia('(max-width:600px)');
const smallScreen=ref(media.matches);
const changeScreen=()=>{smallScreen.value=media.matches};
onMounted(()=>media.addEventListener('change',changeScreen));onBeforeUnmount(()=>media.removeEventListener('change',changeScreen));
const tab=ref('results'),selected=ref(''),collapsed=ref(smallScreen.value||!props.conversation.rounds?.at(-1)?.report),manuallyToggled=ref(false);
watch(()=>props.conversation.id,()=>{tab.value='results';selected.value='';collapsed.value=smallScreen.value||!props.conversation.rounds?.at(-1)?.report;manuallyToggled.value=false});
const rounds=computed(()=>props.conversation.rounds??[]);
const latest=computed(()=>rounds.value.at(-1));
const round=computed(()=>rounds.value.find(r=>r.id===selected.value)??latest.value);
const report=computed(()=>round.value?.report);
const oldRound=computed(()=>!!round.value&&round.value.id!==latest.value?.id);
const tools=computed(()=>round.value?.id.startsWith('legacy:')?(round.value.tools?.length?round.value.tools:!oldRound.value?props.execution?.tools??[]:[]):round.value?.tools??props.execution?.tools??[]);
const presentation=computed(()=>props.task?taskState(props.task,props.conversation,props.execution):undefined);
const label=computed(()=>oldRound.value?'历史轮次':presentation.value?.label??({running:'回复中',starting:'准备中',interrupted:'会话已断开',failed:'会话失败'}[props.conversation.runState]??'项目概览'));
watch([()=>report.value,smallScreen],()=>{if(!manuallyToggled.value)collapsed.value=smallScreen.value||!report.value});
function toggle(){collapsed.value=!collapsed.value;manuallyToggled.value=true}
const stage=computed(()=>report.value?.stage??(props.conversation.runState==='running'?'developing':'planning'));
const evidence=(check:ReportCheck)=>{const tool=round.value?.tools.find(t=>check.toolId&&t.id===check.toolId);return !tool?'缺少本轮依据':tool.failed?'执行失败':tool.result?(tool.name==='bash'?'命令完成':'工具已返回'):'执行中'};
const claim=(check:ReportCheck)=>({passed:'AI 报告通过',failed:'AI 报告失败',not_run:'未执行',unknown:'待核实'}[check.claim]??'待核实');
const viewedSummary=computed(()=>props.task?taskState(props.task,props.conversation,props.execution,round.value?.id):undefined);
const currentSummary=computed(()=>report.value?.summary??viewedSummary.value?.summary??'');
const stats=computed(()=>[{label:'正在推进',n:props.tasks.filter(t=>t.status==='running').length},{label:'等待确认',n:props.tasks.filter(t=>t.status==='needs_confirmation').length},{label:'需要处理',n:props.tasks.filter(t=>['failed','interrupted'].includes(t.status)).length}]);
const deliveryActive=computed(()=>props.delivery?isDeliveryActive(props.delivery.phase):false);
const confirmable=computed(()=>!oldRound.value&&presentation.value?.status!=='waiting_user'&&!!presentation.value?.confirmRoundId&&!deliveryActive.value);
const hasActiveInteraction=computed(()=>props.conversation.interactions?.some(ix=>ix.status==='pending'));
const showQuestion=computed(()=>oldRound.value?!!report.value?.question:(!hasActiveInteraction.value&&!!report.value?.question));
const dirtyFiles=computed(()=>props.git?.dirtyFiles??[]);
const roundOptions=computed(()=>getRoundOptions(rounds.value,props.conversation));
function resetToLatest(){selected.value=''}
function time(value?:string){return value?new Date(value).toLocaleString([], {month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}):''}
function chooseTab(event:KeyboardEvent){const tabs=['plan','results','checks','records'];if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();tab.value=event.key==='Home'?'plan':event.key==='End'?'records':tabs[(tabs.indexOf(tab.value)+(event.key==='ArrowRight'?1:3))%4]!;(event.currentTarget as HTMLElement).querySelector<HTMLButtonElement>(`[data-tab="${tab.value}"]`)?.focus()}
</script>
<template>
 <div v-if="task&&!oldRound&&presentation?.needsResume&&!confirmable&&!presentation.resultConfirmed" class="progress-attention recovery-bar auto-recovery" role="status">
  <div><strong>{{presentation.status==='completed'?'结果已保留':presentation.status==='failed'?'上次执行未完成':'上次执行已中断'}}</strong><p>{{conversation.runState==='failed'?'再次发送消息会尝试接回原会话；原因见对话中的失败提示。':presentation.status==='completed'?'发送新消息时会尝试接回原会话，已有结果保留。':'直接发消息或点继续开发，系统会尝试接回原会话。已有记录和修改保留。'}}</p></div>
 </div>
 <section v-if="confirmable" class="result-review" aria-label="本轮成果验收">
  <strong>开发已结束，先看看效果</strong><p>查看实际效果与运行结果，符合要求后再确认完成。</p>
  <div class="row result-main-actions"><button class="primary" @click="emit('preview',presentation!.resultRoundId!)">查看效果</button><button :disabled="pending" @click="emit('revise')">继续修改</button></div>
  <div class="result-accept"><small>确认后将任务标记为完成，代码集成另行审核。</small><button :disabled="!online||pending" @click="emit('confirm',presentation!.confirmRoundId!)">确认完成</button></div>
 </section>
 <section v-else-if="!oldRound&&presentation?.status==='completed'&&presentation.resultConfirmed" class="task-completed result-review-compact" role="status">
  <div class="completed-compact-bar">
   <div class="completed-compact-status">
    <span class="completed-badge">已完成</span>
    <span v-if="git?.state==='merged'" class="merged-tag">已合并 · {{git?.mainBranch||'主分支'}}</span>
    <span v-else-if="!deliveryActive" class="unmerged-label">{{gitStatusLabel(git)}}<template v-if="git?.mainBranch"> · {{git.mainBranch}}</template></span>
   </div>
   <div class="completed-compact-actions">
    <button class="compact-btn" @click="emit('preview',presentation.resultRoundId!)">查看效果</button>
    <button class="compact-btn" :disabled="pending" @click="emit('revise')">继续修改</button>
    <button v-if="!deliveryActive&&git?.state!=='merged'&&git?.canMerge" class="compact-btn merge-btn" :disabled="!online||pending" @click="emit('merge')">合并到主分支</button>
   </div>
  </div>
 </section>
 <div v-if="task&&!oldRound&&presentation?.status==='interrupted'&&!presentation.needsResume" class="progress-attention continuation-hint" role="status">上次执行尚未完成，直接发送消息或点继续开发即可接着处理。</div>
 <section class="progress-card" aria-label="结构化进展">
  <div class="progress-top">
   <div class="row between">
    <span class="progress-caption">{{task?'任务概况':'项目概览'}}</span>
    <div class="row history-controls">
     <details v-if="roundOptions.length>1" class="history-dropdown">
      <summary class="history-toggle" aria-label="往期记录">往期记录 ▾</summary>
      <div class="history-select-pop">
       <CustomSelect
        aria-label="查看历史记录"
        :model-value="round?.id"
        :options="roundOptions.map(opt => ({ value: opt.id, label: opt.label }))"
        compact
        @update:model-value="selected = $event"
       />
      </div>
     </details>
     <span class="progress-state">{{label}}</span>
    </div>
   </div>
   <div v-if="oldRound" class="history-banner">
    <span>正在查看历史记录</span>
    <button class="return-latest-btn" @click="resetToLatest">返回最新</button>
   </div>
   <button v-if="oldRound&&viewedSummary?.resultRoundId" class="result-history-button" @click="emit('preview',viewedSummary.resultRoundId)">查看本轮结果</button>
   <div v-if="!task" class="project-metrics"><div v-for="s in stats" :key="s.label"><b>{{s.n}}</b><span>{{s.label}}</span></div></div>
   <details v-if="task" class="task-requirement"><summary>任务要求：{{task.request}}</summary><p>{{task.request}}</p></details>
   <div v-if="currentSummary" class="summary-heading"><small>{{report?'AI 进展摘要':presentation?.summarySource??'最近回复'}}</small><button v-if="!report&&viewedSummary?.replyId" @click="emit('reply',viewedSummary.replyId)">查看完整回复 ↗</button></div>
   <p v-if="currentSummary" class="progress-summary" :class="{'summary-compact':collapsed}">{{currentSummary}}</p>
   <p v-else-if="task" class="progress-summary">{{presentation?.status==='interrupted'?'执行未完成，尚未收到文字回复。已有记录和修改保留。':presentation?.status==='running'?'正在执行，收到回复后会更新进展。':'尚未开始开发。任务要求已保留。'}}</p>
   <div v-if="task" class="execution-facts"><span v-if="presentation?.returnedTools">{{presentation.returnedTools}} 条已返回</span><span v-if="presentation?.unfinishedTools">{{presentation.unfinishedTools}} 条{{presentation.status==='running'?'执行中':'未结束'}}</span><span v-if="dirtyFiles.length">{{dirtyFiles.length}} 个文件待提交</span><span>{{gitStatusLabel(git)}}<template v-if="git?.mainBranch"> · {{git.mainBranch}}</template></span><span v-if="!report?.checks?.length">验证结果未确认</span></div>
   <button class="progress-collapse details-toggle" :aria-expanded="!collapsed" aria-label="收起或展开进展卡" @click="toggle">{{collapsed?'查看方案、成果与记录 ⌄':'收起详情 ⌃'}}</button>
   <ul v-if="collapsed&&report?.results?.length" class="result-preview"><li v-for="(item,i) in report.results.slice(0,2)" :key="i">{{item}}</li></ul>
   <p v-if="round?.reportError" class="check-missing" role="status">{{round.reportError}}</p>
  </div>
  <template v-if="!collapsed">
   <div class="report-tabs" role="tablist" aria-label="任务进展内容" @keydown="chooseTab"><button v-for="item in [{id:'plan',label:'方案'},{id:'results',label:'成果'},{id:'checks',label:'验证'},{id:'records',label:'记录'}]" :key="item.id" :data-tab="item.id" role="tab" :aria-selected="tab===item.id" :tabindex="tab===item.id?0:-1" @click="tab=item.id">{{item.label}}<small v-if="item.id==='results'&&report?.results?.length"> {{report.results.length}}</small></button></div>
   <div class="report-body" role="tabpanel" :aria-label="{plan:'方案',results:'成果',checks:'验证',records:'记录'}[tab]">
    <template v-if="tab==='plan'"><ol v-if="report?.plan?.length" class="plan-list"><li v-for="(item,i) in report.plan" :key="i">{{item}}</li></ol><p v-else class="progress-empty">{{task?.request??'选择卡片查看方案；项目对话用于讨论整体安排。'}}</p></template>
    <template v-if="tab==='results'"><div v-for="(item,i) in report?.results??[]" :key="i" class="feature-row"><span class="feature-check">✓</span><span>{{item}}</span><small>{{viewedSummary?.resultConfirmed?'本轮已确认':'AI 报告 · 待验收'}}</small></div><p v-if="!report?.results?.length" class="progress-empty">{{task?.executionId?'本次执行尚无完整成果报告，请结合最近回复和实际修改核对。':'开始开发前，可在“方案”查看任务要求。'}}</p><details v-if="dirtyFiles.length" class="changed-files"><summary>实际修改 · {{dirtyFiles.length}} 个文件</summary><ul><li v-for="file in dirtyFiles" :key="file.path"><code>{{file.path}}</code></li></ul></details><div v-if="report?.next?.length" class="progress-next"><small>下一步</small><p v-for="(item,i) in report.next" :key="i">{{item}}</p></div></template>
    <template v-if="tab==='checks'"><div v-for="(check,i) in report?.checks??[]" :key="i" class="check-row"><div class="row between"><span>{{check.title}}</span><small>{{claim(check)}}</small></div><details v-if="check.toolId&&round?.tools.some(t=>t.id===check.toolId)"><summary :class="{failed:evidence(check)==='执行失败'}">{{evidence(check)}} · 查看依据</summary><template v-for="tool in round.tools.filter(t=>t.id===check.toolId)" :key="tool.id"><MarkdownContent v-if="tool.input" :text="tool.input" format="json"/><MarkdownContent v-if="tool.result" :text="tool.result" format="json"/></template></details><p v-else class="check-missing">{{check.claim==='not_run'?'没有执行验证':'缺少本轮执行依据，结果待核实'}}</p></div><p v-if="!report?.checks?.length" class="progress-empty">{{tools.length?'已有工具记录，尚未提供可核对的验证报告。':'未执行验证'}}</p></template>
    <template v-if="tab==='records'"><ul class="tool-records"><li v-for="tool in tools" :key="tool.id"><details><summary>{{tool.name}} · {{tool.failed?'失败':tool.result?'已返回':presentation?.status==='running'?'执行中':'未收到结束回执'}}</summary><MarkdownContent v-if="tool.input" :text="tool.input" format="json"/><MarkdownContent v-if="tool.result" :text="tool.result" format="json"/></details></li></ul><p v-if="!tools.length" class="progress-empty">暂无执行记录。</p><details v-if="execution?.summary" class="legacy-result"><summary>查看原始结果摘要</summary><MarkdownContent :text="execution.summary"/></details></template>
   </div>
  </template>
  <div v-if="task&&!collapsed" class="report-foot"><details><summary>{{gitStatusLabel(git)}}<span v-if="git?.mainBranch"> · {{git.mainBranch}}</span></summary><GitStatusView :status="git" detail/></details><button :disabled="!git?.canMerge||oldRound" @click="emit('merge')">合并预检 ↗</button></div>
 </section>
 <div v-if="report?.blockers?.length||showQuestion" class="progress-attention" role="status"><strong>{{showQuestion?'需要你决定':'当前阻塞'}}</strong><p v-if="showQuestion">{{report?.question}}</p><p v-for="(item,i) in report?.blockers??[]" :key="i">{{item}}</p></div>
</template>

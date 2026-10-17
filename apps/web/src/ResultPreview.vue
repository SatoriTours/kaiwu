<script setup lang="ts">
import {computed} from 'vue';
import type {Task,Conversation,Execution,TaskPreview} from './types.ts';
import MarkdownContent from './MarkdownContent.vue';
import {taskState} from './task-state.ts';
import {safePreviewURL} from './preview.ts';
const props=defineProps<{task:Task;conversation:Conversation;execution?:Execution;roundId:string;preview?:TaskPreview;busy:boolean;online:boolean;error:string}>();
const emit=defineEmits<{back:[];chat:[];refresh:[];start:[];stop:[];revise:[];instructions:[]}>();
const rounds=computed(()=>props.conversation.rounds??[]);
const round=computed(()=>rounds.value.find(r=>r.id===props.roundId));
const historical=computed(()=>!!round.value&&round.value.id!==rounds.value.at(-1)?.id);
const index=computed(()=>rounds.value.findIndex(r=>r.id===props.roundId)+1);
const presentation=computed(()=>taskState(props.task,props.conversation,props.execution,props.roundId));
const reply=computed(()=>props.conversation.messages.find(m=>m.id===presentation.value.replyId));
const tools=computed(()=>round.value?.tools??props.execution?.tools??[]);
const frameURL=computed(()=>!historical.value&&props.preview?.state==='running'?safePreviewURL(props.preview.url):undefined);
const canStart=computed(()=>!historical.value&&!!props.preview?.command?.length&&['stopped','failed'].includes(props.preview.state));
const stateLabel=computed(()=>({running:'服务运行中',starting:'正在启动',stopped:'服务已停止',failed:'预览启动失败',unavailable:'暂无网页预览'}[props.preview?.state??'unavailable']));
</script>
<template>
<section class="result-pane" role="region" aria-label="任务成果与效果">
 <header class="result-toolbar"><button @click="emit('back')">← 返回看板</button><div class="row"><button :disabled="busy||!online" @click="emit('refresh')">刷新状态</button><button class="preview-chat-back" @click="emit('chat')">返回任务对话</button></div></header>
 <div class="result-pane-heading"><h2>{{task.title}}</h2><small>{{index?'第 '+index+' 轮成果':'本轮成果'}} · {{presentation.resultConfirmed?'你已确认':'待验收'}}</small><p>来自任务独立工作区；Git 合并状态单独核对。</p></div>
 <div class="result-pane-scroll">
  <p v-if="historical" class="preview-warning">正在查看历史成果记录。当前工作区已用于后续开发，不能以当前预览复现历史效果。</p>
  <div v-if="error" class="preview-warning" role="alert">{{error}}<span> 如操作结果未知，请刷新状态，系统不会自动重发启动或停止命令。</span></div>
  <section class="preview-surface" aria-label="网页效果">
   <div class="preview-status"><strong>{{stateLabel}}</strong><small v-if="preview?.path">{{preview.path}}</small><button v-if="preview?.state==='running'" :disabled="busy||!online" @click="emit('stop')">停止预览</button></div>
   <iframe v-if="frameURL" :key="frameURL" class="task-preview-frame" title="任务效果" :src="frameURL" sandbox="allow-scripts" referrerpolicy="no-referrer"></iframe>
   <div v-else class="preview-empty"><h3>{{busy?'正在核对预览状态…':stateLabel}}</h3><p>{{preview?.message||(historical?'历史轮次仅展示原成果与运行记录。':'正在读取任务预览配置，已有成果和运行记录见下方。')}}</p>
    <template v-if="canStart"><p>将使用任务工作区运行以下命令：</p><code class="preview-command">{{JSON.stringify(preview?.command)}}</code><button class="primary" :disabled="busy||!online||!preview?.manifestId" @click="emit('start')">{{preview?.state==='failed'?'重试启动预览':'启动预览'}}</button></template>
    <button v-else-if="preview?.state==='unavailable'&&!historical" :disabled="!online" @click="emit('instructions')">补充预览说明</button>
    <p v-if="preview?.state==='running'&&!frameURL" class="preview-warning">预览入口尚未通过校验，请刷新状态。</p>
   </div>
   <details v-if="preview?.logs" class="preview-logs"><summary>预览诊断</summary><pre>{{preview.logs}}</pre></details>
  </section>
  <section class="result-output" aria-label="成果与运行结果"><div class="row between"><h3>成果与运行结果</h3><button @click="emit('revise')">继续修改</button></div>
   <MarkdownContent v-if="reply" :text="reply.displayText??reply.text"/>
   <p v-else-if="presentation.summary">{{presentation.summary}}</p><p v-else class="muted">本轮没有文字成果说明，请结合下方执行记录核对。</p>
   <ul v-if="round?.report?.results?.length"><li v-for="(result,i) in round.report.results" :key="i">{{result}}</li></ul>
   <details v-for="tool in tools" :key="tool.id" class="result-tool"><summary>{{tool.name}} · {{tool.failed?'执行失败':tool.result?'已返回':'未收到结束回执'}}</summary><MarkdownContent v-if="tool.result" :text="tool.result" format="json"/><p v-else>本条没有可核对的运行输出。</p></details>
   <p v-if="!round?.report?.checks?.length" class="preview-warning">本轮未提供可核对的验证报告；确认完成不会把缺失的验证改成通过。</p>
  </section>
  <details class="preview-scope"><summary>预览使用范围</summary><p>当前提供任务工作区的只读 HTTP 效果。需要登录、实时热更新或使用根路径资源的应用可能无法完整预览。切换卡片或确认完成不会停止预览；继续开发、合并或服务退出时会安全停止。</p></details>
 </div>
</section>
</template>

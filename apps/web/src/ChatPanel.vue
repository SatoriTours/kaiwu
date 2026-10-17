<script setup lang="ts">
import { ref, watch, nextTick, computed } from "vue";
import type { Conversation, Task, Execution, GitStatus, Message } from "./types.ts";
import { isStartRequest } from "./workbench.ts";
import {taskState} from "./task-state.ts";
import MergeIcon from "./MergeIcon.vue";
import { isMergeRequest } from "./git.ts";
import TaskActionIcon from "./TaskActionIcon.vue";
import ProgressCard from "./ProgressCard.vue";
import AssistantReply from "./AssistantReply.vue";
import {groupChatMessages} from './chat-messages.ts';
import MarkdownContent from "./MarkdownContent.vue";
const props = defineProps<{
  conversation: Conversation;
  tasks:Task[];
  task?: Task;
  git?: GitStatus;
  execution?: Execution;
  projectName: string;
  draft: string;
  pending: boolean;
  online: boolean;
  agentAvailable: boolean;
  visible: boolean;
}>();
const emit = defineEmits<{
  draft: [string];
  send: [];
  project: [];
  create: [];
  resume: [];
  start: [];
  merge: [];
  model: [];
  confirmResult:[string];
  previewResult:[string];
}>();
const taskStatus=computed(()=>props.task?taskState(props.task,props.conversation,props.execution).status:'idle');
const chatEntries=computed(()=>groupChatMessages(props.conversation.messages));
const messages = ref<HTMLElement>();
async function showLatestReply(messageID?:string){
 await nextTick();const replies=Array.from(messages.value?.querySelectorAll<HTMLElement>('.message.assistant')??[]);const target=messageID?Array.from(messages.value?.querySelectorAll<HTMLElement>('[data-message-id]')??[]).find(el=>el.dataset.messageId===messageID):undefined;const el=messageID?target?.closest<HTMLElement>('.message.assistant'):replies.at(-1);
 if(!el||!messages.value)return;
 const detail=el.querySelector<HTMLDetailsElement>('details.assistant-detail');if(detail)detail.open=true;
 messages.value.scrollTop+=el.getBoundingClientRect().top-messages.value.getBoundingClientRect().top;
}

const editor = ref<HTMLTextAreaElement>();
const previewing = ref(false);
async function focusEditor(){previewing.value=false;await nextTick();editor.value?.focus()}
defineExpose({focusEditor});
watch(
  () => props.conversation.id,
  () => {
    previewing.value = false;
  },
);
watch(
  () => props.draft,
  (draft) => {
    if (!draft) previewing.value = false;
  },
);
async function togglePreview() {
  previewing.value = !previewing.value;
  if (!previewing.value) {
    await nextTick();
    editor.value?.focus();
  }
}
let renderedID = "";
watch(
  [() => props.conversation.id, () => props.visible],
  async ([id, visible], _previous, onCleanup) => {
    let current = true;
    onCleanup(() => { current = false; });
    // Initial mount, scope changes and reopening on mobile all start at the latest reply.
    await nextTick();
    if (!current) return;
    renderedID = id;
    if (messages.value && visible && messages.value.clientHeight > 0)
      messages.value.scrollTop = messages.value.scrollHeight;
  },
  { flush: "post", immediate: true },
);
watch(
  () => props.conversation.messages.map((m) => m.text).join("\n"),
  async () => {
    const id = props.conversation.id,
      el = messages.value;
    const bottom =
      props.visible &&
      id === renderedID &&
      el && el.clientHeight > 0 &&
      el.scrollHeight - el.scrollTop - el.clientHeight < 2;
    await nextTick();
    if (el && bottom && props.conversation.id === id && props.visible)
      el.scrollTop = el.scrollHeight;
  },
);
function keydown(event: KeyboardEvent) {
  const fromDraft = event.target === editor.value ||
    (event.target instanceof HTMLElement && event.target.classList.contains("draft-preview"));
  if (
    event.key === "Enter" &&
    (fromDraft || event.ctrlKey || event.metaKey) &&
    !event.shiftKey &&
    !event.isComposing && event.keyCode !== 229
  ) {
    event.preventDefault();
    emit("send");
  }
}
const states: Record<string, string> = {
  idle: "就绪",
  starting: "启动中",
  running: "回复中",
  interrupted: "已中断",
  failed: "调用失败",
};
function deliveryLabel(message:Message){
 if(message.delivery==='uncertain')return message.role==='assistant'?'回复中断':'上次执行中断';
 if(message.role==='user'&&['accepted','complete'].includes(message.delivery))return '已发送';
 return deliveryLabels[message.delivery];
}
const deliveryLabels: Record<string, string> = {
  pending: "待接受",
  accepted: "已接受",
  complete: "轮次结束",
  failed: "失败",
  uncertain: "发送结果未知",
  not_sent: "未发送",
};
</script>
<template>
  <aside class="chat" id="chat" aria-label="项目或任务聊天">
    <header class="chat-head" :title="'pi 会话：'+conversation.sessionId">
      <div class="row">
        <span class="avatar">π</span>
        <div>
          <h2 id="chat-title" tabindex="-1">
            {{
              task ? "任务对话 · " + task.title : "项目对话 · " + projectName
            }}
          </h2>
          <small id="chat-session"
            >pi 独立会话 · {{ conversation.sessionId.slice(0, 18) }}</small
          >
        </div>
      </div>
      <div v-if="task" class="chat-actions">
        <button
          v-if="task.executionId"
          class="merge-task"
          aria-label="合并到主分支"
          :title="git?.reason || '合并到主分支'"
          :disabled="!online || pending || task.status === 'running'"
          @click="emit('merge')"
        >
          <MergeIcon />
        </button>
        <button class="return" @click="emit('project')">← 返回项目对话</button>
        <button
          v-if="['idle', 'failed', 'interrupted'].includes(taskStatus)"
          class="start-task"
          :aria-label="
            pending
              ? '启动中…'
              : taskStatus === 'failed'
                ? '重试开发'
                : taskStatus === 'interrupted' ? '继续开发' : '开始开发'
          "
          :title="
            pending
              ? '启动中…'
              : taskStatus === 'failed'
                ? '重试开发'
                : taskStatus === 'interrupted' ? '继续开发' : '开始开发'
          "
          :aria-busy="pending"
          :disabled="
            !online ||
            pending ||
            ['starting', 'running'].includes(
              conversation.runState,
            )
          "
          @click="emit('start')"
        >
          <TaskActionIcon :status="taskStatus" :pending="pending" />
        </button>
      </div>
    </header>

    <div v-if="!task" class="scope">
      {{
        task
          ? "仅与当前任务沟通，其他会话独立运行。"
          : "项目级沟通：讨论需求、创建卡片、协调工作。"
      }}<span>
        · {{ states[conversation.runState] ?? conversation.runState }}</span
      >
    </div>
      <div v-if="!task&&conversation.error" class="context-card project-recovery" role="status">
        <p>{{conversation.runState==='failed'?'会话接回失败，请检查原因后再次发送重试。':'发送下一条消息时会尝试自动接回原会话。'}}</p>
        <details><summary>查看原因</summary><p>{{conversation.error}}</p></details>
      </div>
    <div class="progress-panel"><ProgressCard :conversation="conversation" :task="task" :tasks="tasks" :execution="execution" :git="git" :online="online" :pending="pending" :agent-available="agentAvailable" @resume="emit('resume')" @reply="showLatestReply" @confirm="id=>emit('confirmResult',id)" @merge="emit('merge')" @revise="focusEditor" @preview="id=>emit('previewResult',id)"/></div>
    <div
      class="messages"
      id="messages"
      ref="messages"
      aria-label="当前会话消息"
    >

      <article
        v-for="entry in chatEntries"
        :key="entry.id"
        class="message"
        :data-message-id="entry.message.id"
        :class="entry.message.role"
      >
        <div class="author">
          <span class="avatar">{{ entry.message.role === "user" ? "我" : "π" }}</span
          ><strong>{{
            entry.message.role === "user"
              ? "你"
              : entry.updates ? '执行进展' : task
                ? "pi · 任务 Agent"
                : "pi · 项目 Agent"
          }}</strong
          ><span>{{ entry.updates?entry.updates.length+' 条更新':deliveryLabel(entry.message) }}</span>
        </div>
        <AssistantReply v-if="entry.message.role==='assistant'" :message="entry.message" :updates="entry.updates"/><MarkdownContent v-else class="text" :text="entry.message.text" />
      </article>
      <div v-if="!conversation.messages.length" class="context-card">
        <strong>{{ task ? "开始这个任务的沟通" : "从一个想法开始" }}</strong>
        <p>
          {{
            task?.request ??
            "先讨论整个项目；需要独立推进的工作，可以创建为左侧的任务卡片。"
          }}
        </p>
        <button
          v-if="!task"
          class="quick-create"
          :disabled="!online"
          @click="emit('create')"
        >
          ＋ 创建任务卡片
        </button>
      </div>


    </div>
    <div class="composer">
      <div class="context-label row between">
        <span
          >发送至{{ task ? "任务 " + task.title : "项目 " + projectName }}</span
        >
        <button
          class="preview-toggle"
          :aria-label="previewing ? '返回编辑' : '预览 Markdown'"
          :title="previewing ? '返回编辑' : '预览 Markdown'"
          :aria-pressed="previewing"
          @click="togglePreview"
        >
          <svg
            v-if="!previewing"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            aria-hidden="true"
          >
            <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <svg
            v-else
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="m16 3 5 5-12 12-6 1 1-6Z M13 6l5 5" />
          </svg>
        </button>
      </div>
      <div class="editor" @keydown="keydown">
        <div
          v-if="previewing"
          class="draft-preview"
          aria-label="当前草稿预览"
          tabindex="0"
        >
          <MarkdownContent v-if="draft.trim()" :text="draft" />
          <span v-else class="draft-preview-empty">暂无内容可预览</span>
        </div>
        <textarea
          v-show="!previewing"
          id="draft"
          ref="editor"
          aria-label="当前会话草稿"
          :value="draft"
          maxlength="5000"
          :placeholder="
            task ? '补充这个任务的要求……' : '提出项目想法，或讨论接下来做什么……'
          "
          @input="emit('draft', ($event.target as HTMLTextAreaElement).value)"
        />
        <div class="row">
          <button type="button" class="model-picker" aria-label="选择会话模型" :title="conversation.messages.length || conversation.sessionFile ? '会话已启动，保留原模型配置' : '选择当前会话的 pi 模型'" :disabled="pending || !online || !!conversation.messages.length || !!conversation.sessionFile || conversation.runState !== 'idle'" @click="emit('model')">pi · {{ conversation.profile.model || '选择模型' }}</button
          ><button
            id="send"
            class="primary"
            :disabled="
              !online ||
              (!agentAvailable &&
                !isStartRequest(draft) &&
                !isMergeRequest(draft)) ||
              pending ||
              (['starting', 'running'].includes(
                conversation.runState,
              ) &&
                !isMergeRequest(draft)) ||
              !draft.trim()
            "
            @click="emit('send')"
          >
            {{ pending ? "提交中…" : "发送 ↑" }}
          </button>
        </div>
      </div>
      <div class="composer-tip">
        Enter 发送 · Shift + Enter 换行{{
          task && !task.executionId ? " · 输入“开始开发”启动任务" : ""
        }}
      </div>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from "vue";
import type { Conversation, Task, Execution, GitStatus } from "./types.ts";
import { isStartRequest } from "./workbench.ts";
import GitStatusView from "./GitStatus.vue";
import MergeIcon from "./MergeIcon.vue";
import { isMergeRequest } from "./git.ts";
import TaskActionIcon from "./TaskActionIcon.vue";
import MarkdownContent from "./MarkdownContent.vue";
const props = defineProps<{
  conversation: Conversation;
  task?: Task;
  git?: GitStatus;
  execution?: Execution;
  projectName: string;
  draft: string;
  pending: boolean;
  online: boolean;
  agentAvailable: boolean;
  visible: boolean;
  scroll: Record<string, number>;
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
}>();
const messages = ref<HTMLElement>();
const editor = ref<HTMLTextAreaElement>();
const previewing = ref(false);
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
let renderedID = props.conversation.id;
function save() {
  if (messages.value && messages.value.clientHeight > 0)
    props.scroll[renderedID] = messages.value.scrollTop;
}
watch(
  () => [props.conversation.id, props.visible] as const,
  async () => {
    save();
    await nextTick();
    renderedID = props.conversation.id;
    if (messages.value && props.visible)
      messages.value.scrollTop = props.scroll[renderedID] ?? 0;
  },
  { flush: "pre" },
);
watch(
  () => props.conversation.messages.map((m) => m.text).join("\n"),
  async () => {
    const id = props.conversation.id,
      el = messages.value;
    const bottom =
      props.visible &&
      id === renderedID &&
      el &&
      el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    await nextTick();
    if (el && bottom && props.conversation.id === id && props.visible)
      el.scrollTop = el.scrollHeight;
  },
);
onBeforeUnmount(save);
function keydown(event: KeyboardEvent) {
  if (
    event.key === "Enter" &&
    (event.ctrlKey || event.metaKey) &&
    !event.isComposing
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
const deliveryLabels: Record<string, string> = {
  pending: "待接受",
  accepted: "已接受",
  complete: "轮次结束",
  failed: "失败",
  uncertain: "结果待确认",
  not_sent: "未发送",
};
</script>
<template>
  <aside class="chat" id="chat" aria-label="项目或任务聊天">
    <header class="chat-head">
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
          v-if="['idle', 'failed'].includes(task.status)"
          class="start-task"
          :aria-label="
            pending
              ? '启动中…'
              : task.status === 'failed'
                ? '重试开发'
                : '开始开发'
          "
          :title="
            pending
              ? '启动中…'
              : task.status === 'failed'
                ? '重试开发'
                : '开始开发'
          "
          :aria-busy="pending"
          :disabled="
            !online ||
            pending ||
            ['starting', 'running', 'interrupted'].includes(
              conversation.runState,
            )
          "
          @click="emit('start')"
        >
          <TaskActionIcon :status="task.status" :pending="pending" />
        </button>
      </div>
    </header>
    <GitStatusView
      v-if="task?.executionId"
      :status="git"
      detail
      class="chat-git"
    />
    <div class="scope">
      {{
        task
          ? "仅与当前任务沟通，其他会话独立运行。"
          : "项目级沟通：讨论需求、创建卡片、协调工作。"
      }}<span>
        · {{ states[conversation.runState] ?? conversation.runState }}</span
      >
    </div>
    <div
      class="messages"
      id="messages"
      ref="messages"
      aria-label="当前会话消息"
      @scroll="save"
    >
      <article
        v-for="message in conversation.messages"
        :key="message.id"
        class="message"
        :class="message.role"
      >
        <div class="author">
          <span class="avatar">{{ message.role === "user" ? "我" : "π" }}</span
          ><strong>{{
            message.role === "user"
              ? "你"
              : task
                ? "pi · 任务 Agent"
                : "pi · 项目 Agent"
          }}</strong
          ><span>{{ deliveryLabels[message.delivery] }}</span>
        </div>
        <MarkdownContent class="text" :text="message.text" />
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
      <div v-if="conversation.error" class="context-card error" role="alert">
        <p>{{ conversation.error }}</p>
        <button
          :disabled="!online || pending || !agentAvailable"
          @click="emit('resume')"
        >
          恢复原会话
        </button>
        <p class="muted">恢复不会重发结果未知的消息。</p>
      </div>
      <details
        v-if="execution"
        class="execution-result"
        :open="execution.phase === 'awaiting_review'"
      >
        <summary>
          {{
            execution.phase === "awaiting_review"
              ? "本轮结果 · 待确认"
              : execution.phase === "failed"
                ? "执行失败"
                : execution.phase === "interrupted"
                  ? "执行中断"
                  : "开发进度"
          }}
        </summary>
        <p v-if="execution.error">{{ execution.error }}</p>
        <MarkdownContent
          v-if="execution.summary"
          class="result-summary"
          :text="execution.summary"
        />
        <p v-if="execution.workspace" class="muted">
          工作区：{{ execution.workspace }}
        </p>
        <p class="muted">
          {{
            execution.tools.some((t) => t.name === "bash" && !!t.result)
              ? "下方为实际命令记录，请核对验证结果。"
              : "未执行验证"
          }}
        </p>
        <ul v-if="execution.tools.length" class="tool-records">
          <li v-for="tool in execution.tools" :key="tool.id">
            <details>
              <summary>
                {{ tool.name }} ·
                {{ tool.failed ? "失败" : tool.result ? "已返回" : "执行中" }}
              </summary>
              <MarkdownContent
                v-if="tool.input"
                :text="tool.input"
                format="json"
              />
              <MarkdownContent
                v-if="tool.result"
                :text="tool.result"
                format="json"
              />
            </details>
          </li>
        </ul>
      </details>
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
              (['starting', 'running', 'interrupted'].includes(
                conversation.runState,
              ) &&
                !isMergeRequest(draft)) ||
              (conversation.runState === 'failed' &&
                !isStartRequest(draft) &&
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
        Ctrl / ⌘ + Enter 发送{{
          task && !task.executionId ? " · 输入“开始开发”启动任务" : ""
        }}
      </div>
    </div>
  </aside>
</template>

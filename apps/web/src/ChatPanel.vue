<script setup lang="ts">
import AttachmentList from "./AttachmentList.vue";
import { ref, watch, nextTick, computed, onMounted, onUnmounted } from "vue";
import type { Conversation, Task, Execution, GitStatus, Message, DeliverySummary, TaskPreview, Attachment } from "./types.ts";
import { isStartRequest } from "./workbench.ts";
import {taskState} from "./task-state.ts";
import MergeIcon from "./MergeIcon.vue";
import { isMergeRequest } from "./git.ts";
import TaskActionIcon from "./TaskActionIcon.vue";
import AssistantReply from "./AssistantReply.vue";
import {groupChatMessages, filterChatEntries, type ChatEntry} from './chat-messages.ts';
import {failureView} from './failure.ts';
import MarkdownContent from "./MarkdownContent.vue";
import DeliveryStatus from "./DeliveryStatus.vue";
import { isDeliveryActive } from "./delivery.ts";
import InteractionCard from "./InteractionCard.vue";
import InteractionHistory from './InteractionHistory.vue';
import InteractionIcon from './InteractionIcon.vue';
import "./interaction.css";
import {
  splitInteractions,
  interactionPresentation,
  isInteractionCollapsed,
  type InteractionRequest,
  type InteractionAnswers,
  type InteractionOthers,
} from "./interaction.ts";
const props = defineProps<{
  conversation: Conversation;
  tasks:Task[];
  task?: Task;
  git?: GitStatus;
  execution?: Execution;
  delivery?: DeliverySummary;
  projectName: string;
  draft: string;
  attachments?:Attachment[];
  uploading?:boolean;
  pending: boolean;
  online: boolean;
  agentAvailable: boolean;
  visible: boolean;
  preview?: TaskPreview;
}>();
const emit = defineEmits<{
  draft: [string];
  attach:[File[]];
  removeAttachment:[string];
  screenshot:[];
  send: [];
  project: [];
  create: [];
  close: [];
  resume: [];
  start: [];
  merge: [];
  model: [];
  files:[string];
  previewResult:[string];
  acceptDelivery: [summary: DeliverySummary];
  pauseDelivery: [summary: DeliverySummary];
  resumeDelivery: [summary: DeliverySummary];
  cancelDelivery: [summary: DeliverySummary];
  extendBudgetDelivery: [summary: DeliverySummary];
  answerInteraction: [
    payload: {
      conversationId: string;
      requestId: string;
      requestVersion: number;
      answerMode: "form" | "text";
      answers?: InteractionAnswers;
      others?: InteractionOthers;
      comment?: string;
      text?: string;
    },
  ];
  cancelInteraction: [payload: { conversationId: string; requestId: string; requestVersion: number }];
  retryInteraction: [payload: { conversationId: string; requestId: string; requestVersion: number }];
}>();
const attachmentInput=ref<HTMLInputElement>();
function acceptFiles(files:FileList|null){if(files?.length)emit("attach",Array.from(files))}
function pasteFiles(e:ClipboardEvent){if(e.clipboardData?.files.length){e.preventDefault();acceptFiles(e.clipboardData.files)}}
function dropFiles(e:DragEvent){if(e.dataTransfer?.files.length){e.preventDefault();acceptFiles(e.dataTransfer.files)}}
const now = ref(Date.now());
const taskStatus=computed(()=>props.task?taskState(props.task,props.conversation,props.execution,undefined,now.value).status:pendingInteractions.value.some(ix=>ix.blocking)?'waiting_user':'idle');
// The notice is derived from the selected conversation only, so switching scope
// can never carry another conversation's reason.
const failure=computed(()=>failureView(props.conversation,props.execution));
const deliveryActive=computed(()=>props.delivery?isDeliveryActive(props.delivery.phase):false);
const chatFilter = ref<'all' | 'questions'>('all');

const rawChatEntries = computed(() =>
  groupChatMessages(props.conversation.messages, {
    runState: props.conversation.runState,
    taskStatus: taskStatus.value,
    rounds: props.conversation.rounds,
  })
);

const chatEntries = computed(() => filterChatEntries(rawChatEntries.value, chatFilter.value));

async function setChatFilter(filter: 'all' | 'questions') {
  chatFilter.value = filter;
}

function onQuestionKeydown(e: KeyboardEvent, messageId: string) {
  if (chatFilter.value !== 'questions') return;
  if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
    e.preventDefault();
    locateQuestion(messageId);
  }
}

async function locateQuestion(messageId: string) {
  chatFilter.value = 'all';
  userInteractedScroll.value = true;
  await nextTick();
  const el = messages.value?.querySelector<HTMLElement>(`[data-message-id~="${messageId}"]`)
    ?? messages.value?.querySelector<HTMLElement>(`[data-message-id="${messageId}"]`);
  if (el && messages.value) {
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    const nextAssistant = el.nextElementSibling as HTMLElement | null;
    if (nextAssistant?.classList.contains('assistant')) {
      const details = nextAssistant.querySelector<HTMLDetailsElement>('details.assistant-turn-details, details.assistant-detail');
      if (details) details.open = true;
    }
  }
}
// Expiry is wall-clock driven: a pending native request past its deadline must
// leave the answer queue even when no snapshot arrives in between.
let expiryClock: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  expiryClock = setInterval(() => {
    now.value = Date.now();
  }, 1000);
});
onUnmounted(() => {
  if (expiryClock) clearInterval(expiryClock);
});
const interactionLists = computed(() => splitInteractions(props.conversation.interactions, now.value));
const pendingInteractions = computed(() => interactionLists.value.pending);
const interactionCard = ref<InstanceType<typeof InteractionCard>>();
const interactionView = computed(() => pendingInteractions.value[0] ? interactionPresentation(pendingInteractions.value[0]) : undefined);
const interactionCollapsed = ref(false);
const userChoseChatOverride = ref(false);

watch(() => props.conversation.id, () => {
  interactionCollapsed.value = pendingInteractions.value[0] ? isInteractionCollapsed(pendingInteractions.value[0]) : false;
  userChoseChatOverride.value = false;
});

const shouldCollapseComposer = computed(() => {
  return pendingInteractions.value.length > 0 && !interactionCollapsed.value && !userChoseChatOverride.value;
});

watch(() => pendingInteractions.value.map(ix=>`${ix.id}:${ix.version}`).join(','), () => {
  // A new prompt must not remove the composer while the user is typing in it.
  userChoseChatOverride.value = document.activeElement === editor.value;
});
const historyInteractions = computed(() => interactionLists.value.history);
const interactionEntries = computed(() => {
  const byMessage = new Map<string, string>(), byRound = new Map<string, string>();
  for (const entry of rawChatEntries.value) {
    if (entry.role !== 'assistant') continue;
    for (const message of entry.updates ?? [entry.message]) byMessage.set(message.id, entry.id);
    const roundId = entry.round?.id ?? entry.userMessage?.id;
    if (roundId && !byRound.has(roundId)) byRound.set(roundId, entry.id);
  }
  return new Map((props.conversation.interactions ?? []).map(ix=>[ix.id,
    (ix.sourceMessageId ? byMessage.get(ix.sourceMessageId) : undefined) ?? (ix.roundId ? byRound.get(ix.roundId) : undefined)]));
});
function interactionEntryId(ix: InteractionRequest): string | undefined { return interactionEntries.value.get(ix.id); }
function entryInteractions(entry: ChatEntry, requests: InteractionRequest[]) {
  return requests.filter(ix=>interactionEntryId(ix) === entry.id);
}
const detachedHistory = computed(() => {
  // Filtering messages must not hide the only route to a saved answer.
  const visibleEntries = new Set(chatEntries.value.map(entry=>entry.id));
  return historyInteractions.value.filter(ix=>!visibleEntries.has(interactionEntryId(ix) ?? ''));
});
function retryInteraction(ix: InteractionRequest) {
  emit('retryInteraction', { conversationId: props.conversation.id, requestId: ix.id, requestVersion: ix.version });
}
async function openInteraction(requestId?: string) {
  userChoseChatOverride.value = false;
  await nextTick();
  if (interactionCard.value) {
    await interactionCard.value.openRequest(requestId);
    return;
  }
  // A saved but unconfirmed answer is handled in history, never resent by viewing it.
  const target = Array.from(messages.value?.querySelectorAll<HTMLDetailsElement>('details.interaction-history-card') ?? []).find(el=>
    requestId ? el.dataset.interactionId === requestId : !!el.querySelector('.uncertain, .not_sent, .recorded, .dispatching'));
  if (target) { target.open = true; target.scrollIntoView({ block: 'nearest' }); target.querySelector('summary')?.focus(); }
}
const messages = ref<HTMLElement>();
const userInteractedScroll = ref(false);
async function showLatestReply(messageID?: string) {
  await nextTick();
  userInteractedScroll.value = true;
  const replies = Array.from(messages.value?.querySelectorAll<HTMLElement>('.message.assistant') ?? []);
  const target = messageID
    ? Array.from(messages.value?.querySelectorAll<HTMLElement>('[data-message-id]') ?? []).find(
        (el) => el.dataset.messageId === messageID || el.dataset.messageId?.split(/\s+/).includes(messageID),
      )
    : undefined;
  const el = messageID ? target?.closest<HTMLElement>('.message.assistant') ?? target : replies.at(-1);
  if (!el || !messages.value) return;
  const detail = el.querySelector<HTMLDetailsElement>('details.assistant-turn-details, details.assistant-detail');
  if (detail) detail.open = true;
  messages.value.scrollTop += el.getBoundingClientRect().top - messages.value.getBoundingClientRect().top;
}

const editor = ref<HTMLTextAreaElement>();
const previewing = ref(false);
async function focusEditor(){previewing.value=false;await nextTick();editor.value?.focus()}
defineExpose({focusEditor,openInteraction});
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
    if (userInteractedScroll.value) {
      userInteractedScroll.value = false;
      return;
    }
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
  <aside @paste="pasteFiles" @dragover.prevent @drop="dropFiles" class="chat" id="chat" aria-label="项目或任务聊天">
    <header class="chat-head" :title="'pi 会话：'+conversation.sessionId">
      <div class="row">
        <div>
          <h2 id="chat-title" tabindex="-1">
            {{
              task ? "任务对话 · " + task.title : "项目对话 · " + projectName
            }}
          </h2>

          <button v-if="interactionView" class="chat-interaction-status" @click="openInteraction()"><InteractionIcon :kind="interactionView.kind" />{{ interactionView.label }}<span v-if="pendingInteractions.length > 1"> · {{ pendingInteractions.length }}</span></button>
        </div>
      </div>
      <div v-if="task" class="chat-actions">
        <button
          v-if="task.executionId && !deliveryActive"
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
          v-if="['idle', 'failed', 'interrupted'].includes(taskStatus) && !deliveryActive"
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
    <div class="chat-filter-bar row between" role="toolbar" aria-label="聊天过滤">
      <div class="chat-filter-tabs">
        <button
          type="button"
          class="chat-filter-btn"
          :class="{ active: chatFilter === 'all' }"
          :aria-pressed="chatFilter === 'all'"
          @click="setChatFilter('all')"
        >
          全部
        </button>
        <button
          type="button"
          class="chat-filter-btn"
          :class="{ active: chatFilter === 'questions' }"
          :aria-pressed="chatFilter === 'questions'"
          @click="setChatFilter('questions')"
        >
          只看我的问题
        </button>
      </div>

    </div>
      <button class="chat-close" aria-label="收起聊天" title="收起聊天" autofocus @click="emit('close')">×</button>
    </header>

    <div class="progress-panel">
      <DeliveryStatus
        v-if="delivery"
        :summary="delivery"
        :task-title="task?.title"
        :accepting="pending"
        :controlling="pending"
        @accept="s => emit('acceptDelivery', s)"
        @pause="s => emit('pauseDelivery', s)"
        @resume="s => emit('resumeDelivery', s)"
        @cancel="s => emit('cancelDelivery', s)"
        @extendBudget="s => emit('extendBudgetDelivery', s)"
      />
    </div>
    <div
      class="messages"
      id="messages"
      ref="messages"
      aria-label="当前会话消息"
    >

      <article
        v-for="(entry, index) in chatEntries"
        :key="entry.id"
        class="message"
        :data-message-id="entry.updates?.length ? entry.updates.map(u => u.id).join(' ') : entry.message.id"
        :class="[entry.role, { 'question-clickable': chatFilter === 'questions' }]"
        :data-delivery="entry.message.delivery"
        :tabindex="chatFilter === 'questions' ? 0 : undefined"
        :role="chatFilter === 'questions' ? 'button' : undefined"
        :aria-label="chatFilter === 'questions' ? '点击定位此问题：' + entry.message.text.slice(0, 30) : entry.role === 'user' ? '我的消息' : entry.role === 'assistant' ? 'pi 的回复' : '系统消息'"
        @click="chatFilter === 'questions' ? locateQuestion(entry.message.id) : undefined"
        @keydown="chatFilter === 'questions' ? onQuestionKeydown($event, entry.message.id) : undefined"
      >
        <div class="message-stack">
        <div v-if="entry.role === 'system' || chatFilter === 'questions'" class="message-meta">
          <strong v-if="entry.role === 'system'">系统</strong>
          <span v-if="chatFilter === 'questions'" class="jump-hint">点击定位至对话 ↗</span>
        </div>
        <AssistantReply
          v-if="entry.role === 'assistant' && (!entry.isPlaceholder || !entryInteractions(entry, pendingInteractions).length)"
          :entry="entry"
          :message="entry.message"
          :updates="entry.updates"
          :is-latest="index === chatEntries.length - 1"
          :task="task"
          :conversation="conversation"
          :git="git"
          :execution="execution"
          :delivery="delivery"
          :online="online"
          :pending="pending"
          :preview="preview"
          @files="id => emit('files', id)"
          @preview="id => emit('previewResult', id)"
          @merge="emit('merge')"
          @reply="showLatestReply"
        />
        <template v-else-if="entry.role !== 'assistant'"><MarkdownContent class="text" :text="entry.message.text" /><AttachmentList :items="entry.message.attachments??[]" :conversation-id="conversation.id" /></template>
        <button v-for="ix in entryInteractions(entry, pendingInteractions)" :key="ix.id" class="interaction-inline-prompt" @click="openInteraction(ix.id)"><InteractionIcon :kind="interactionPresentation(ix).kind" />{{ interactionPresentation(ix).heading }} · 打开处理</button>
        <InteractionHistory :requests="entryInteractions(entry, historyInteractions)" :now="now" :pending="pending" :online="online" @retry="retryInteraction" />
        <div v-if="entry.role !== 'assistant'" class="message-status"><span>{{ deliveryLabel(entry.message) }}</span></div>
        </div>
      </article>

      <!-- A settled failure belongs where it happened: in the conversation,
           with its cause readable without opening the progress details. -->
      <div v-if="failure" class="chat-failure" :data-kind="failure.kind" role="alert" aria-label="本轮失败原因">
        <strong class="chat-failure-title">{{failure.title}}</strong>
        <p class="chat-failure-reason">{{failure.hasReason?failure.reason:'pi 未提供具体原因。'}}</p>
        <details v-if="failure.detail" class="chat-failure-full">
          <summary>展开完整原因</summary><p>{{failure.detail}}</p>
        </details>
        <small v-if="failure.resultPreserved" class="chat-failure-preserved">此前的结果仍然保留，可在上方查看或确认。</small>
        <small class="chat-failure-hint">{{failure.hint}}</small>
      </div>
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

      <!-- Legacy requests without a matching message remain readable, never reactivated. -->
      <InteractionHistory :requests="detachedHistory" :now="now" :pending="pending" :online="online" @retry="retryInteraction" />

      <!-- Interaction parse error if any -->
      <div v-if="conversation.rounds?.some(r => r.interactionError)" class="context-card" role="alert">
        <strong>交互解析提示</strong>
        <p>{{ conversation.rounds.find(r => r.interactionError)?.interactionError }}</p>
        <small>可直接在下方输入框中发送文字消息继续沟通。</small>
      </div>

      <!-- Interaction notice if any (e.g. unknown method / oversized content cancelled) -->
      <div
        v-for="round in (conversation.rounds || []).filter(r => r.interactionNotice)"
        :key="round.id + '-notice'"
        class="context-card"
        role="status"
      >
        <strong>交互能力说明</strong>
        <p>{{ round.interactionNotice }}</p>
      </div>

    </div>

    <!-- Active pending interaction card (non-modal local card inside chat panel) -->
    <InteractionCard
      v-if="pendingInteractions.length > 0"
      ref="interactionCard"
      :requests="pendingInteractions"
      :conversation-id="conversation.id"
      :pending="pending"
      :online="online"
      @collapse-change="val => interactionCollapsed = val"
      @submit="payload => emit('answerInteraction', payload)"
      @cancel="payload => emit('cancelInteraction', payload)"
      @retry="payload => emit('retryInteraction', payload)"
    />

    <div v-if="shouldCollapseComposer" class="composer-collapsed-bar" role="status">
      <span>{{ interactionView?.label }}，不是开发完成</span>
      <button
        type="button"
        class="composer-expand-btn"
        @click="userChoseChatOverride = true"
      >
        发送其他消息 ↗
      </button>
    </div>

    <div v-else class="composer">
      <AttachmentList :items="attachments??[]" :conversation-id="conversation.id" editable @remove="emit('removeAttachment',$event)" />
      <p v-if="attachments?.length" class="attachment-draft-note">仅作为本次消息附件，不写入项目</p>
      <p v-if="uploading" role="status">正在上传附件，完成后可发送…</p>
      <input ref="attachmentInput" type="file" multiple hidden @change="acceptFiles(($event.target as HTMLInputElement).files);($event.target as HTMLInputElement).value=''" />
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
          <button type="button" aria-label="添加附件" title="添加图片或文件，也可粘贴／拖入" :disabled="uploading||pending||!online" @click="attachmentInput?.click()">＋</button>
          <button type="button" aria-label="截图" title="截图（需要浏览器安全上下文）" :disabled="uploading||pending||!online" @click="emit('screenshot')">▣</button>
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
          <button type="button" class="model-picker" aria-label="选择会话模型" :title="conversation.profile.model ? `当前模型: ${conversation.profile.model}${conversation.pendingProfile ? ' (下一轮将变更为 ' + conversation.pendingProfile.model + ')' : ''}` : '选择当前会话的 pi 模型'" :disabled="pending || !online" @click="emit('model')">pi · {{ conversation.pendingProfile?.model ? `${conversation.pendingProfile.model} (待生效)` : (conversation.profile.model || '选择模型') }}</button
          ><button
            id="send"
            class="primary"
            :disabled="
              !online ||
              (!agentAvailable &&
                !isStartRequest(draft) &&
                !isMergeRequest(draft)) ||
              pending || uploading ||
              (['starting', 'running'].includes(
                conversation.runState,
              ) &&
                !isMergeRequest(draft)) ||
              (!draft.trim()&&!attachments?.length)
            "
            @click="emit('send')"
          >
            {{ pending ? "提交中…" : "发送 ↑" }}
          </button>
        </div>
      </div>

    </div>
  </aside>
</template>

<style scoped>.attachment-draft-note{font-size:10px;color:var(--muted);margin:4px 0}</style>

<script setup lang="ts">
import { computed, ref, nextTick, onMounted, onUnmounted, useId } from 'vue';
import type { Topic, Task, Conversation, GitStatus, Execution, DeliverySummary, SubagentSummary } from "./types.ts";
import {taskState} from "./task-state.ts";
import {deliveryView, isDeliveryActive} from "./delivery.ts";
import {createCompletionIntent} from './task-completion.ts';
import GitStatusView from "./GitStatus.vue";
import MergeIcon from "./MergeIcon.vue";
import TaskActionIcon from "./TaskActionIcon.vue";
import CustomSelect from "./CustomSelect.vue";
import SubagentBadge from "./SubagentBadge.vue";
import {summarizeSubagents} from "./subagents.ts";
const props = defineProps<{
  subagents?: Record<string, SubagentSummary>;
  topics: Topic[];
  executions:Record<string,Execution>;
  git: Record<string, GitStatus>;
  deliveries?: Record<string, DeliverySummary>;
  tasks: Task[];
  unread: Record<string, number>;
  selected: string;
  mobileTopic: string;
  online: boolean;
  pending: Record<string, boolean>;
  conversations: Record<string, Conversation>;
}>();
const emit = defineEmits<{
  subagents: [Task];
  select: [Task];
  process: [Task];
  project: [];
  start: [Task];
  merge: [Task];
  complete: [Task];
  topic: [string];
  addTopic: [];
  addTask: [string];
  deleteTopic: [Topic];
}>();
const now = ref(Date.now());
const isMobile = ref(window.innerWidth <= 599);
const boardRoot = ref<HTMLElement>();
let lastBoardFocus: HTMLElement | undefined;
function rememberBoardFocus(event: FocusEvent) { lastBoardFocus = event.target as HTMLElement; }
function forgetBoardFocus(event: FocusEvent) {
  if (event.relatedTarget && !boardRoot.value?.contains(event.relatedTarget as Node)) lastBoardFocus = undefined;
}
const topicDescriptionId = useId();
const topicCounts = computed(() => {
  const counts: Record<string, number> = {};
  for (const task of props.tasks) counts[task.topicId] = (counts[task.topicId] ?? 0) + 1;
  return counts;
});
const topicOptions = computed(() => props.topics.map(topic => ({ value: topic.id, label: topic.name })));
const currentTopic = computed(() => props.topics.find(topic => topic.id === props.mobileTopic));
const openMenu=ref('');let menuTrigger:HTMLElement|undefined;
const menuPosition = ref({ left: '0px', top: '0px' });
function closeMenu() {
  const panel = openMenu.value ? document.getElementById('task-menu-' + openMenu.value) : null;
  const restoreFocus = !!panel?.contains(document.activeElement);
  openMenu.value = '';
  if (restoreFocus && menuTrigger?.isConnected) menuTrigger.focus({ preventScroll: true });
}
function completion(task:Task){return createCompletionIntent({task,conversation:props.conversations[task.conversationId],execution:props.executions[task.executionId??''],delivery:props.deliveries?.[task.id],online:props.online,pending:props.pending[task.conversationId]})}
async function toggleMenu(id: string, event: MouseEvent) {
  menuTrigger = event.currentTarget as HTMLElement;
  openMenu.value = openMenu.value === id ? '' : id;
  if (!openMenu.value) return;
  await nextTick();
  const panel = document.getElementById('task-menu-' + id);
  if (!panel) return;
  const trigger = menuTrigger.getBoundingClientRect();
  const bounds = panel.getBoundingClientRect();
  const left = Math.max(8, Math.min(trigger.right - bounds.width, window.innerWidth - bounds.width - 8));
  const above = trigger.top - bounds.height - 6;
  const top = Math.max(8, Math.min(above >= 8 ? above : trigger.bottom + 6, window.innerHeight - bounds.height - 8));
  menuPosition.value = { left: left + 'px', top: top + 'px' };
  await nextTick();
  panel.querySelector<HTMLButtonElement>('button')?.focus();
}
function dismissMenu(e:Event){if(!(e.target as Element).closest('.task-actions-menu'))openMenu.value=''}
function menuKeys(e:KeyboardEvent){const el=e.currentTarget as HTMLElement;const items=[...el.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];if(e.key==='Escape'){e.preventDefault();e.stopPropagation();openMenu.value='';menuTrigger?.focus()}else if(['ArrowDown','ArrowUp','Home','End'].includes(e.key)){e.preventDefault();const i=items.indexOf(document.activeElement as HTMLButtonElement);items[e.key==='Home'?0:e.key==='End'?items.length-1:(i+(e.key==='ArrowDown'?1:-1)+items.length)%items.length]?.focus()}}
function menuBlur(e:FocusEvent){if(!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node|null))openMenu.value=''}

function resizeBoard() {
  closeMenu();
  const mobile = window.innerWidth <= 599;
  if (mobile === isMobile.value) return;
  // Media-query hiding can blur the old control before the resize event fires.
  const active = document.activeElement === document.body ? lastBoardFocus : document.activeElement as HTMLElement | null;
  const fromMobile = !!active?.closest('.mobile-board-header');
  const fromDesktop = !!active?.closest('.toolbar button');
  const fromColumn = !!active?.closest('.column-head button');
  const focusedTopic = fromColumn ? active?.closest<HTMLElement>('.column')?.dataset.topicid : undefined;
  const wasTask = ['new-task', 'column-new-task'].includes(active?.dataset.action ?? '');
  isMobile.value = mobile;
  if (mobile && focusedTopic && focusedTopic !== props.mobileTopic) emit('topic', focusedTopic);
  if (fromMobile || fromDesktop || fromColumn) void nextTick(() => {
    const selector = mobile
      ? (wasTask ? '.mobile-board-header [data-action="new-task"]' : '.mobile-board-header [role="combobox"]')
      : (wasTask ? '.toolbar [data-action="new-task"]' : '.toolbar [data-action="new-topic"]');
    boardRoot.value?.querySelector<HTMLElement>(selector)?.focus({ preventScroll: true });
  });
}
let expiryClock: ReturnType<typeof setInterval> | undefined;
onMounted(()=>{document.addEventListener('pointerdown',dismissMenu); window.addEventListener('resize',resizeBoard); expiryClock=setInterval(()=>{now.value=Date.now()},1000); });
onUnmounted(()=>{document.removeEventListener('pointerdown',dismissMenu); window.removeEventListener('resize',resizeBoard); if(expiryClock)clearInterval(expiryClock); });
function presentation(task:Task){return taskState(task,props.conversations[task.conversationId],props.executions[task.executionId??''],undefined,now.value)}
const waitingCount = computed(()=>props.tasks.filter(task=>presentation(task).pendingInteractionCount>0).length);
function actionLabel(task: Task) {
 if(presentation(task).status==='interrupted')return '继续开发';
 if(presentation(task).status==='waiting_user')return presentation(task).label;
  return props.pending[task.conversationId]
    ? "启动中…"
    : task.status === "running"
      ? "执行中"
      : task.status === "idle"
        ? "开始开发"
        : task.status === "failed"
          ? "重试开发"
          : "查看结果";
}
</script>
<template>
  <section ref="boardRoot" class="board-side" aria-label="项目任务看板" @focusin="rememberBoardFocus" @focusout="forgetBoardFocus">
    <div v-if="!isMobile || waitingCount" class="toolbar" :class="{ 'mobile-waiting-toolbar': isMobile }">
      <span class="running">{{ tasks.length }} 个任务</span>
      <span v-if="waitingCount" class="board-interaction-count" role="status">{{ waitingCount }} 项待你处理</span>
      <button
        v-if="!isMobile"
        data-action="new-topic"
        :disabled="!online"
        @click="emit('addTopic')"
      >
        ＋ 专题</button
      ><button
        v-if="!isMobile"
        class="primary"
        data-action="new-task"
        :disabled="!online"
        @click="emit('addTask', '')"
      >
        ＋ 任务</button
      >
    </div>
    <div class="board" id="board" @scroll="closeMenu">
      <header v-if="isMobile" class="mobile-board-header" aria-label="当前专题操作">
        <span :id="topicDescriptionId" class="sr-only">{{ currentTopic?.name }}，{{ topicCounts[mobileTopic] ?? 0 }} 个任务</span>
        <CustomSelect
          class="mobile-topic"
          aria-label="当前看板专题"
          :aria-describedby="topicDescriptionId"
          :title="currentTopic?.name"
          :model-value="mobileTopic"
          :options="topicOptions"
          @update:model-value="emit('topic', $event)"
        >
          <template #trigger="{ option }">
            <span class="mobile-topic-name">{{ option?.label ?? '选择专题' }}</span>
            <span class="mobile-topic-count">{{ topicCounts[mobileTopic] ?? 0 }}</span>
          </template>
          <template #option="{ option }">
            <span class="mobile-option-name">{{ option.label }}</span>
            <span class="mobile-option-count">{{ topicCounts[option.value] ?? 0 }}</span>
          </template>
          <template #footer="{ close }">
            <button type="button" class="mobile-add-topic" data-action="new-topic" :disabled="!online" @click="close(); emit('addTopic')">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
              新建专题
            </button>
            <button v-if="currentTopic && !currentTopic.protected" type="button" class="mobile-delete-topic" :aria-label="'删除专题 ' + currentTopic.name" title="删除当前专题" :disabled="!online" @click="close(); emit('deleteTopic', currentTopic)">
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5M14 11v5" /></svg>
            </button>
          </template>
        </CustomSelect>
        <button type="button" class="mobile-new-task" data-action="new-task" :aria-label="'在 ' + (currentTopic?.name ?? '当前专题') + ' 新建任务'" title="新建任务" :disabled="!online || !currentTopic" @click="emit('addTask', mobileTopic)">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
          任务
        </button>
      </header>
      <section
        v-for="topic in topics"
        :key="topic.id"
        class="column"
        :class="{ 'mobile-hidden': mobileTopic !== topic.id }"
        :data-topicid="topic.id"
        :aria-label="topic.name"
      >
        <header class="column-head">
          <h2>{{ topic.name }}</h2>
          <span class="count">{{
            tasks.filter((t) => t.topicId === topic.id).length
          }}</span>
          <div class="column-actions">
            <button
              v-if="!topic.protected"
              class="icon"
              data-action="column-delete-topic"
              :aria-label="'删除专题 ' + topic.name"
              :disabled="!online"
              @click="emit('deleteTopic', topic)"
            >
              ⋯</button
            ><button
              class="icon"
              data-action="column-new-task"
              :aria-label="'在 ' + topic.name + ' 新建任务'"
              :disabled="!online"
              @click="emit('addTask', topic.id)"
            >
              ＋
            </button>
          </div>
        </header>
        <div class="column-cards" @scroll="closeMenu">
          <article
            v-for="task in tasks.filter((t) => t.topicId === topic.id)"
            :key="task.id"
            class="task-card"
            :class="{ selected: selected === task.id, 'waiting-for-user': presentation(task).pendingInteractionCount > 0 }"
            :data-taskid="task.id"
            @click="emit('select', task)"
          >
            <div class="task-top">
              <h3>
                <button
                  class="task-select"
                  :aria-pressed="selected === task.id"
                  :aria-label="'选择任务 ' + task.title"
                  :aria-describedby="'task-status-' + task.id + (unread[task.conversationId] ? ' task-unread-' + task.id : '')"
                  :title="task.title"
                  @click.stop="emit('select', task)"
                >
                  <span class="task-title">{{ task.title }}</span>
                  <span
                    v-if="unread[task.conversationId]"
                    :id="'task-unread-' + task.id"
                    class="unread"
                    role="img"
                    aria-label="有新回复"
                  />
                </button>
              </h3>
              <span :id="'task-status-' + task.id" class="task-status" :data-status="presentation(task).status">{{
                presentation(task).label
              }}<small v-if="presentation(task).pendingInteractionCount > 1"> · {{ presentation(task).pendingInteractionCount }}</small></span>
            </div>
            <div
              v-if="deliveries?.[task.id]?.phase || summarizeSubagents(subagents,task.projectId,task.conversationId).length"
              class="task-auxiliary"
            >
              <SubagentBadge :runs="summarizeSubagents(subagents,task.projectId,task.conversationId)" :online="online" @open="emit('subagents',task)" />
              <span
                v-if="deliveries?.[task.id]?.phase"
                class="task-delivery-badge"
                :class="deliveries[task.id].phase"
                :title="'自动交付: ' + deliveryView(deliveries[task.id]).label"
              >交付 · {{ deliveryView(deliveries[task.id]).label }}</span>
            </div>
            <div class="task-bottom">
              <div class="task-identity">
                <span class="task-number">KW-{{ task.id.slice(0, 6) }}</span>
                <GitStatusView v-if="task.executionId" :status="git[task.id]" />
              </div>
              <div class="task-actions">
              <button
                v-if="task.executionId && !isDeliveryActive(deliveries?.[task.id]?.phase)"
                class="merge-task"
                aria-label="合并到主分支"
                :title="git[task.id]?.reason || '合并到主分支'"
                :disabled="!online || task.status === 'running' || presentation(task).status === 'waiting_user'"
                @click.stop="emit('merge', task)"
              >
                <MergeIcon />
              </button>
              <button
                v-if="presentation(task).status === 'waiting_user' || !isDeliveryActive(deliveries?.[task.id]?.phase)"
                class="start-task"
                :class="{ 'needs-participation': presentation(task).status === 'waiting_user' }"
                :aria-label="actionLabel(task)"
                :title="actionLabel(task)"
                :aria-busy="
                  presentation(task).status !== 'waiting_user' && (!!pending[task.conversationId] || task.status === 'running')
                "
                :disabled="
                  presentation(task).status !== 'waiting_user' && (!online ||
                  pending[task.conversationId] ||
                  task.status === 'running' ||
                  (['idle', 'failed', 'interrupted'].includes(presentation(task).status) &&
                    ['starting', 'running'].includes(
                      conversations[task.conversationId]?.runState ?? '',
                    )))
                "
                @click.stop="
                  presentation(task).status === 'waiting_user'
                    ? emit('process', task)
                    : ['idle', 'failed', 'interrupted'].includes(presentation(task).status)
                      ? emit('start', task)
                      : emit('select', task)
                "
              >
                <TaskActionIcon
                  :status="presentation(task).status"
                  :interaction-kind="presentation(task).interactionKind"
                  :pending="pending[task.conversationId]"
                />
              </button>
              <div class="task-actions-menu" @focusout="menuBlur" @click.stop @keydown.stop>
                <button type="button" class="task-menu-toggle" aria-label="任务操作" title="任务操作" aria-haspopup="menu" :aria-expanded="openMenu===task.id" :aria-controls="'task-menu-'+task.id" @click.stop="toggleMenu(task.id,$event)">⋯</button>
                <div v-if="openMenu===task.id" :id="'task-menu-'+task.id" :style="menuPosition" class="task-menu-panel" role="menu" aria-label="任务操作" @keydown="menuKeys">
                  <button role="menuitem" @click="openMenu='';emit('select',task)">打开任务对话</button>
                  <button v-if="completion(task)" role="menuitem" @click="openMenu='';menuTrigger?.focus();emit('complete',task)">标记任务完成</button>
                </div>
              </div>
              </div>
            </div>
          </article>
          <div v-if="!tasks.some((t) => t.topicId === topic.id)" class="empty">
            暂无任务，点击上方 ＋ 添加
          </div>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.task-actions-menu{position:relative;flex-shrink:0}.task-menu-toggle{border:0;background:transparent;color:var(--muted);width:26px;min-height:28px;padding:0;font-size:16px}.task-menu-panel{position:absolute;right:0;bottom:32px;width:180px;border:1px solid var(--border);border-radius:8px;background:var(--theme-surface,#fff);padding:5px;box-shadow:0 6px 20px var(--theme-shadow,#17203220);z-index:2}.task-menu-panel button{display:flex;width:100%;text-align:left;min-height:32px;border:0;background:transparent;font-size:12px;padding:5px 9px}.task-menu-panel button:hover{background:var(--theme-hover,#f0f3ef)}
</style>

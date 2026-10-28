<script setup lang="ts">
import { computed, ref, nextTick, onMounted, onUnmounted } from 'vue';
import type { Topic, Task, Conversation, GitStatus, Execution, DeliverySummary } from "./types.ts";
import {taskState} from "./task-state.ts";
import {deliveryView, isDeliveryActive} from "./delivery.ts";
import {createCompletionIntent} from './task-completion.ts';
import GitStatusView from "./GitStatus.vue";
import MergeIcon from "./MergeIcon.vue";
import TaskActionIcon from "./TaskActionIcon.vue";
import CustomSelect from "./CustomSelect.vue";
const props = defineProps<{
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
const openMenu=ref('');let menuTrigger:HTMLElement|undefined;
function completion(task:Task){return createCompletionIntent({task,conversation:props.conversations[task.conversationId],execution:props.executions[task.executionId??''],delivery:props.deliveries?.[task.id],online:props.online,pending:props.pending[task.conversationId]})}
async function toggleMenu(id:string,event:MouseEvent){menuTrigger=event.currentTarget as HTMLElement;openMenu.value=openMenu.value===id?'':id;await nextTick();document.getElementById('task-menu-'+id)?.querySelector<HTMLButtonElement>('button')?.focus()}
function dismissMenu(e:Event){if(!(e.target as Element).closest('.task-actions-menu'))openMenu.value=''}
function menuKeys(e:KeyboardEvent){const el=e.currentTarget as HTMLElement;const items=[...el.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')];if(e.key==='Escape'){e.preventDefault();e.stopPropagation();openMenu.value='';menuTrigger?.focus()}else if(['ArrowDown','ArrowUp','Home','End'].includes(e.key)){e.preventDefault();const i=items.indexOf(document.activeElement as HTMLButtonElement);items[e.key==='Home'?0:e.key==='End'?items.length-1:(i+(e.key==='ArrowDown'?1:-1)+items.length)%items.length]?.focus()}}
function menuBlur(e:FocusEvent){if(!(e.currentTarget as HTMLElement).contains(e.relatedTarget as Node|null))openMenu.value=''}

let expiryClock: ReturnType<typeof setInterval> | undefined;
onMounted(()=>{document.addEventListener('pointerdown',dismissMenu); expiryClock=setInterval(()=>{now.value=Date.now()},1000); });
onUnmounted(()=>{document.removeEventListener('pointerdown',dismissMenu); if(expiryClock)clearInterval(expiryClock); });
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
  <section class="board-side" aria-label="项目任务看板">
    <div class="toolbar">
      <span class="running">{{ tasks.length }} 个任务</span>
      <span v-if="waitingCount" class="board-interaction-count" role="status">{{ waitingCount }} 项待你处理</span>
      <button
        data-action="new-topic"
        :disabled="!online"
        @click="emit('addTopic')"
      >
        ＋ 专题</button
      ><button
        class="primary"
        data-action="new-task"
        :disabled="!online"
        @click="emit('addTask', '')"
      >
        ＋ 任务</button
      ><CustomSelect
        class="mobile-topic"
        aria-label="当前看板专题"
        :model-value="mobileTopic"
        :options="
          topics.map((topic) => ({
            value: topic.id,
            label: `${topic.name} · ${tasks.filter((t) => t.topicId === topic.id).length}`,
          }))
        "
        compact
        @update:model-value="emit('topic', $event)"
      />
    </div>
    <div class="board" id="board">
      <section
        v-for="topic in topics"
        :key="topic.id"
        class="column"
        :class="{ 'mobile-hidden': mobileTopic !== topic.id }"
        :data-topicid="topic.id"
        :aria-label="topic.name"
      >
        <header class="column-head">
          <span class="column-dot" />
          <h2>{{ topic.name }}</h2>
          <span class="count">{{
            tasks.filter((t) => t.topicId === topic.id).length
          }}</span>
          <div class="column-actions">
            <button
              v-if="!topic.protected"
              class="icon"
              :aria-label="'删除专题 ' + topic.name"
              :disabled="!online"
              @click="emit('deleteTopic', topic)"
            >
              ⋯</button
            ><button
              class="icon"
              :aria-label="'在 ' + topic.name + ' 新建任务'"
              :disabled="!online"
              @click="emit('addTask', topic.id)"
            >
              ＋
            </button>
          </div>
        </header>
        <div class="column-cards">
          <article
            v-for="task in tasks.filter((t) => t.topicId === topic.id)"
            :key="task.id"
            class="task-card"
            :class="{ selected: selected === task.id, 'waiting-for-user': presentation(task).pendingInteractionCount > 0 }"
            :data-taskid="task.id"
            @click="emit('select', task)"
          >
            <button
              class="task-select"
              :aria-pressed="selected === task.id"
              :aria-label="'选择任务 ' + task.title"
              @click.stop="emit('select', task)"
            >
              <div class="task-top">
                <span class="task-number">KW-{{ task.id.slice(0, 6) }}</span
                ><span
                  v-if="deliveries?.[task.id]?.phase"
                  class="task-delivery-badge"
                  :class="deliveries[task.id].phase"
                  :title="'自动交付: ' + deliveryView(deliveries[task.id]).label"
                >{{ deliveryView(deliveries[task.id]).label }}</span>
                <span class="task-status" :data-status="presentation(task).status">{{
                  presentation(task).label
                }}<small v-if="presentation(task).pendingInteractionCount > 1"> · {{ presentation(task).pendingInteractionCount }}</small></span>
              </div>
              <h3>
                {{ task.title }}
                <span v-if="unread[task.conversationId]" class="unread"
                  >新回复</span
                >
              </h3>
            </button>
            <div class="task-bottom">
              <span class="owner"><span class="avatar">π</span>pi</span>
              <GitStatusView v-if="task.executionId" :status="git[task.id]" />
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
                <div v-if="openMenu===task.id" :id="'task-menu-'+task.id" class="task-menu-panel" role="menu" aria-label="任务操作" @keydown="menuKeys">
                  <button role="menuitem" @click="openMenu='';emit('select',task)">打开任务对话</button>
                  <button v-if="completion(task)" role="menuitem" @click="openMenu='';menuTrigger?.focus();emit('complete',task)">标记任务完成</button>
                </div>
              </div>
            </div>
          </article>
          <div v-if="!tasks.some((t) => t.topicId === topic.id)" class="empty">
            暂无任务<br />点击上方 ＋ 添加第一张卡片
          </div>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.task-actions-menu{position:relative;flex-shrink:0}.task-menu-toggle{border:0;background:transparent;color:var(--muted);width:26px;min-height:28px;padding:0;font-size:16px}.task-menu-panel{position:absolute;right:0;bottom:32px;width:180px;border:1px solid var(--border);border-radius:8px;background:var(--theme-surface,#fff);padding:5px;box-shadow:0 6px 20px var(--theme-shadow,#17203220);z-index:2}.task-menu-panel button{display:flex;width:100%;text-align:left;min-height:32px;border:0;background:transparent;font-size:12px;padding:5px 9px}.task-menu-panel button:hover{background:var(--theme-hover,#f0f3ef)}
</style>

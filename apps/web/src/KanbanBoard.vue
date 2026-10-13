<script setup lang="ts">
import type { Topic, Task, Conversation, GitStatus } from "./types.ts";
import GitStatusView from "./GitStatus.vue";
import MergeIcon from "./MergeIcon.vue";
import TaskActionIcon from "./TaskActionIcon.vue";
const props = defineProps<{
  topics: Topic[];
  git: Record<string, GitStatus>;
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
  start: [Task];
  merge: [Task];
  topic: [string];
  addTopic: [];
  addTask: [string];
  deleteTopic: [Topic];
}>();
const labels: Record<string, string> = {
  idle: "○ 未开始",
  running: "◐ 执行中",
  needs_confirmation: "! 需要确认",
  completed: "✓ 执行完成 · 查看结果",
  failed: "× 执行失败",
};
function actionLabel(task: Task) {
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
      <h1>项目看板</h1>
      <span class="running">{{ tasks.length }} 个任务</span
      ><button
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
      ><select
        class="mobile-topic"
        aria-label="当前看板专题"
        :value="mobileTopic"
        @change="emit('topic', ($event.target as HTMLSelectElement).value)"
      >
        <option v-for="topic in topics" :key="topic.id" :value="topic.id">
          {{ topic.name }} ·
          {{ tasks.filter((t) => t.topicId === topic.id).length }}
        </option>
      </select>
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
            :class="{ selected: selected === task.id }"
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
                ><span class="task-status" :data-status="task.status">{{
                  labels[task.status] ?? task.status
                }}</span>
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
                v-if="task.executionId"
                class="merge-task"
                aria-label="合并到主分支"
                :title="git[task.id]?.reason || '合并到主分支'"
                :disabled="!online || task.status === 'running'"
                @click.stop="emit('merge', task)"
              >
                <MergeIcon />
              </button>
              <button
                class="start-task"
                :aria-label="actionLabel(task)"
                :title="actionLabel(task)"
                :aria-busy="
                  !!pending[task.conversationId] || task.status === 'running'
                "
                :disabled="
                  !online ||
                  pending[task.conversationId] ||
                  task.status === 'running' ||
                  (['idle', 'failed'].includes(task.status) &&
                    ['starting', 'running', 'interrupted'].includes(
                      conversations[task.conversationId]?.runState ?? '',
                    ))
                "
                @click.stop="
                  ['idle', 'failed'].includes(task.status)
                    ? emit('start', task)
                    : emit('select', task)
                "
              >
                <TaskActionIcon
                  :status="task.status"
                  :pending="pending[task.conversationId]"
                />
              </button>
            </div>
          </article>
          <div v-if="!tasks.some((t) => t.topicId === topic.id)" class="empty">
            暂无任务<br />点击上方 ＋ 添加第一张卡片
          </div>
        </div>
      </section>
    </div>
    <div class="board-bottom">
      专题组织任务 · 选择卡片沟通 · 点击开始开发执行任务
    </div>
  </section>
</template>

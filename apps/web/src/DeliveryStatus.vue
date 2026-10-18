<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import type { DeliverySummary, DeliveryStep, DeliveryCI } from './types.ts';
import { deliveryView, isSafeGitHubURL } from './delivery.ts';
import { api } from './api.ts';

const props = defineProps<{
  summary?: DeliverySummary;
  taskTitle?: string;
  accepting?: boolean;
  controlling?: boolean;
}>();

const emit = defineEmits<{
  accept: [summary: DeliverySummary];
  pause: [summary: DeliverySummary];
  resume: [summary: DeliverySummary];
  cancel: [summary: DeliverySummary];
  extendBudget: [summary: DeliverySummary];
}>();

const view = computed(() => deliveryView(props.summary));

const pagedSteps = ref<DeliveryStep[]>([]);
const stepsLoading = ref(false);
const stepsError = ref('');
const nextCursor = ref<number | undefined>(undefined);
const fullyLoaded = ref(false);
let loadGen = 0;

// 当切换交付周期、任务或 ID/Cycle 改变时，立即清空分页步骤与错误
watch(
  () => [props.summary?.id, props.summary?.cycle],
  () => {
    loadGen++;
    pagedSteps.value = [];
    stepsError.value = '';
    stepsLoading.value = false;
    nextCursor.value = undefined;
    fullyLoaded.value = false;
  },
  { immediate: true }
);

async function loadMoreSteps() {
  const deliveryId = props.summary?.id;
  if (!deliveryId || stepsLoading.value || fullyLoaded.value) return;
  const currentGen = ++loadGen;
  stepsLoading.value = true;
  stepsError.value = '';
  try {
    const after = nextCursor.value !== undefined ? nextCursor.value : 0;
    const res = await api<{ items: DeliveryStep[]; next: number }>('delivery/steps', {
      deliveryId,
      after,
      limit: 100,
    });
    if (currentGen !== loadGen || props.summary?.id !== deliveryId) return;
    if (res && Array.isArray(res.items)) {
      pagedSteps.value.push(...res.items);
      if (res.next && res.next > 0) {
        nextCursor.value = res.next;
      } else {
        nextCursor.value = undefined;
        fullyLoaded.value = true;
      }
    } else {
      fullyLoaded.value = true;
    }
  } catch (e) {
    if (currentGen === loadGen && props.summary?.id === deliveryId) {
      stepsError.value = e instanceof Error ? e.message : '加载较早历史步骤失败';
    }
  } finally {
    if (currentGen === loadGen && props.summary?.id === deliveryId) {
      stepsLoading.value = false;
    }
  }
}

const displaySteps = computed(() => {
  const recent = props.summary?.steps ?? [];
  const map = new Map<string, DeliveryStep>();
  // 先把分页拉回的历史条目放入
  for (const s of pagedSteps.value) {
    if (s && s.id) map.set(s.id, s);
  }
  // 再把最新 snapshot 中的 recent 放入（更新状态）
  for (const s of recent) {
    if (s && s.id) map.set(s.id, s);
  }
  const all = Array.from(map.values());
  all.sort((a, b) => a.sequence - b.sequence);
  return all;
});

const canLoadMore = computed(() => {
  if (fullyLoaded.value) return false;
  // 如果尚未进行过分页加载（nextCursor 为 undefined），且当前 summary.steps 达到 20 条，说明可能存在未展示的较早步骤
  if (nextCursor.value === undefined) {
    return (props.summary?.steps?.length ?? 0) >= 20;
  }
  return nextCursor.value > 0;
});

function formatTime(iso?: string) {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}
</script>

<template>
  <div v-if="view.active || summary" class="delivery-status-panel">
    <div class="delivery-header">
      <div class="delivery-headline">
        <span class="delivery-tag" :class="view.phase">{{ view.label }}</span>
        <span class="delivery-waiting-badge" v-if="view.waiting" :title="view.waitingReason">{{ view.waitingText || '交互等待中' }}</span>
        <span class="delivery-repairs" v-if="view.repairsText">{{ view.repairsText }}</span>
        <span class="delivery-turn-badge" v-if="view.holdsTurn">持有回合</span>
      </div>
      <div class="delivery-actions-bar">
        <button
          v-if="view.canAccept"
          class="primary accept-btn"
          :disabled="accepting"
          @click="props.summary && emit('accept', props.summary)"
        >
          {{ accepting ? '确认交付中…' : '效果验收通过 · 合并主分支' }}
        </button>

        <button
          v-if="view.canExtendBudget"
          class="small-btn primary"
          :disabled="controlling"
          @click="props.summary && emit('extendBudget', props.summary)"
        >
          追加一份有限额度
        </button>

        <button
          v-if="view.canPause"
          class="small-btn"
          :disabled="controlling"
          @click="props.summary && emit('pause', props.summary)"
        >
          暂停
        </button>

        <button
          v-if="view.canResume"
          class="small-btn primary"
          :disabled="controlling"
          @click="props.summary && emit('resume', props.summary)"
        >
          恢复
        </button>

        <button
          v-if="view.canCancel"
          class="small-btn danger"
          :disabled="controlling"
          title="取消交付：未完成核验，已推送的远程分支不会撤销"
          @click="props.summary && emit('cancel', props.summary)"
        >
          取消
        </button>
      </div>
    </div>

    <p class="delivery-desc">{{ view.description }}</p>

    <div v-if="view.waiting" class="delivery-waiting-box" role="status">
      <strong>{{ view.waitingText || '交付等待中' }}</strong>
      <p>{{ view.waitingReason || '需要先完成相关交互答复或补充说明，交付流程才会自动继续。' }}</p>
    </div>

    <div v-if="view.blocked" class="delivery-blocker-box" role="alert">
      <strong>交付阻塞 ({{ view.blockCode || 'BLOCKED' }}):</strong>
      <p>{{ view.blockReason || '未提供具体受阻原因' }}</p>
    </div>

    <!-- SHA details -->
    <details v-if="view.candidateSHA || view.localMainSHA || view.remoteMainSHA" class="delivery-tech-details">
      <summary class="tech-summary">Git 提交详情</summary>
      <div class="delivery-sha-strip">
        <span v-if="view.candidateSHA" title="待交付候选提交">任务: <code>{{ view.candidateSHA.slice(0, 8) }}</code></span>
        <span v-if="view.localMainSHA" title="本地主分支基线">本地主: <code>{{ view.localMainSHA.slice(0, 8) }}</code></span>
        <span v-if="view.remoteMainSHA" title="远程主分支最新">远端主: <code>{{ view.remoteMainSHA.slice(0, 8) }}</code></span>
      </div>
    </details>

    <!-- CI runs -->
    <details v-if="view.ciRuns.length > 0" class="delivery-tech-details">
      <summary class="tech-summary">GitHub Actions 运行记录 ({{ view.ciRuns.length }})</summary>
      <div class="delivery-ci-runs">
        <div v-for="(ci, i) in view.ciRuns" :key="i" class="ci-run-item">
          <span class="ci-badge" :class="[ci.status, ci.conclusion]">
            {{ ci.stage === 'task' ? '任务 CI' : '主分支 CI' }} · {{ ci.conclusion || ci.status }}
          </span>
          <a :href="ci.url" target="_blank" rel="noopener noreferrer" class="ci-link">
            查看 Actions 详情 ↗
          </a>
        </div>
      </div>
    </details>

    <!-- Steps history -->
    <div v-if="displaySteps.length > 0" class="delivery-steps-history">
      <details>
        <summary class="tech-summary">交付步骤记录 ({{ displaySteps.length }}{{ canLoadMore ? '+' : '' }})</summary>
        <div v-if="canLoadMore || stepsLoading" class="steps-pagination">
          <button class="small-btn" :disabled="stepsLoading" @click="loadMoreSteps">
            {{ stepsLoading ? '加载中…' : '加载完整/较早记录' }}
          </button>
        </div>
        <p v-if="stepsError" class="steps-error">{{ stepsError }}</p>
        <ul class="steps-list">
          <li v-for="step in displaySteps" :key="step.id || step.sequence" class="step-item">
            <span class="step-time">{{ formatTime(step.startedAt) }}</span>
            <span class="step-kind">{{ step.kind }}</span>
            <span class="step-summary">{{ step.summary }}</span>
            <span v-if="step.exitKnown" class="step-exit" :class="step.exitCode === 0 ? 'exit-ok' : 'exit-fail'">
              {{ step.exitCode === 0 ? 'OK' : `ERR(${step.exitCode})` }}
            </span>
          </li>
        </ul>
      </details>
    </div>
  </div>
</template>

<style scoped>
.delivery-status-panel {
  margin: 12px 0;
  padding: 12px 14px;
  background: var(--bg-hover, #f8f9fa);
  border: 1px solid var(--border, #e5e5e5);
  border-radius: 6px;
  font-size: 13px;
}
.delivery-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.delivery-headline {
  display: flex;
  align-items: center;
  gap: 8px;
}
.delivery-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: bold;
  background: #e6f7ff;
  color: #096dd9;
  border: 1px solid #91d5ff;
}
.delivery-tag.awaiting_acceptance {
  background: #fff7e6;
  color: #d46b08;
  border-color: #ffd591;
}
.delivery-tag.blocked {
  background: #fff1f0;
  color: #cf1322;
  border-color: #ffa39e;
}
.delivery-tag.succeeded {
  background: #f6ffed;
  color: #389e0d;
  border-color: #b7eb8f;
}
.delivery-repairs {
  font-size: 12px;
  color: var(--text-muted, #666);
}
.delivery-turn-badge {
  font-size: 11px;
  padding: 1px 6px;
  background: #e6fffb;
  color: #08979c;
  border: 1px solid #87e8de;
  border-radius: 4px;
}
.delivery-actions-bar {
  display: flex;
  align-items: center;
  gap: 6px;
}
.accept-btn {
  font-weight: bold;
  padding: 4px 10px;
}
.small-btn {
  font-size: 12px;
  padding: 2px 8px;
}
.small-btn.danger {
  color: #cf1322;
  border-color: #ffa39e;
}
.delivery-desc {
  margin: 8px 0 4px 0;
  color: var(--text, #333);
  line-height: 1.4;
}
.delivery-blocker-box {
  background: #fff1f0;
  border: 1px solid #ffa39e;
  padding: 8px 10px;
  border-radius: 4px;
  margin: 8px 0;
  color: #cf1322;
}
.delivery-sha-strip {
  display: flex;
  gap: 12px;
  font-size: 12px;
  color: var(--text-muted, #666);
  margin-top: 6px;
}
.delivery-ci-runs {
  margin-top: 8px;
  border-top: 1px dashed var(--border, #ddd);
  padding-top: 6px;
}
.ci-runs-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--text-muted, #666);
  margin-bottom: 4px;
}
.ci-run-item {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  margin-bottom: 2px;
}
.ci-badge {
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
  background: #f0f0f0;
}
.ci-badge.failure {
  background: #fff1f0;
  color: #cf1322;
}
.ci-badge.success {
  background: #f6ffed;
  color: #389e0d;
}
.ci-link {
  color: #1890ff;
  text-decoration: none;
}
.ci-link:hover {
  text-decoration: underline;
}
.delivery-steps-history {
  margin-top: 8px;
  font-size: 12px;
}
.delivery-steps-history summary {
  cursor: pointer;
  color: var(--text-muted, #666);
}
.steps-list {
  margin: 6px 0 0 0;
  padding-left: 0;
  list-style: none;
}
.step-item {
  display: flex;
  gap: 8px;
  padding: 2px 0;
  border-bottom: 1px dotted #f0f0f0;
}
.step-time {
  color: #999;
}
.step-kind {
  font-weight: 500;
}
.step-summary {
  flex: 1;
}
.step-exit.exit-ok {
  color: #389e0d;
}
.step-exit.exit-fail {
  color: #cf1322;
}
</style>

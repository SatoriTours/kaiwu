<script setup lang="ts">
import { computed } from 'vue';
import type { Message, Task, GitStatus, Execution, DeliverySummary, ToolRecord, ReportCheck, Conversation } from './types.ts';
import type { ChatEntry } from './chat-messages.ts';
import MarkdownContent from './MarkdownContent.vue';
import { messageText, turnSummary, replySummary, replyLinks } from './chat-messages.ts';
import { taskState } from './task-state.ts';
import { gitStatusLabel } from './git.ts';
import { isDeliveryActive } from './delivery.ts';
import { isWebPreviewable } from './preview.ts';
import type { TaskPreview } from './types.ts';

const props = withDefaults(
  defineProps<{
    entry?: ChatEntry;
    message?: Message;
    updates?: Message[];
    isLatest?: boolean;
    task?: Task;
    git?: GitStatus;
    execution?: Execution;
    delivery?: DeliverySummary;
    conversation?: Conversation;
    online?: boolean;
    pending?: boolean;
    preview?: TaskPreview;
  }>(),
  {
    isLatest: false,
    online: true,
    pending: false,
  }
);

const emit = defineEmits<{
  confirm: [roundId: string];
  preview: [roundId: string];
  files: [roundId: string];
  merge: [];
  reply: [messageId: string];
}>();

const effectiveEntry = computed<ChatEntry>(() => {
  if (props.entry) return props.entry;
  const msg = props.message ?? { id: 'fallback', role: 'assistant', text: '', delivery: 'complete' };
  return {
    id: msg.id,
    role: 'assistant',
    message: msg,
    updates: props.updates ?? [msg],
  };
});

const summary = computed(() => {
  if (props.entry) {
    return turnSummary(props.entry);
  }
  const summaryMsg = [...(props.updates ?? [])].reverse().find(
    m => messageText(m).trim() || (m.displayText !== undefined && m.text.includes('<kaiwu-report>'))
  ) ?? props.message;
  return summaryMsg ? replySummary(summaryMsg) : '无文字说明';
});

const loading = computed(() => !!effectiveEntry.value.loading);
const round = computed(() => effectiveEntry.value.round);
const report = computed(() => round.value?.report);

const roundTools = computed<ToolRecord[]>(() => {
  if (round.value?.tools?.length) return round.value.tools;
  if (props.isLatest && props.execution?.tools?.length) return props.execution.tools;
  return [];
});

const updatesList = computed(() => {
  if (effectiveEntry.value.updates?.length) return effectiveEntry.value.updates;
  if (props.updates?.length) return props.updates;
  if (props.message) return [props.message];
  return [];
});

const links = computed(() => {
  const set = new Set<string>();
  for (const u of updatesList.value) {
    for (const link of replyLinks(u)) {
      set.add(link);
      if (set.size >= 6) break;
    }
    if (set.size >= 6) break;
  }
  return [...set];
});

const deliveryActive = computed(() => (props.delivery ? isDeliveryActive(props.delivery.phase) : false));

// Result actions for latest round only
const presentation = computed(() => {
  if (!props.task) return undefined;
  return taskState(props.task, props.conversation, props.execution, props.isLatest ? undefined : round.value?.id);
});

const latestPresentation = computed(() => {
  if (!props.task) return undefined;
  return taskState(props.task, props.conversation, props.execution);
});

const confirmable = computed(() => {
  if (!props.isLatest || !latestPresentation.value) return false;
  return (
    !deliveryActive.value &&
    latestPresentation.value.status !== 'waiting_user' &&
    !!latestPresentation.value.confirmRoundId
  );
});

const completedConfirmed = computed(() => {
  if (!props.isLatest || !latestPresentation.value) return false;
  return latestPresentation.value.status === 'completed' && latestPresentation.value.resultConfirmed;
});

const previewButtonLabel = computed(() => {
  if (props.preview && isWebPreviewable(props.preview)) {
    return '打开网页';
  }
  return '查看结果';
});

function claimLabel(claim?: string) {
  return (
    (
      {
        passed: 'AI 报告通过',
        failed: 'AI 报告失败',
        not_run: '未执行',
        unknown: '待核实',
      } as Record<string, string>
    )[claim ?? ''] ?? '待核实'
  );
}

function evidenceLabel(check: ReportCheck) {
  const tool = roundTools.value.find(t => check.toolId && t.id === check.toolId);
  if (!tool) return '缺少本轮依据';
  if (tool.failed) return '执行失败';
  if (tool.result) return tool.name === 'bash' ? '命令完成' : '工具已返回';
  return '执行中';
}
function onPreviewClick() {
  if (latestPresentation.value?.resultRoundId) {
    emit('preview', latestPresentation.value.resultRoundId);
  }
}
</script>

<template>
  <div class="assistant-turn-container">
    <details class="assistant-turn-details">
      <summary class="assistant-turn-summary" :aria-label="'AI 回复及详情展开'">
        <span v-if="loading" class="turn-loading-indicator" role="status" aria-label="执行中">
          <span class="turn-spinner"></span>
        </span>
        <div class="turn-summary-wrapper">
          <MarkdownContent class="assistant-summary-markdown" :text="summary" />
        </div>
        <span class="turn-expand-icon" aria-hidden="true">展开详情 ▾</span>
      </summary>

      <div class="assistant-turn-body">
        <!-- 1. Report details (if round.report exists) -->
        <div v-if="report" class="round-report-section">
          <!-- 方案 Plan -->
          <details v-if="report.plan?.length" class="round-sub-details" open>
            <summary>方案 ({{ report.plan.length }})</summary>
            <ol class="plan-list">
              <li v-for="(p, i) in report.plan" :key="i">{{ p }}</li>
            </ol>
          </details>

          <!-- 成果 Results -->
          <details v-if="report.results?.length" class="round-sub-details" open>
            <summary>成果 ({{ report.results.length }})</summary>
            <div v-for="(res, i) in report.results" :key="i" class="feature-row">
              <span class="feature-check">✓</span>
              <span>{{ res }}</span>
            </div>
          </details>

          <!-- 验证 Checks -->
          <details v-if="report.checks?.length" class="round-sub-details" open>
            <summary>验证 ({{ report.checks.length }})</summary>
            <div v-for="(check, i) in report.checks" :key="i" class="check-row">
              <div class="row between">
                <span>{{ check.title }}</span>
                <small>{{ claimLabel(check.claim) }}</small>
              </div>
              <details v-if="check.toolId && roundTools.some(t => t.id === check.toolId)" class="check-evidence-details">
                <summary>{{ evidenceLabel(check) }} · 查看依据</summary>
                <template v-for="t in roundTools.filter(t => t.id === check.toolId)" :key="t.id">
                  <MarkdownContent v-if="t.input" :text="t.input" format="json" />
                  <MarkdownContent v-if="t.result" :text="t.result" format="json" />
                </template>
              </details>
              <p v-else class="check-missing">
                {{ check.claim === 'not_run' ? '未执行验证' : '缺少本轮执行依据，结果待核实' }}
              </p>
            </div>
          </details>

          <!-- 下一步 Next -->
          <div v-if="report.next?.length" class="progress-next">
            <small>下一步</small>
            <p v-for="(item, i) in report.next" :key="i">{{ item }}</p>
          </div>

          <!-- 问题与阻塞 Question & Blockers -->
          <div v-if="report.question || report.blockers?.length" class="progress-attention" role="status">
            <strong v-if="report.question">需要你决定：{{ report.question }}</strong>
            <p v-for="b in report.blockers ?? []" :key="b">{{ b }}</p>
          </div>
        </div>

        <!-- 2. Tool records (所有工具) -->
        <details v-if="roundTools.length" class="round-sub-details tools-details">
          <summary>工具执行记录 ({{ roundTools.length }})</summary>
          <ul class="tool-records">
            <li v-for="tool in roundTools" :key="tool.id">
              <details>
                <summary>
                  {{ tool.name }} · {{ tool.failed ? '失败' : tool.result ? '已返回' : loading ? '执行中' : '未收到结束回执' }}
                </summary>
                <MarkdownContent v-if="tool.input" :text="tool.input" format="json" />
                <MarkdownContent v-if="tool.result" :text="tool.result" format="json" />
              </details>
            </li>
          </ul>
        </details>

        <!-- 3. Original messages / updates / raw report (旧原文) -->
        <details v-if="updatesList.length" class="round-sub-details updates-details" :open="!report">
          <summary>对话原文与过程记录 ({{ updatesList.length }})</summary>
          <div v-for="(update, i) in updatesList" :key="update.id" class="progress-update" :data-message-id="update.id">
            <small v-if="updatesList.length > 1">第 {{ i + 1 }} 条更新</small>
            <MarkdownContent class="text" :text="messageText(update) || update.text" />
            <details v-if="update.displayText !== undefined || update.text.includes('<kaiwu-report>')" class="raw-report">
              <summary>原始回复与报告数据</summary>
              <MarkdownContent :text="update.text" />
            </details>
          </div>
        </details>

        <!-- 4. Report error if any -->
        <p v-if="round?.reportError" class="check-missing" role="status">{{ round.reportError }}</p>

        <!-- 5. Links -->
        <div v-if="links.length" class="reply-links">
          <small>相关链接</small>
          <a v-for="url in links" :key="url" :href="url" target="_blank" rel="noopener noreferrer">{{ url }}</a>
        </div>
      </div>
    </details>

    <!-- 6. Bottom action bar for latest round only -->
    <div v-if="isLatest && (confirmable || completedConfirmed)" class="turn-result-actions" role="region" aria-label="成果验收与操作">
      <template v-if="confirmable">
        <div class="result-action-row">
          <button type="button" class="primary compact-btn" @click.stop="onPreviewClick">
            {{ previewButtonLabel }}
          </button>
          <button v-if="preview?.artifacts?.length" type="button" class="compact-btn" :disabled="!online || pending" @click.stop="emit('files', latestPresentation!.resultRoundId!)">文件产物（{{preview.artifacts.length}}）</button>
          <button v-if="git && !deliveryActive" type="button" class="compact-btn merge-btn" :disabled="!online || pending" @click.stop="emit('merge')">{{ git.state === 'merged' ? '审核推送' : '合并到主分支' }}</button>
          <button type="button" class="compact-btn confirm-btn" :disabled="!online || pending" @click.stop="emit('confirm', latestPresentation!.confirmRoundId!)">
            确认完成
          </button>
        </div>
      </template>
      <template v-else-if="completedConfirmed">
        <div class="completed-compact-bar">
          <div class="completed-compact-status">
            <span class="completed-badge">已完成</span>
            <span v-if="git?.state === 'merged'" class="merged-tag">已合并 · {{ git?.mainBranch || '主分支' }}</span>
            <span v-else-if="git && !deliveryActive" class="unmerged-label">{{ gitStatusLabel(git) }}<template v-if="git?.mainBranch"> · {{ git.mainBranch }}</template></span>
          </div>
          <div class="completed-compact-actions">
            <button type="button" class="compact-btn" @click.stop="emit('preview', latestPresentation!.resultRoundId!)">
              {{ previewButtonLabel }}
            </button>
            <button v-if="preview?.artifacts?.length" type="button" class="compact-btn" :disabled="!online || pending" @click.stop="emit('files', latestPresentation!.resultRoundId!)">文件产物（{{preview.artifacts.length}}）</button>
            <button
              v-if="!deliveryActive && git"
              type="button"
              class="compact-btn merge-btn"
              :disabled="!online || pending"
              @click.stop="emit('merge')"
            >
              {{ git?.state === 'merged' ? '审核推送' : '合并到主分支' }}
            </button>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

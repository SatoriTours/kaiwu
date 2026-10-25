<script setup lang="ts">
import type { InteractionRequest } from './types.ts';
import MarkdownContent from './MarkdownContent.vue';
import { effectiveInteractionStatus, formatAnswerSummary, interactionHistoryTitle, INTERACTION_DELIVERY_LABELS, INTERACTION_STATUS_LABELS } from './interaction.ts';
defineProps<{ requests: InteractionRequest[]; now: number; pending: boolean; online: boolean }>();
const emit = defineEmits<{ retry: [request: InteractionRequest] }>();
</script>
<template>
  <div v-if="requests.length" class="interaction-history-list" aria-label="交互历史记录">
    <details v-for="ix in requests" :key="ix.id" class="interaction-history-card" :data-interaction-id="ix.id">
      <summary class="interaction-history-head">
        <span class="interaction-history-mark" aria-hidden="true">{{ ix.status === 'answered' ? '✓' : '−' }}</span>
        <span class="interaction-history-title">{{ interactionHistoryTitle(ix, now) }}</span>
        <span class="interaction-history-status" :class="ix.response?.delivery || effectiveInteractionStatus(ix, now)">{{ ix.response && ix.status === 'answered' ? INTERACTION_DELIVERY_LABELS[ix.response.delivery] : INTERACTION_STATUS_LABELS[effectiveInteractionStatus(ix, now)] }}</span>
        <time v-if="ix.response?.answeredAt" :datetime="ix.response.answeredAt" class="interaction-history-time">{{ new Date(ix.response.answeredAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) }}</time>
      </summary>
      <div class="interaction-history-body">
        <MarkdownContent v-if="ix.description" class="interaction-desc" :text="ix.description" />
        <p class="interaction-history-summary"><strong>答复内容：</strong>{{ ix.response ? formatAnswerSummary(ix) : '未答复（已失效，只可查看）' }}</p>
        <p v-if="ix.statusReason" class="interaction-history-note">{{ ix.statusReason }}</p>
        <p v-if="ix.response?.answeredAt" class="interaction-history-note">提交时间：{{ new Date(ix.response.answeredAt).toLocaleString('zh-CN') }}</p>
        <p v-if="ix.response?.note" class="interaction-history-note">{{ ix.response.note }}</p>
        <p v-if="ix.response?.delivery === 'uncertain'" class="interaction-history-note">回答已保存，送达待核查；不会自动重复发送。</p>
        <p v-else-if="ix.response?.delivery === 'recorded'" class="interaction-history-note">回答已保存，尚未确认送达。</p>
        <button v-if="ix.response?.delivery === 'not_sent'" type="button" class="interaction-btn primary" :disabled="pending || !online" @click="emit('retry', ix)">重试发送</button>
      </div>
    </details>
  </div>
</template>

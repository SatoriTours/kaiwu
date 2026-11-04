<script setup lang="ts">
import { computed } from 'vue';
import type { SubagentSummary } from './types.ts';
import { formatSubagentBadgeText } from './subagents.ts';
const props = defineProps<{ runs: SubagentSummary[]; online: boolean }>();
const emit = defineEmits<{ open: [] }>();
const badge = computed(() => formatSubagentBadgeText(props.runs));
</script>
<template>
  <button v-if="badge" type="button" class="subagent-badge" :data-tone="badge.tone" aria-haspopup="dialog" @click.stop="emit('open')">
    {{ badge.text }}<span v-if="!online"> · 离线</span>
  </button>
</template>
<style scoped>
.subagent-badge{border:0;background:transparent;color:var(--muted);font-size:11px;min-height:28px;padding:3px 6px;text-align:left;white-space:normal;overflow-wrap:anywhere}
.subagent-badge[data-tone=running]{color:var(--accent, #47705b)}
.subagent-badge[data-tone=unknown]{color:var(--ink)}
.subagent-badge:hover{background:var(--theme-hover, #f0f3ef)}
</style>

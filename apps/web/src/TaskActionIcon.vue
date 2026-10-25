<script setup lang="ts">
import InteractionIcon from './InteractionIcon.vue';
import type { InteractionKind } from './interaction.ts';
defineProps<{ status: string; pending?: boolean; interactionKind?: InteractionKind }>();
</script>
<template>
  <InteractionIcon v-if="status === 'waiting_user'" :kind="interactionKind" class="task-action-waiting" />
  <svg v-else
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.7"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    :class="{ 'task-action-spinner': pending || status === 'running' }"
  >
    <path v-if="pending || status === 'running'" d="M20 12a8 8 0 1 1-8-8" />
    <path v-else-if="['failed','interrupted'].includes(status)" d="M3 11a9 9 0 1 1 2.7 7M3 4v7h7" />
    <template v-else-if="status === 'needs_confirmation'"><rect x="5" y="4" width="14" height="17" rx="2" /><path d="m8 12 3 3 5-6M9 4V2h6v2" /></template>
    <path v-else-if="status !== 'idle'" d="M5 12h14m-7-7 7 7-7 7" />
    <path v-else d="m9 5.5 10 6.5-10 6.5z" />
  </svg>
</template>

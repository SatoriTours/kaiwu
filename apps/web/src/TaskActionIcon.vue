<script setup lang="ts">
defineProps<{ status: string; pending?: boolean }>();
</script>
<template>
  <svg
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
    <path v-else-if="status !== 'idle'" d="M5 12h14m-7-7 7 7-7 7" />
    <path v-else d="m9 5.5 10 6.5-10 6.5z" />
  </svg>
</template>

<script setup lang="ts">
import type { InteractionKind } from './interaction.ts';
defineProps<{ kind?: InteractionKind }>();
</script>
<template>
  <svg class="interaction-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
    <template v-if="kind === 'confirm'">
      <path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6Z" />
      <path d="M12 8v4m0 3h.01" />
    </template>
    <template v-else>
      <path d="M20 11a8 8 0 0 1-8 8H6l-4 3V11a8 8 0 0 1 8-8h2a8 8 0 0 1 8 8Z" />
      <path v-if="kind === 'select'" d="m8 11 3 3 5-5" />
      <path v-else d="M11 7v5m0 3h.01" />
    </template>
  </svg>
</template>

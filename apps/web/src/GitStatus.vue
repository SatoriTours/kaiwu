<script setup lang="ts">
import type { GitStatus } from "./types.ts";
import { gitStatusLabel } from "./git.ts";
defineProps<{ status?: GitStatus; detail?: boolean }>();
</script>
<template>
  <div
    class="git-state"
    :data-git-state="status?.state"
    :title="status?.reason || '根据本地 Git 提交及工作区检查'"
  >
    <span class="git-state-dot" /><span>{{ gitStatusLabel(status) }}</span
    ><span v-if="detail && status?.mainBranch" class="git-target"
      >→ {{ status.mainBranch }}
      <span v-if="status.taskSHA"
        >· {{ status.taskSHA.slice(0, 7) }}</span
      ></span
    >
    <p v-if="detail && status?.reason" class="git-reason">
      {{ status.reason }}
    </p>
    <small v-if="detail && status?.checkedAt" class="git-checked"
      >检查于 {{ new Date(status.checkedAt).toLocaleTimeString() }}</small
    >
  </div>
</template>

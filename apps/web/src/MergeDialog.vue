<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import type { MergePreview, MergeOperation } from "./types.ts";
const props = defineProps<{
  preview: MergePreview;
  taskTitle: string;
  saving: boolean;
  operation?: MergeOperation;
  error: string;
}>();
const emit = defineEmits<{ close: []; confirm: [string]; recheck: [] }>();
const dialog = ref<HTMLDialogElement>();
const message = ref(props.taskTitle);
onMounted(() => dialog.value?.showModal());
const active = computed(
  () =>
    props.operation &&
    ["queued", "preparing", "committing", "committed", "merging"].includes(
      props.operation.phase,
    ),
);
const phases: Record<string, string> = {
  queued: "等待集成",
  preparing: "核对工作区",
  committing: "正在提交任务修改",
  committed: "任务已提交",
  merging: "正在合并主分支",
  succeeded: "合并完成",
  failed: "合并未完成",
  needs_check: "结果需要核查",
};
</script>
<template>
  <dialog
    ref="dialog"
    class="merge-dialog"
    aria-labelledby="merge-title"
    @cancel.prevent="emit('close')"
  >
    <h2 id="merge-title">合并到主分支</h2>
    <p class="merge-task-title">{{ taskTitle }}</p>
    <div class="merge-target">
      <span>任务 {{ preview.taskSHA?.slice(0, 7) || "首个提交" }}</span
      ><span>→</span><strong>{{ preview.mainBranch || "未确定" }}</strong
      ><small>{{ preview.mainSHA?.slice(0, 7) || "尚无提交" }}</small>
    </div>
    <template v-if="!operation"
      ><p>
        {{
          preview.needsCommit
            ? "将先提交以下修改，再合并到主分支。"
            : "将合并任务已有提交。"
        }}
      </p>
      <details v-if="preview.files.length" open>
        <summary>{{ preview.files.length }} 个待提交文件</summary>
        <ul class="merge-files">
          <li v-for="file in preview.files" :key="file.path">
            <code>{{ file.status }}</code
            ><span>{{ file.path }}</span>
          </li>
        </ul>
      </details>
      <label
        ><span>提交说明</span
        ><input v-model="message" maxlength="500" required /></label
      ><small class="muted"
        >提交身份：{{ preview.commitName }} &lt;{{
          preview.commitEmail
        }}&gt;</small
      >
      <p v-if="preview.reason" class="error" role="alert">
        {{ preview.reason }}
      </p>
      <p class="muted">合并后保留任务分支和工作区。代码留在本机。</p></template
    >
    <div v-else class="merge-progress" role="status">
      <strong>{{ phases[operation.phase] || operation.phase }}</strong>
      <p v-if="operation.error" class="error">{{ operation.error }}</p>
      <p v-if="operation.taskAfterSHA" class="muted">
        任务提交 {{ operation.taskAfterSHA.slice(0, 7) }}
      </p>
      <p v-if="operation.phase === 'succeeded'" class="muted">
        主分支已接收任务提交，可继续在原卡片沟通。
      </p>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="dialog-actions">
      <button type="button" @click="emit('close')">
        {{ operation ? "关闭" : "取消" }}</button
      ><button
        v-if="!operation"
        class="primary"
        :disabled="saving || !preview.canMerge || !message.trim()"
        @click="emit('confirm', message)"
      >
        {{
          saving
            ? "正在确认…"
            : preview.needsCommit
              ? "提交并合并"
              : "合并到主分支"
        }}</button
      ><button
        v-if="operation && !active && operation.phase !== 'succeeded'"
        @click="emit('recheck')"
      >
        重新检查
      </button>
    </div>
  </dialog>
</template>

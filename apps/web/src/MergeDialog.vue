<script setup lang="ts">
import { ref, onMounted, computed } from "vue";
import type { MergePreview, MergeReview, MergeOperation } from "./types.ts";
import { mergeChoiceHint, mergeFacts, mergeFinished, mergePhaseLabel } from "./merge.ts";
import { MERGE_MODE_LOCAL, MERGE_MODE_PUSH, mergeModeLabel, type MergeMode } from "./merge-intent.ts";
const props = defineProps<{
  preview: MergePreview;
  review?: MergeReview;
  mode: MergeMode;
  stale?: boolean;
  taskTitle: string;
  saving: boolean;
  operation?: MergeOperation;
  error: string;
}>();
const emit = defineEmits<{ close: []; confirm: [string, "local" | "push"]; recheck: [] }>();
const dialog = ref<HTMLDialogElement>();
const message = ref(props.taskTitle);
onMounted(() => dialog.value?.showModal());
const active = computed(() => !!props.operation && !mergeFinished(props.operation.phase));
const facts = computed(() => mergeFacts(props.operation));
const pushOnly = computed(() => props.mode === "push_only");
const localChoice = computed(() => {
  return props.preview.canMerge && !pushOnly.value;
});
const pushChoice = computed(() => props.review?.canPush === true);
const targetBranch = computed(
  () => props.preview.mainBranch || props.review?.mainBranch || "未确定",
);
const uploads = computed(() => props.review?.uploads ?? []);
const pending = computed(() => props.review?.pending ?? []);
const localFiles = computed(() => props.preview.files ?? []);
function fmt(sha?: string) {
  return sha ? sha.slice(0, 7) : "—";
}
const confirmLabel = computed(() => {
  if (props.saving) return "正在确认…";
  if (pushOnly.value) return mergeModeLabel("push_only");
  if (props.mode === MERGE_MODE_PUSH) return mergeModeLabel(MERGE_MODE_PUSH);
  return props.preview.needsCommit ? "提交并合并到主分支" : mergeModeLabel(MERGE_MODE_LOCAL);
});
</script>
<template>
  <dialog
    ref="dialog"
    class="merge-dialog"
    aria-labelledby="merge-title"
    @cancel.prevent="emit('close')"
  >
    <h2 id="merge-title">{{ pushOnly ? "推送到远程" : mergeModeLabel(mode) }}</h2>
    <p class="merge-task-title">{{ taskTitle }}</p>
    <div class="merge-target">
      <span>任务 {{ fmt(preview.taskSHA) }}</span><span>→</span
      ><strong>{{ targetBranch }}</strong
      ><small>{{ fmt(review?.localMainSHA || preview.mainSHA) }}</small>
    </div>
    <p v-if="review?.remoteName" class="merge-remote">
      远程 <code>{{ review.remoteName }}</code> · {{ review.mainRef || targetBranch }} ·
      已观察 {{ fmt(review.observedRemoteSHA) }}
      <small v-if="review.remoteUrl">{{ review.remoteUrl }}</small>
    </p>

    <template v-if="!operation">
      <p v-if="pushOnly" class="muted">
        主分支已经包含本次成果，本操作只上传已有提交，不会再次提交或合并。
      </p>
      <p v-else class="muted">
        {{
          preview.needsCommit
            ? "本地合并会先提交以下修改，再合并到主分支。"
            : "本地合并会合并任务已有提交。"
        }}
      </p>

      <details v-if="uploads.length" open>
        <summary>{{ uploads.length }} 个待上传提交</summary>
        <ul class="merge-files">
          <li v-for="u in uploads" :key="u.sha">
            <code>{{ fmt(u.sha) }}</code><span>{{ u.subject }}</span>
          </li>
        </ul>
      </details>
      <details v-if="localFiles.length" :open="!pushOnly">
        <summary>{{ localFiles.length }} 个待提交文件</summary>
        <ul class="merge-files">
          <li v-for="file in localFiles" :key="file.path">
            <code>{{ file.status }}</code><span>{{ file.path }}</span>
          </li>
        </ul>
      </details>
      <p v-if="pending.length" class="muted">
        {{ review?.pendingNote || `${pending.length} 个待提交文件将在本次操作中先提交；尚未形成的提交不在上方历史列表内。` }}
      </p>
      <p v-if="review?.safetyProof" class="muted">
        安全审核指纹：{{ review.safetyProof }}
      </p>

      <label v-if="!pushOnly"
        ><span>提交说明</span
        ><input v-model="message" maxlength="500" required /></label
      ><small class="muted"
        >提交身份：{{ preview.commitName }} &lt;{{
          preview.commitEmail
        }}&gt;</small
      >
      <p v-if="review?.pushReason" class="muted" role="note">
        远程：{{ review.pushReason }}
      </p>
      <p v-if="preview.reason" class="error" role="alert">{{ preview.reason }}</p>
      <p class="muted">{{ mergeChoiceHint(mode) }}</p>
      <p v-if="review" class="muted">远程操作会上传审核列出的提交（及明确列出的待提交改动），可能触发远程构建／部署；本页面不据此声称 CI 或部署已通过。</p>
    </template>

    <div v-else class="merge-progress" role="status">
      <strong>{{ mergePhaseLabel(operation.phase) }}</strong>
      <p v-if="operation.error" class="error">{{ operation.error }}</p>
      <ul v-if="facts" class="merge-facts">
        <li :data-state="facts.local.state">本地合并：{{ facts.local.label }}</li>
        <li v-if="facts.remote.state !== 'not_requested'" :data-state="facts.remote.state">
          远程上传：{{ facts.remote.label }}
        </li>
        <li :data-state="facts.ci.state">CI／部署：{{ facts.ci.label }}</li>
      </ul>
      <p v-if="operation.mainAfterSHA" class="muted">已记录本地主分支 {{ fmt(operation.mainAfterSHA) }}</p>
      <p v-if="operation.phase === 'needs_check'" class="muted">仅查询已记录的操作与 SHA，需核查实际本地／远程状态；不会重新提交、合并或推送。</p>
      <p v-if="operation.taskAfterSHA" class="muted">
        任务提交 {{ fmt(operation.taskAfterSHA) }}
      </p>
    </div>
    <p v-if="error" class="error" role="alert">{{ error }}</p>
    <div class="dialog-actions">
      <button type="button" @click="emit('close')">
        {{ active ? "关闭" : "取消" }}
      </button>
      <button
        v-if="!operation && localChoice && !pushOnly"
        class="primary"
        :data-merge-mode="MERGE_MODE_LOCAL"
        :disabled="saving || stale || !message.trim()"
        @click="emit('confirm', message, MERGE_MODE_LOCAL)"
      >
        {{ confirmLabel }}
      </button>
      <button
        v-if="!operation && pushChoice"
        :class="{ primary: pushOnly || !localChoice }"
        :data-merge-mode="MERGE_MODE_PUSH"
        :disabled="saving || stale || (preview.needsCommit && !message.trim())"
        @click="emit('confirm', message, MERGE_MODE_PUSH)"
      >
        {{ pushOnly ? mergeModeLabel("push_only") : mergeModeLabel("push") }}
      </button>
      <button
        v-if="stale || (operation && (!active || operation.phase === 'needs_check') && operation.phase !== 'succeeded')"
        @click="emit('recheck')"
      >
        {{ operation?.phase === 'needs_check' ? '只读核查' : '重新检查' }}
      </button>
    </div>
  </dialog>
</template>

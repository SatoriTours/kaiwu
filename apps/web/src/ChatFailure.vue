<script setup lang="ts">
import { ref, watch } from "vue";
import type { FailureView } from "./failure.ts";
const props = defineProps<{ failure: FailureView; canRetry?: boolean }>();
defineEmits<{ retry: [] }>();
const dialog = ref<HTMLDialogElement>();
const opener = ref<HTMLButtonElement>();
function close() { dialog.value?.close(); opener.value?.focus(); }
watch([() => props.failure.text, () => props.failure.detail, () => props.failure.at],
 () => dialog.value?.close());
function timestamp(value?: string) {
 if (!value) return "";
 const date = new Date(value);
 return Number.isNaN(date.getTime()) ? "" : date.toLocaleString("zh-CN", {hour12:false});
}
</script>
<template>
  <div class="turn-notice" :class="{warning: failure.kind === 'execution' || failure.kind === 'unknown'}" :data-kind="failure.kind" role="status" aria-label="本轮状态">
    <span class="notice-icon" aria-hidden="true">{{ failure.kind === 'session' ? 'Ⅱ' : '!' }}</span>
    <span class="notice-text">{{ failure.text }}</span>
    <button v-if="failure.retry && canRetry" type="button" @click="$emit('retry')">重试</button>
    <button v-if="failure.detail" ref="opener" type="button" @click="dialog?.showModal()">详情</button>
    <dialog v-if="failure.detail" ref="dialog" class="notice-dialog" aria-label="本轮详情" @click="event => { if (event.target === dialog) close(); }">
      <header><h3>本轮详情</h3><button type="button" aria-label="关闭详情" @click="close">×</button></header>
      <small v-if="timestamp(failure.at)">{{ timestamp(failure.at) }}</small>
      <p>{{ failure.text }}</p>
      <pre>{{ failure.detail }}</pre>
    </dialog>
  </div>
</template>
<style scoped>
.turn-notice{display:flex;align-items:baseline;flex-wrap:wrap;gap:4px 8px;margin-top:12px;font-size:12px;line-height:1.7;color:var(--muted);overflow-wrap:anywhere}
.turn-notice.warning{color:var(--theme-warning,#947d50)}
.notice-icon{font-size:11px;flex:0 0 auto}
.notice-text{min-width:0}
.turn-notice>button{font:inherit;font-size:12px;color:var(--muted);border:0;background:transparent;padding:3px 4px;min-height:28px;border-radius:4px}
.turn-notice>button:hover{color:var(--ink);background:var(--theme-hover,#edf2e9)}
.notice-dialog{margin:auto;width:min(440px,calc(100vw - 32px));max-height:75dvh;overflow:auto;padding:18px;border:1px solid var(--border);border-radius:12px;background:var(--theme-surface,#fff);color:var(--ink);box-shadow:0 12px 48px #0003}
.notice-dialog::backdrop{background:var(--theme-backdrop,#17203255)}
.notice-dialog header{display:flex;align-items:center;justify-content:space-between;gap:16px}
.notice-dialog h3{font-size:14px;font-weight:500;margin:0}
.notice-dialog header button{border:0;background:transparent;color:var(--muted);font-size:22px;padding:0 5px}
.notice-dialog small{display:block;margin-top:10px;font-size:11px;color:var(--muted)}
.notice-dialog p{font-size:12px;margin:16px 0 10px}
.notice-dialog pre{margin:0;padding:10px;background:var(--theme-canvas,#f6f8f6);border-radius:6px;font:12px/1.8 inherit;white-space:pre-wrap;overflow-wrap:anywhere;color:var(--muted)}
@media(max-width:600px){.turn-notice>button{min-height:32px}}
</style>

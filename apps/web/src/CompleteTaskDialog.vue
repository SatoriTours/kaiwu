<script setup lang="ts">
import { ref, onMounted, onBeforeUnmount } from 'vue';
import type { CompletionIntent } from './task-completion.ts';
defineProps<{ intent:Readonly<CompletionIntent>; busy:boolean; eligible:boolean; error:string }>();
const emit=defineEmits<{close:[];confirm:[]}>();
const dialog=ref<HTMLDialogElement>();onMounted(()=>dialog.value?.showModal());onBeforeUnmount(()=>dialog.value?.close());
</script>
<template>
 <dialog ref="dialog" class="complete-task-dialog" aria-labelledby="complete-task-title" @cancel.prevent="!busy&&emit('close')">
  <h2 id="complete-task-title">标记任务完成</h2>
  <p class="complete-task-name">{{intent.title}}</p>
  <p>仅更新任务状态，不会运行测试、提交、合并、推送或部署代码。你仍然可以继续聊天。</p>
  <p v-if="intent.missingChecks" class="complete-task-warning">{{intent.missingChecks}} 项检查缺少本轮成功执行依据，请先自行核实。这不是验证通过。</p>
  <p v-if="!eligible" role="status">任务状态已变化，请取消后重新查看最新结果。</p>
  <p v-if="error" class="form-error" role="alert">{{error}}</p>
  <div class="dialog-actions"><button :disabled="busy" autofocus @click="emit('close')">取消</button><button class="primary" :disabled="busy||!eligible" @click="emit('confirm')">{{busy?'正在标记…':'标记完成'}}</button></div>
 </dialog>
</template>
<style scoped>
.complete-task-dialog{width:min(400px,calc(100vw - 32px))}.complete-task-dialog p{font-size:12px;line-height:1.9;margin:12px 0;color:var(--muted);overflow-wrap:anywhere}.complete-task-dialog .complete-task-name{color:var(--ink);font-size:13px}.complete-task-dialog .complete-task-warning{color:var(--theme-warning,#947d50)}
</style>

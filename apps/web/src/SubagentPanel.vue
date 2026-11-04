<script setup lang="ts">
import { computed, ref, watch, onMounted, onBeforeUnmount, nextTick } from 'vue';
import { api } from './api.ts';
import type { SubagentSummary, SubagentEvent, SubagentDetailPage } from './types.ts';
import { createSubagentQueryState, mergeSubagentEvents, formatSubagentStatusLabel, shouldFetchSubagentIncremental } from './subagents.ts';
const props = defineProps<{ projectId: string; conversationId: string; runs: SubagentSummary[]; online: boolean }>();
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLDialogElement>();
const selected = ref(''), events = ref<SubagentEvent[]>([]), request = ref(''), error = ref('');
const loaded = ref<SubagentSummary>(), busy = ref(false), after = ref(0), hasMore = ref(false);
const current = computed(() => props.runs.find(r => r.id === selected.value));
const query = createSubagentQueryState();
const trigger = document.activeElement as HTMLElement | null;
const listButtons = new Map<string, HTMLElement>();
async function load() {
  if (!selected.value || !props.online || busy.value) return;
  const runId = selected.value, p = props.projectId, c = props.conversationId;
  const token = query.nextRequest(p, c, runId);
  busy.value = true; error.value = '';
  try {
    const page = await api<SubagentDetailPage>('subagents/detail', { projectId: p, conversationId: c, runId, after: after.value, limit: 100 });
    if (!query.isCurrent(token, props.projectId, props.conversationId, selected.value)) return;
    if (page.run.id !== runId || page.run.projectId !== p || page.run.conversationId !== c) throw new Error('执行记录归属不匹配');
    events.value = mergeSubagentEvents(events.value, page.events ?? []);
    request.value = page.request; loaded.value = page.run; after.value = page.next; hasMore.value = page.hasMore;
  } catch (e) {
    if (query.isCurrent(token, props.projectId, props.conversationId, selected.value)) error.value = e instanceof Error ? e.message : '读取失败';
  } finally {
    if (query.isCurrent(token, props.projectId, props.conversationId, selected.value)) {
      busy.value = false;
      if (!error.value && !hasMore.value && current.value && shouldFetchSubagentIncremental(loaded.value?.version, current.value.version)) void load();
    }
  }
}
function reset() { query.reset(); selected.value = ''; events.value = []; request.value = ''; error.value = ''; loaded.value = undefined; after.value = 0; hasMore.value = false; busy.value = false; }
async function choose(run: SubagentSummary) { reset(); selected.value = run.id; await nextTick(); dialog.value?.querySelector<HTMLButtonElement>('[data-back]')?.focus(); void load(); }
async function back() { const id = selected.value; reset(); await nextTick(); listButtons.get(id)?.focus(); }
watch(() => [props.projectId, props.conversationId], reset);
watch(() => [current.value?.version, props.online], () => {
  if (!props.online) { query.reset(); busy.value = false; }
  else if (current.value && !hasMore.value && shouldFetchSubagentIncremental(loaded.value?.version, current.value.version)) void load();
});
function trapTab(e: KeyboardEvent) {
  if (e.key !== 'Tab') return;
  const controls = [...dialog.value!.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')].filter(el => el.getClientRects().length);
  const first = controls[0], last = controls.at(-1);
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus(); }
  else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus(); }
}
onMounted(() => dialog.value?.showModal());
onBeforeUnmount(() => { query.reset(); dialog.value?.close(); if (trigger?.isConnected) trigger.focus(); });
</script>
<template>
  <dialog ref="dialog" class="subagent-panel" aria-label="子 Agent 执行记录" @cancel.stop.prevent="emit('close')" @keydown="trapTab">
    <header><button v-if="selected" type="button" data-back @click="back">← 返回列表</button><h2>{{ selected ? '执行详情' : '子 Agent' }}</h2><button type="button" aria-label="关闭子 Agent 面板" @click="emit('close')">×</button></header>
    <p v-if="!online" class="subagent-notice" role="status">连接已断开，记录可能不是最新；重连后更新。</p>
    <div class="subagent-scroll">
      <template v-if="!selected">
        <p v-if="!runs.length">暂无子 Agent 记录</p>
        <button v-for="run in runs" :key="run.id" :ref="el => { if(el) listButtons.set(run.id, el as HTMLElement); else listButtons.delete(run.id) }" type="button" class="subagent-row" @click="choose(run)">
          <span><strong>{{ run.title }}</strong><span>{{ formatSubagentStatusLabel(run.status) }}</span></span>
          <span>{{ run.taskSummary }}</span><small>{{ new Date(run.startedAt).toLocaleString() }} · {{ run.profile.model }}</small>
        </button>
      </template>
      <template v-else>
        <h3>{{ (current ?? loaded)?.title }}</h3>
        <p>{{ (current ?? loaded) ? formatSubagentStatusLabel((current ?? loaded)!.status) : '记录待核查' }} · 运行结束不代表验收通过</p>
        <p v-if="(current ?? loaded)?.truncated" class="subagent-notice">记录已达到保存上限，部分内容已截断。</p>
        <h4>任务要求</h4><pre>{{ request || (busy ? '正在读取…' : '暂无已读取的任务内容') }}</pre>
        <h4>执行记录</h4><p v-if="!events.length && !busy">暂无可见执行内容</p>
        <article v-for="event in events" :key="event.sequence" class="subagent-event">
          <small>{{ event.sequence }} · {{ new Date(event.at).toLocaleTimeString() }} · {{ event.toolName || (event.kind === 'text' ? '回复' : event.kind === 'status' ? '状态' : '工具') }}<span v-if="event.failed"> · 执行失败</span></small>
          <p v-if="event.status">{{ formatSubagentStatusLabel(event.status) }}</p>
          <pre v-if="event.text">{{ event.text }}</pre>
          <details v-if="event.input"><summary>工具输入</summary><pre>{{ event.input }}</pre></details>
          <pre v-if="event.output">{{ event.output }}</pre>
        </article>
      </template>
    </div>
    <footer v-if="selected"><p v-if="error" role="alert">{{ error }} · 已读取内容仍保留</p><span v-if="busy" role="status">正在读取…</span><button v-if="error || hasMore" type="button" :disabled="!online || busy" @click="load">{{ error ? '重试' : '加载更多' }}</button></footer>
  </dialog>
</template>
<style src="./subagents.css"></style>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue';
import type { Project } from './types.ts';

const props = defineProps<{ projects: Project[]; currentId: string }>();
const emit = defineEmits<{ choose: [id: string]; add: []; git: []; delivery: []; refresh: [] }>();
const root = ref<HTMLElement>();
const trigger = ref<HTMLButtonElement>();
const menu = ref<HTMLElement>();
const expanded = ref(false);
const menuId = useId();
const current = computed(() => props.projects.find(p => p.id === props.currentId));
const items = () => Array.from(menu.value?.querySelectorAll<HTMLButtonElement>('[role^="menuitem"]') ?? []);

function close(restoreFocus = false) {
  expanded.value = false;
  if (restoreFocus) trigger.value?.focus();
}
async function show(last = false) {
  expanded.value = true;
  await nextTick();
  if (!expanded.value) return;
  const options = items();
  const selected = options.find(item => item.getAttribute('aria-checked') === 'true');
  (last ? options.at(-1) : selected ?? options[0])?.focus();
}
function toggle() {
  if (expanded.value) close(true);
  else void show();
}
function choose(id: string) {
  close(true);
  // Clicking the current project must not discard the selected task or draft.
  if (id !== props.currentId) emit('choose', id);
}
function action(kind: 'add' | 'git' | 'delivery' | 'refresh') {
  close(true);
  if (kind === 'add') emit('add');
  else if (kind === 'git') emit('git');
  else if (kind === 'delivery') emit('delivery');
  else emit('refresh');
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && expanded.value) {
    event.preventDefault();
    event.stopPropagation();
    close(true);
    return;
  }
  if (event.key === 'Tab' && expanded.value) {
    // Return to the trigger before normal Tab moves to the global controls.
    if (event.shiftKey) event.preventDefault();
    close(true);
    return;
  }
  if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
  event.preventDefault();
  if (!expanded.value) {
    void show(event.key === 'ArrowUp' || event.key === 'End');
    return;
  }
  const options = items();
  const index = options.indexOf(document.activeElement as HTMLButtonElement);
  const target = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1
    : (index + (event.key === 'ArrowDown' ? 1 : -1) + options.length) % options.length;
  options[target]?.focus();
}
function outside(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) close();
}
function focusout(event: FocusEvent) {
  if (!root.value?.contains(event.relatedTarget as Node | null)) close();
}
watch(() => props.currentId, () => close());
onMounted(() => document.addEventListener('pointerdown', outside));
onBeforeUnmount(() => document.removeEventListener('pointerdown', outside));
</script>

<template>
  <div ref="root" class="project-menu" @keydown="keydown" @focusout="focusout">
    <button ref="trigger" class="project-trigger" type="button" aria-label="当前项目"
      aria-haspopup="menu" :aria-expanded="expanded" :aria-controls="expanded ? menuId : undefined"
      :data-project-id="currentId" :title="current?.name ?? '选择或添加项目'" @click="toggle">
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7V5a1 1 0 0 1 1-1h5l3 3h8a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1Z" /></svg>
      <span class="project-trigger-name">{{ current?.name ?? '选择项目' }}</span>
      <svg class="project-chevron" :class="{ expanded }" viewBox="0 0 24 24" aria-hidden="true"><path d="m7 10 5 5 5-5" /></svg>
    </button>
    <div v-if="expanded" :id="menuId" ref="menu" class="project-dropdown" role="menu" aria-label="项目菜单">
      <div class="project-menu-caption" role="presentation">切换项目</div>
      <div class="project-options" role="group" aria-label="项目列表">
        <button v-for="p in projects" :key="p.id" type="button" role="menuitemradio" tabindex="-1"
          :aria-checked="p.id === currentId" :aria-label="p.name" :data-project-id="p.id" @click="choose(p.id)">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 7V5a1 1 0 0 1 1-1h5l3 3h8a1 1 0 0 1 1 1v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1Z" /></svg>
          <span>{{ p.name }}</span>
          <svg v-if="p.id === currentId" class="project-check" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>
        </button>
        <p v-if="!projects.length" class="project-menu-empty">还没有项目，添加一个开始使用</p>
      </div>
      <template v-if="current">
        <div class="project-menu-divider" role="separator"></div>
        <button type="button" role="menuitem" tabindex="-1" @click="action('git')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m10 3-.6 2.2-2 .9-2-.6-2 3.5L5 10.6v2.8l-1.6 1.6 2 3.5 2-.6 2 .9L10 21h4l.6-2.2 2-.9 2 .6 2-3.5-1.6-1.6v-2.8L20.6 9l-2-3.5-2 .6-2-.9L14 3Z" /><circle cx="12" cy="12" r="3" /></svg>
          <span>Git 设置</span>
        </button>
        <button type="button" role="menuitem" tabindex="-1" @click="action('delivery')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" /></svg>
          <span>交付设置</span>
        </button>
        <button type="button" role="menuitem" tabindex="-1" @click="action('refresh')">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 7v5h-5M4 17v-5h5M6.1 6.1A8 8 0 0 1 20 12M4 12a8 8 0 0 0 13.9 5.9" /></svg>
          <span>刷新 Git 状态</span>
        </button>
      </template>
      <div class="project-menu-divider" role="separator"></div>
      <button type="button" role="menuitem" tabindex="-1" @click="action('add')">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
        <span>添加项目</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.project-menu { position: relative; min-width: 0; max-width: 240px; }
.project-menu svg { width: 16px; height: 16px; flex: none; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
.project-trigger { display: flex; align-items: center; gap: 8px; width: 100%; min-height: 30px; padding: 4px 8px; border: 1px solid transparent; border-radius: 7px; background: transparent; font-size: 12px; text-align: left; }
.project-trigger:hover, .project-trigger[aria-expanded=true] { background: #f0f3f0; }
.project-trigger-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 500; }
.project-trigger .project-chevron { width: 13px; height: 13px; color: #8a938e; margin-left: auto; transition: transform .15s; }
.project-chevron.expanded { transform: rotate(180deg); }
.project-dropdown { position: absolute; z-index: 60; top: calc(100% + 9px); left: 0; width: min(290px, calc(100vw - 64px)); max-height: calc(100dvh - 72px); overflow: auto; padding: 6px; background: #fff; border: 1px solid #e4e9e5; border-radius: 10px; box-shadow: 0 10px 32px #25392c12, 0 2px 6px #25392c08; }
.project-menu-caption { padding: 6px 9px 7px; color: #8a928d; font-size: 11px; }
.project-options { max-height: min(280px, 40dvh); overflow: auto; scrollbar-width: thin; }
.project-dropdown button { display: flex; align-items: center; gap: 9px; width: 100%; min-height: 36px; padding: 8px 9px; border: 0; border-radius: 6px; background: transparent; text-align: left; font-size: 12px; line-height: 1.5; }
.project-dropdown button > span { min-width: 0; overflow-wrap: anywhere; }
.project-dropdown button > svg { color: #7e8b82; }
.project-dropdown button[aria-checked=true] { color: #3e604b; background: #edf3ee; }
.project-dropdown button:hover { background: #f2f5f2; }
.project-dropdown button:focus-visible { outline: 2px solid #849e90; outline-offset: -2px; background: #f2f5f2; }
.project-dropdown button > .project-check { margin-left: auto; color: #507363; width: 15px; }
.project-menu-divider { height: 1px; margin: 5px 3px; background: #edf0ed; }
.project-menu-empty { padding: 4px 9px 8px; margin: 0; color: var(--muted); font-size: 12px; }
@media (max-width: 599px) {
  .project-menu { flex: 1; }
  .project-trigger { min-height: 32px; padding: 4px 6px; gap: 6px; }
  .project-dropdown button { min-height: 42px; }
}
@media (prefers-reduced-motion: reduce) { .project-chevron { transition: none; } }
</style>

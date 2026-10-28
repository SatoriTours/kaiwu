<script setup lang="ts">
import { ref, watch } from 'vue';
const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLDialogElement>();
watch([() => props.open, dialog], ([open, el]) => {
  if (!el) return;
  if (open && !el.open) el.showModal();
  else if (!open && el.open) el.close();
}, { flush: 'post' });
function trapTab(event: KeyboardEvent) {
  if (event.key !== 'Tab') return;
  const controls = [...dialog.value!.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), a[href], [tabindex="0"]')]
    .filter(el => el.getClientRects().length && getComputedStyle(el).visibility !== 'hidden');
  const first = controls[0], last = controls.at(-1);
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
}
let pressedBackdrop = false;
function outside(event: MouseEvent) {
  const box = dialog.value!.getBoundingClientRect();
  return event.clientX < box.left || event.clientX >= box.right || event.clientY < box.top || event.clientY >= box.bottom;
}
function dismiss(event: MouseEvent) {
  if (event.target === dialog.value && pressedBackdrop && outside(event)) emit('close');
  pressedBackdrop = false;
}
</script>
<template>
  <!-- Keep the conversation mounted: hiding a view must never end a CLI session. -->
  <dialog ref="dialog" class="chat-drawer" aria-label="聊天面板"
    @cancel.stop.prevent="emit('close')" @keydown="trapTab"
    @pointerdown="pressedBackdrop = outside($event)" @click="dismiss">
    <slot />
  </dialog>
</template>
<style>
.chat-drawer {
  position: fixed;
  inset: 0 0 0 auto;
  margin: 0;
  padding: 0;
  width: min(720px, 100vw);
  height: 100dvh;
  max-width: 100vw;
  max-height: 100dvh;
  border: 0;
  border-left: 1px solid var(--border);
  border-radius: 0;
  background: var(--theme-surface, #fff);
  color: var(--ink);
  box-shadow: -12px 0 40px var(--theme-shadow, #17203220);
  overflow: hidden;
  transform: translateX(100%);
  transition: transform .22s ease, display .22s allow-discrete, overlay .22s allow-discrete;
}
.chat-drawer[open] { display: flex; transform: translateX(0); }
.chat-drawer::backdrop { background: var(--theme-backdrop, #101a164d); }
@starting-style { .chat-drawer[open] { transform: translateX(100%); } }
.chat-drawer .chat { display: flex; flex: 1; width: 100%; height: 100%; border: 0; }
.chat-drawer .chat-close { flex: 0 0 30px; width: 30px; height: 30px; padding: 0; border: 0; background: transparent; color: var(--muted); font-size: 20px; }
.chat-drawer .chat-close:hover { background: var(--theme-inset, #f2f4f2); }
.chat-drawer .chat-head > .row { min-width: 0; }
@media(max-width:600px) {
  .chat-drawer { width: 100vw; border: 0; }
  .chat-drawer .chat-actions { order: 3; width: 100%; justify-content: flex-end; }
}
@media(prefers-reduced-motion:reduce) { .chat-drawer { transition: none; } }
</style>

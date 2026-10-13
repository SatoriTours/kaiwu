<script setup lang="ts">
import { computed, ref, watch, onBeforeUnmount } from "vue";
import { renderContent, type ContentFormat } from "./markdown.ts";
import { copyText } from "./clipboard.ts";
import "./markdown.css";
const props = defineProps<{ text: string; format?: ContentFormat }>();
const html = computed(() => renderContent(props.text, props.format));
const notice = ref("");
const failed = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;
let copiedButton: HTMLButtonElement | undefined;
let disposed = false;
function reset() {
  clearTimeout(timer);
  if (copiedButton?.isConnected) copiedButton.textContent = "复制";
  copiedButton = undefined;
  notice.value = "";
  failed.value = false;
}
async function copy(event: MouseEvent) {
  const target = event.target as Element;
  const button = target.closest<HTMLButtonElement>("button[data-copy-code]");
  if (!button || !(event.currentTarget as Element).contains(button)) return;
  const code = button.closest(".code-block")?.querySelector("pre code");
  if (!code) return;
  reset();
  const source = props.text;
  try {
    await copyText(code.textContent || "");
    if (disposed || source !== props.text) return;
    if (button.isConnected) {
      copiedButton = button;
      button.textContent = "已复制";
    }
    notice.value = "已复制代码";
  } catch {
    if (disposed || source !== props.text) return;
    failed.value = true;
    notice.value = "复制失败，请手动选择代码";
  }
  timer = setTimeout(reset, 2000);
}
watch(() => props.text, reset);
onBeforeUnmount(() => {
  disposed = true;
  reset();
});
</script>
<template>
  <div class="markdown-content">
    <div class="markdown-body" v-html="html" @click="copy" />
    <span role="status" :class="failed ? 'copy-error' : 'visually-hidden'">{{
      notice
    }}</span>
  </div>
</template>

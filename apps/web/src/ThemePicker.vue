<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import CustomSelect from './CustomSelect.vue';

type Preference = 'system' | 'light' | 'dark';
const options = [
  { value: 'system' as const, label: '跟随系统' },
  { value: 'light' as const, label: '浅色' },
  { value: 'dark' as const, label: '深色' },
];
function normalize(value: string | null | undefined): Preference {
  return value === 'light' || value === 'dark' ? value : 'system';
}
const preference = ref<Preference>(normalize(document.documentElement.dataset.theme));
function apply(value: Preference) {
  preference.value = value;
  document.documentElement.dataset.theme = value;
}
function choose(value: Preference) {
  apply(value);
  try {
    if (value === 'system') localStorage.removeItem('kaiwu.theme');
    else localStorage.setItem('kaiwu.theme', value);
  } catch { /* This tab can still switch without persistent storage. */ }
}
function stored(event: StorageEvent) {
  if (event.key === 'kaiwu.theme' || event.key === null) apply(normalize(event.newValue));
}
onMounted(() => window.addEventListener('storage', stored));
onBeforeUnmount(() => window.removeEventListener('storage', stored));
</script>

<template>
  <div class="theme-picker">
    <CustomSelect :model-value="preference" :options="options" aria-label="外观主题"
      :title="'外观：' + options.find(o => o.value === preference)?.label" compact @update:model-value="choose" />
    <svg viewBox="0 0 24 24" aria-hidden="true" class="theme-icon">
      <template v-if="preference === 'system'">
        <rect x="3" y="4" width="18" height="13" rx="2" /><path d="M8 21h8m-4-4v4" />
      </template>
      <template v-else-if="preference === 'light'">
        <circle cx="12" cy="12" r="4" /><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5" />
      </template>
      <path v-else d="M20.8 13A9 9 0 0 1 11 3.2 9 9 0 1 0 20.8 13Z" />
    </svg>
  </div>
</template>

<style scoped>
.theme-picker { position: relative; display: flex; align-items: center; width: 30px; flex: 0 0 30px; color: var(--muted); }
.theme-picker :deep(.custom-select-trigger) { width: 30px; height: 30px; padding: 0; border-color: transparent; background: transparent; }
.theme-picker :deep(.custom-select-label), .theme-picker :deep(.custom-select-chevron) { display: none; }
.theme-picker :deep(.custom-select-dropdown) { left: auto; right: 0; width: 150px; min-width: 150px; }
.theme-icon { position: absolute; inset: 7px; width: 16px; height: 16px; pointer-events: none; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
</style>

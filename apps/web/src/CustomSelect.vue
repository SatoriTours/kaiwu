<script setup lang="ts" generic="T extends string | number">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from "vue";
import {
  calculateNextIndex,
  getSelectAriaAttributes,
  formatOptionLabel,
  type SelectOption,
} from "./select-logic.ts";

const props = withDefaults(
  defineProps<{
    modelValue?: T;
    value?: T;
    options: SelectOption<T>[];
    placeholder?: string;
    disabled?: boolean;
    required?: boolean;
    name?: string;
    id?: string;
    ariaLabel?: string;
    title?: string;
    compact?: boolean;
  }>(),
  {
    placeholder: "请选择",
    disabled: false,
    required: false,
    compact: false,
  }
);

const emit = defineEmits<{
  "update:modelValue": [val: T];
  change: [val: T];
}>();

const root = ref<HTMLElement>();
const trigger = ref<HTMLButtonElement>();
const listbox = ref<HTMLElement>();
const expanded = ref(false);
const listboxId = useId();
const activeIndex = ref(-1);

const currentValue = computed<T | undefined>(() => {
  return props.modelValue !== undefined ? props.modelValue : props.value;
});

const currentOption = computed(() => {
  return props.options.find((opt) => opt.value === currentValue.value);
});

const displayLabel = computed(() => {
  if (currentOption.value) {
    return formatOptionLabel(currentOption.value);
  }
  return props.placeholder;
});

const ariaAttrs = computed(() =>
  getSelectAriaAttributes({
    expanded: expanded.value,
    listboxId,
    disabled: props.disabled,
    required: props.required,
  })
);

function getOptionElements(): HTMLButtonElement[] {
  return Array.from(
    listbox.value?.querySelectorAll<HTMLButtonElement>('[role="option"]') ?? []
  );
}

function close(restoreFocus = false) {
  if (!expanded.value) return;
  expanded.value = false;
  activeIndex.value = -1;
  if (restoreFocus) {
    trigger.value?.focus();
  }
}

async function show(selectLast = false) {
  if (props.disabled) return;
  expanded.value = true;
  await nextTick();
  if (!expanded.value) return;

  const enabledOptions = props.options
    .map((opt, i) => (opt.disabled ? -1 : i))
    .filter((i) => i !== -1);
  if (enabledOptions.length === 0) return;

  let targetIndex = props.options.findIndex(
    (opt) => opt.value === currentValue.value && !opt.disabled
  );
  if (targetIndex === -1) {
    targetIndex = selectLast
      ? enabledOptions[enabledOptions.length - 1]
      : enabledOptions[0];
  }
  activeIndex.value = targetIndex;

  const optEls = getOptionElements();
  optEls[targetIndex]?.focus();
}

function toggle() {
  if (expanded.value) close(true);
  else void show();
}

function select(option: SelectOption<T>) {
  if (option.disabled || props.disabled) return;
  emit("update:modelValue", option.value);
  emit("change", option.value);
  close(true);
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (props.disabled) return;
  if (event.key === "Escape" && expanded.value) {
    event.preventDefault();
    event.stopPropagation();
    close(true);
    return;
  }
  if (event.key === "ArrowDown" || event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    event.stopPropagation();
    void show(false);
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    event.stopPropagation();
    void show(true);
  }
}

function onListboxKeydown(event: KeyboardEvent) {
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation(); // 关键：阻止冒泡，不误关外层 dialog
    close(true);
    return;
  }
  if (event.key === "Tab") {
    close(false);
    return;
  }
  if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
    event.preventDefault();
    event.stopPropagation();
    const nextIdx = calculateNextIndex(
      activeIndex.value,
      event.key as "ArrowDown" | "ArrowUp" | "Home" | "End",
      props.options
    );
    if (nextIdx !== -1) {
      activeIndex.value = nextIdx;
      const optEls = getOptionElements();
      optEls[nextIdx]?.focus();
    }
    return;
  }
  if (event.key === "Enter" || event.key === " ") {
    event.preventDefault();
    event.stopPropagation();
    if (activeIndex.value >= 0 && activeIndex.value < props.options.length) {
      select(props.options[activeIndex.value]);
    }
  }
}

function outside(event: PointerEvent) {
  if (!root.value?.contains(event.target as Node)) {
    close(false);
  }
}

function focusout(event: FocusEvent) {
  if (!root.value?.contains(event.relatedTarget as Node | null)) {
    close(false);
  }
}

watch(
  () => props.disabled,
  (val) => {
    if (val && expanded.value) close(false);
  }
);

onMounted(() => document.addEventListener("pointerdown", outside));
onBeforeUnmount(() => document.removeEventListener("pointerdown", outside));
</script>

<template>
  <div
    ref="root"
    class="custom-select"
    :class="{ 'custom-select-compact': compact, disabled }"
    @focusout="focusout"
  >
    <!-- Hidden native input to support native form submission and validation if name/required is present -->
    <input
      v-if="name"
      type="hidden"
      :name="name"
      :value="currentValue ?? ''"
      :required="required"
    />

    <button
      :id="id"
      ref="trigger"
      type="button"
      class="custom-select-trigger"
      :aria-label="ariaLabel"
      :title="title || displayLabel"
      :disabled="disabled"
      v-bind="ariaAttrs"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <span
        class="custom-select-label"
        :class="{ 'custom-select-placeholder': !currentOption && placeholder }"
      >
        {{ displayLabel }}
      </span>
      <svg
        class="custom-select-chevron"
        :class="{ expanded }"
        viewBox="0 0 24 24"
        aria-hidden="true"
      >
        <path d="m7 10 5 5 5-5" />
      </svg>
    </button>

    <div
      v-if="expanded"
      :id="listboxId"
      ref="listbox"
      class="custom-select-dropdown"
      role="listbox"
      tabindex="-1"
      :aria-label="ariaLabel || '选项列表'"
      @keydown="onListboxKeydown"
    >
      <div class="custom-select-options" role="group">
        <button
          v-for="(option, index) in options"
          :key="String(option.value)"
          type="button"
          role="option"
          tabindex="-1"
          class="custom-select-option"
          :data-value="String(option.value)"
          :class="{
            active: index === activeIndex,
            selected: option.value === currentValue,
          }"
          :aria-selected="option.value === currentValue"
          :aria-disabled="option.disabled || undefined"
          :disabled="option.disabled"
          @click="select(option)"
          @mouseenter="activeIndex = index"
        >
          <span class="custom-select-option-text">{{
            formatOptionLabel(option)
          }}</span>
          <svg
            v-if="option.value === currentValue"
            class="custom-select-check"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path d="m5 12 4 4L19 6" />
          </svg>
        </button>
        <p v-if="!options.length" class="custom-select-empty">无可用选项</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.custom-select {
  position: relative;
  display: inline-block;
  width: 100%;
  font-size: 12px;
}
.custom-select.disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.custom-select-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  min-height: 32px;
  padding: 6px 10px;
  border: 1px solid var(--border, #e4e9e5);
  border-radius: 7px;
  background: #ffffff;
  color: var(--ink, #303635);
  font-size: 12px;
  text-align: left;
  cursor: pointer;
  transition: border-color 0.15s, background-color 0.15s;
}

.custom-select-trigger:hover:not(:disabled) {
  background: #f0f3f0;
  border-color: #d1ded5;
}

.custom-select-trigger[aria-expanded="true"] {
  background: #f0f3f0;
  border-color: #849e90;
}

.custom-select-trigger:focus-visible {
  outline: 2px solid #849e90;
  outline-offset: 1px;
}

.custom-select-trigger:disabled {
  background: #f7f9f7;
  border-color: #e8ede9;
  cursor: not-allowed;
}

.custom-select-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.custom-select-placeholder {
  color: var(--muted, #8a938e);
}

.custom-select-chevron {
  width: 14px;
  height: 14px;
  flex: none;
  fill: none;
  stroke: #8a938e;
  stroke-width: 1.6;
  stroke-linecap: round;
  stroke-linejoin: round;
  transition: transform 0.15s ease;
}

.custom-select-chevron.expanded {
  transform: rotate(180deg);
}

.custom-select-dropdown {
  position: absolute;
  z-index: 100;
  top: calc(100% + 4px);
  left: 0;
  width: 100%;
  min-width: 160px;
  max-height: 240px;
  overflow: auto;
  padding: 5px;
  background: #ffffff;
  border: 1px solid #e4e9e5;
  border-radius: 9px;
  box-shadow: 0 8px 24px rgba(37, 57, 44, 0.1), 0 2px 6px rgba(37, 57, 44, 0.05);
  scrollbar-width: thin;
}

.custom-select-options {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.custom-select-option {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  width: 100%;
  min-height: 32px;
  padding: 6px 9px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ink, #303635);
  text-align: left;
  font-size: 12px;
  line-height: 1.4;
  cursor: pointer;
}

.custom-select-option:hover:not(:disabled),
.custom-select-option.active:not(:disabled) {
  background: #f2f5f2;
}

.custom-select-option.selected {
  color: #3e604b;
  background: #edf3ee;
  font-weight: 500;
}

.custom-select-option:focus,
.custom-select-option:focus-visible {
  outline: 2px solid #849e90;
  outline-offset: -2px;
  background: #f2f5f2;
}

.custom-select-option:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.custom-select-option-text {
  flex: 1;
  min-width: 0;
  overflow-wrap: anywhere;
}

.custom-select-check {
  width: 14px;
  height: 14px;
  flex: none;
  fill: none;
  stroke: #507363;
  stroke-width: 2;
  stroke-linecap: round;
  stroke-linejoin: round;
}

.custom-select-empty {
  padding: 8px 10px;
  color: var(--muted, #8a938e);
  font-size: 12px;
  text-align: center;
}

/* Compact mode (for top bar or inline cards) */
.custom-select-compact .custom-select-trigger {
  min-height: 26px;
  padding: 3px 6px;
  font-size: 11px;
}

.custom-select-compact .custom-select-option {
  min-height: 28px;
  padding: 4px 7px;
  font-size: 11px;
}

@media (max-width: 599px) {
  .custom-select-trigger {
    min-height: 34px;
  }
  .custom-select-option {
    min-height: 36px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .custom-select-chevron {
    transition: none;
  }
}
</style>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from "vue";
import type {
  InteractionRequest,
  InteractionField,
} from "./types.ts";
import {
  validateInteractionAnswers,
  validateTextFallback,
  loadInteractionDraft,
  saveInteractionDraft,
  normalizeSubmissionAnswers,
  type InteractionAnswers,
  type InteractionOthers,
  type InteractionDraft,
} from "./interaction.ts";

const props = defineProps<{
  requests: InteractionRequest[];
  conversationId: string;
  pending: boolean;
  online: boolean;
}>();

const emit = defineEmits<{
  submit: [
    payload: {
      conversationId: string;
      requestId: string;
      requestVersion: number;
      answerMode: "form" | "text";
      answers?: InteractionAnswers;
      others?: InteractionOthers;
      comment?: string;
      text?: string;
    },
  ];
  cancel: [payload: { conversationId: string; requestId: string; requestVersion: number }];
  retry: [payload: { conversationId: string; requestId: string; requestVersion: number }];
  collapseChange: [collapsed: boolean];
}>();

const currentIndex = ref(0);
const collapsed = ref(false);
// User manual collapse memory keyed by conversation:request:version
const collapsedByUserKeys = ref<Set<string>>(new Set());

// DOM refs for accessible focus management
const cardSectionRef = ref<HTMLElement | null>(null);
const expandBtnRef = ref<HTMLButtonElement | null>(null);

// Keep index within bounds
watch(
  () => props.requests.length,
  (len) => {
    if (currentIndex.value >= len) {
      currentIndex.value = Math.max(0, len - 1);
    }
  },
);

const currentRequest = computed<InteractionRequest | undefined>(() => {
  return props.requests[currentIndex.value];
});

// Draft state
const mode = ref<"form" | "text">("form");
const answers = ref<InteractionAnswers>({});
const others = ref<InteractionOthers>({});
// Independent local UI flag for "other" option checkbox/radio selection
const otherFlags = ref<Record<string, boolean>>({});
const comment = ref("");
const fallbackText = ref("");
const errors = ref<Record<string, string>>({});

// When request changes, initialize draft
function initDraft(req?: InteractionRequest) {
  errors.value = {};
  if (!req) {
    mode.value = "form";
    answers.value = {};
    others.value = {};
    otherFlags.value = {};
    comment.value = "";
    fallbackText.value = "";
    return;
  }

  const saved = loadInteractionDraft(props.conversationId, req.id, req.version);
  if (saved) {
    mode.value = saved.mode;
    answers.value = { ...saved.answers };
    others.value = { ...saved.others };
    otherFlags.value = { ...saved.otherFlags };
    comment.value = saved.comment;
    fallbackText.value = saved.text;
  } else {
    mode.value = "form";
    const initialAnswers: InteractionAnswers = {};
    for (const f of req.fields) {
      if (f.type === "textarea" && f.prefill) {
        initialAnswers[f.id] = f.prefill;
      }
    }
    answers.value = initialAnswers;
    others.value = {};
    otherFlags.value = {};
    comment.value = "";
    fallbackText.value = "";
  }

  // Check if user manually collapsed this exact request version
  const key = `${props.conversationId}:${req.id}:${req.version}`;
  if (collapsedByUserKeys.value.has(key)) {
    collapsed.value = true;
  } else {
    collapsed.value = false;
  }
  emit("collapseChange", collapsed.value);
}

watch(
  () => [props.conversationId, currentRequest.value?.id, currentRequest.value?.version],
  () => {
    initDraft(currentRequest.value);
  },
  { immediate: true },
);

function persistDraft() {
  const req = currentRequest.value;
  if (!req) return;
  const draft: InteractionDraft = {
    mode: mode.value,
    answers: answers.value,
    others: others.value,
    otherFlags: otherFlags.value,
    comment: comment.value,
    text: fallbackText.value,
  };
  saveInteractionDraft(props.conversationId, req.id, req.version, draft);
}

// Watch changes to save draft
watch([mode, answers, others, otherFlags, comment, fallbackText], () => {
  persistDraft();
}, { deep: true });

function prevRequest() {
  if (currentIndex.value > 0) currentIndex.value--;
}

function nextRequest() {
  if (currentIndex.value < props.requests.length - 1) currentIndex.value++;
}

async function toggleCollapse() {
  collapsed.value = !collapsed.value;
  emit("collapseChange", collapsed.value);
  const req = currentRequest.value;
  if (req) {
    const key = `${props.conversationId}:${req.id}:${req.version}`;
    if (collapsed.value) {
      collapsedByUserKeys.value.add(key);
      await nextTick();
      expandBtnRef.value?.focus();
    } else {
      collapsedByUserKeys.value.delete(key);
      await nextTick();
      focusFirstField();
    }
  }
}

function focusFirstField() {
  if (!cardSectionRef.value) return;
  const focusable = cardSectionRef.value.querySelector<HTMLElement>(
    'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex="0"]',
  );
  focusable?.focus();
}

function handleCardKeyDown(e: KeyboardEvent) {
  // Scoped strictly to the card's local non-modal dialog DOM
  if (e.key === "Escape" && !collapsed.value) {
    e.stopPropagation();
    e.preventDefault();
    toggleCollapse();
  }
}

function handleConfirmChoice(fieldId: string, choice: boolean) {
  answers.value[fieldId] = choice;
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleSingleSelectOption(fieldId: string, optionId: string) {
  answers.value[fieldId] = optionId;
  otherFlags.value[fieldId] = false;
  delete others.value[fieldId];
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleSingleSelectOther(fieldId: string) {
  delete answers.value[fieldId];
  otherFlags.value[fieldId] = true;
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleMultiSelectOption(fieldId: string, optionId: string, checked: boolean) {
  const current = Array.isArray(answers.value[fieldId])
    ? [...(answers.value[fieldId] as string[])]
    : [];
  if (checked) {
    if (!current.includes(optionId)) current.push(optionId);
  } else {
    const idx = current.indexOf(optionId);
    if (idx >= 0) current.splice(idx, 1);
  }
  answers.value[fieldId] = current;
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleMultiSelectOther(fieldId: string, checked: boolean) {
  otherFlags.value[fieldId] = checked;
  if (!checked) {
    delete others.value[fieldId];
  }
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleNumberInput(fieldId: string, raw: string) {
  if (raw === "") {
    delete answers.value[fieldId];
  } else {
    answers.value[fieldId] = Number(raw);
  }
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleTextInput(fieldId: string, val: string) {
  answers.value[fieldId] = val;
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function handleOtherInput(fieldId: string, val: string) {
  others.value[fieldId] = val;
  if (errors.value[fieldId]) {
    delete errors.value[fieldId];
  }
}

function submitForm() {
  const req = currentRequest.value;
  if (!req || props.pending || !props.online) return;

  // Deep clone and freeze snapshot of answers & others to prevent drift during
  // submission; required allowEmpty fields become explicit empty strings.
  const frozenAnswers = normalizeSubmissionAnswers(req, answers.value);
  const frozenOthers: InteractionOthers = {};
  for (const [k, v] of Object.entries(others.value)) {
    if (v !== undefined && v !== "") frozenOthers[k] = v;
  }
  const frozenComment = comment.value.trim();
  const frozenText = fallbackText.value.trim();

  if (mode.value === "text") {
    const res = validateTextFallback(frozenText, req);
    if (!res.valid) {
      errors.value = res.errors;
      return;
    }
    errors.value = {};
    emit("submit", {
      conversationId: props.conversationId,
      requestId: req.id,
      requestVersion: req.version,
      answerMode: "text",
      text: frozenText,
    });
  } else {
    const res = validateInteractionAnswers(
      req,
      frozenAnswers,
      frozenOthers,
      frozenComment,
    );
    if (!res.valid) {
      errors.value = res.errors;
      return;
    }
    errors.value = {};
    emit("submit", {
      conversationId: props.conversationId,
      requestId: req.id,
      requestVersion: req.version,
      answerMode: "form",
      answers: frozenAnswers,
      others: frozenOthers,
      comment: frozenComment ? frozenComment : undefined,
    });
  }
}

function cancelRequest() {
  const req = currentRequest.value;
  if (!req || props.pending || !props.online || !req.allowCancel) return;
  emit("cancel", {
    conversationId: props.conversationId,
    requestId: req.id,
    requestVersion: req.version,
  });
}

function retryAnswer() {
  const req = currentRequest.value;
  if (!req || props.pending || !props.online) return;
  emit("retry", {
    conversationId: props.conversationId,
    requestId: req.id,
    requestVersion: req.version,
  });
}
</script>

<template>
  <div v-if="currentRequest" class="interaction-container">
    <!-- Collapsed pill / banner -->
    <div
      v-if="collapsed"
      class="interaction-collapsed-bar"
      role="status"
      aria-live="polite"
    >
      <div class="interaction-collapsed-info">
        <span class="interaction-badge" :class="{ blocking: currentRequest.blocking }">
          {{ currentRequest.blocking ? "待你处理 · 阻塞" : "待你处理" }}
        </span>
        <strong>{{ currentRequest.title }}</strong>
        <span v-if="requests.length > 1" class="interaction-queue-hint">
          ({{ currentIndex + 1 }}/{{ requests.length }})
        </span>
      </div>
      <button
        ref="expandBtnRef"
        class="interaction-btn primary"
        data-action="expand-interaction"
        @click="toggleCollapse"
      >
        展开处理
      </button>
    </div>

    <!-- Full non-modal local dialog card -->
    <section
      v-else
      ref="cardSectionRef"
      class="interaction-card"
      :aria-labelledby="'interaction-title-' + currentRequest.id"
      role="dialog"
      aria-modal="false"
      @keydown="handleCardKeyDown"
    >
      <header class="interaction-header">
        <div class="interaction-title-group">
          <div class="interaction-badges">
            <span
              class="interaction-badge"
              :class="{ blocking: currentRequest.blocking, native: currentRequest.source === 'agent_ui' }"
            >
              {{ currentRequest.blocking ? "待你处理" : "可选补充" }}
            </span>
            <span v-if="currentRequest.source === 'agent_ui'" class="interaction-badge native">
              原生交互
            </span>
          </div>
          <h3 :id="'interaction-title-' + currentRequest.id" class="interaction-title">
            {{ currentRequest.title }}
          </h3>
          <p v-if="currentRequest.description" class="interaction-desc">
            {{ currentRequest.description }}
          </p>
        </div>

        <div v-if="requests.length > 1" class="interaction-queue-nav">
          <span>待答 ({{ currentIndex + 1 }}/{{ requests.length }})</span>
          <button
            :disabled="currentIndex === 0"
            aria-label="上一个问题"
            @click="prevRequest"
          >
            ←
          </button>
          <button
            :disabled="currentIndex === requests.length - 1"
            aria-label="下一个问题"
            @click="nextRequest"
          >
            →
          </button>
        </div>
      </header>

      <!-- Global error banner (e.g. timeout expired) -->
      <div v-if="errors._global" class="interaction-field-error" role="alert">
        {{ errors._global }}
      </div>

      <!-- Mode switch for assistant_question with allowTextFallback -->
      <div
        v-if="currentRequest.allowTextFallback && currentRequest.source === 'assistant_question'"
        class="interaction-fallback-switch"
      >
        <button
          type="button"
          class="interaction-text-btn"
          @click="mode = mode === 'form' ? 'text' : 'form'"
        >
          {{ mode === 'form' ? '改用文字回答本次问题' : '返回表单选择' }}
        </button>
      </div>

      <!-- Text fallback mode -->
      <div v-if="mode === 'text'" class="interaction-body">
        <div class="interaction-field-item">
          <label :for="'field_text_fallback_' + currentRequest.id" class="interaction-field-label">
            文字说明
            <span class="interaction-required-star">*</span>
          </label>
          <textarea
            :id="'field_text_fallback_' + currentRequest.id"
            v-model="fallbackText"
            class="interaction-textarea"
            maxlength="5000"
            :disabled="pending"
            placeholder="直接输入针对本次问题的说明或回答…"
          />
          <span v-if="errors.text" class="interaction-field-error" role="alert">{{ errors.text }}</span>
        </div>
      </div>

      <!-- Form mode -->
      <div v-else class="interaction-body">
        <div
          v-for="field in currentRequest.fields"
          :key="field.id"
          class="interaction-field-item"
        >
          <label
            v-if="field.type !== 'confirm'"
            :for="'field_' + currentRequest.id + '_' + field.id"
            class="interaction-field-label"
          >
            {{ field.label }}
            <span v-if="field.required" class="interaction-required-star">*</span>
          </label>
          <!-- A confirm is a group of two labelled decisions; a for= label on
               the first button would hide both button names from assistive
               technology. -->
          <span v-else class="interaction-field-label">
            {{ field.label }}
            <span v-if="field.required" class="interaction-required-star">*</span>
          </span>
          <span v-if="field.description" class="interaction-field-desc">
            {{ field.description }}
          </span>

          <!-- Confirm field -->
          <div
            v-if="field.type === 'confirm'"
            class="interaction-confirm-group"
            role="group"
            :aria-label="field.label || '确认选项'"
          >
            <button
              type="button"
              class="interaction-confirm-btn"
              :class="{ 'selected-yes': answers[field.id] === true }"
              :aria-pressed="answers[field.id] === true"
              :disabled="pending"
              @click="handleConfirmChoice(field.id, true)"
            >
              {{ field.yesLabel || "允许" }}
            </button>
            <button
              type="button"
              class="interaction-confirm-btn"
              :class="{ 'selected-no': answers[field.id] === false }"
              :aria-pressed="answers[field.id] === false"
              :disabled="pending"
              @click="handleConfirmChoice(field.id, false)"
            >
              {{ field.noLabel || "拒绝" }}
            </button>
          </div>

          <!-- Text field -->
          <input
            v-else-if="field.type === 'text'"
            :id="'field_' + currentRequest.id + '_' + field.id"
            type="text"
            class="interaction-input"
            :placeholder="field.placeholder || ''"
            :maxlength="field.maxLength || 2000"
            :disabled="pending"
            :value="typeof answers[field.id] === 'string' ? (answers[field.id] as string) : ''"
            @input="handleTextInput(field.id, ($event.target as HTMLInputElement).value)"
          />

          <!-- Textarea field -->
          <textarea
            v-else-if="field.type === 'textarea'"
            :id="'field_' + currentRequest.id + '_' + field.id"
            class="interaction-textarea"
            :placeholder="field.placeholder || ''"
            :maxlength="field.maxLength || 5000"
            :disabled="pending"
            :value="typeof answers[field.id] === 'string' ? (answers[field.id] as string) : ''"
            @input="handleTextInput(field.id, ($event.target as HTMLTextAreaElement).value)"
          />

          <!-- Single select -->
          <div
            v-else-if="field.type === 'single_select'"
            class="interaction-options-list"
            role="radiogroup"
            :aria-label="field.label"
          >
            <label
              v-for="opt in field.options"
              :key="opt.id"
              :for="'opt_' + currentRequest.id + '_' + field.id + '_' + opt.id"
              class="interaction-option-label"
            >
              <input
                :id="'opt_' + currentRequest.id + '_' + field.id + '_' + opt.id"
                type="radio"
                :name="'radio_' + currentRequest.id + '_' + field.id"
                :value="opt.id"
                :checked="answers[field.id] === opt.id && !otherFlags[field.id]"
                :disabled="pending"
                @change="handleSingleSelectOption(field.id, opt.id)"
              />
              <div class="interaction-option-info">
                <span>
                  {{ opt.label }}
                  <span v-if="opt.recommended" class="interaction-rec-badge">推荐</span>
                </span>
                <small v-if="opt.description" class="interaction-field-desc">
                  {{ opt.description }}
                </small>
              </div>
            </label>

            <!-- Other option for single select -->
            <label
              v-if="field.allowOther"
              :for="'opt_' + currentRequest.id + '_' + field.id + '_other'"
              class="interaction-option-label"
            >
              <input
                :id="'opt_' + currentRequest.id + '_' + field.id + '_other'"
                type="radio"
                :name="'radio_' + currentRequest.id + '_' + field.id"
                value="__custom_other__"
                :checked="otherFlags[field.id] === true"
                :disabled="pending"
                @change="handleSingleSelectOther(field.id)"
              />
              <span class="interaction-option-info">其他</span>
            </label>
            <input
              v-if="field.allowOther && otherFlags[field.id]"
              :id="'field_' + currentRequest.id + '_' + field.id + '_other_text'"
              type="text"
              class="interaction-input interaction-other-input"
              placeholder="请填写具体说明…"
              maxlength="2000"
              :disabled="pending"
              :value="others[field.id] || ''"
              @input="handleOtherInput(field.id, ($event.target as HTMLInputElement).value)"
            />
          </div>

          <!-- Multi select -->
          <div
            v-else-if="field.type === 'multi_select'"
            class="interaction-options-list"
            role="group"
            :aria-label="field.label"
          >
            <label
              v-for="opt in field.options"
              :key="opt.id"
              :for="'opt_' + currentRequest.id + '_' + field.id + '_' + opt.id"
              class="interaction-option-label"
            >
              <input
                :id="'opt_' + currentRequest.id + '_' + field.id + '_' + opt.id"
                type="checkbox"
                :value="opt.id"
                :checked="Array.isArray(answers[field.id]) && (answers[field.id] as string[]).includes(opt.id)"
                :disabled="pending"
                @change="handleMultiSelectOption(field.id, opt.id, ($event.target as HTMLInputElement).checked)"
              />
              <div class="interaction-option-info">
                <span>
                  {{ opt.label }}
                  <span v-if="opt.recommended" class="interaction-rec-badge">推荐</span>
                </span>
                <small v-if="opt.description" class="interaction-field-desc">
                  {{ opt.description }}
                </small>
              </div>
            </label>

            <!-- Other option for multi select -->
            <label
              v-if="field.allowOther"
              :for="'opt_' + currentRequest.id + '_' + field.id + '_other'"
              class="interaction-option-label"
            >
              <input
                :id="'opt_' + currentRequest.id + '_' + field.id + '_other'"
                type="checkbox"
                value="__custom_other__"
                :checked="otherFlags[field.id] === true"
                :disabled="pending"
                @change="handleMultiSelectOther(field.id, ($event.target as HTMLInputElement).checked)"
              />
              <span class="interaction-option-info">其他</span>
            </label>
            <input
              v-if="field.allowOther && otherFlags[field.id]"
              :id="'field_' + currentRequest.id + '_' + field.id + '_other_text'"
              type="text"
              class="interaction-input interaction-other-input"
              placeholder="请填写“其他”的具体说明…"
              maxlength="2000"
              :disabled="pending"
              :value="others[field.id] || ''"
              @input="handleOtherInput(field.id, ($event.target as HTMLInputElement).value)"
            />
          </div>

          <!-- Number field -->
          <div v-else-if="field.type === 'number'" class="interaction-number-wrap">
            <input
              :id="'field_' + currentRequest.id + '_' + field.id"
              type="number"
              class="interaction-input"
              :step="field.integer ? '1' : 'any'"
              :min="field.min"
              :max="field.max"
              :disabled="pending"
              :value="typeof answers[field.id] === 'number' ? (answers[field.id] as number) : ''"
              @input="handleNumberInput(field.id, ($event.target as HTMLInputElement).value)"
            />
            <span v-if="field.unit" class="interaction-unit">{{ field.unit }}</span>
          </div>

          <span v-if="errors[field.id]" class="interaction-field-error" role="alert">
            {{ errors[field.id] }}
          </span>
        </div>

        <!-- Optional comment (only for assistant_question) -->
        <div v-if="currentRequest.source === 'assistant_question'" class="interaction-field-item">
          <label :for="'field_comment_' + currentRequest.id" class="interaction-field-label">补充说明（可选）</label>
          <textarea
            :id="'field_comment_' + currentRequest.id"
            v-model="comment"
            class="interaction-textarea"
            placeholder="如有其他特殊要求或背景说明，可在此填写…"
            maxlength="2000"
            :disabled="pending"
          />
          <span v-if="errors._comment" class="interaction-field-error" role="alert">
            {{ errors._comment }}
          </span>
        </div>
      </div>

      <!-- Delivery / retry status in case this request had a failed attempt -->
      <div
        v-if="currentRequest.response && currentRequest.response.delivery === 'not_sent'"
        class="interaction-field-error"
        role="alert"
      >
        <span>发送失败：答复已在本地保存，但未送出。</span>
        <button
          type="button"
          class="interaction-btn primary"
          :disabled="pending || !online"
          @click="retryAnswer"
        >
          重试发送
        </button>
      </div>

      <!-- Actions footer -->
      <footer class="interaction-actions">
        <div class="interaction-actions-left">
          <button
            type="button"
            class="interaction-btn"
            data-action="collapse-interaction"
            @click="toggleCollapse"
          >
            稍后处理
          </button>
          <button
            v-if="currentRequest.allowCancel"
            type="button"
            class="interaction-btn danger"
            data-action="cancel-interaction"
            :disabled="pending || !online"
            @click="cancelRequest"
          >
            取消本次请求
          </button>
        </div>

        <div class="interaction-actions-right">
          <button
            type="button"
            class="interaction-btn primary"
            data-action="submit-interaction"
            :disabled="pending || !online"
            @click="submitForm"
          >
            {{ pending ? "提交中…" : "发送答复" }}
          </button>
        </div>
      </footer>
    </section>
  </div>
</template>

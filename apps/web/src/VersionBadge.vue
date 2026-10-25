<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from "vue";
import {
  formatRevision,
  isRevisionMismatch,
  formatDateTime,
  formatDeploymentStatus,
  fetchVersionInfo,
  type VersionInfo,
} from "./version.ts";

const frontendRevision =
  (import.meta.env.VITE_BUILD_REVISION as string | undefined) || "";

const version = ref<VersionInfo | null>(null);
const loading = ref(false);
const fetchError = ref("");
const isOpen = ref(false);

const triggerRef = ref<HTMLButtonElement | null>(null);
const closeButtonRef = ref<HTMLButtonElement | null>(null);
const dialogRef = ref<HTMLDivElement | null>(null);

let isMounted = false;

const buttonLabel = computed(() => {
  if (fetchError.value || !version.value) {
    return "版本未知";
  }
  return formatRevision(version.value.revision, version.value.shortRevision);
});

const isOutdated = computed(() => {
  if (!version.value) return false;
  return isRevisionMismatch(frontendRevision, version.value.revision);
});

const deploymentStatus = computed(() => {
  const dep = version.value?.deployment;
  return formatDeploymentStatus(dep?.status, dep?.message);
});

async function loadVersion(isManual = false) {
  if (loading.value) {
    return;
  }
  loading.value = true;
  if (isManual) {
    // When manual refresh fails, we must not continue showing stale success state
    fetchError.value = "";
  }
  try {
    const data = await fetchVersionInfo();
    if (!isMounted) return;
    version.value = data;
    fetchError.value = "";
  } catch (err: any) {
    if (!isMounted) return;
    fetchError.value = err?.message || "获取版本信息失败";
    // Loading failure clears stale success data to ensure honest display
    version.value = null;
  } finally {
    if (isMounted) {
      loading.value = false;
    }
  }
}

function openDialog() {
  isOpen.value = true;
  // Opening dialog triggers refresh to get latest status without polling
  loadVersion();
  window.addEventListener("keydown", onKeydown);
  nextTick(() => {
    closeButtonRef.value?.focus();
  });
}

function closeDialog() {
  isOpen.value = false;
  window.removeEventListener("keydown", onKeydown);
  nextTick(() => {
    triggerRef.value?.focus();
  });
}

function onKeydown(e: KeyboardEvent) {
  if (!isOpen.value) return;

  if (e.key === "Escape") {
    e.preventDefault();
    closeDialog();
    return;
  }

  if (e.key === "Tab") {
    handleTabFocusTrap(e);
  }
}

function handleTabFocusTrap(e: KeyboardEvent) {
  const dialog = dialogRef.value;
  if (!dialog) return;

  const focusable = dialog.querySelectorAll<HTMLElement>(
    'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
  );
  if (!focusable.length) return;

  const first = focusable[0];
  const last = focusable[focusable.length - 1];

  if (e.shiftKey) {
    if (document.activeElement === first || !dialog.contains(document.activeElement)) {
      e.preventDefault();
      last.focus();
    }
  } else {
    if (document.activeElement === last || !dialog.contains(document.activeElement)) {
      e.preventDefault();
      first.focus();
    }
  }
}

function reloadPage() {
  window.location.reload();
}

function handleWindowFocus() {
  loadVersion();
}

function handleVisibilityChange() {
  if (document.visibilityState === "visible") {
    loadVersion();
  }
}

onMounted(() => {
  isMounted = true;
  loadVersion();
  window.addEventListener("focus", handleWindowFocus);
  document.addEventListener("visibilitychange", handleVisibilityChange);
});

onBeforeUnmount(() => {
  isMounted = false;
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("focus", handleWindowFocus);
  document.removeEventListener("visibilitychange", handleVisibilityChange);
});
</script>

<template>
  <div class="version-container">
    <button
      ref="triggerRef"
      type="button"
      class="version-trigger"
      :class="{
        'is-dev': version?.revision === 'dev' || version?.shortRevision === 'dev',
        'is-unknown': !version || !!fetchError,
        'is-outdated': isOutdated,
      }"
      :aria-expanded="isOpen"
      aria-haspopup="dialog"
      aria-controls="version-dialog"
      :title="`版本信息: ${buttonLabel}`"
      @click="openDialog"
    >
      <span class="version-text">{{ buttonLabel }}</span>
      <span v-if="isOutdated" class="outdated-dot" title="页面版本已过期">●</span>
    </button>

    <Teleport to="body">
      <div
        v-if="isOpen"
        class="version-backdrop"
        @click.self="closeDialog"
      >
        <div
          id="version-dialog"
          ref="dialogRef"
          class="version-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="version-dialog-title"
        >
          <div class="dialog-header">
            <h3 id="version-dialog-title">系统版本与部署状态</h3>
            <button
              ref="closeButtonRef"
              type="button"
              class="close-button"
              aria-label="关闭版本详情"
              @click="closeDialog"
            >
              ✕
            </button>
          </div>

          <div class="dialog-body">
            <!-- Outdated page notice -->
            <div v-if="isOutdated" class="alert-box alert-warning" role="alert">
              <div class="alert-content">
                <strong>页面版本已过期，请刷新</strong>
                <p>当前页面构建号与服务器运行版本不一致。</p>
              </div>
              <button
                type="button"
                class="reload-button"
                @click="reloadPage"
              >
                刷新页面
              </button>
            </div>

            <!-- Fetch error banner -->
            <div v-if="fetchError" class="alert-box alert-error" role="alert">
              <strong>获取版本失败</strong>
              <p>{{ fetchError }}</p>
            </div>

            <!-- Running version section -->
            <section class="info-section">
              <h4 class="section-title">当前运行程序</h4>
              <dl class="info-grid">
                <dt>运行版本</dt>
                <dd>
                  <code class="sha-code">{{ version?.revision || "未知" }}</code>
                </dd>

                <dt>提交说明</dt>
                <dd class="summary-text">{{ version?.summary || "无" }}</dd>

                <dt>构建时间</dt>
                <dd>{{ formatDateTime(version?.builtAt) }}</dd>

                <dt>激活部署</dt>
                <dd>{{ formatDateTime(version?.deployedAt) }}</dd>
              </dl>
            </section>

            <!-- Recent deployment attempt section -->
            <section class="info-section">
              <h4 class="section-title">最近部署尝试</h4>
              <div
                v-if="deploymentStatus.isFailed"
                class="failure-banner"
                role="alert"
              >
                {{ deploymentStatus.fallbackMessage }}
              </div>

              <dl class="info-grid">
                <dt>尝试状态</dt>
                <dd>
                  <span
                    class="status-tag"
                    :class="`status-${version?.deployment?.status || 'unknown'}`"
                  >
                    {{ deploymentStatus.label }}
                  </span>
                </dd>

                <dt>阶段说明</dt>
                <dd>{{ version?.deployment?.message || (deploymentStatus.isFailed ? deploymentStatus.fallbackMessage : "无") }}</dd>

                <dt>尝试版本</dt>
                <dd>
                  <code class="sha-code">{{ version?.deployment?.revision || "无" }}</code>
                </dd>

                <dt>尝试时间</dt>
                <dd>{{ formatDateTime(version?.deployment?.at) }}</dd>
              </dl>
            </section>
          </div>

          <div class="dialog-footer">
            <button
              type="button"
              class="refresh-button"
              :disabled="loading"
              @click="loadVersion(true)"
            >
              {{ loading ? "正在刷新..." : "重新检查" }}
            </button>
            <button
              type="button"
              class="dismiss-button"
              @click="closeDialog"
            >
              关闭
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.version-container {
  display: inline-flex;
  align-items: center;
}

.version-trigger {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-height: 28px;
  padding: 2px 8px;
  border: 1px solid var(--theme-border, #d6ddd7);
  border-radius: 6px;
  background: var(--theme-canvas, #f7f9f8);
  color: var(--theme-text, #3b423f);
  font-size: 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  cursor: pointer;
  max-width: 110px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: background-color 0.15s ease, border-color 0.15s ease;
}

.version-trigger:hover {
  background: var(--theme-inset, #edf1ee);
  border-color: var(--theme-border, #bcc6c0);
}

.version-trigger:focus-visible {
  outline: 2px solid var(--theme-focus, #456490);
  outline-offset: 1px;
}

.version-trigger.is-dev {
  font-family: inherit;
  color: var(--theme-accent, #2b5c46);
  background: var(--theme-green-bg, #eef7f2);
  border-color: var(--theme-green-border, #c2e2d1);
}

.version-trigger.is-unknown {
  font-family: inherit;
  color: var(--theme-muted, #7b8580);
  border-style: dashed;
}

.version-trigger.is-outdated {
  border-color: var(--theme-warning-border, #d97706);
}

.version-text {
  overflow: hidden;
  text-overflow: ellipsis;
}

.outdated-dot {
  color: var(--theme-warning, #d97706);
  font-size: 10px;
  line-height: 1;
}

/* Modal Dialog */
.version-backdrop {
  position: fixed;
  inset: 0;
  z-index: 1000;
  background: rgba(0, 0, 0, 0.4);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 12px;
}

.version-dialog {
  background: var(--theme-surface, #ffffff);
  border: 1px solid var(--theme-border, #d6ddd7);
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.15);
  width: 100%;
  max-width: 440px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  max-height: calc(100vh - 32px);
}

.dialog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--theme-border, #edf1ee);
  background: var(--theme-canvas, #fafbfa);
}

.dialog-header h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--theme-text, #1a201c);
}

.close-button {
  background: transparent;
  border: none;
  font-size: 14px;
  line-height: 1;
  color: var(--theme-muted, #7b8580);
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}

.close-button:hover {
  background: var(--theme-inset, #edf1ee);
  color: var(--theme-text, #1a201c);
}

.close-button:focus-visible {
  outline: 2px solid var(--theme-focus, #456490);
}

.dialog-body {
  padding: 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.alert-box {
  padding: 10px 12px;
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.4;
}

.alert-warning {
  background: var(--theme-warning-bg, #fffbeb);
  border: 1px solid var(--theme-warning-border, #fde68a);
  color: var(--theme-warning, #92400e);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.alert-warning p {
  margin: 2px 0 0;
  font-size: 11px;
  color: var(--theme-warning, #b45309);
}

.reload-button {
  background: #d97706;
  color: #ffffff;
  border: none;
  border-radius: 4px;
  padding: 4px 10px;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}

.reload-button:hover {
  background: #b45309;
}

.alert-error {
  background: var(--theme-danger-bg, #fef2f2);
  border: 1px solid var(--theme-danger-border, #fecaca);
  color: var(--theme-danger, #991b1b);
}

.alert-error p {
  margin: 2px 0 0;
}

.info-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.section-title {
  margin: 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--theme-text, #555f59);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.failure-banner {
  background: var(--theme-warning-bg, #fff1e7);
  color: var(--theme-warning, #854b21);
  border-left: 3px solid var(--theme-warning-border, #e06c27);
  padding: 6px 10px;
  font-size: 12px;
  border-radius: 0 4px 4px 0;
}

.info-grid {
  display: grid;
  grid-template-columns: 80px 1fr;
  row-gap: 6px;
  column-gap: 10px;
  margin: 0;
  font-size: 12px;
}

.info-grid dt {
  color: var(--theme-muted, #7b8580);
  white-space: nowrap;
}

.info-grid dd {
  margin: 0;
  color: var(--theme-text, #1a201c);
  word-break: break-all;
}

.sha-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 11px;
  background: var(--theme-inset, #f2f4f2);
  padding: 1px 4px;
  border-radius: 3px;
}

.summary-text {
  word-break: break-word;
}

.status-tag {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 11px;
}

.status-succeeded {
  background: var(--theme-green-bg, #eef7f2);
  color: var(--theme-accent, #1e7048);
}

.status-failed {
  background: var(--theme-danger-bg, #fef2f2);
  color: var(--theme-danger, #b91c1c);
}

.status-testing,
.status-building,
.status-deploying {
  background: var(--theme-blue-bg, #eff6ff);
  color: var(--theme-blue, #1d4ed8);
}

.status-unknown {
  background: var(--theme-inset, #f3f4f6);
  color: var(--theme-text, #4b5563);
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--theme-border, #edf1ee);
  background: var(--theme-canvas, #fafbfa);
}

.refresh-button,
.dismiss-button {
  padding: 5px 12px;
  font-size: 12px;
  border-radius: 5px;
  cursor: pointer;
}

.refresh-button {
  background: var(--theme-inset, #f2f4f2);
  border: 1px solid var(--theme-border, #d6ddd7);
  color: var(--theme-text, #2e3531);
}

.refresh-button:hover:not(:disabled) {
  background: var(--theme-inset, #e5e9e6);
}

.refresh-button:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.dismiss-button {
  background: transparent;
  border: 1px solid transparent;
  color: var(--theme-text, #555f59);
}

.dismiss-button:hover {
  background: var(--theme-inset, #edf1ee);
}

@media (max-width: 599px) {
  .version-trigger {
    max-width: 80px;
    padding: 2px 6px;
  }

  .version-dialog {
    max-width: calc(100vw - 16px);
  }

  .info-grid {
    grid-template-columns: 70px 1fr;
  }
}
</style>

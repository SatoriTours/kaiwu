<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { api } from './api.ts';
import type { Project, ProjectDeliveryPolicy, PreflightReport } from './types.ts';
import { defaultPolicyDraft, validatePolicyDraft, isZeroTime, type PolicyDraft } from './delivery.ts';
import { createOperationID } from './workbench.ts';

const props = defineProps<{
  project: Project;
  saving: boolean;
  error: string;
}>();

const emit = defineEmits<{
  close: [];
  save: [draft: PolicyDraft, operationId: string];
}>();

const dialog = ref<HTMLDialogElement>();
const loading = ref(true);
const loadError = ref('');
const preflightLoading = ref(false);
const preflightReport = ref<PreflightReport | null>(null);
const localError = ref('');

// Policy draft state (retained even if save fails)
const draft = ref<PolicyDraft>(defaultPolicyDraft(props.project.id));
const checksJson = ref('[\n  {\n    "id": "test",\n    "argv": ["npm", "test"],\n    "dir": ".",\n    "timeoutSeconds": 300\n  }\n]');
const taskWorkflowsJson = ref('[\n  {\n    "id": 1,\n    "jobs": []\n  }\n]');
const mainWorkflowsJson = ref('[\n  {\n    "id": 1,\n    "jobs": []\n  }\n]');

// Fixed credential references
const credentialOptions = [
  { value: 'server_github_https', label: '服务器配置的 GitHub HTTPS 凭据 (server_github_https)' },
  { value: 'server_github_ssh', label: '服务器配置的 GitHub SSH 凭据 (server_github_ssh)' },
];

async function loadPolicy() {
  const currentProjectId = props.project.id;
  loading.value = true;
  loadError.value = '';
  try {
    const res = await api<{ policy: ProjectDeliveryPolicy }>('delivery/policy?projectId=' + encodeURIComponent(currentProjectId));
    // Asynchronous arrival check: ignore if project switched
    if (props.project.id !== currentProjectId) return;
    if (res && res.policy) {
      const p = res.policy;
      const isRevoked = !!p.revokedAt && !isZeroTime(p.revokedAt);
      const hasValidAuth = !!p.authorizedAt && !isZeroTime(p.authorizedAt);
      draft.value = {
        projectId: p.projectId || currentProjectId,
        version: p.version || 0,
        enabled: !!p.enabled,
        authorized: false, // 无论是新开还是重新配置，启用/修改授权 scope 必须真实显式确认勾选，不自动凭已 enabled 免勾选
        requireAcceptance: p.requireAcceptance !== false,
        allowCreateMain: !!p.allowCreateMain,
        repository: {
          id: p.repository?.id || 0,
          owner: p.repository?.owner || '',
          name: p.repository?.name || '',
          url: p.repository?.url || '',
          mainBranch: p.repository?.mainBranch || props.project.mainBranchOverride || props.project.defaultBranch || 'main',
        },
        checks: p.checks || [],
        taskWorkflows: p.taskWorkflows || [],
        mainWorkflows: p.mainWorkflows || [],
        maxRepairs: p.maxRepairs || 3,
        activeSeconds: p.activeSeconds || 3600,
        triggerSeconds: p.triggerSeconds || 300,
        ciSeconds: p.ciSeconds || 1800,
        gitCredentialRef: p.gitCredentialRef || 'server_github_https',
        apiCredentialRef: p.apiCredentialRef || 'server_github_api',
        authorizationDigest: p.authorizationDigest || '',
        actor: p.actor || 'kaiwu_web',
        authorizedAt: hasValidAuth ? p.authorizedAt : '',
        revokedAt: isRevoked ? p.revokedAt : undefined,
      };
      if (p.checks && p.checks.length) {
        checksJson.value = JSON.stringify(p.checks, null, 2);
      }
      if (p.taskWorkflows && p.taskWorkflows.length) {
        taskWorkflowsJson.value = JSON.stringify(p.taskWorkflows, null, 2);
      }
      if (p.mainWorkflows && p.mainWorkflows.length) {
        mainWorkflowsJson.value = JSON.stringify(p.mainWorkflows, null, 2);
      }
    }
  } catch (e) {
    if (props.project.id === currentProjectId) {
      loadError.value = e instanceof Error ? e.message : '加载交付配置失败';
    }
  } finally {
    if (props.project.id === currentProjectId) {
      loading.value = false;
    }
  }
}

async function runPreflight() {
  const currentProjectId = props.project.id;
  preflightLoading.value = true;
  preflightReport.value = null;
  localError.value = '';
  try {
    const rep = await api<any>('delivery/preflight', {
      projectId: currentProjectId,
    });
    if (props.project.id !== currentProjectId) return;
    // Support both lowercase tags and legacy uppercase tags
    preflightReport.value = {
      ready: rep.ready ?? rep.Ready ?? false,
      fingerprint: rep.fingerprint ?? rep.Fingerprint ?? '',
      blockers: rep.blockers ?? rep.Blockers ?? [],
    };
  } catch (e) {
    if (props.project.id === currentProjectId) {
      localError.value = e instanceof Error ? e.message : '只读预检失败';
    }
  } finally {
    if (props.project.id === currentProjectId) {
      preflightLoading.value = false;
    }
  }
}

function parseJsonField<T>(str: string, fieldName: string): { ok: boolean; val?: T; err?: string } {
  try {
    const val = JSON.parse(str);
    return { ok: true, val };
  } catch (e) {
    return { ok: false, err: `${fieldName} 格式不符合有效 JSON` };
  }
}

function handleSave() {
  localError.value = '';
  // Parse JSON fields
  const pChecks = parseJsonField<any[]>(checksJson.value, '本地检查');
  if (!pChecks.ok) {
    localError.value = pChecks.err!;
    return;
  }
  const pTaskWf = parseJsonField<any[]>(taskWorkflowsJson.value, '任务分支 workflow');
  if (!pTaskWf.ok) {
    localError.value = pTaskWf.err!;
    return;
  }
  const pMainWf = parseJsonField<any[]>(mainWorkflowsJson.value, '主分支 workflow');
  if (!pMainWf.ok) {
    localError.value = pMainWf.err!;
    return;
  }

  draft.value.checks = pChecks.val!;
  draft.value.taskWorkflows = pTaskWf.val!;
  draft.value.mainWorkflows = pMainWf.val!;

  const valErr = validatePolicyDraft(draft.value);
  if (valErr) {
    localError.value = valErr;
    return;
  }

  const opId = createOperationID();
  emit('save', draft.value, opId);
}

watch(() => props.project.id, () => {
  void loadPolicy();
});

onMounted(() => {
  dialog.value?.showModal();
  void loadPolicy();
});
</script>

<template>
  <dialog ref="dialog" class="delivery-settings-dialog" aria-labelledby="delivery-settings-title" @cancel.prevent="!saving && emit('close')">
    <div class="delivery-settings-content">
      <h2 id="delivery-settings-title">开发与交付设置 · {{ project.name }}</h2>
      <p class="muted">配置基于 GitHub Actions 与独立 worktree 的固定自动开发交付流程。一次配置，任务沿用。</p>

      <div v-if="loading" class="entry"><p>正在加载交付设置…</p></div>
      <div v-else-if="loadError" class="error" role="alert">{{ loadError }}</div>
      <form v-else @submit.prevent="handleSave">
        <section class="settings-section">
          <h3>启用与授权</h3>
          <p class="section-notice">
            开启后，系统在任务通过本地检查后将向 GitHub 推送任务分支（<code>kaiwu/task-*</code>）并观察 CI；技术与效果验收后按队列合并推送到主分支。
            推送会消耗 CI 与模型额度，主分支推送可能触发部署或发包。请确认已知悉并明确授权。
          </p>
          <div class="row items-center">
            <label class="checkbox-label">
              <input type="checkbox" v-model="draft.enabled" :disabled="saving" />
              <span>启用本项目自动开发交付流程</span>
            </label>
          </div>
          <div v-if="draft.enabled" class="row items-center auth-box">
            <label class="checkbox-label">
              <input type="checkbox" v-model="draft.authorized" :disabled="saving" required />
              <strong>我确认授权上述远程写入与外部触发副作用（不配置任意私有 Token，使用服务端受控凭据）</strong>
            </label>
          </div>
        </section>

        <section class="settings-section">
          <h3>GitHub 仓库与分支</h3>
          <label>
            <span>规范仓库地址 (HTTPS 或 SSH，不得包含密码或凭据)</span>
            <input v-model="draft.repository.url" placeholder="https://github.com/owner/repo.git 或 ssh://git@github.com/owner/repo.git" :disabled="saving" />
          </label>
          <div class="row">
            <label>
              <span>Owner</span>
              <input v-model="draft.repository.owner" placeholder="例如: SatoriTours" :disabled="saving" />
            </label>
            <label>
              <span>Repository Name</span>
              <input v-model="draft.repository.name" placeholder="例如: kaiwu" :disabled="saving" />
            </label>
            <label>
              <span>Repository ID (正整数)</span>
              <input type="number" v-model.number="draft.repository.id" placeholder="例如: 123456" :disabled="saving" />
            </label>
          </div>
          <div class="row">
            <label>
              <span>目标主分支 (默认 main)</span>
              <input v-model="draft.repository.mainBranch" placeholder="main" :disabled="saving" />
            </label>
            <label class="checkbox-label inline-checkbox">
              <input type="checkbox" v-model="draft.allowCreateMain" :disabled="saving" />
              <span>主分支不存在时允许首次创建</span>
            </label>
          </div>
        </section>

        <section class="settings-section">
          <h3>凭据与模式</h3>
          <div class="row">
            <label>
              <span>Git 远程凭据</span>
              <select v-model="draft.gitCredentialRef" :disabled="saving">
                <option v-for="opt in credentialOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
              </select>
            </label>
            <label>
              <span>GitHub API 凭据</span>
              <input value="server_github_api (服务器只读凭据)" disabled />
            </label>
          </div>
          <div class="row items-center">
            <label class="checkbox-label">
              <input type="checkbox" v-model="draft.requireAcceptance" :disabled="saving" />
              <span>需要人工效果验收（通过本地与任务 CI 后，先查看运行效果并确认再合并主分支；建议开启）</span>
            </label>
          </div>
        </section>

        <section class="settings-section">
          <h3>有限预算与超时</h3>
          <div class="row">
            <label>
              <span>自动修复上限次数</span>
              <input type="number" v-model.number="draft.maxRepairs" min="1" max="10" :disabled="saving" />
            </label>
            <label>
              <span>活动超时 (秒)</span>
              <input type="number" v-model.number="draft.activeSeconds" min="60" max="86400" :disabled="saving" />
            </label>
            <label>
              <span>CI 等待触发 (秒)</span>
              <input type="number" v-model.number="draft.triggerSeconds" min="30" max="3600" :disabled="saving" />
            </label>
            <label>
              <span>CI 排队与运行 (秒)</span>
              <input type="number" v-model.number="draft.ciSeconds" min="60" max="7200" :disabled="saving" />
            </label>
          </div>
        </section>

        <section class="settings-section">
          <h3>必需本地检查 (JSON 定义，直接 argv 不拼 shell)</h3>
          <p class="muted">至少 1 项。每项必须包含 id、argv 数组、dir 相对目录及 timeoutSeconds。</p>
          <textarea v-model="checksJson" rows="4" class="json-editor" :disabled="saving" />
        </section>

        <section class="settings-section">
          <h3>必需 Actions Workflow (JSON 定义)</h3>
          <div class="row">
            <div class="column-half">
              <label><span>任务分支 CI Workflows (含 id 及可选 jobs)</span></label>
              <textarea v-model="taskWorkflowsJson" rows="4" class="json-editor" :disabled="saving" />
            </div>
            <div class="column-half">
              <label><span>主分支 CI Workflows (含 id 及可选 jobs)</span></label>
              <textarea v-model="mainWorkflowsJson" rows="4" class="json-editor" :disabled="saving" />
            </div>
          </div>
        </section>

        <section class="settings-section preflight-section">
          <div class="row between items-center">
            <h4>只读预检报告</h4>
            <button type="button" :disabled="preflightLoading || saving" @click="runPreflight">
              {{ preflightLoading ? '预检中…' : '执行只读预检' }}
            </button>
          </div>
          <div v-if="preflightReport" class="preflight-box">
            <p :class="preflightReport.ready ? 'ready' : 'blocked'">
              {{ preflightReport.ready ? '✓ 预检通过，具备自动交付就绪状态' : '✕ 预检发现阻碍，请核查以下事项：' }}
            </p>
            <ul v-if="preflightReport.blockers && preflightReport.blockers.length">
              <li v-for="(b, i) in preflightReport.blockers" :key="i">{{ b }}</li>
            </ul>
          </div>
        </section>

        <p v-if="localError || error" class="error" role="alert">{{ localError || error }}</p>

        <div class="dialog-actions">
          <button type="button" :disabled="saving" @click="emit('close')">取消</button>
          <button class="primary" :disabled="saving">{{ saving ? '保存中…' : '保存设置' }}</button>
        </div>
      </form>
    </div>
  </dialog>
</template>

<style scoped>
.delivery-settings-dialog {
  max-width: 680px;
  width: 90vw;
  max-height: 90vh;
  padding: 0;
  border: 1px solid var(--border, #ccc);
  border-radius: 8px;
}
.delivery-settings-content {
  padding: 24px;
  overflow-y: auto;
  max-height: 85vh;
}
.settings-section {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border-light, #eee);
}
.settings-section h3 {
  margin: 0 0 8px 0;
  font-size: 15px;
}
.section-notice {
  font-size: 12px;
  color: var(--text-muted, #666);
  background: var(--bg-hover, #f8f9fa);
  padding: 8px;
  border-radius: 4px;
  line-height: 1.5;
}
.auth-box {
  background: #fff8e6;
  padding: 10px;
  border: 1px solid #ffd591;
  border-radius: 4px;
  margin-top: 8px;
}
.checkbox-label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  cursor: pointer;
}
.inline-checkbox {
  margin-top: 24px;
}
.json-editor {
  font-family: monospace;
  font-size: 12px;
  width: 100%;
}
.column-half {
  flex: 1;
  display: flex;
  flex-direction: column;
}
.preflight-box {
  margin-top: 8px;
  padding: 10px;
  border-radius: 4px;
  background: var(--bg-hover, #f8f9fa);
  font-size: 13px;
}
.preflight-box p.ready {
  color: #389e0d;
  font-weight: bold;
}
.preflight-box p.blocked {
  color: #cf1322;
  font-weight: bold;
}
.preflight-box ul {
  margin: 6px 0 0 16px;
  padding: 0;
  color: #cf1322;
}
.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
}
</style>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue';
import { api } from './api.ts';
import type { Project, ProjectDeliveryPolicy, PreflightReport } from './types.ts';
import { defaultPolicyDraft, validatePolicyDraft, isZeroTime, type PolicyDraft } from './delivery.ts';
import { createOperationID } from './workbench.ts';
import {
  parseArgsString,
  formatArgsString,
  checksToUI,
  uiToChecks,
  generateNextCheckId,
  parseGitHubRepo,
  isEquivalentRepo,
  type CheckItemUI,
} from './delivery-settings.ts';

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

// 视图控制：0 为概览，1 为向导第1步（目标仓库），2 为向导第2步（通过标准），3 为向导第3步（失败处理与授权）
const activeStep = ref<number>(0);

// 是否已加载过既有已启用的策略（保护模式）
const isExistingEnabled = ref(false);

// Policy draft state (即使保存失败，用户输入仍然保留)
const draft = ref<PolicyDraft>(defaultPolicyDraft(props.project.id));

// 加载时记录的原始仓库信息（用于规范等价比对，判定是否保留原 ID）
const loadedRepo = ref<{ id: number; url: string; owner: string; name: string }>({
  id: 0,
  url: '',
  owner: '',
  name: '',
});

// 本地检查逐项 UI 模型
const checkItems = ref<CheckItemUI[]>([]);

// GitHub 仓库输入缓存（用于向导实时输入）
const repoUrlInput = ref('');
const mainBranchInput = ref(props.project.mainBranchOverride || props.project.defaultBranch || 'main');

// 交付方式：'manual' (人工先看效果再合并) 或 'auto' (直接自动合并)
const deliveryMode = computed({
  get: () => (draft.value.requireAcceptance ? 'manual' : 'auto'),
  set: (v: 'manual' | 'auto') => {
    draft.value.requireAcceptance = (v === 'manual');
  },
});

// 解析出的仓库 owner / name
const parsedRepo = computed(() => {
  return parseGitHubRepo(repoUrlInput.value);
});

// 概览摘要展示（以当前输入优先，若清空则显示未配置）
const summaryRepoText = computed(() => {
  const url = repoUrlInput.value.trim();
  const branch = mainBranchInput.value.trim() || props.project.mainBranchOverride || props.project.defaultBranch || 'main';
  if (!url) {
    return '尚未配置 GitHub 仓库';
  }
  const parsed = parsedRepo.value;
  if (!parsed.owner && !parsed.name) {
    return `仓库地址未识别 · 主分支 ${branch}`;
  }
  return `${parsed.owner}/${parsed.name} · 主分支 ${branch}`;
});

const summaryChecksText = computed(() => {
  const checks = checkItems.value.filter((c) => c.program.trim() !== '');
  let localPart = '未配置本地检查';
  if (checks.length > 0) {
    const first = checks[0];
    const fullCmd = [first.program, ...first.args].join(' ');
    localPart = `本地：${fullCmd}${checks.length > 1 ? ` 等 ${checks.length} 项` : ''}`;
  }
  let ciPart = 'CI：未配置 Actions';
  if (draft.value.taskWorkflows && draft.value.taskWorkflows.length > 0) {
    ciPart = `CI：已关联 ${draft.value.taskWorkflows.length} 项 workflow`;
  }
  return `${localPart} · ${ciPart}`;
});

const summaryRepairsText = computed(() => {
  return `最多自动修复 ${draft.value.maxRepairs || 3} 次 · 任务活跃超时 ${Math.round((draft.value.activeSeconds || 3600) / 3600)} 小时`;
});

async function loadPolicy() {
  const currentProjectId = props.project.id;
  loading.value = true;
  loadError.value = '';
  try {
    const res = await api<{ policy: ProjectDeliveryPolicy }>('delivery/policy?projectId=' + encodeURIComponent(currentProjectId));
    if (props.project.id !== currentProjectId) return;
    if (res && res.policy) {
      const p = res.policy;
      const isRevoked = !!p.revokedAt && !isZeroTime(p.revokedAt);
      const hasValidAuth = !!p.authorizedAt && !isZeroTime(p.authorizedAt);
      isExistingEnabled.value = !!p.enabled;
      draft.value = {
        projectId: p.projectId || currentProjectId,
        version: p.version || 0,
        enabled: !!p.enabled,
        authorized: false, // 无论是新开还是重新配置，启用/修改授权 scope 必须真实显式确认勾选
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

      loadedRepo.value = {
        id: p.repository?.id || 0,
        url: p.repository?.url || '',
        owner: p.repository?.owner || '',
        name: p.repository?.name || '',
      };

      repoUrlInput.value = p.repository?.url || '';
      mainBranchInput.value = p.repository?.mainBranch || props.project.mainBranchOverride || props.project.defaultBranch || 'main';
      checkItems.value = checksToUI(p.checks);
    } else {
      // 空白初始状态：不杜撰假数据
      draft.value = defaultPolicyDraft(currentProjectId);
      const defaultBranch = props.project.mainBranchOverride || props.project.defaultBranch || 'main';
      draft.value.repository.mainBranch = defaultBranch;
      mainBranchInput.value = defaultBranch;
      repoUrlInput.value = '';
      loadedRepo.value = { id: 0, url: '', owner: '', name: '' };
      checkItems.value = [];
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

function openWizard(step: number) {
  activeStep.value = step;
  localError.value = '';
}

function exitWizard() {
  syncWizardStepToDraft();
  activeStep.value = 0;
  localError.value = '';
}

function addCheck() {
  const nextId = generateNextCheckId(checkItems.value);
  checkItems.value.push({
    id: nextId,
    program: '',
    args: [],
    dir: '.',
    timeoutSeconds: 300,
  });
}

function removeCheck(idx: number) {
  checkItems.value.splice(idx, 1);
}

function addArg(check: CheckItemUI) {
  check.args.push('');
}

function removeArg(check: CheckItemUI, argIdx: number) {
  check.args.splice(argIdx, 1);
}

function syncWizardStepToDraft() {
  const rawUrl = repoUrlInput.value.trim();
  const targetBranch = mainBranchInput.value.trim() || props.project.mainBranchOverride || props.project.defaultBranch || 'main';

  if (!rawUrl) {
    // 仓库地址清空：真正清空 url, owner, name, id，保留目标主分支
    draft.value.repository.url = '';
    draft.value.repository.owner = '';
    draft.value.repository.name = '';
    draft.value.repository.id = 0;
    draft.value.repository.mainBranch = targetBranch;
  } else {
    draft.value.repository.url = rawUrl;
    const parsed = parseGitHubRepo(rawUrl);
    draft.value.repository.owner = parsed.owner || '';
    draft.value.repository.name = parsed.name || '';
    draft.value.repository.mainBranch = targetBranch;

    // 检查是否与原加载仓库等价（https 带/不带 .git，或者对应 SSH）
    if (loadedRepo.value.id > 0 && isEquivalentRepo(rawUrl, loadedRepo.value.url)) {
      draft.value.repository.id = loadedRepo.value.id;
    } else {
      // 仓库身份变更：清零旧 ID
      draft.value.repository.id = 0;
    }
  }

  // 同步 Step 2 检查项
  draft.value.checks = uiToChecks(checkItems.value);
}

function handleSaveDraft() {
  localError.value = '';

  if (loading.value || loadError.value) {
    localError.value = '配置尚未成功加载，无法保存草稿';
    return;
  }

  syncWizardStepToDraft();

  if (isExistingEnabled.value) {
    localError.value = '当前版本不支持在线直接修改已启用的交付策略；请先在服务端或联系管理员重置';
    return;
  }

  // 严格确保草稿为未启用、未授权状态
  draft.value.enabled = false;
  draft.value.authorized = false;

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
  <dialog
    ref="dialog"
    class="delivery-modal-dialog"
    aria-labelledby="modalTitle"
    data-testid="delivery-design-dialog"
    @cancel.prevent="!saving && emit('close')"
  >
    <div class="delivery-modal">

      <!-- 固定顶部 Header -->
      <div class="modal-header">
        <div class="title-group">
          <h2 id="modalTitle">自动交付</h2>
          <span class="project-badge">{{ project.name }} · 交付设置</span>
        </div>
        <button
          type="button"
          class="btn-close"
          :disabled="saving"
          @click="emit('close')"
          aria-label="关闭面板"
          title="关闭"
        >
          &times;
        </button>
      </div>

      <!-- 单一滚动 Body -->
      <div class="modal-body">

        <div v-if="loading" class="entry"><p>正在加载交付设置…</p></div>
        <div v-else-if="loadError" class="error-box" role="alert">{{ loadError }}</div>

        <!-- ================= 概览视图 (Overview) ================= -->
        <div v-else-if="activeStep === 0" class="overview-view" data-testid="overview">

          <!-- 简短用途说明（无四步流程图） -->
          <section class="purpose-box" aria-label="用途说明">
            <p class="purpose-lead">开发完成后，自动运行检查、推送至 GitHub 并由独立 CI 验证，按需合并入主分支。</p>
            <p class="purpose-note">这是代码交付设置，不是网站部署。</p>
          </section>

          <!-- 版本开放状态通知条 -->
          <div class="notice-banner" role="status">
            <span class="notice-icon">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="12" y1="8" x2="12" y2="12"></line>
                <line x1="12" y1="16" x2="12.01" y2="16"></line>
              </svg>
            </span>
            <span>当前版本暂未开放自动交付。可先保存配置草稿，项目仍可正常开发。</span>
          </div>

          <!-- 已启用只读警告 -->
          <div v-if="isExistingEnabled" class="alert-banner" role="alert">
            <span>本项目已在服务端启用自动交付。当前界面暂不支持直接改写已启用策略，以下仅供只读查阅。</span>
          </div>

          <!-- 常用：交付方式两项单选 -->
          <section aria-labelledby="deliveryModeTitle">
            <div id="deliveryModeTitle" class="section-title">
              <span>交付方式</span>
            </div>
            <div class="delivery-mode-group" role="radiogroup" aria-labelledby="deliveryModeTitle">
              <label
                class="mode-card"
                :class="{ selected: deliveryMode === 'manual' }"
                id="modeCardManual"
              >
                <input
                  type="radio"
                  name="deliveryMode"
                  value="manual"
                  v-model="deliveryMode"
                  :disabled="saving || isExistingEnabled"
                  class="mode-radio"
                />
                <div class="mode-content">
                  <div class="mode-headline">
                    <span>我先查看效果，再合并</span>
                    <span class="badge-recommend">推荐</span>
                  </div>
                  <div class="mode-desc">任务与 CI 检查通过后，由你在工作台中点击验收确认，再触发主分支合并与推送。</div>
                </div>
              </label>

              <label
                class="mode-card"
                :class="{ selected: deliveryMode === 'auto' }"
                id="modeCardAuto"
              >
                <input
                  type="radio"
                  name="deliveryMode"
                  value="auto"
                  v-model="deliveryMode"
                  :disabled="saving || isExistingEnabled"
                  class="mode-radio"
                />
                <div class="mode-content">
                  <div class="mode-headline">
                    <span>检查通过后直接合并</span>
                  </div>
                  <div class="mode-desc">所有本地与远程 CI 检查绿色通过后自动推入主分支；注：自动合并跳过人工点击，不代表已进行人工视觉验收。</div>
                </div>
              </label>
            </div>
          </section>

          <!-- 交付设置三行摘要 -->
          <section aria-labelledby="checklistTitle">
            <div id="checklistTitle" class="section-title">
              <span>交付设置</span>
              <span class="section-subtitle">点击右侧“调整”可分步修改</span>
            </div>
            <div class="checklist-card">
              <!-- 目标仓库行 -->
              <div class="check-row">
                <div class="check-left">
                  <div class="check-icon-box">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22"></path>
                    </svg>
                  </div>
                  <div class="check-info">
                    <div class="check-title">
                      <span>目标仓库</span>
                    </div>
                    <div class="check-detail" id="summaryRepoDetail">{{ summaryRepoText }}</div>
                  </div>
                </div>
                <button
                  type="button"
                  class="btn-text"
                  :disabled="isExistingEnabled"
                  @click="openWizard(1)"
                  data-testid="configure-repository"
                >
                  调整
                </button>
              </div>

              <!-- 通过标准行 -->
              <div class="check-row">
                <div class="check-left">
                  <div class="check-icon-box">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <polyline points="9 11 12 14 22 4"></polyline>
                      <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"></path>
                    </svg>
                  </div>
                  <div class="check-info">
                    <div class="check-title">
                      <span>通过标准</span>
                    </div>
                    <div class="check-detail" id="summaryCheckDetail">{{ summaryChecksText }}</div>
                  </div>
                </div>
                <button
                  type="button"
                  class="btn-text"
                  :disabled="isExistingEnabled"
                  @click="openWizard(2)"
                  data-testid="configure-checks"
                >
                  配置
                </button>
              </div>

              <!-- 失败处理行 -->
              <div class="check-row">
                <div class="check-left">
                  <div class="check-icon-box">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
                    </svg>
                  </div>
                  <div class="check-info">
                    <div class="check-title">失败处理</div>
                    <div class="check-detail" id="summaryRepairDetail">{{ summaryRepairsText }}</div>
                  </div>
                </div>
                <button
                  type="button"
                  class="btn-text"
                  :disabled="isExistingEnabled"
                  @click="openWizard(3)"
                  data-testid="configure-repair"
                >
                  调整
                </button>
              </div>
            </div>
          </section>

          <!-- 高级设置折叠 (默认收起) -->
          <details class="advanced-box">
            <summary>
              <span>高级技术参数与只读预检 (非必需)</span>
              <span class="collapse-icon">▼ 展开</span>
            </summary>
            <div class="advanced-content">
              <div class="adv-grid">
                <div class="adv-item">
                  <label>允许自动创建远程主分支</label>
                  <div class="adv-val">否 (默认保护已有主分支，不自动新建)</div>
                </div>
                <div class="adv-item">
                  <label>CI 触发等待超时</label>
                  <div class="adv-val">{{ draft.triggerSeconds || 300 }} 秒 (5 分钟)</div>
                </div>
                <div class="adv-item">
                  <label>CI 排队与运行最长等待</label>
                  <div class="adv-val">{{ draft.ciSeconds || 1800 }} 秒 (30 分钟)</div>
                </div>
                <div class="adv-item">
                  <label>GitHub 凭据绑定状态</label>
                  <div class="adv-val">服务端受控凭据 (未检查)</div>
                </div>
              </div>

              <!-- 只读预检区域 -->
              <div class="preflight-section">
                <div class="preflight-header">
                  <div>
                    <strong>只读预检</strong>
                    <span class="preflight-tip">（核对已保存至后端的交付配置，不验证当前未保存草稿）</span>
                  </div>
                  <button
                    type="button"
                    class="btn-secondary btn-sm"
                    :disabled="preflightLoading || saving"
                    @click="runPreflight"
                  >
                    {{ preflightLoading ? '预检核查中…' : '执行只读预检' }}
                  </button>
                </div>
                <div v-if="preflightReport" class="preflight-box">
                  <p :class="preflightReport.ready ? 'ready' : 'blocked'">
                    {{ preflightReport.ready ? '✓ 预检通过，已保存策略具备自动交付就绪状态' : '✕ 预检发现阻碍，请核查已保存配置：' }}
                  </p>
                  <ul v-if="preflightReport.blockers && preflightReport.blockers.length" class="blockers-list">
                    <li v-for="(b, i) in preflightReport.blockers" :key="i">{{ b }}</li>
                  </ul>
                </div>
              </div>

            </div>
          </details>

        </div>

        <!-- ================= 3步配置向导 (Wizard) ================= -->
        <div v-else class="wizard-view" data-testid="wizard-view">

          <!-- 向导导航头 -->
          <div class="wizard-nav">
            <div class="wizard-steps-indicator">
              <div class="wstep" :class="{ current: activeStep === 1, done: activeStep > 1 }">
                <span class="wstep-num">1</span>
                <span class="wstep-title">目标仓库</span>
              </div>
              <span class="wstep-sep">/</span>
              <div class="wstep" :class="{ current: activeStep === 2, done: activeStep > 2 }">
                <span class="wstep-num">2</span>
                <span class="wstep-title">通过标准</span>
              </div>
              <span class="wstep-sep">/</span>
              <div class="wstep" :class="{ current: activeStep === 3 }">
                <span class="wstep-num">3</span>
                <span class="wstep-title">失败处理</span>
              </div>
            </div>
            <button type="button" class="btn-text" @click="exitWizard">返回概览</button>
          </div>

          <!-- Step 1: 目标仓库 -->
          <div v-if="activeStep === 1" class="wizard-panel">
            <div class="form-group">
              <label for="repoUrlInput">
                <span>GitHub 仓库规范地址</span>
                <span class="hint">例如 https://github.com/owner/repo.git</span>
              </label>
              <input
                id="repoUrlInput"
                type="text"
                v-model="repoUrlInput"
                placeholder="https://github.com/owner/repo.git"
              />
              <div class="form-tip">提示：仅支持受信任的公共或自有 GitHub 规范地址，禁止附带任何账号密码令牌。</div>
            </div>

            <div class="parsed-repo-box">
              <div class="parsed-repo-row">
                <span>解析 Owner:</span>
                <span class="parsed-repo-val">{{ parsedRepo.owner || '—' }}</span>
              </div>
              <div class="parsed-repo-row">
                <span>解析 Name:</span>
                <span class="parsed-repo-val">{{ parsedRepo.name || '—' }}</span>
              </div>
            </div>

            <div class="form-group">
              <label for="branchOverride">
                <span>目标主分支</span>
                <span class="hint">当前项目默认分支为 {{ project.mainBranchOverride || project.defaultBranch || 'main' }}</span>
              </label>
              <input
                id="branchOverride"
                type="text"
                v-model="mainBranchInput"
                placeholder="例如 main"
              />
            </div>

            <div class="wizard-actions">
              <span></span>
              <button
                type="button"
                class="btn-primary"
                @click="openWizard(2)"
                data-testid="wizard-next"
              >
                下一步：配置通过标准 →
              </button>
            </div>
          </div>

          <!-- Step 2: 通过标准 -->
          <div v-if="activeStep === 2" class="wizard-panel">
            <div class="form-group">
              <div class="row between items-center">
                <label>
                  <span>本地检查命令 (argv 数组逐项安全传参，不使用 shell 拼接)</span>
                </label>
                <button type="button" class="btn-secondary btn-sm" @click="addCheck">+ 添加本地检查</button>
              </div>

              <div v-if="checkItems.length === 0" class="empty-hint">
                尚未配置任何本地检查。若启用自动交付至少需要配置 1 项。
              </div>

              <div v-for="(check, idx) in checkItems" :key="check.id" class="check-edit-card">
                <div class="check-edit-header">
                  <strong>检查项 #{{ idx + 1 }} ({{ check.id }})</strong>
                  <button type="button" class="btn-danger-text" @click="removeCheck(idx)">删除</button>
                </div>
                <div class="check-edit-fields">
                  <div class="form-group">
                    <label :for="'checkProgram_' + idx">程序/可执行文件</label>
                    <input
                      :id="'checkProgram_' + idx"
                      type="text"
                      v-model="check.program"
                      placeholder="例如 go 或 npm"
                    />
                  </div>

                  <!-- 逐项编辑参数 -->
                  <div class="args-section">
                    <div class="args-header">
                      <span class="args-label">命令参数 (逐项输入，支持任意空格/引号/制表符)</span>
                      <button type="button" class="btn-text btn-sm" @click="addArg(check)">+ 添加参数</button>
                    </div>

                    <div v-if="check.args.length === 0" class="args-empty">
                      无附加参数（仅执行该程序）
                    </div>

                    <div v-for="(arg, aIdx) in check.args" :key="aIdx" class="arg-row">
                      <span class="arg-idx">[{{ aIdx + 1 }}]</span>
                      <input
                        :id="'checkArg_' + idx + '_' + aIdx"
                        type="text"
                        v-model="check.args[aIdx]"
                        class="arg-input"
                        placeholder="例如 test、./... 或带空格/引号内容"
                      />
                      <button
                        type="button"
                        class="btn-danger-text btn-sm"
                        @click="removeArg(check, aIdx)"
                        title="删除该参数"
                      >
                        ✕
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div class="form-group">
              <label>
                <span>远程 GitHub Actions Workflow</span>
                <span class="hint">当前服务器元数据发现能力尚未开放，无法在线枚举 actions/workflows；已有配置将完整保留。</span>
              </label>
              <div class="notice-box-subtle">
                <p>暂无法在线自动获取云端 workflow 列表。现有任务将保留后端已存储的关联校验。</p>
              </div>
            </div>

            <div class="wizard-actions">
              <button
                type="button"
                class="btn-secondary"
                @click="openWizard(1)"
                data-testid="wizard-back"
              >
                ← 上一步
              </button>
              <button
                type="button"
                class="btn-primary"
                @click="openWizard(3)"
                data-testid="wizard-next"
              >
                下一步：失败处理与授权 →
              </button>
            </div>
          </div>

          <!-- Step 3: 失败处理与明确授权 -->
          <div v-if="activeStep === 3" class="wizard-panel">
            <div class="form-group">
              <label for="maxRepairs">最大自动修复次数</label>
              <select id="maxRepairs" v-model.number="draft.maxRepairs">
                <option :value="1">1 次 (严格控制额度)</option>
                <option :value="3">3 次 (推荐，兼顾成功率)</option>
                <option :value="5">5 次</option>
              </select>
            </div>

            <!-- 具体授权与风险提示摘要卡片 -->
            <div class="auth-summary-card">
              <h4>授权风险与执行确认</h4>
              <ul class="auth-item-list">
                <li class="auth-item">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"></polyline></svg>
                  <span>仅授权操作指定仓库：<strong id="authRepoName">{{ (parsedRepo.owner && parsedRepo.name) ? `${parsedRepo.owner}/${parsedRepo.name}` : (draft.repository.owner && draft.repository.name ? `${draft.repository.owner}/${draft.repository.name}` : '未指定仓库') }}</strong></span>
                </li>
                <li class="auth-item">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"></polyline></svg>
                  <span>任务分支命名为 <code>kaiwu/task-*</code>，本地检查通过后先上传用于 CI 运行</span>
                </li>
                <li class="auth-item">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"></polyline></svg>
                  <span>交付目标主分支为 <code id="authMainBranch">{{ mainBranchInput || 'main' }}</code>；若已配置持续部署，<strong>任务分支或主分支推送均可能触发已有自动化构建或发包</strong></span>
                </li>
                <li class="auth-item">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"></polyline></svg>
                  <span>执行过程遇到故障时，最多自动修复 <span id="authRepairTimes">{{ draft.maxRepairs || 3 }}</span> 次，防止无休止消耗额度</span>
                </li>
              </ul>
            </div>

            <div class="wizard-actions">
              <button
                type="button"
                class="btn-secondary"
                @click="openWizard(2)"
                data-testid="wizard-back"
              >
                ← 上一步
              </button>
              <button
                type="button"
                class="btn-primary"
                @click="exitWizard"
                data-testid="wizard-finish"
              >
                完成调整并返回概览
              </button>
            </div>
          </div>

        </div>

        <p v-if="localError || error" class="error-msg" role="alert">{{ localError || error }}</p>

      </div>

      <!-- 固定底部 Footer -->
      <div class="modal-footer">
        <div class="footer-left">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"></circle>
            <line x1="12" y1="16" x2="12" y2="12"></line>
            <line x1="12" y1="8" x2="12.01" y2="8"></line>
          </svg>
          <span>草稿只保存设置，不会启动交付</span>
        </div>
        <div class="footer-actions">
          <button
            type="button"
            class="btn-secondary"
            :disabled="saving"
            @click="emit('close')"
          >
            取消
          </button>
          <button
            type="button"
            class="btn-secondary"
            :disabled="saving || loading || !!loadError || isExistingEnabled"
            @click="handleSaveDraft"
            data-testid="save-draft"
          >
            {{ saving ? '保存中…' : '保存草稿' }}
          </button>
          <button
            type="button"
            class="btn-primary"
            disabled
            title="当前版本暂未开放自动交付"
            data-testid="activate-delivery"
          >
            启用自动交付
          </button>
        </div>
      </div>

    </div>
  </dialog>
</template>

<style scoped>
/* 模态弹窗对话框基础：与全局 dialog 85dvh 对齐，dialog 本身无外边框且不滚动，由内部 Body 单一滚动 */
.delivery-modal-dialog {
  padding: 0 !important;
  border: none !important;
  background: transparent !important;
  max-width: 760px;
  width: min(760px, calc(100vw - 30px));
  height: 85dvh;
  max-height: 85dvh;
  overflow: hidden !important;
  box-shadow: none !important;
}
.delivery-modal-dialog::backdrop {
  background: rgba(18, 26, 23, 0.45);
  backdrop-filter: blur(2px);
}

.delivery-modal {
  width: 100%;
  height: 100%;
  max-height: 100%;
  background: #ffffff;
  border: 1px solid #d4ded8;
  border-radius: 12px;
  box-shadow: 0 20px 48px rgba(18, 26, 23, 0.16);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  position: relative;
  font-family: inherit;
  color: #1a2420;
}

/* 固定 Header */
.modal-header {
  padding: 14px 20px;
  border-bottom: 1px solid #e1e8e4;
  display: flex;
  align-items: center;
  justify-content: space-between;
  background: #ffffff;
  flex-shrink: 0;
}
.title-group {
  display: flex;
  align-items: center;
  gap: 10px;
}
.modal-header h2 {
  font-size: 16px;
  font-weight: 600;
  margin: 0;
  color: #1a2420;
}
.project-badge {
  font-size: 12px;
  padding: 2px 8px;
  background: #edf3f0;
  color: #2d453b;
  border-radius: 4px;
}
.btn-close {
  background: transparent;
  border: none;
  font-size: 22px;
  line-height: 1;
  color: #6c7d75;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.btn-close:hover {
  background: #f0f4f2;
  color: #1a2420;
}

/* 单一滚动 Body */
.modal-body {
  padding: 16px 20px 20px;
  overflow-y: auto;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 14px;
  background: #fafbfb;
  min-height: 0;
}

/* 用途说明 */
.purpose-box {
  background: #ffffff;
  border: 1px solid #e1e8e4;
  border-radius: 8px;
  padding: 10px 12px;
}
.purpose-lead {
  font-size: 13px;
  color: #1a2420;
  margin: 0 0 3px 0;
  line-height: 1.5;
}
.purpose-note {
  font-size: 12px;
  color: #6c7d75;
  margin: 0;
}

/* 通知条 */
.notice-banner {
  background: #fffbf0;
  border: 1px solid #faecc5;
  border-radius: 8px;
  padding: 9px 12px;
  font-size: 12px;
  color: #8c6b1f;
  display: flex;
  align-items: center;
  gap: 10px;
  line-height: 1.4;
}
.notice-icon {
  display: flex;
  align-items: center;
  flex-shrink: 0;
}

.alert-banner {
  background: #fef2f2;
  border: 1px solid #fecaca;
  border-radius: 8px;
  padding: 9px 12px;
  font-size: 12px;
  color: #991b1b;
  line-height: 1.4;
}

/* 交付方式单选组 (防全局样式污染) */
.section-title {
  font-size: 13px;
  font-weight: 600;
  color: #1a2420;
  margin-bottom: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.section-subtitle {
  font-weight: normal;
  color: #6c7d75;
  font-size: 11px;
}
.delivery-mode-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.mode-card {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid #d4ded8;
  border-radius: 8px;
  background: #ffffff;
  cursor: pointer;
  transition: all 0.15s ease;
}
.mode-card:hover {
  border-color: #2d453b;
  background: #f7faf8;
}
.mode-card.selected {
  border-color: #2d453b;
  background: #edf3f0;
}
/* 严格隔离 radio 宽度与尺寸 */
.mode-card input[type="radio"].mode-radio {
  width: 16px !important;
  min-width: 16px !important;
  max-width: 16px !important;
  height: 16px !important;
  margin-top: 3px !important;
  cursor: pointer;
  accent-color: #2d453b;
  flex-shrink: 0;
}
.mode-content {
  flex: 1;
}
.mode-headline {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #1a2420;
  margin-bottom: 2px;
}
.badge-recommend {
  font-size: 11px;
  background: #e4ebe7;
  color: #2d453b;
  padding: 1px 6px;
  border-radius: 4px;
}
.mode-desc {
  font-size: 12px;
  color: #6c7d75;
  line-height: 1.4;
}

/* 交付设置三行摘要 */
.checklist-card {
  background: #ffffff;
  border: 1px solid #d4ded8;
  border-radius: 8px;
  overflow: hidden;
}
.check-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 12px;
  border-bottom: 1px solid #edf0ee;
}
.check-row:last-child {
  border-bottom: none;
}
.check-left {
  display: flex;
  align-items: center;
  gap: 10px;
  overflow: hidden;
}
.check-icon-box {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  background: #edf3f0;
  color: #2d453b;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}
.check-info {
  display: flex;
  flex-direction: column;
  gap: 1px;
  overflow: hidden;
}
.check-title {
  font-size: 13px;
  font-weight: 600;
  color: #1a2420;
}
.check-detail {
  font-size: 12px;
  color: #6c7d75;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 高级设置折叠 */
details.advanced-box {
  border: 1px solid #d4ded8;
  border-radius: 8px;
  background: #edf3f0;
  overflow: hidden;
}
details.advanced-box summary {
  padding: 9px 12px;
  font-size: 12px;
  font-weight: 600;
  color: #2d453b;
  cursor: pointer;
  user-select: none;
  list-style: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
details.advanced-box summary::-webkit-details-marker {
  display: none;
}
details.advanced-box summary:hover {
  background: #e4ebe7;
}
.collapse-icon {
  font-size: 11px;
  font-weight: normal;
}
.advanced-content {
  padding: 12px 14px;
  border-top: 1px solid #d4ded8;
  background: #ffffff;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.adv-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.adv-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.adv-item label {
  color: #6c7d75;
  font-size: 11px;
  font-weight: 500;
}
.adv-item .adv-val {
  font-size: 12px;
  color: #1a2420;
  padding: 5px 8px;
  background: #f7faf8;
  border: 1px solid #e1e8e4;
  border-radius: 4px;
}

/* 只读预检区域 */
.preflight-section {
  border-top: 1px solid #edf0ee;
  padding-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.preflight-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.preflight-tip {
  font-size: 11px;
  color: #6c7d75;
  font-weight: normal;
}
.preflight-box {
  margin-top: 4px;
  padding: 9px;
  background: #f7faf8;
  border: 1px solid #e1e8e4;
  border-radius: 6px;
  font-size: 12px;
}
.preflight-box p.ready {
  color: #2e7d32;
  margin: 0;
  font-weight: 500;
}
.preflight-box p.blocked {
  color: #c62828;
  margin: 0;
  font-weight: 500;
}
.blockers-list {
  margin: 6px 0 0 18px;
  padding: 0;
  color: #c62828;
}

/* 向导视图 */
.wizard-view {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.wizard-nav {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid #e1e8e4;
  padding-bottom: 10px;
  margin-bottom: 2px;
}
.wizard-steps-indicator {
  display: flex;
  align-items: center;
  gap: 8px;
}
.wstep {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #8c9b94;
}
.wstep.current {
  color: #2d453b;
  font-weight: 600;
}
.wstep.done {
  color: #2e7d32;
}
.wstep-num {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: #edf3f0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
}
.wstep.current .wstep-num {
  background: #2d453b;
  color: #ffffff;
}
.wstep.done .wstep-num {
  background: #e8f5e9;
  color: #2e7d32;
}
.wstep-sep {
  color: #c0ccc5;
}
.wizard-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.form-group {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.form-group label {
  font-size: 12px;
  font-weight: 600;
  color: #1a2420;
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.form-group .hint {
  font-weight: normal;
  color: #6c7d75;
  font-size: 11px;
}
.form-group input[type="text"],
.form-group select {
  padding: 7px 10px;
  border: 1px solid #d4ded8;
  border-radius: 6px;
  font-size: 13px;
  background: #ffffff;
  color: #1a2420;
}
.form-group input[type="text"]:focus,
.form-group select:focus {
  outline: none;
  border-color: #2d453b;
  box-shadow: 0 0 0 2px rgba(45, 69, 59, 0.15);
}
.form-tip {
  font-size: 11px;
  color: #6c7d75;
  line-height: 1.4;
}

.parsed-repo-box {
  background: #f7faf8;
  border: 1px solid #e1e8e4;
  border-radius: 6px;
  padding: 8px 10px;
  display: flex;
  gap: 18px;
  font-size: 12px;
}
.parsed-repo-row {
  display: flex;
  gap: 6px;
}
.parsed-repo-val {
  font-weight: 600;
  color: #2d453b;
}

.check-edit-card {
  background: #ffffff;
  border: 1px solid #d4ded8;
  border-radius: 6px;
  padding: 10px 12px;
  margin-top: 6px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.check-edit-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: #1a2420;
}
.check-edit-fields {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.args-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  background: #fafbfb;
  border: 1px solid #e8eee9;
  border-radius: 6px;
}
.args-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.args-label {
  font-size: 11px;
  font-weight: 600;
  color: #455a50;
}
.args-empty {
  font-size: 11px;
  color: #8c9b94;
  font-style: italic;
  padding: 2px 0;
}
.arg-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.arg-idx {
  font-size: 11px;
  color: #8c9b94;
  font-family: monospace;
  width: 24px;
  text-align: right;
  flex-shrink: 0;
}
.arg-input {
  flex: 1;
  padding: 5px 8px !important;
  font-size: 12px !important;
  font-family: monospace;
}

.notice-box-subtle {
  background: #f7faf8;
  border: 1px solid #e1e8e4;
  border-radius: 6px;
  padding: 8px 10px;
  font-size: 12px;
  color: #6c7d75;
}
.notice-box-subtle p {
  margin: 0;
}

.empty-hint {
  font-size: 12px;
  color: #8c9b94;
  padding: 6px 0;
}

/* 授权卡片 */
.auth-summary-card {
  background: #ffffff;
  border: 1px solid #d4ded8;
  border-radius: 8px;
  padding: 12px;
}
.auth-summary-card h4 {
  margin: 0 0 8px 0;
  font-size: 13px;
  color: #1a2420;
}
.auth-item-list {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.auth-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12px;
  color: #1a2420;
  line-height: 1.4;
}
.auth-item svg {
  color: #2e7d32;
  margin-top: 2px;
  flex-shrink: 0;
}

.wizard-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 6px;
  padding-top: 10px;
  border-top: 1px solid #e1e8e4;
}

/* 底部 Footer */
.modal-footer {
  padding: 12px 20px;
  border-top: 1px solid #e1e8e4;
  background: #ffffff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}
.footer-left {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #6c7d75;
}
.footer-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 按钮通用 */
button {
  font-family: inherit;
  cursor: pointer;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
  transition: all 0.15s ease;
}
.btn-primary {
  background: #2d453b;
  color: #ffffff;
  border: 1px solid #243830;
  padding: 7px 14px;
}
.btn-primary:hover:not(:disabled) {
  background: #243830;
}
.btn-primary:disabled {
  background: #d4ded8;
  border-color: #c0ccc5;
  color: #8c9b94;
  cursor: not-allowed;
}
.btn-secondary {
  background: #ffffff;
  color: #1a2420;
  border: 1px solid #d4ded8;
  padding: 7px 14px;
}
.btn-secondary:hover:not(:disabled) {
  background: #f0f4f2;
}
.btn-text {
  background: transparent;
  color: #2d453b;
  border: none;
  padding: 4px 8px;
  font-weight: 600;
}
.btn-text:hover {
  background: #edf3f0;
}
.btn-danger-text {
  background: transparent;
  color: #c62828;
  border: none;
  font-size: 11px;
  padding: 2px 6px;
}
.btn-danger-text:hover {
  background: #ffebee;
}
.btn-sm {
  padding: 3px 8px;
  font-size: 11px;
}

.error-msg {
  color: #c62828;
  font-size: 12px;
  margin: 4px 0 0 0;
}
.error-box {
  background: #ffebee;
  border: 1px solid #ffcdd2;
  color: #c62828;
  padding: 9px 12px;
  border-radius: 6px;
  font-size: 12px;
}

/* 响应式断点支持 (390x844 & 360x800 移动端无横向溢出) */
@media (max-width: 640px) {
  .delivery-modal-dialog {
    width: 100vw !important;
    max-width: 100vw !important;
    height: 100dvh !important;
    max-height: 100dvh !important;
    border-radius: 0 !important;
  }
  .delivery-modal {
    max-width: 100%;
    height: 100dvh;
    max-height: 100dvh;
    border-radius: 0;
    border: none;
  }
  .modal-header {
    padding: 12px 16px;
  }
  .modal-body {
    padding: 12px 16px 18px;
  }
  .modal-footer {
    padding: 10px 16px;
    flex-direction: column;
    align-items: stretch;
    gap: 8px;
  }
  .footer-left {
    justify-content: center;
    order: 2;
    font-size: 11px;
  }
  .footer-actions {
    width: 100%;
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 8px;
    order: 1;
  }
  .footer-actions button {
    min-height: 38px;
    width: 100%;
  }
  .adv-grid {
    grid-template-columns: 1fr;
  }
  .wstep-title {
    display: none;
  }
}
</style>

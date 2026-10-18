import type { DeliverySummary, DeliveryStep, DeliveryCI, ProjectDeliveryPolicy } from "./types.ts";

export interface DeliveryView {
  active: boolean;
  phase: string;
  label: string;
  description: string;
  blocked: boolean;
  paused: boolean;
  canAccept: boolean;
  canPause: boolean;
  canResume: boolean;
  canCancel: boolean;
  canExtendBudget: boolean;
  ciFailed: boolean;
  repairsText: string;
  repairsUsed: number;
  repairsLimit: number;
  blockCode?: string;
  blockReason?: string;
  candidateSHA?: string;
  localMainSHA?: string;
  remoteMainSHA?: string;
  holdsTurn: boolean;
  queueStatusText?: string;
  steps: DeliveryStep[];
  ciRuns: DeliveryCI[];
}

const PHASE_LABELS: Record<string, string> = {
  queued: "排队中",
  developing: "开发中",
  local_checking: "本地检查中",
  committing: "生成提交中",
  pushing_task: "推送任务分支中",
  task_ci: "任务 CI 中",
  preparing_integration: "准备集成中",
  awaiting_acceptance: "待验收并交付",
  integrating: "合并到主分支中",
  integration_checking: "集成检查中",
  pushing_main: "推送到主分支中",
  main_ci: "主分支 CI 中",
  repairing: "自动修复中",
  blocked: "交付受阻",
  paused: "已暂停",
  needs_check: "待核对状态",
  succeeded: "技术交付成功",
  cancelled: "已取消",
};

export function isZeroTime(iso?: string): boolean {
  if (!iso || !iso.trim()) return true;
  return iso.startsWith("0001-01-01");
}

export function isSafeGitHubURL(raw?: string): boolean {
  if (!raw) return false;
  try {
    const parsed = new URL(raw);
    if (parsed.protocol !== "https:") return false;
    if (parsed.hostname !== "github.com") return false;
    if (parsed.username || parsed.password) return false;
    return true;
  } catch {
    return false;
  }
}

export function filterSafeCI(ciList?: DeliveryCI[]): DeliveryCI[] {
  if (!ciList) return [];
  return ciList.filter((ci) => isSafeGitHubURL(ci.url));
}

export function canAcceptDelivery(summary?: DeliverySummary): boolean {
  if (!summary) return false;
  if (summary.phase !== "awaiting_acceptance") return false;
  if (!summary.requireAcceptance) return false;
  if (!summary.acceptanceToken || !summary.acceptanceToken.trim()) return false;
  if (!summary.candidateSHA) return false;
  return true;
}

export function isDeliveryActive(phase?: string): boolean {
  switch (phase) {
    case "queued":
    case "developing":
    case "local_checking":
    case "committing":
    case "pushing_task":
    case "task_ci":
    case "preparing_integration":
    case "awaiting_acceptance":
    case "integrating":
    case "integration_checking":
    case "pushing_main":
    case "main_ci":
    case "repairing":
    case "blocked":
    case "paused":
    case "needs_check":
      return true;
    default:
      return false;
  }
}

export function canPauseDelivery(phase?: string): boolean {
  if (!phase || !isDeliveryActive(phase)) return false;
  if (phase === "blocked" || phase === "paused") return false;
  return true;
}

export function buildAcceptPayload(
  operationId: string,
  deliveryId: string,
  token: string,
  expectedVersion: number,
) {
  return {
    operationId,
    deliveryId,
    token,
    expectedVersion,
  };
}

export function buildControlPayload(
  operationId: string,
  deliveryId: string,
  action: "pause" | "resume" | "cancel" | "extend_budget",
  expectedVersion: number,
) {
  return {
    operationId,
    deliveryId,
    action,
    expectedVersion,
  };
}

export interface PolicyDraft {
  projectId: string;
  version: number;
  enabled: boolean;
  authorized: boolean;
  requireAcceptance: boolean;
  allowCreateMain: boolean;
  repository: {
    id: number;
    owner: string;
    name: string;
    url: string;
    mainBranch: string;
  };
  checks: Array<{
    id: string;
    argv: string[];
    dir: string;
    timeoutSeconds: number;
  }>;
  taskWorkflows: Array<{
    id: number;
    jobs: string[];
  }>;
  mainWorkflows: Array<{
    id: number;
    jobs: string[];
  }>;
  maxRepairs: number;
  activeSeconds: number;
  triggerSeconds: number;
  ciSeconds: number;
  gitCredentialRef: string;
  apiCredentialRef: string;
  authorizationDigest: string;
  actor: string;
  authorizedAt: string;
  revokedAt?: string;
}

export function defaultPolicyDraft(projectId: string): PolicyDraft {
  return {
    projectId,
    version: 0,
    enabled: false,
    authorized: false,
    requireAcceptance: true,
    allowCreateMain: false,
    repository: {
      id: 0,
      owner: "",
      name: "",
      url: "",
      mainBranch: "main",
    },
    checks: [],
    taskWorkflows: [],
    mainWorkflows: [],
    maxRepairs: 3,
    activeSeconds: 3600,
    triggerSeconds: 300,
    ciSeconds: 1800,
    gitCredentialRef: "",
    apiCredentialRef: "",
    authorizationDigest: "",
    actor: "kaiwu_web",
    authorizedAt: "",
  };
}

export function buildSavePolicyPayload(
  operationId: string,
  draft: PolicyDraft,
): {
  operationId: string;
  expectedVersion: number;
  policy: ProjectDeliveryPolicy;
} {
  const policy: ProjectDeliveryPolicy = {
    projectId: draft.projectId,
    version: draft.version,
    enabled: draft.enabled,
    requireAcceptance: draft.requireAcceptance,
    allowCreateMain: draft.allowCreateMain,
    repository: {
      id: Number(draft.repository.id),
      owner: draft.repository.owner.trim(),
      name: draft.repository.name.trim(),
      url: draft.repository.url.trim(),
      mainBranch: draft.repository.mainBranch.trim(),
    },
    checks: draft.checks,
    taskWorkflows: draft.taskWorkflows,
    mainWorkflows: draft.mainWorkflows,
    maxRepairs: Number(draft.maxRepairs),
    activeSeconds: Number(draft.activeSeconds),
    triggerSeconds: Number(draft.triggerSeconds),
    ciSeconds: Number(draft.ciSeconds),
    gitCredentialRef: draft.gitCredentialRef,
    apiCredentialRef: "server_github_api",
    authorizationDigest: draft.authorized
      ? draft.authorizationDigest || `auth_${draft.projectId}_${Date.now()}`
      : "",
    actor: draft.actor || "kaiwu_web",
    authorizedAt: draft.authorized
      ? !isZeroTime(draft.authorizedAt)
        ? draft.authorizedAt
        : new Date().toISOString()
      : "",
  };

  return {
    operationId,
    expectedVersion: draft.version,
    policy,
  };
}

export function validatePolicyDraft(draft: PolicyDraft): string | null {
  if (draft.maxRepairs < 1 || draft.maxRepairs > 100) {
    return "自动修复上限需在 1–100 次之间";
  }

  // Repository URL credentials check: reject even when disabled so token doesn't leak
  if (draft.repository && draft.repository.url && draft.repository.url.trim()) {
    const raw = draft.repository.url.trim();
    if (raw.includes("@") && !raw.startsWith("git@github.com:") && !raw.startsWith("ssh://git@github.com/")) {
      return "仓库地址不能包含用户名或凭据";
    }
    try {
      if (raw.startsWith("http://") || raw.startsWith("https://")) {
        const u = new URL(raw);
        if (u.username || u.password) {
          return "仓库地址不能包含凭据";
        }
      }
    } catch {
      return "仓库地址格式不正确";
    }
  }

  if (!draft.enabled) {
    return null;
  }
  if (!draft.authorized) {
    return "启用前需明确勾选确认授权范围与外部触发副作用";
  }
  if (!draft.repository.url || !draft.repository.url.trim()) {
    return "请填写规范的 GitHub 仓库地址";
  }
  if (!draft.repository.owner || !draft.repository.name) {
    return "请填写完整的 GitHub 仓库 owner/name";
  }
  if (!draft.repository.id || draft.repository.id <= 0) {
    return "请填写有效的 GitHub repository ID（可在预检中自动获取）";
  }
  if (!draft.repository.mainBranch || !draft.repository.mainBranch.trim()) {
    return "请填写目标主分支名称";
  }
  if (!Array.isArray(draft.checks) || draft.checks.length === 0) {
    return "启用自动交付至少需要配置 1 项本地检查";
  }
  for (const c of draft.checks) {
    if (!c || typeof c !== "object") return "本地检查项无效";
    if (!c.id || !c.id.trim()) return "本地检查标识不能为空";
    if (!Array.isArray(c.argv) || c.argv.length === 0) {
      return `检查 ${c.id} 的命令参数必须是非空数组`;
    }
    for (const arg of c.argv) {
      if (typeof arg !== "string" || arg.trim() === "") {
        return `检查 ${c.id} 的每个参数都必须是有效字符串`;
      }
    }
    if (!c.dir || typeof c.dir !== "string" || c.dir.startsWith("../") || c.dir.includes("/../")) {
      return `检查 ${c.id} 的目录必须位于工作区内`;
    }
    if (!c.timeoutSeconds || c.timeoutSeconds <= 0 || c.timeoutSeconds > 604800) {
      return `检查 ${c.id} 的超时时间无效`;
    }
  }
  if (!Array.isArray(draft.taskWorkflows) || draft.taskWorkflows.length === 0) {
    return "请至少配置 1 个任务分支必需 workflow";
  }
  if (!Array.isArray(draft.mainWorkflows) || draft.mainWorkflows.length === 0) {
    return "请至少配置 1 个主分支必需 workflow";
  }
  for (const w of [...draft.taskWorkflows, ...draft.mainWorkflows]) {
    if (!w || typeof w !== "object" || !w.id || w.id <= 0) return "workflow ID 必须是正整数";
    if (w.jobs && !Array.isArray(w.jobs)) return "workflow jobs 必须是数组";
  }
  if (
    draft.gitCredentialRef !== "server_github_https" &&
    draft.gitCredentialRef !== "server_github_ssh"
  ) {
    return "Git 凭据引用必须是 server_github_https 或 server_github_ssh";
  }
  if (draft.apiCredentialRef !== "server_github_api") {
    return "API 凭据引用必须是 server_github_api";
  }
  return null;
}

export function deliveryView(summary?: DeliverySummary): DeliveryView {
  if (!summary || !summary.phase) {
    return {
      active: false,
      phase: "",
      label: "未启用",
      description: "当前任务尚未配置或启动自动交付。",
      blocked: false,
      paused: false,
      canAccept: false,
      canPause: false,
      canResume: false,
      canCancel: false,
      canExtendBudget: false,
      ciFailed: false,
      repairsText: "",
      repairsUsed: 0,
      repairsLimit: 3,
      holdsTurn: false,
      steps: [],
      ciRuns: [],
    };
  }

  const phase = summary.phase;
  const active = isDeliveryActive(phase);
  const blocked = phase === "blocked";
  const paused = phase === "paused";
  const canAccept = canAcceptDelivery(summary);
  const canPause = canPauseDelivery(phase);
  const canResume = paused;
  const canCancel = active && phase !== "succeeded" && phase !== "cancelled";
  const canExtendBudget = blocked;

  const safeCI = filterSafeCI(summary.ci);
  const ciFailed = (summary.ci ?? []).some(
    (c) => c.status === "completed" && c.conclusion === "failure",
  );

  const limit = summary.repairsLimit > 0 ? summary.repairsLimit : 3;
  const used = summary.repairsUsed ?? 0;
  const repairsText = `${used}/${limit} 次修复`;

  const label = PHASE_LABELS[phase] ?? phase;

  let description = "";
  if (phase === "awaiting_acceptance") {
    description = "本地检查与任务 CI 已通过，请验收实际效果并确认交付。";
  } else if (blocked) {
    description = summary.blockReason || "交付因门禁或限制受阻，需人工干预。";
  } else if (paused) {
    description = "交付流程已暂停。发送新消息前请在此恢复或重试。";
  } else if (phase === "repairing") {
    description = `检测到问题，正在进行第 ${used} 次自动修复…`;
  } else if (phase === "succeeded") {
    description = summary.requireAcceptance
      ? "代码已合并到主分支并通过全部门禁，技术交付完成。"
      : "代码已合并到主分支并通过全部门禁（未人工验收模式），技术交付完成。";
  } else if (phase === "cancelled") {
    description = "自动交付已取消（远程已推送分支不会撤销）。";
  } else {
    description = `交付流程进行中：${label}`;
  }

  let queueStatusText: string | undefined;
  if (summary.holdsTurn) {
    queueStatusText = "当前正在独占集成队列执行中";
  }

  return {
    active,
    phase,
    label,
    description,
    blocked,
    paused,
    canAccept,
    canPause,
    canResume,
    canCancel,
    canExtendBudget,
    ciFailed,
    repairsText,
    repairsUsed: used,
    repairsLimit: limit,
    blockCode: summary.blockCode,
    blockReason: summary.blockReason,
    candidateSHA: summary.candidateSHA,
    localMainSHA: summary.localMainSHA,
    remoteMainSHA: summary.remoteMainSHA,
    holdsTurn: summary.holdsTurn,
    queueStatusText,
    steps: summary.steps ?? [],
    ciRuns: safeCI,
  };
}

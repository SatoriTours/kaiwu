import { httpAPI } from "./api.ts";

export interface DeploymentInfo {
  revision: string;
  status: "testing" | "building" | "deploying" | "succeeded" | "failed" | "unknown";
  at: string;
  message: string;
}

export interface VersionInfo {
  revision: string;
  shortRevision: string;
  summary: string;
  builtAt: string;
  deployedAt: string;
  deployment: DeploymentInfo;
}

/**
 * Checks if a string looks like a valid git hexadecimal revision (7 to 40 hex chars).
 */
export function isValidSHA(sha?: string): boolean {
  if (!sha || typeof sha !== "string") return false;
  return /^[0-9a-f]{7,40}$/i.test(sha);
}

/**
 * Formats running revision for topbar button display.
 * Dev builds show "开发版本", empty/unknown show "版本未知", valid commit returns shortRevision or 7-char prefix.
 */
export function formatRevision(revision?: string, shortRevision?: string): string {
  const rev = (revision ?? "").trim();
  const shortRev = (shortRevision ?? "").trim();

  if (rev === "dev" || shortRev === "dev") {
    return "开发版本";
  }
  if (shortRev) {
    return shortRev;
  }
  if (rev) {
    return rev.length > 7 ? rev.slice(0, 7) : rev;
  }
  return "版本未知";
}

/**
 * Checks if the frontend bundle's build revision conflicts with the backend running revision.
 * Only triggers if BOTH frontend and backend have valid commit SHAs and they do not match.
 * Dev builds without SHA or backend dev builds never trigger a false mismatch warning.
 */
export function isRevisionMismatch(frontendSHA?: string, backendSHA?: string): boolean {
  const front = (frontendSHA ?? "").trim().toLowerCase();
  const back = (backendSHA ?? "").trim().toLowerCase();

  if (!isValidSHA(front) || !isValidSHA(back)) {
    return false;
  }

  // Prefix match (e.g. 7-char short vs 40-char full) or exact match
  if (back.startsWith(front) || front.startsWith(back)) {
    return false;
  }

  return true;
}

/**
 * Safely formats an RFC3339 UTC date-time string to a readable representation.
 */
export function formatDateTime(isoString?: string): string {
  if (!isoString || typeof isoString !== "string" || !isoString.trim()) {
    return "未知";
  }
  const date = new Date(isoString.trim());
  if (Number.isNaN(date.getTime())) {
    return isoString.trim();
  }
  const pad = (n: number) => String(n).padStart(2, "0");
  const y = date.getFullYear();
  const m = pad(date.getMonth() + 1);
  const d = pad(date.getDate());
  const h = pad(date.getHours());
  const min = pad(date.getMinutes());
  const s = pad(date.getSeconds());
  return `${y}-${m}-${d} ${h}:${min}:${s}`;
}

export interface FormattedDeploymentStatus {
  label: string;
  isFailed: boolean;
  fallbackMessage: string;
}

/**
 * Formats deployment status and ensures failure does not mask current version or lack honest wording.
 */
export function formatDeploymentStatus(
  status?: string,
  message?: string,
): FormattedDeploymentStatus {
  const normalized = (status ?? "").trim().toLowerCase();
  const fallbackMessage = "部署未完成，请核对当前版本/服务状态";

  switch (normalized) {
    case "failed":
      return {
        label: "失败",
        isFailed: true,
        fallbackMessage,
      };
    case "succeeded":
      return {
        label: "成功",
        isFailed: false,
        fallbackMessage,
      };
    case "testing":
      return {
        label: "测试中",
        isFailed: false,
        fallbackMessage,
      };
    case "building":
      return {
        label: "构建中",
        isFailed: false,
        fallbackMessage,
      };
    case "deploying":
      return {
        label: "部署中",
        isFailed: false,
        fallbackMessage,
      };
    default:
      return {
        label: "未知",
        isFailed: false,
        fallbackMessage,
      };
  }
}

/**
 * Fetches version info from backend via authenticated HTTP GET /api/version (no-store).
 */
export async function fetchVersionInfo(
  fetcher: <T>(path: string) => Promise<T> = httpAPI,
): Promise<VersionInfo> {
  return fetcher<VersionInfo>("version");
}

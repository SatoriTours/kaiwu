// Read-only presentation of a merge operation's three independent facts:
// the local integration, the remote upload, and CI. The server owns the values;
// this module only refuses to collapse them into one "delivered" claim.
import type { MergeOperation } from "./types.ts";

export type MergeFactState =
  | "not_requested"
  | "pending"
  | "done"
  | "failed"
  | "unknown";

export interface MergeFact {
  state: MergeFactState;
  label: string;
  sha?: string;
}

export interface MergeFacts {
  local: MergeFact;
  remote: MergeFact;
  ci: MergeFact;
}

const notRequested: MergeFact = { state: "not_requested", label: "未请求" };

function localFact(op: MergeOperation): MergeFact {
  const label = (state: MergeFactState, text: string, sha?: string): MergeFact => ({ state, label: text, sha });
  switch (op.phase) {
    case "queued":
      return label("pending", "等待本地集成");
    case "preparing":
      return label("pending", "核对工作区");
    case "committing":
      return label("pending", "正在提交任务修改");
    case "committed":
      return label("pending", "任务已提交，正在合并主分支", op.taskAfterSHA);
    case "merging":
      return label("pending", "正在合并主分支", op.taskAfterSHA);
    case "merged":
      return label("done", "主分支已合并", op.mainAfterSHA);
    case "pushing":
    case "remote_done":
    case "succeeded":
      return label("done", "主分支已合并", op.mainAfterSHA);
    case "needs_check":
      // Neither a success nor a failure is proven: never claim the merge.
      return label("unknown", "本地合并结果需要核查", op.mainAfterSHA);
    case "failed":
      // A failed phase keeps the local proof when main already moved.
      return op.mainAfterSHA
        ? label("done", "主分支已合并", op.mainAfterSHA)
        : label("failed", op.error || "本地合并未完成");
    default:
      return label("unknown", "本地合并状态未知");
  }
}

function remoteFact(op: MergeOperation): MergeFact {
  if (op.mode !== "push" && !op.push) return notRequested;
  if (op.remoteAfterSHA) return { state: "done", label: "已上传到远程主分支", sha: op.remoteAfterSHA };
  switch (op.phase) {
    case "pushing":
      return { state: "pending", label: "正在上传远程主分支" };
    case "needs_check":
      return { state: "unknown", label: "远程上传结果需要核查，不会自动重发" };
    case "failed":
      return op.pushAttemptedAt
        ? { state: "failed", label: op.error || "远程上传未完成", sha: op.intendedSHA }
        : { state: "failed", label: op.error || "本地合并未完成，未上传" };
    default:
      return op.pushAttemptedAt
        ? { state: "unknown", label: "远程上传回执未确认，需只读核查" }
        : { state: "pending", label: "本地合并完成后上传", sha: op.intendedSHA };
  }
}

function ciFact(op: MergeOperation): MergeFact {
  switch (op.ciStatus) {
    case "passed":
    case "success":
      return { state: "done", label: "CI 已通过" };
    case "failed":
    case "failure":
      return { state: "failed", label: "CI 失败" };
    case "pending":
    case "running":
      return { state: "pending", label: "CI 运行中" };
    case "not_checked":
    default:
      return { state: "not_requested", label: "CI 未检查" };
  }
}

export function mergeFacts(op: MergeOperation | undefined): MergeFacts | undefined {
  if (!op) return undefined;
  return { local: localFact(op), remote: remoteFact(op), ci: ciFact(op) };
}

/** The terminal phases a user can retry or close from. */
export function mergeFinished(phase: string | undefined): boolean {
  return phase === "succeeded" || phase === "failed" || phase === "needs_check";
}

/** One line telling the user what the chosen mode actually does. */
export function mergeChoiceHint(mode: string): string {
  switch (mode) {
    case "push_only":
      return "只上传已审核的提交范围；不创建提交、不合并、不打标签。";
    case "push":
      return "先本地合并，再上传精确的主分支提交；CI／部署需另行核查。";
    default:
      return "只在本机合并，代码留在本机，不上传任何提交。";
  }
}

export function mergePhaseLabel(phase: string | undefined): string {
  return (
    (
      {
        queued: "等待集成",
        preparing: "核对工作区",
        committing: "正在提交任务修改",
        committed: "任务已提交",
        merging: "正在合并主分支",
        merged: "主分支已合并",
        pushing: "正在上传远程主分支",
        remote_done: "远程已上传",
        succeeded: "操作完成",
        failed: "操作未完成",
        needs_check: "结果需要核查",
      } as Record<string, string>
    )[phase ?? ""] ?? (phase || "未知状态")
  );
}

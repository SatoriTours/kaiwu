import type { GitStatus } from "./types.ts";
export function isMergeRequest(text: string): boolean {
  return (
    /^[ \t]*(?:请)?(?:提交并)?合并到主分支[。！!.]*[ \t]*$/.test(text) &&
    !/[\r\n]/.test(text)
  );
}
export function gitStatusLabel(status?: GitStatus): string {
  return (
    (
      {
        not_started: "未开始",
        no_changes: "无改动",
        dirty: "待提交",
        unmerged: "未合并",
        merged: "已合并",
        unknown: "需核查",
      } as Record<string, string>
    )[status?.state ?? ""] ?? "检查中"
  );
}

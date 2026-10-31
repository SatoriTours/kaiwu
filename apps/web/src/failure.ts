import type { Conversation, Execution } from "./types.ts";
import type { ChatEntry } from "./chat-messages.ts";

export interface FailureView {
  kind: "send" | "execution" | "session" | "unknown";
  text: string;
  detail: string;
  at?: string;
  placement: "user" | "assistant";
  retry: boolean;
}
type ConversationLike = Pick<Conversation, "runState" | "error"> & Partial<Pick<Conversation, "messages" | "rounds">>;
type ExecutionLike = Pick<Execution, "phase" | "error">;
const FAILED = ["failed", "interrupted"];
const CLOSED = ["answered", "finished", "confirmed", "awaiting_user"];

function reason(text: string): string {
  if (!text || /^(pi 会话已中断|服务已重启|消息未能确认完成|模型调用失败或被中断；已有内容保留$)/.test(text)) return "";
  return text.replace(/^模型调用失败；已有内容保留：/, "").replace(/^模型切换失败，本轮请求未发送：/, "").trim();
}

/** A notice requires evidence about THIS turn; a disconnected CLI is not one. */
export function failureView(
  conversation: ConversationLike,
  execution?: ExecutionLike,
  entry?: ChatEntry,
): FailureView | undefined {
  if (!entry) return undefined;
  const user = entry.role === "user" ? entry.message : entry.userMessage;
  if (!user) return undefined;
  const round = entry.round ?? conversation.rounds?.find(r => r.id === user.id);
  if (round && CLOSED.includes(round.phase)) return undefined;
  const latestUser = conversation.messages?.filter(m => m.role === "user").at(-1);
  const isLatest = latestUser?.id === user.id;
  const persisted = round?.failure;
  const failedRound = !!round && FAILED.includes(round.phase);
  // A new running round must not inherit a previous conversation-level error.
  if (!persisted && round && !failedRound) return undefined;
  const legacyFailure = !round && isLatest && FAILED.includes(conversation.runState) &&
    (["pending", "accepted", "failed", "uncertain", "not_sent"].includes(user.delivery) ||
      (entry.role === "assistant" && ["failed", "uncertain"].includes(entry.message.delivery)));
  if (!persisted && !failedRound && !legacyFailure) return undefined;
  const text = persisted?.detail ?? (isLatest ? conversation.error || execution?.error || "" : "");
  let kind: FailureView["kind"] = persisted?.kind ??
    (/消息未能确认完成|未能发送|本轮请求未发送/.test(text) ? "send" :
      /模型调用失败/.test(text) ? "execution" :
      (round?.phase === "interrupted" || conversation.runState === "interrupted") ? "session" : "unknown");
  const definitelyUnsent = user.delivery === "not_sent";
  if (definitelyUnsent) kind = "send";
  const send = kind === "send";
  const detail = reason(text);
  let label = kind === "session" ? "回复已中断" : "本轮未完成";
  if (send) label = definitelyUnsent ? "未发送" : "发送状态待确认";
  else if (detail) {
    if (/(?:\b429\b|速率限制|rate.?limit)/i.test(detail)) label = "本次请求受到限流，请稍后再试。";
    else if (/(?:\b401\b|登录.*失效|凭据.*过期)/i.test(detail)) label = "模型登录已失效，请检查服务器登录。";
    else {
      const points = Array.from(detail);
      label = points.length > 90 ? points.slice(0, 89).join("") + "…" : detail;
    }
  }
  return {
    kind, text: label,
    // No expander for a generic connection/status sentence.
    detail: send && !detail ? (definitelyUnsent ? "" : "尚未确认消息是否送达。不会自动重发，请核对本轮记录后再决定下一步。") : detail,
    at: round?.finishedAt, placement: send ? "user" : "assistant", retry: definitelyUnsent,
  };
}

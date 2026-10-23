import type { Conversation, Execution } from "./types.ts";

// One reading of a stored failure for the chat surface. The backend keeps the
// full text in conversation.error (and mirrors it to execution.error); pi's own
// provider reason sits at the END of a long fixed prefix, so the reader must
// never be left with only the prefix or a collapsed generic sentence.
export type FailureKind = "send" | "execution" | "session" | "unknown";

export interface FailureView {
  kind: FailureKind;
  title: string;
  /** Bounded inline reason; empty when pi reported none. */
  reason: string;
  hasReason: boolean;
  /** Full stored text, only when it adds something to the inline reason. */
  detail: string;
  hint: string;
  /** True when an earlier development result survives this failure. */
  resultPreserved: boolean;
}

const EXECUTION_PREFIX = "模型调用失败；已有内容保留：";
const EXECUTION_FALLBACK = "模型调用失败或被中断；已有内容保留";
const MODEL_SWITCH_PREFIX = "模型切换失败，本轮请求未发送：";
// Long page-sized reasons must stay readable inline: keep the head AND the tail
// so the actionable sentence at the end is never hidden behind an ellipsis.
const INLINE_REASON_LIMIT = 200;

const SEND_HINT = "消息未确认送达；再次发送会尝试接回原会话，历史已保留。";
const RECONNECT_HINT = "发送下一条消息会尝试自动接回原会话；已有记录和结果保留。";

type ConversationLike = Pick<Conversation, "runState" | "error">;
type ExecutionLike = Pick<Execution, "phase" | "error">;

/** Chat failure notice, or undefined while a round is running or healthy. */
export function failureView(
  conversation: ConversationLike,
  execution?: ExecutionLike,
): FailureView | undefined {
  // Only a settled failure may speak: a running or idle round must never show
  // the previous round's reason (switching tasks inherits nothing either).
  if (!["failed", "interrupted"].includes(conversation.runState)) return undefined;
  const stored = (conversation.error ?? "").trim();
  const fromExecution = (execution?.error ?? "").trim();
  const text = stored || fromExecution;
  const kind = failureKind(text, execution);
  const reason = inlineReason(text, kind);
  return {
    kind,
    title: TITLES[kind],
    reason,
    hasReason: reason !== "",
    detail: text !== reason ? text : "",
    hint: kind === "send" ? SEND_HINT : RECONNECT_HINT,
    resultPreserved: ["awaiting_review", "confirmed"].includes(execution?.phase ?? ""),
  };
}

const TITLES: Record<FailureKind, string> = {
  send: "消息发送失败",
  execution: "执行失败",
  session: "会话已中断",
  unknown: "本轮未完成",
};

function failureKind(text: string, execution?: ExecutionLike): FailureKind {
  // Shape first, then the stored text: a send failure must never be reported as
  // an execution failure just because the task already had an execution.
  if (text.includes("消息未能确认完成") || text.includes("未能发送")) return "send";
  if (text.includes("模型调用失败") || execution?.phase === "failed") return "execution";
  if (text.includes("会话已中断") || text.includes("接回原会话")) return "session";
  return "unknown";
}

function inlineReason(text: string, kind: FailureKind): string {
  if (!text) return "";
  // Status sentences already say what the title and hint say; keep them in the
  // expandable detail instead of repeating them as a "cause".
  if (kind === "send" || kind === "session") return "";
  if (text === EXECUTION_FALLBACK) return "";
  let reason = text;
  if (reason.startsWith(EXECUTION_PREFIX)) reason = reason.slice(EXECUTION_PREFIX.length);
  else if (reason.startsWith(EXECUTION_FALLBACK))
    reason = reason.slice(EXECUTION_FALLBACK.length).replace(/^[；;：:\s]+/, "");
  else if (reason.startsWith(MODEL_SWITCH_PREFIX))
    reason = reason.slice(MODEL_SWITCH_PREFIX.length);
  reason = reason.trim();
  if (!reason) return "";
  const points = Array.from(reason);
  if (points.length <= INLINE_REASON_LIMIT) return reason;
  const head = points.slice(0, Math.ceil((INLINE_REASON_LIMIT - 1) / 2));
  const tail = points.slice(points.length - (INLINE_REASON_LIMIT - 1 - head.length));
  return head.join("") + "…" + tail.join("");
}

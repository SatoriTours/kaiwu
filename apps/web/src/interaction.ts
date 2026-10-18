import type {
  InteractionRequest,
  InteractionField,
  InteractionResponse,
  InteractionDelivery,
  InteractionStatus,
} from "./types.ts";

export type {
  InteractionRequest,
  InteractionField,
  InteractionResponse,
  InteractionDelivery,
  InteractionStatus,
};

export type InteractionAnswers = Record<string, boolean | string | string[] | number>;
export type InteractionOthers = Record<string, string>;

export interface ValidationResult {
  valid: boolean;
  errors: Record<string, string>;
}

export interface InteractionDraft {
  mode: "form" | "text";
  answers: InteractionAnswers;
  others: InteractionOthers;
  otherFlags: Record<string, boolean>;
  comment: string;
  text: string;
}

/**
 * Counts Unicode runes/code points accurately.
 */
export function countRunes(str: string): number {
  return Array.from(str).length;
}

export function isExpired(expiresAt?: string, now = Date.now()): boolean {
  if (!expiresAt) return false;
  const time = Date.parse(expiresAt);
  if (Number.isNaN(time)) return false;
  return now >= time;
}

/**
 * Validates user answers strictly aligned with backend rules:
 * - single_select with custom other: field is omitted from answers, others[fieldId] contains non-empty text.
 * - single_select with option: answers[fieldId] is valid option id, others[fieldId] must NOT be present.
 * - multi_select with custom other: answers[fieldId] contains ONLY valid option ids (no "other" pseudo-id). others[fieldId] provides the other text.
 * - false and 0 are preserved.
 * - allowEmpty: true allows empty string even if touched or required.
 */
export function validateInteractionAnswers(
  request: InteractionRequest,
  answers: InteractionAnswers,
  others: InteractionOthers,
  comment?: string,
  now = Date.now(),
): ValidationResult {
  const errors: Record<string, string> = {};

  if (isExpired(request.expiresAt, now)) {
    errors._global = "该请求已超时失效，无法提交答复";
    return { valid: false, errors };
  }

  if (comment !== undefined && comment !== null && comment !== "") {
    if (countRunes(comment) > 2000) {
      errors._comment = "补充说明不能超过 2000 个字符";
    }
  }

  for (const field of request.fields) {
    const hasAnswer = Object.prototype.hasOwnProperty.call(answers, field.id);
    const val = answers[field.id];
    const otherVal = others[field.id];
    const hasOther = Object.prototype.hasOwnProperty.call(others, field.id) && otherVal !== undefined && otherVal !== "";

    switch (field.type) {
      case "confirm": {
        if (!hasAnswer || typeof val !== "boolean") {
          errors[field.id] = `请选择${field.label || "是否允许"}`;
        }
        break;
      }

      case "text":
      case "textarea": {
        if (hasOther) {
          errors[field.id] = "文本字段不支持填写“其他”";
          break;
        }
        const maxLimit = field.type === "text" ? 2000 : 5000;
        const max = field.maxLength !== undefined ? field.maxLength : maxLimit;
        const min = field.minLength !== undefined ? field.minLength : 0;

        if (!hasAnswer || val === undefined || val === null) {
          if (field.required && !field.allowEmpty) {
            errors[field.id] = `请填写${field.label}`;
          }
        } else {
          if (typeof val !== "string") {
            errors[field.id] = `${field.label}必须是文本`;
          } else {
            const runes = countRunes(val);
            if (field.required && !field.allowEmpty && val.trim() === "") {
              errors[field.id] = `请填写${field.label}`;
            } else if (val !== "" || !field.allowEmpty) {
              if (runes < min) {
                errors[field.id] = `${field.label}长度不能少于 ${min} 个字符`;
              } else if (runes > max) {
                errors[field.id] = `${field.label}长度不能超过 ${max} 个字符`;
              }
            }
          }
        }
        break;
      }

      case "single_select": {
        const optionChosen = hasAnswer && typeof val === "string" && val !== "";
        if (optionChosen && hasOther) {
          errors[field.id] = `${field.label}不能同时选择预设选项和填写其他`;
          break;
        }

        if (hasOther) {
          if (!field.allowOther) {
            errors[field.id] = "该字段不支持自定义选项";
          } else {
            const trimmed = otherVal.trim();
            if (!trimmed) {
              errors[field.id] = `请填写“其他”的具体说明`;
            } else if (countRunes(trimmed) > 2000) {
              errors[field.id] = `“其他”说明不能超过 2000 个字符`;
            }
          }
        } else if (optionChosen) {
          const opt = field.options?.find((o) => o.id === val);
          if (!opt) {
            errors[field.id] = "请选择列表中的有效选项";
          }
        } else if (field.required) {
          errors[field.id] = `请选择${field.label}`;
        }
        break;
      }

      case "multi_select": {
        const arr = Array.isArray(val) ? (val as string[]) : [];
        if (hasAnswer && !Array.isArray(val)) {
          errors[field.id] = `${field.label}选项格式无效`;
          break;
        }

        // Check for duplicates and unknown option ids
        const seen = new Set<string>();
        let hasInvalidOption = false;
        for (const optId of arr) {
          if (seen.has(optId)) {
            errors[field.id] = `${field.label}存在重复选择项`;
            hasInvalidOption = true;
            break;
          }
          seen.add(optId);
          if (!field.options?.some((o) => o.id === optId)) {
            errors[field.id] = `${field.label}包含未知选项`;
            hasInvalidOption = true;
            break;
          }
        }
        if (hasInvalidOption) break;

        if (hasOther) {
          if (!field.allowOther) {
            errors[field.id] = "该字段不支持自定义选项";
            break;
          }
          const trimmed = otherVal.trim();
          if (!trimmed) {
            errors[field.id] = `选择“其他”时需填写具体说明`;
            break;
          }
          if (countRunes(trimmed) > 2000) {
            errors[field.id] = `“其他”说明不能超过 2000 个字符`;
            break;
          }
        }

        const totalSelected = arr.length + (hasOther ? 1 : 0);
        const min = field.minSelections !== undefined ? field.minSelections : (field.required ? 1 : 0);
        const max = field.maxSelections !== undefined ? field.maxSelections : (field.options?.length ?? 20) + (field.allowOther ? 1 : 0);

        if (totalSelected < min) {
          errors[field.id] = `至少需要选择 ${min} 项`;
        } else if (totalSelected > max) {
          errors[field.id] = `最多可选择 ${max} 项`;
        }
        break;
      }

      case "number": {
        if (!hasAnswer || val === undefined || val === null || val === "") {
          if (field.required) {
            errors[field.id] = `请填写${field.label}`;
          }
        } else {
          const num = typeof val === "number" ? val : Number(val);
          if (Number.isNaN(num) || !Number.isFinite(num)) {
            errors[field.id] = `${field.label}必须是有效数字`;
          } else if (field.integer && !Number.isInteger(num)) {
            errors[field.id] = `${field.label}必须是整数`;
          } else if (field.min !== undefined && num < field.min) {
            errors[field.id] = `${field.label}不能小于 ${field.min}`;
          } else if (field.max !== undefined && num > field.max) {
            errors[field.id] = `${field.label}不能大于 ${field.max}`;
          }
        }
        break;
      }
    }
  }

  return {
    valid: Object.keys(errors).length === 0,
    errors,
  };
}

export function validateTextFallback(text: string, request?: InteractionRequest, now = Date.now()): ValidationResult {
  if (request && isExpired(request.expiresAt, now)) {
    return {
      valid: false,
      errors: { text: "该请求已超时失效，无法提交答复" },
    };
  }
  const trimmed = text ? text.trim() : "";
  if (!trimmed) {
    return {
      valid: false,
      errors: { text: "文字替代回答不能为空" },
    };
  }
  if (countRunes(trimmed) > 5000) {
    return {
      valid: false,
      errors: { text: "文字替代回答不能超过 5000 个字符" },
    };
  }
  return { valid: true, errors: {} };
}

/**
 * Splits the interaction list into the currently answerable queue and read-only
 * history. A pending request past its deadline is history: it stays visible
 * but can no longer be answered.
 */
export function splitInteractions(
  interactions?: InteractionRequest[],
  now = Date.now(),
): { pending: InteractionRequest[]; history: InteractionRequest[] } {
  const pending: InteractionRequest[] = [];
  const history: InteractionRequest[] = [];
  for (const ix of interactions ?? []) {
    if (ix.status === "pending" && !isExpired(ix.expiresAt, now)) pending.push(ix);
    else history.push(ix);
  }
  return { pending, history };
}

/** The status a user should see right now (a pending request can be expired). */
export function effectiveInteractionStatus(
  request: InteractionRequest,
  now = Date.now(),
): InteractionStatus {
  if (request.status === "pending" && isExpired(request.expiresAt, now)) return "expired";
  return request.status;
}

/**
 * Builds the answers payload for submission. Native input/editor fields are
 * required but may legitimately be empty; an untouched field must be sent as
 * an explicit empty string, otherwise the backend rejects the native answer.
 * false and 0 are copied unchanged.
 */
export function normalizeSubmissionAnswers(
  request: InteractionRequest,
  answers: InteractionAnswers,
): InteractionAnswers {
  const out: InteractionAnswers = {};
  for (const [key, value] of Object.entries(answers)) {
    out[key] = Array.isArray(value) ? [...value] : value;
  }
  for (const field of request.fields) {
    if (
      (field.type === "text" || field.type === "textarea") &&
      field.required &&
      field.allowEmpty &&
      !(field.id in out)
    ) {
      out[field.id] = "";
    }
  }
  return out;
}

export function getPendingInteractions(
  interactions?: InteractionRequest[],
  now = Date.now(),
): InteractionRequest[] {
  if (!interactions || !Array.isArray(interactions)) return [];
  return interactions.filter((ix) => ix.status === "pending" && !isExpired(ix.expiresAt, now));
}

export function hasBlockingPendingInteraction(
  interactions?: InteractionRequest[],
  now = Date.now(),
): boolean {
  if (!interactions || !Array.isArray(interactions)) return false;
  return interactions.some((ix) => ix.status === "pending" && ix.blocking && !isExpired(ix.expiresAt, now));
}

export function getInteractionDraftKey(
  conversationId: string,
  requestId: string,
  version: number,
): string {
  return `kaiwu_ix_draft:${conversationId}:${requestId}:${version}`;
}

export function loadInteractionDraft(
  conversationId: string,
  requestId: string,
  version: number,
): InteractionDraft | null {
  try {
    if (typeof sessionStorage === "undefined") return null;
    const raw = sessionStorage.getItem(getInteractionDraftKey(conversationId, requestId, version));
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    return {
      mode: parsed.mode === "text" ? "text" : "form",
      answers: typeof parsed.answers === "object" && parsed.answers !== null ? parsed.answers : {},
      others: typeof parsed.others === "object" && parsed.others !== null ? parsed.others : {},
      otherFlags: typeof parsed.otherFlags === "object" && parsed.otherFlags !== null ? parsed.otherFlags : {},
      comment: typeof parsed.comment === "string" ? parsed.comment : "",
      text: typeof parsed.text === "string" ? parsed.text : "",
    };
  } catch {
    return null;
  }
}

export function saveInteractionDraft(
  conversationId: string,
  requestId: string,
  version: number,
  draft: InteractionDraft,
): void {
  try {
    if (typeof sessionStorage === "undefined") return;
    sessionStorage.setItem(
      getInteractionDraftKey(conversationId, requestId, version),
      JSON.stringify(draft),
    );
  } catch {
    // Ignore storage quota errors
  }
}

export function clearInteractionDraft(
  conversationId: string,
  requestId: string,
  version: number,
): void {
  try {
    if (typeof sessionStorage === "undefined") return;
    sessionStorage.removeItem(getInteractionDraftKey(conversationId, requestId, version));
  } catch {
    // Ignore
  }
}

export function clearInteractionDrafts(): void {
  try {
    if (typeof sessionStorage === "undefined") return;
    const keys: string[] = [];
    for (let i = 0; i < sessionStorage.length; i++) {
      const k = sessionStorage.key(i);
      if (k && k.startsWith("kaiwu_ix_draft:")) {
        keys.push(k);
      }
    }
    for (const k of keys) {
      sessionStorage.removeItem(k);
    }
  } catch {
    // Ignore
  }
}

export function formatAnswerSummary(req: InteractionRequest): string {
  const resp = req.response;
  if (!resp) return "尚未答复";
  if (resp.mode === "cancel") return "已取消本次请求";
  if (resp.mode === "text") return resp.text ? `文字替代答复：${resp.text}` : "文字替代答复";

  const parts: string[] = [];
  const answers = resp.answers ?? {};
  const others = resp.others ?? {};

  for (const field of req.fields) {
    const val = answers[field.id];
    const otherVal = others[field.id];
    const label = field.label || field.id;

    if (field.type === "confirm") {
      if (typeof val === "boolean") {
        const text = val ? field.yesLabel || "允许" : field.noLabel || "不允许";
        parts.push(`${label}: ${text}`);
      }
    } else if (field.type === "single_select") {
      if (otherVal) {
        parts.push(`${label}: 其他（${otherVal}）`);
      } else if (val) {
        const opt = field.options?.find((o) => o.id === val);
        parts.push(`${label}: ${opt?.label || val}`);
      }
    } else if (field.type === "multi_select") {
      const names: string[] = [];
      if (Array.isArray(val) && val.length > 0) {
        for (const id of val) {
          const opt = field.options?.find((o) => o.id === id);
          names.push(opt?.label || id);
        }
      }
      if (otherVal) {
        names.push(`其他（${otherVal}）`);
      }
      if (names.length > 0) {
        parts.push(`${label}: ${names.join("、")}`);
      }
    } else if (val !== undefined && val !== null && val !== "") {
      const unit = field.unit ? ` ${field.unit}` : "";
      parts.push(`${label}: ${val}${unit}`);
    }
  }

  if (resp.comment) {
    parts.push(`补充说明: ${resp.comment}`);
  }

  return parts.length > 0 ? parts.join("；") : "已提交答复";
}

export const INTERACTION_DELIVERY_LABELS: Record<InteractionDelivery, string> = {
  recorded: "已持久记录",
  dispatching: "发送中…",
  accepted: "已发送给 AI",
  not_sent: "未发送",
  uncertain: "送达待核查",
};

export const INTERACTION_STATUS_LABELS: Record<InteractionStatus, string> = {
  pending: "待你处理",
  answered: "已答复",
  cancelled: "已取消",
  expired: "已失效",
  superseded: "已由新版本替代",
};

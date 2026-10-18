import type { Receipt, Conversation } from "./types.ts";
import { clearInteractionDraft } from "./interaction.ts";

export interface InteractionCommandPayload {
  type: "answer_interaction" | "cancel_interaction" | "retry_interaction_answer";
  projectId: string;
  conversationId: string;
  taskId: string;
  requestId: string;
  requestVersion: number;
  answerMode?: "form" | "text";
  answers?: Record<string, unknown>;
  others?: Record<string, string>;
  comment?: string;
  text?: string;
}

export interface DispatchContext {
  authEpoch: number;
  getCurrentEpoch: () => number;
  online: boolean;
  createOperationId: () => string;
  operations: Map<string, { id: string; signature: string }>;
  localPending: Record<string, boolean>;
  api: <T = unknown>(path: string, payload?: unknown) => Promise<T>;
  refresh: () => Promise<void>;
  report: (e: unknown) => void;
  setError: (msg: string) => void;
  getConversation: (conversationId: string) => Conversation | undefined;
}

/**
 * Executes interaction command dispatch safely with idempotency tracking:
 * - Detects duplicate in-flight dispatch (same scope + identical payload)
 * - Detects unconfirmed conflict (same scope + different payload)
 * - Queries operations/:id on status 0 (network partition/unknown delivery)
 * - Handles 404 (operation never reached server) vs status 0
 * - Only clears local draft when receipt/operation is definitively accepted/successful
 */
export async function dispatchInteractionCommand(
  ctx: DispatchContext,
  payload: InteractionCommandPayload,
): Promise<{ success: boolean; receipt?: Receipt; notice?: string }> {
  const { authEpoch, getCurrentEpoch, online, operations, localPending, api, refresh, report, setError, getConversation } = ctx;

  if (!online) {
    setError("网络已断开，无法发送交互操作");
    return { success: false };
  }

  const { conversationId, requestId, requestVersion, type } = payload;
  const scope = `ix_cmd:${type}:${requestId}:${requestVersion}`;
  const signature = JSON.stringify(payload);

  let reuseId = "";
  const existing = operations.get(scope);
  if (existing) {
    if (existing.signature !== signature) {
      setError("上一交互操作尚未确认，请勿修改内容并重试");
      return { success: false };
    }
    // Same signature is already in-flight or awaiting reconciliation.
    localPending[conversationId] = true;
    try {
      const receipt = await api<Receipt>("operations/" + existing.id);
      if (getCurrentEpoch() !== authEpoch) return { success: false };
      operations.delete(scope);
      applyReceipt(getConversation(conversationId), receipt);
      clearInteractionDraft(conversationId, requestId, requestVersion);
      await refresh();
      return { success: true, receipt };
    } catch (e: any) {
      if (getCurrentEpoch() !== authEpoch) return { success: false };
      if (e?.status === 404) {
        // The server has no receipt for this exact operation yet. Keep the
        // SAME operation identity and let this explicit user retry re-send it:
        // an unknown-but-late first request is deduplicated by operationId
        // plus fingerprint instead of applying a second, different identity.
        reuseId = existing.id;
      } else if (e?.status === 0) {
        setError("网络连通仍不稳定，无法确认上一操作结果；不会自动重发");
        return { success: false };
      } else {
        // A definitive rejection has no side effect to reconcile.
        operations.delete(scope);
        report(e);
        return { success: false };
      }
    } finally {
      if (getCurrentEpoch() === authEpoch) localPending[conversationId] = false;
    }
  }

  const operationId = reuseId || ctx.createOperationId();
  if (!reuseId) operations.set(scope, { id: operationId, signature });
  localPending[conversationId] = true;
  setError("");

  try {
    const receipt = await api<Receipt>("commands", {
      ...payload,
      operationId,
    });
    if (getCurrentEpoch() !== authEpoch) return { success: false };
    operations.delete(scope);
    applyReceipt(getConversation(conversationId), receipt);
    // Definitive server success: safe to remove draft
    clearInteractionDraft(conversationId, requestId, requestVersion);
    if (receipt.notice) setError(receipt.notice);
    await refresh();
    return { success: true, receipt, notice: receipt.notice };
  } catch (e: any) {
    if (getCurrentEpoch() !== authEpoch) return { success: false };
    if (e?.status === 0) {
      // Network interruption: check operations/:id immediately
      try {
        const receipt = await api<Receipt>("operations/" + operationId);
        if (getCurrentEpoch() !== authEpoch) return { success: false };
        operations.delete(scope);
        applyReceipt(getConversation(conversationId), receipt);
        clearInteractionDraft(conversationId, requestId, requestVersion);
        await refresh();
        return { success: true, receipt };
      } catch {
        if (getCurrentEpoch() !== authEpoch) return { success: false };
        setError("答复送达结果未知，已保留输入草稿，请恢复网络后核实或点击重试");
        return { success: false };
      }
    } else {
      operations.delete(scope);
      report(e);
      return { success: false };
    }
  } finally {
    if (getCurrentEpoch() === authEpoch) localPending[conversationId] = false;
  }
}

function applyReceipt(conv: Conversation | undefined, receipt: Receipt | undefined) {
  if (!conv || !receipt || !receipt.interaction) return;
  if (!conv.interactions) {
    conv.interactions = [receipt.interaction];
    return;
  }
  const idx = conv.interactions.findIndex((ix) => ix.id === receipt.interaction!.id);
  if (idx >= 0) {
    conv.interactions[idx] = receipt.interaction;
  } else {
    conv.interactions.push(receipt.interaction);
  }
}

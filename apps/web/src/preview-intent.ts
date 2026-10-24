// Unified in-page preview intent.
//
// One intent per user gesture: it owns the generation (monotonic), the blank tab
// it pre-opened, and whether that tab has been navigated. Selection / project /
// task / latest-round / auth changes invalidate the whole generation, so a late
// receipt can never revive a navigation — even after A → B → A returns to the
// same scope values. Nothing here depends on the result panel being rendered.
//
// This is intentionally a small helper for the preview flow, not a scheduler.

export type PreviewScope = { projectId: string; taskId: string; roundId: string };
export type PreviewGesture = "open" | "start";
export type PreviewStartState = "inflight" | "unknown" | "succeeded" | "failed";

export type PreviewStartDecision =
  /** No receipt for this operation yet: issue the write. */
  | { action: "write"; operationId: string }
  /** The same write is already in flight: wait, never duplicate it. */
  | { action: "wait"; operationId: string }
  /** Unknown receipt: reconcile read-only; never write again for this key. */
  | { action: "blocked"; reason: "unknown"; operationId: string };
export type PreviewBlank = { closed: boolean; close(): void; location?: { href: string } };

export type PreviewIntent<B extends PreviewBlank = PreviewBlank> = {
  id: number;
  gesture: PreviewGesture;
  scope: PreviewScope;
  key: string;
  /** Epoch snapshot: invalidated as soon as the page scope changes. */
  epoch: number;
  blank?: B | null;
  navigated: boolean;
};

export type PreviewIntentState<B extends PreviewBlank = PreviewBlank> = {
  /** Monotonic invalidation counter; compared with each intent's generation. */
  epoch: number;
  generation: number;
  current?: PreviewIntent<B>;
  lastCompleted?: { key: string; at: number };
  starts: Map<string, { operationId: string; state: PreviewStartState }>;
  blankOpener?: (() => B | null | undefined) | undefined;
  now: () => number;
  completedDedupeMs: number;
  createOperationId: () => string;
};

export type PreviewIntentOptions<B extends PreviewBlank = PreviewBlank> = {
  now?: () => number;
  completedDedupeMs?: number;
  createOperationId: () => string;
  openBlank?: () => B | null | undefined;
};

// Same-command/completed-gesture dedupe window. It only needs to cover rapid
// repeat clicks (acceptance: 120ms / 250ms); later explicit clicks must work.
export const PREVIEW_DEDUPE_MS = 700;

/**
 * One preview target per scope: the auto open path, the explicit open entry and
 * the panel's Start all address the same preview of the same round, so they must
 * share in-flight and just-completed dedupe instead of racing each other.
 */
export function previewIntentKey(scope: PreviewScope): string {
  return `${scope.projectId}:${scope.taskId}:${scope.roundId}:preview`;
}

export function createPreviewIntent<B extends PreviewBlank = PreviewBlank>(
  options: PreviewIntentOptions<B>,
): PreviewIntentState<B> {
  return {
    epoch: 0,
    generation: 0,
    current: undefined,
    lastCompleted: undefined,
    starts: new Map(),
    blankOpener: options.openBlank,
    now: options.now ?? (() => Date.now()),
    completedDedupeMs: options.completedDedupeMs ?? PREVIEW_DEDUPE_MS,
    createOperationId: options.createOperationId,
  };
}

function closeOwnBlank(intent: PreviewIntent<PreviewBlank>): void {
  const blank = intent.blank;
  if (!blank || intent.navigated || blank.closed) return;
  try {
    blank.close();
  } catch {}
}

/**
 * Start (or reject) a gesture. Returns the new intent, or undefined when the
 * gesture must be ignored: an identical request is still in flight, or the same
 * gesture just completed inside the short repeat window.
 */
export function beginPreviewIntent<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  input: { scope: PreviewScope; gesture?: PreviewGesture },
): PreviewIntent<B> | undefined {
  const gesture = input.gesture ?? "open";
  const key = previewIntentKey(input.scope);
  // Only one gesture may own a blank tab at a time. A repeat click while the
  // first request is still in flight is ignored instead of pre-opening a second.
  const active = state.current;
  if (active) return;
  // The same gesture that just completed (and navigated) is ignored for a short
  // window; a later explicit click must still work, so this never locks.
  const last = state.lastCompleted;
  if (last && last.key === key && state.now() - last.at < state.completedDedupeMs) return;

  closeOwnBlankOfPrevious(state, active);
  let blank: B | null | undefined;
  try {
    blank = state.blankOpener ? state.blankOpener() : undefined;
  } catch {
    blank = undefined;
  }
  // The intent remembers the invalidation counter it belongs to; any later
  // invalidation advances the epoch and makes this intent permanently stale.
  state.generation += 1;
  const intent: PreviewIntent<B> = {
    id: state.generation,
    gesture,
    scope: { ...input.scope },
    key,
    epoch: state.epoch,
    blank,
    navigated: false,
  };
  state.current = intent;
  return intent;
}

function closeOwnBlankOfPrevious<B extends PreviewBlank>(state: PreviewIntentState<B>, previous?: PreviewIntent<B>): void {
  if (!previous) return;
  closeOwnBlank(previous);
  previous.blank = undefined;
}

/** True only when this intent is still the live one for the current scope values. */
export function isPreviewIntentCurrent<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  intent: PreviewIntent<B> | undefined,
  currentScope: PreviewScope,
  response?: { taskId?: string; roundId?: string },
): boolean {
  if (!intent || intent !== state.current) return false;
  // Any invalidation since the gesture started makes the intent stale, even if
  // the scope values happen to look the same again (A → B → A).
  if (intent.epoch !== state.epoch) return false;
  if (intent.scope.projectId !== currentScope.projectId) return false;
  if (intent.scope.taskId !== currentScope.taskId) return false;
  if (intent.scope.roundId !== currentScope.roundId) return false;
  if (response) {
    if (response.taskId !== undefined && response.taskId !== intent.scope.taskId) return false;
    if (response.roundId !== undefined && response.roundId !== intent.scope.roundId) return false;
  }
  return true;
}

/** The intent navigated *its own* reserved blank, so it is no longer closable. */
export function markPreviewNavigated(intent: PreviewIntent<PreviewBlank>): void {
  intent.navigated = true;
}

export function releasePreviewIntent<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  intent: PreviewIntent<B> | undefined,
): void {
  if (!intent) return;
  closeOwnBlank(intent);
  intent.blank = undefined;
  if (state.current && state.current.id === intent.id) {
    state.current = undefined;
    // Only a non-stale intent records the repeat window; an invalidated gesture
    // must not temporarily block the next legitimate click.
    if (intent.epoch === state.epoch && intent.navigated) {
      state.lastCompleted = { key: intent.key, at: state.now() };
    }
  }
}

/**
 * Selection / project / task / latest round / auth changed, or the component is
 * unmounting: increment the generation and cancel the current intent. Safe to
 * call repeatedly and safe when no result panel is open.
 */
export function invalidatePreviewIntent<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  _reason?: string,
): void {
  const active = state.current;
  state.epoch += 1;
  state.generation += 1;
  state.current = undefined;
  state.lastCompleted = undefined;
  if (active) {
    closeOwnBlank(active);
    active.blank = undefined;
  }
}

/** Read the current start state without creating or consuming an operation. */
export function peekPreviewStart<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  key: string,
): PreviewStartState | undefined {
  return state.starts.get(key)?.state;
}

/** Explicit Stop succeeded: the completed gesture no longer blocks reopening. */
export function clearPreviewCompletion<B extends PreviewBlank>(state: PreviewIntentState<B>): void {
  state.lastCompleted = undefined;
}

/** Auth change or unmount: this login's start receipts are not reusable. */
export function clearPreviewStarts<B extends PreviewBlank>(state: PreviewIntentState<B>): void {
  state.starts.clear();
}

/**
 * Starting a preview command writes to the server. Keep one operationId per
 * (scope, manifest) until it is resolved; while the receipt is unknown no
 * further write may be issued, not even with the same id, because status carries
 * no operation receipt and cannot prove the first write never ran.
 */
export function previewStartOperation<B extends PreviewBlank>(state: PreviewIntentState<B>, key: string): PreviewStartDecision {
  const entry = state.starts.get(key);
  if (!entry) {
    const operationId = state.createOperationId();
    state.starts.set(key, { operationId, state: "inflight" });
    return { action: "write", operationId };
  }
  if (entry.state === "inflight") return { action: "wait", operationId: entry.operationId };
  // Unknown, or a status check that could not prove the original write never
  // executed: stay blocked on the original id and require read-only status.
  if (entry.state === "unknown") return { action: "blocked", reason: "unknown", operationId: entry.operationId };
  // A settled attempt (the service reported it started, or explicitly rejected
  // it before doing any work) is over. A new explicit gesture — e.g. Stop then
  // start again — is a new operation with a new id.
  entry.operationId = state.createOperationId();
  entry.state = "inflight";
  return { action: "write", operationId: entry.operationId };
}

/**
 * Settle exactly the operation that produced this receipt. A late reply for an
 * older (or post-relogin) operation must not resolve the current entry.
 */
export function settlePreviewStart<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  key: string,
  operationId: string,
  outcome: "succeeded" | "failed" | "unknown",
): void {
  const entry = state.starts.get(key);
  if (!entry || entry.operationId !== operationId) return;
  entry.state = outcome === "succeeded" ? "succeeded" : outcome === "failed" ? "failed" : "unknown";
}

/**
 * Read-only `previews/status` reconciliation after an unknown receipt.
 *
 * `previews/status` carries no operation receipt (the server keeps operations in
 * memory), so "not running" cannot prove the original write was never executed,
 * queued or lost across a restart: it stays unknown and blocked. Running only
 * proves the preview is up, which is enough to *open* it — never to write again.
 */
export function reconcilePreviewStart<B extends PreviewBlank>(
  state: PreviewIntentState<B>,
  key: string,
  running: boolean,
): "open-only" | "blocked" {
  const entry = state.starts.get(key);
  if (!entry) return "blocked";
  if (entry.state !== "unknown") return "blocked";
  return running ? "open-only" : "blocked";
}

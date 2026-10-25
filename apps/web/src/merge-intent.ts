// Merge action intent: a small pure-TS state machine for the two user-visible
// Git actions ("merge to main branch" and "merge and push").
//
// The service owns the real write path (review, operation id, leases, phases).
// This module owns only what the browser must not get wrong:
//   * which mode a click means, decided from observed Git facts — never from a
//     guess such as "the branch looks already merged";
//   * one stable operation id per user confirmation, so a double click cannot
//     submit two writes;
//   * a late receipt is accepted only for the scope it was bound to (task,
//     round, mode, review fingerprint, Git epoch). Anything else is ignored, so
//     a stale approval can never resurrect or submit an old review;
//   * an unknown result is only ever reconciled read-only, never re-sent.
import type { GitStatus, MergeReview } from "./types.ts";

export const MERGE_MODE_LOCAL = "local";
export const MERGE_MODE_PUSH = "push";

export type MergeMode =
  | typeof MERGE_MODE_LOCAL
  | typeof MERGE_MODE_PUSH
  // Push-only: the reviewed content is already on the main branch, so the only
  // remaining action is uploading the exact commit. It is not a separate mode
  // the user picks or the client infers loosely; the review must prove it.
  | "push_only";

/** What the primary action should offer for one task. */
export interface MergeActionOffer {
  mode: MergeMode;
  label: string;
}

export interface MergeReviewLike extends Omit<Partial<MergeReview>, "uploads"> {
  canMerge?: boolean;
  canPush?: boolean;
  pushOnly?: boolean;
  alreadyMerged?: boolean;
  uploads?: { sha: string; subject?: string }[];
  mainBranch?: string;
}

export interface MergeScope {
  projectId: string;
  taskId: string;
  roundId: string;
  mode: Exclude<MergeMode, "push_only">;
}

function hasUploads(review: MergeReviewLike | undefined): boolean {
  return Array.isArray(review?.uploads) && review.uploads.length > 0;
}

/**
 * PushOnly is a server-verified fact, never a client guess. The client may call
 * the action "push to remote" only when the review itself reports pushOnly with
 * an actual upload set, so an ancestry coincidence can never turn a merge into a
 * silent push and a push can never re-create a commit or merge.
 */
export function isPushOnlyReview(review: MergeReviewLike | undefined): boolean {
  return review?.pushOnly === true && hasUploads(review);
}

export function mergeModeLabel(mode: MergeMode): string {
  switch (mode) {
    case MERGE_MODE_LOCAL:
      return "合并到主分支";
    case MERGE_MODE_PUSH:
      return "合并并推送";
    case "push_only":
      return "推送到远程";
  }
}

/**
 * Decide the offered action from observed state only.
 * - already merged with uploads pending  -> push only
 * - otherwise                            -> the same two explicit choices
 * The local action stays available when the remote side is blocked (missing
 * credential, rejected remote, unknown remote state).
 */
export function mergeActionOffer(input: {
  git?: GitStatus;
  review?: MergeReviewLike;
  remoteBlocked?: boolean;
}): MergeActionOffer | undefined {
  const { git, review } = input;
  if (git?.state === "merged" || review?.alreadyMerged === true) {
    if (review && isPushOnlyReview(review)) return { mode: "push_only", label: mergeModeLabel("push_only") };
    // Already merged but nothing provably pending: no action, and definitely no
    // second commit or merge.
    return undefined;
  }
  if (git && git.canMerge === false && !review?.canMerge) return undefined;
  return { mode: MERGE_MODE_LOCAL, label: mergeModeLabel(MERGE_MODE_LOCAL) };
}

/** Both explicit choices: local always, push only when the review allows it. */
export function mergeChoices(input: {
  git?: GitStatus;
  review?: MergeReviewLike;
}): MergeActionOffer[] {
  if (isPushOnlyReview(input.review)) return [{ mode: "push_only", label: mergeModeLabel("push_only") }];
  const choices: MergeActionOffer[] = [];
  if (input.git === undefined || input.git.canMerge !== false || input.review?.canMerge) {
    choices.push({ mode: MERGE_MODE_LOCAL, label: mergeModeLabel(MERGE_MODE_LOCAL) });
  }
  if (input.review?.canPush === true) {
    choices.push({ mode: MERGE_MODE_PUSH, label: mergeModeLabel(MERGE_MODE_PUSH) });
  }
  return choices;
}

export function mergeScopeKey(scope: MergeScope): string {
  return [scope.projectId, scope.taskId, scope.roundId, scope.mode].join("\u0000");
}

export interface MergeIntentState {
  epoch: number;
  /** Stable id of the confirmed gesture, bound to its scope. */
  pending?: { operationId: string; scopeKey: string; roundId: string };
  /** Ids already sent, so a late or repeated receipt can be recognised. */
  sent: Record<string, string>;
  /** Result unknown: only a read-only lookup may decide what happened. */
  unknown: Record<string, true>;
}

export function createMergeIntent(): MergeIntentState {
  return { epoch: 0, sent: {}, unknown: {} };
}

/**
 * Any change of task, round, mode, or new Git facts invalidates the live gesture.
 * Bumping the epoch means an in-flight receipt for the old scope is dropped.
 */
export function invalidateMergeIntent(state: MergeIntentState): number {
  state.epoch++;
  state.pending = undefined;
  return state.epoch;
}

export function isMergeIntentEpoch(state: MergeIntentState, epoch: number): boolean {
  return state.epoch === epoch;
}

export type MergeBegin =
  | { action: "write"; operationId: string }
  | { action: "wait" }
  | { action: "reconcile" };

/**
 * A confirmed click: mint one stable id for this scope, or reuse the existing one
 * when the same gesture is repeated (double click). An unknown earlier attempt for
 * the same scope never produces another write — it asks for a read-only lookup.
 */
export function beginMerge(state: MergeIntentState, scope: MergeScope, newID: () => string): MergeBegin {
  const scopeKey = mergeScopeKey(scope);
  if (state.pending && state.pending.scopeKey === scopeKey) return { action: "wait" };
  // A mode/round switch cannot prove an earlier write on this task did not run.
  if (uncertainMergeOperation(state, scope)) return { action: "reconcile" };
  const operationId = newID();
  state.pending = { operationId, scopeKey, roundId: scope.roundId };
  state.sent[operationId] = scopeKey;
  // Retain ownership even if closing/switching invalidates the pending UI.
  state.unknown[scopeKey] = true;
  return { action: "write", operationId };
}

export type MergeSettle =
  | { action: "applied"; operationId: string }
  | { action: "unknown"; operationId: string }
  | { action: "stale" };

/**
 * Bind a reply to the gesture it belongs to. A reply for any other scope, or one
 * that arrives after the epoch moved on, is stale: it must not open progress,
 * commit, or clear anything in the current view.
 */
export function settleMerge(
  state: MergeIntentState,
  epoch: number,
  scope: MergeScope,
  reply: { operationId?: string; ok: boolean } | undefined,
): MergeSettle {
  const operationId = state.pending?.operationId;
  if (!operationId) return { action: "stale" };
  if (!isMergeIntentEpoch(state, epoch)) return { action: "stale" };
  if (state.pending?.scopeKey !== mergeScopeKey(scope)) return { action: "stale" };
  if (reply?.operationId && reply.operationId !== operationId) return { action: "stale" };
  state.pending = undefined;
  if (!reply || !reply.ok) {
    // Transport failure, timeout, disconnect: the write may have happened.
    state.unknown[mergeScopeKey(scope)] = true;
    return { action: "unknown", operationId };
  }
  delete state.unknown[mergeScopeKey(scope)];
  return { action: "applied", operationId };
}

/**
 * A read-only lookup after an unknown result. `found` means the operation exists,
 * so the view may resume its progress; it never triggers another write. A missing
 * receipt cannot rule out an original request still in transit.
 */
export function reconcileMerge(
  state: MergeIntentState,
  scope: MergeScope,
  found: boolean | undefined,
): "resume" | "blocked" {
  const scopeKey = mergeScopeKey(scope);
  if (found === true) {
    delete state.unknown[scopeKey];
    return "resume";
  }
  // Lookup itself failed: stay blocked rather than risk a second write.
  return "blocked";
}

/** Most recent unconfirmed write on a task, including an invalidated UI scope. */
export function uncertainMergeOperation(state: MergeIntentState, scope: Pick<MergeScope, "projectId" | "taskId">): string | undefined {
  const prefix = [scope.projectId, scope.taskId, ""].join("\u0000");
  return Object.entries(state.sent).reverse().find(([, key]) => key.startsWith(prefix) && state.unknown[key])?.[0];
}

/** Login teardown only: a new Git snapshot does not prove an old write absent. */
export function clearMergeUnknown(state: MergeIntentState): void {
  state.unknown = {};
}

/** The operation id most recently minted for a scope, if any. */
export function operationForScope(state: MergeIntentState, scope: MergeScope): string | undefined {
  const scopeKey = mergeScopeKey(scope);
  let found: string | undefined;
  for (const [id, key] of Object.entries(state.sent)) if (key === scopeKey) found = id;
  return found;
}

/**
 * The server proved it refused the request (an explicit 4xx conflict/invalid), so
 * no write exists. Clearing the gesture is safe and lets the user review again;
 * it must never be used for transport failures, which stay unknown.
 */
export function rejectMerge(state: MergeIntentState, epoch: number, scope: MergeScope): boolean {
  if (!isMergeIntentEpoch(state, epoch)) return false;
  if (state.pending?.scopeKey !== mergeScopeKey(scope)) return false;
  state.pending = undefined;
  delete state.unknown[mergeScopeKey(scope)];
  return true;
}

/**
 * The round a review must be bound to: the newest finished (or already
 * confirmed) round of the conversation, matching the server's ReviewedTask gate.
 * A legacy task with no rounds uses the same token ConfirmResult synthesizes, so
 * the server accepts it without a second, guessed identifier.
 */
export function reviewedRoundId(
  conversation: { rounds?: { id: string; phase: string; finishedAt?: string }[] } | undefined,
  execution: { id: string; finishedAt?: string; phase?: string } | undefined,
): string | undefined {
  const rounds = conversation?.rounds ?? [];
  const latest = [...rounds].reverse().find(r => r.phase !== "answered");
  if (latest) {
    if (!latest.finishedAt) return undefined;
    if (latest.phase !== "finished" && latest.phase !== "confirmed") return undefined;
    return latest.id;
  }
  if (!execution || !execution.finishedAt) return undefined;
  if (execution.phase !== "awaiting_review" && execution.phase !== "confirmed") return undefined;
  return `legacy:${execution.id}:${execution.finishedAt}`;
}

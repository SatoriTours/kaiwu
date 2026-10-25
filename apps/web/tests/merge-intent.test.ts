import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createMergeIntent,
  invalidateMergeIntent,
  beginMerge,
  settleMerge,
  reconcileMerge,
  isPushOnlyReview,
  mergeActionOffer,
  mergeChoices,
  type MergeScope,
  type MergeReviewLike,
} from "../src/merge-intent.ts";

const scope = (over: Partial<MergeScope> = {}): MergeScope => ({
  projectId: "p1",
  taskId: "t1",
  roundId: "r7",
  mode: "local",
  ...over,
});

const ids = () => {
  let n = 0;
  return () => `op${++n}`;
};

const review = (over: Partial<MergeReviewLike> = {}): MergeReviewLike => ({
  alreadyMerged: false,
  pushOnly: false,
  canPush: false,
  uploads: [],
  mainBranch: "main",
  ...over,
});

test("local mode never becomes a push, and push is an explicit separate choice", () => {
  const git = { canMerge: true } as MergeReviewLike;
  const choices = mergeChoices({ git: git as never, review: review({ canPush: true }) });
  assert.deepEqual(
    choices.map((c) => c.mode),
    ["local", "push"],
  );
  // Remote blocked (missing credential) must not remove the local action.
  const blocked = mergeChoices({ git: git as never, review: review({ canPush: false, pushReason: "缺少凭据" } as never) });
  assert.deepEqual(blocked.map((c) => c.mode), ["local"]);
});

test("push_only requires the review's own pushOnly proof plus real uploads", () => {
  assert.equal(isPushOnlyReview(review({ pushOnly: true, uploads: [{ sha: "a" }] })), true);
  // Already-merged alone, or an ancestry coincidence, is not proof.
  assert.equal(isPushOnlyReview(review({ alreadyMerged: true })), false);
  assert.equal(isPushOnlyReview(review({ pushOnly: true, uploads: [] })), false);
  assert.equal(isPushOnlyReview(undefined), false);
  assert.equal(isPushOnlyReview(review({ alreadyMerged: true, uploads: [{ sha: "a" }] })), false);
});

test("already merged yields at most the push-only action and never a merge action", () => {
  const merged = mergeActionOffer({
    git: { state: "merged", canMerge: false } as never,
    review: review({ alreadyMerged: true, pushOnly: true, uploads: [{ sha: "a" }] }),
  });
  assert.equal(merged?.mode, "push_only");
  const nothingPending = mergeActionOffer({
    git: { state: "merged", canMerge: false } as never,
    review: review({ alreadyMerged: true, uploads: [] }),
  });
  assert.equal(nothingPending, undefined, "no proof of pending uploads means no action");
});

test("a repeated confirmed click reuses one operation id and blocks a second write", () => {
  const state = createMergeIntent();
  const opID = ids();
  const first = beginMerge(state, scope(), opID);
  assert.deepEqual(first, { action: "write", operationId: "op1" });
  // Same scope again (double click): wait, do not mint or send a second id.
  assert.deepEqual(beginMerge(state, scope(), opID), { action: "wait" });
  const settled = settleMerge(state, state.epoch, scope(), { operationId: "op1", ok: true });
  assert.deepEqual(settled, { action: "applied", operationId: "op1" });
  // A new gesture after a clean result is allowed again, with a new id.
  assert.deepEqual(beginMerge(state, scope(), opID), { action: "write", operationId: "op2" });
});

test("switching task, round or mode drops the old gesture instead of reviving it", () => {
  const state = createMergeIntent();
  const opID = ids();
  beginMerge(state, scope(), opID);
  // Same task, new round: the old approval is gone.
  invalidateMergeIntent(state);
  assert.deepEqual(settleMerge(state, 0, scope(), { operationId: "op1", ok: true }), { action: "stale" });
  const next = beginMerge(state, scope({ roundId: "r8" }), opID);
  assert.deepEqual(next, { action: "reconcile" }, "changing round is not proof an in-flight write never ran");
  // A late receipt for the previous epoch can never apply to the new gesture.
  assert.deepEqual(settleMerge(state, state.epoch, scope({ roundId: "r8" }), { operationId: "op1", ok: true }), {
    action: "stale",
  });
});

test("an unknown result is reconciled read-only and never re-sent as a write", () => {
  const state = createMergeIntent();
  const opID = ids();
  const began = beginMerge(state, scope(), opID);
  assert.equal(began.action, "write");
  const unknown = settleMerge(state, state.epoch, scope(), undefined);
  assert.deepEqual(unknown, { action: "unknown", operationId: "op1" });
  // Retrying the same gesture must not mint a second write.
  assert.deepEqual(beginMerge(state, scope(), opID), { action: "reconcile" });
  // A failed lookup keeps it blocked rather than guessing.
  assert.equal(reconcileMerge(state, scope(), undefined), "blocked");
  assert.deepEqual(beginMerge(state, scope(), opID), { action: "reconcile" });
  // Found: resume the existing operation, still no new write.
  assert.equal(reconcileMerge(state, scope(), true), "resume");
  assert.deepEqual(beginMerge(state, scope(), opID), { action: "write", operationId: "op2" });
});

test("a reply for a different scope is stale and cannot clear the live gesture", () => {
  const state = createMergeIntent();
  const opID = ids();
  beginMerge(state, scope(), opID);
  // Push mode reply arriving while the local gesture is live.
  assert.deepEqual(settleMerge(state, state.epoch, scope({ mode: "push" }), { operationId: "op9", ok: true }), {
    action: "stale",
  });
  // The live local gesture still owns its id.
  assert.deepEqual(settleMerge(state, state.epoch, scope(), { operationId: "op1", ok: true }), {
    action: "applied",
    operationId: "op1",
  });
});

test("local success and remote failure stay separate facts", () => {
  // A succeeded local operation with a failed push attempt must keep the local
  // proof: the mode the user confirmed is what the label reports.
  const offer = mergeActionOffer({ git: { state: "unmerged", canMerge: true } as never, review: review() });
  assert.equal(offer?.label, "合并到主分支");
  // Push-only label must never be shown for a plain unmerged task.
  assert.notEqual(offer?.mode, "push_only");
});

test("merged ancestry without a server push-only review never offers a push-only action", () => {
  assert.equal(mergeActionOffer({ git: { state: "merged", canMerge: false } as never }), undefined);
});

test("closing or changing mode never forgets an uncertain write on the same task", () => {
  const state = createMergeIntent();
  const nextID = ids();
  beginMerge(state, scope(), nextID);
  invalidateMergeIntent(state);
  assert.deepEqual(beginMerge(state, scope({ mode: "push" }), nextID), { action: "reconcile" });
});

test("a missing read-only receipt cannot prove a delayed original write will never land", () => {
  const state = createMergeIntent();
  beginMerge(state, scope(), ids());
  settleMerge(state, state.epoch, scope(), undefined);
  assert.equal(reconcileMerge(state, scope(), false), "blocked");
  assert.deepEqual(beginMerge(state, scope({ mode: "push" }), ids()), { action: "reconcile" });
});

// --- staged facts: local / remote / CI must never collapse into one claim ---
import { mergeFacts, mergeFinished } from "../src/merge.ts";

const op = (over: Record<string, unknown> = {}) =>
  ({
    id: "op",
    projectId: "p1",
    taskId: "t1",
    phase: "succeeded",
    startedAt: "2026-01-01T00:00:00Z",
    taskAfterSHA: "t".repeat(40),
    mainAfterSHA: "m".repeat(40),
    preview: {} as never,
    ...over,
  }) as never;

test("local success plus remote failure keeps the local proof and shows no delivery", () => {
  const facts = mergeFacts(op({ mode: "push", phase: "failed", mainAfterSHA: "m".repeat(40), pushAttemptedAt: "2026-01-01T00:01:00Z", error: "远程拒绝了上传" }))!;
  assert.equal(facts.local.state, "done");
  assert.equal(facts.remote.state, "failed");
  assert.equal(facts.ci.state, "not_requested");
  assert.match(facts.ci.label, /未检查/);
});

test("a push-mode operation with an unknown receipt never claims the upload", () => {
  const facts = mergeFacts(op({ mode: "push", phase: "needs_check", mainAfterSHA: "m".repeat(40), intendedSHA: "i".repeat(40), pushAttemptedAt: "2026-01-01T00:01:00Z" }))!;
  assert.equal(facts.local.state, "unknown");
  assert.equal(facts.remote.state, "unknown");
  assert.match(facts.remote.label, /不会自动重发/);
});

test("local mode never reports a remote upload and CI stays not checked", () => {
  const facts = mergeFacts(op({ mode: "local", phase: "succeeded" }))!;
  assert.equal(facts.remote.state, "not_requested");
  assert.equal(facts.ci.state, "not_requested");
  assert.equal(mergeFinished("succeeded"), true);
  assert.equal(mergeFinished("merged"), false, "merged still owns the leases");
});

// --- the review must bind the newest finished round, never a guessed one ---
import { reviewedRoundId } from "../src/merge-intent.ts";

test("reviewed round is the newest finished round or the legacy token", () => {
  const round = (id: string, phase: string, finished = "2026-01-01T00:00:00Z") => ({ id, phase, finishedAt: finished });
  assert.equal(reviewedRoundId({ rounds: [round("r1", "finished")] }, undefined), "r1");
  assert.equal(reviewedRoundId({ rounds: [round("r1", "confirmed")] }, undefined), "r1");
  assert.equal(reviewedRoundId({ rounds: [round("dev", "finished"), round("qa", "answered")] }, undefined), "dev");
  // Still running / not finished: no round may be acknowledged yet.
  assert.equal(reviewedRoundId({ rounds: [round("r1", "finished"), round("r2", "running", "")] }, undefined), undefined);
  assert.equal(reviewedRoundId({ rounds: [round("r1", "interrupted", "")] }, undefined), undefined);
  // Legacy task: exactly the token the server synthesizes.
  assert.equal(
    reviewedRoundId(undefined, { id: "x1", finishedAt: "2026-01-02T00:00:00Z", phase: "awaiting_review" }),
    "legacy:x1:2026-01-02T00:00:00Z",
  );
  assert.equal(reviewedRoundId(undefined, { id: "x1", finishedAt: "", phase: "awaiting_review" }), undefined);
  assert.equal(reviewedRoundId(undefined, undefined), undefined);
});

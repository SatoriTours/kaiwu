import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createLocalState,
  selectConversation,
  setDraft,
  acceptSent,
  mergeSnapshot,
  createRefreshQueue,
  createOperationID,
  isStartRequest,
} from "../src/workbench.ts";
test("only an explicit start phrase starts development", () => {
  for (const s of ["开始开发", " 开始开发！ ", "开始开发。"])
    assert.equal(isStartRequest(s), true);
  for (const s of [
    "不要开始开发",
    "什么时候开始开发？",
    "开始开发这个功能",
    "",
  ])
    assert.equal(isStartRequest(s), false);
});
test("operation IDs work without secure-context randomUUID", () => {
  assert.equal(
    createOperationID((bytes) => {
      bytes.fill(0xab);
      return bytes;
    }),
    "ab".repeat(16),
  );
});
test("project/A/B drafts and late acceptance stay in their captured scope", () => {
  const local = createLocalState();
  selectConversation(local, "project");
  setDraft(local, "project draft");
  selectConversation(local, "A");
  setDraft(local, "old A");
  const revision = local.drafts.A!.revision;
  selectConversation(local, "B");
  setDraft(local, "new B");
  acceptSent(local, "A", revision);
  assert.equal(local.current, "B");
  assert.equal(local.drafts.B!.text, "new B");
  assert.equal(local.drafts.project!.text, "project draft");
  selectConversation(local, "A");
  setDraft(local, "new A");
  acceptSent(local, "A", revision);
  assert.equal(local.drafts.A!.text, "new A");
  selectConversation(local, "A");
  assert.equal(local.drafts.A!.text, "new A");
});
test("older snapshot cannot replace newer data", () => {
  const local = createLocalState();
  mergeSnapshot(local, { revision: 5 } as any);
  mergeSnapshot(local, { revision: 3 } as any);
  assert.equal(local.snapshot?.revision, 5);
});
test("overlapping refresh callers await the queued snapshot", async () => {
  let unblock!: () => void;
  let count = 0;
  const gate = new Promise<void>((resolve) => (unblock = resolve));
  const refresh = createRefreshQueue(async () => {
    count++;
    if (count === 1) await gate;
  });
  const first = refresh();
  let finished = false;
  const second = refresh().then(() => (finished = true));
  await Promise.resolve();
  assert.equal(finished, false);
  unblock();
  await Promise.all([first, second]);
  assert.equal(count, 2);
  assert.equal(finished, true);
});

test("only explicit merge phrases open an approval", async () => {
  const { isMergeRequest, gitStatusLabel } = await import("../src/git.ts");
  for (const text of [
    "合并到主分支",
    "请提交并合并到主分支。",
    " 合并到主分支！ ",
  ])
    assert.equal(isMergeRequest(text), true);
  for (const text of [
    "不要合并到主分支",
    "合并到主分支？",
    "合并到主分支\n",
    '"合并到主分支"',
    "请帮我看看是否合并到主分支",
  ])
    assert.equal(isMergeRequest(text), false);
  assert.equal(gitStatusLabel({ state: "merged" } as any), "已合并");
  assert.notEqual(gitStatusLabel(), "已合并");
});

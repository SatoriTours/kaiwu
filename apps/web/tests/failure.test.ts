import { test } from "node:test";
import assert from "node:assert/strict";
import { failureView } from "../src/failure.ts";

// The exact text the Go backend now stores for a provider failure.
const PROVIDER_ERROR =
  "模型调用失败；已有内容保留：429 已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)";
const PROVIDER_REASON =
  "429 已达到 Token Plan 速率限制：请升级 Token Plan 套餐或切换为按量付费 API 使用。 (2062)";

test("an execution failure shows the provider reason, not the boilerplate prefix", () => {
  const view = failureView({ runState: "failed", error: PROVIDER_ERROR });
  assert.ok(view, "failed run must render a failure notice");
  assert.equal(view.kind, "execution");
  assert.equal(view.title, "执行失败");
  assert.equal(view.hasReason, true);
  assert.equal(view.reason, PROVIDER_REASON);
  assert.match(view.reason, /Token Plan 速率限制/);
  assert.doesNotMatch(view.reason, /已有内容保留：429/);
  assert.match(view.detail, /模型调用失败；已有内容保留：/);
});

test("the notice is not called a session reconnect failure", () => {
  for (const error of [PROVIDER_ERROR, "消息未能确认完成；请检查后明确恢复，不会自动重发"]) {
    const view = failureView({ runState: "failed", error });
    assert.ok(view);
    assert.doesNotMatch(view.title, /会话接回失败/);
    assert.doesNotMatch(view.hint, /会话接回失败/);
  }
});

test("a send failure is distinguished from an execution failure", () => {
  const view = failureView({
    runState: "failed",
    error: "消息未能确认完成；请检查后明确恢复，不会自动重发",
  });
  assert.ok(view);
  assert.equal(view.kind, "send");
  assert.equal(view.title, "消息发送失败");
  assert.equal(view.hasReason, false, "the stored text is status, not a cause");
  assert.match(view.detail, /消息未能确认完成/);
  assert.match(view.hint, /再次发送/);
});

test("an interrupted session keeps its own wording and auto-reconnect hint", () => {
  const view = failureView({
    runState: "interrupted",
    error: "pi 会话已中断，发送下一条消息时自动接回；历史已保留",
  });
  assert.ok(view);
  assert.equal(view.kind, "session");
  assert.equal(view.title, "会话已中断");
  assert.equal(view.hasReason, false);
  assert.match(view.hint, /自动接回/);
});

test("a missing reason is reported as missing instead of invented", () => {
  for (const error of ["", "模型调用失败或被中断；已有内容保留"]) {
    const view = failureView({ runState: "failed", error });
    assert.ok(view, "a failed run without text still needs a notice");
    assert.equal(view.hasReason, false);
    assert.equal(view.reason, "");
    assert.doesNotMatch(view.reason, /429|原因：/);
  }
});

test("the execution error channel is used when the conversation has no text", () => {
  const view = failureView(
    { runState: "failed", error: "" },
    { phase: "failed", error: PROVIDER_ERROR },
  );
  assert.ok(view);
  assert.equal(view.kind, "execution");
  assert.equal(view.reason, PROVIDER_REASON);
});

test("running or idle rounds never show a stale failure", () => {
  for (const runState of ["running", "starting", "idle"]) {
    assert.equal(
      failureView({ runState, error: PROVIDER_ERROR }),
      undefined,
      runState + " must not show the previous failure",
    );
  }
});

test("a long reason keeps both ends inline and stays fully expandable", () => {
  const tail = "可执行的下一步：升级 Token Plan 套餐或改用按量付费 API。";
  const error =
    "模型调用失败；已有内容保留：" +
    "速率限制说明：".repeat(40) +
    tail;
  const view = failureView({ runState: "failed", error });
  assert.ok(view);
  assert.equal(view.hasReason, true);
  assert.ok(view.reason.length < error.length, "inline reason must be bounded");
  assert.ok(view.reason.includes(tail), "the actionable tail must stay visible");
  assert.ok(view.reason.includes("速率限制说明"), "the head must stay visible");
  assert.ok(view.detail.includes(tail), "the full text must stay expandable");
});

test("a preserved result is not reported as lost", () => {
  const view = failureView(
    { runState: "failed", error: PROVIDER_ERROR },
    { phase: "awaiting_review", error: PROVIDER_ERROR },
  );
  assert.ok(view);
  assert.equal(view.resultPreserved, true);
  const noResult = failureView({ runState: "failed", error: PROVIDER_ERROR }, { phase: "failed" });
  assert.ok(noResult);
  assert.equal(noResult.resultPreserved, false);
});

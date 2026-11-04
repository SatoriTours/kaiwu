import { test } from "node:test";
import assert from "node:assert/strict";
import {
  summarizeSubagents,
  formatSubagentBadgeText,
  sortSubagentSummaries,
  mergeSubagentEvents,
  shouldFetchSubagentIncremental,
  createSubagentQueryState,
  isRunTerminal,
  formatSubagentStatusLabel,
} from "../src/subagents.ts";
import type { SubagentSummary, SubagentEvent } from "../src/types.ts";

test("summarizeSubagents filters strictly by project and conversation", () => {
  const allRuns: Record<string, SubagentSummary> = {
    "run-1": {
      id: "run-1",
      projectId: "p1",
      conversationId: "c1",
      taskId: "t1",
      roundId: "r1",
      parentSessionId: "s1",
      title: "Task 1",
      taskSummary: "summary 1",
      status: "running",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:01:00Z",
      version: 1,
      truncated: false,
    },
    "run-2": {
      id: "run-2",
      projectId: "p1",
      conversationId: "c2", // different conversation
      taskId: "t2",
      roundId: "r1",
      parentSessionId: "s2",
      title: "Task 2",
      taskSummary: "summary 2",
      status: "running",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:01:00Z",
      version: 1,
      truncated: false,
    },
    "run-3": {
      id: "run-3",
      projectId: "p2", // different project
      conversationId: "c1",
      taskId: "t3",
      roundId: "r1",
      parentSessionId: "s3",
      title: "Task 3",
      taskSummary: "summary 3",
      status: "running",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:01:00Z",
      version: 1,
      truncated: false,
    },
  };

  const runs = summarizeSubagents(allRuns, "p1", "c1");
  assert.equal(runs.length, 1);
  assert.equal(runs[0].id, "run-1");
});

test("formatSubagentBadgeText implements precise 3-tier rules", () => {
  // Empty
  assert.equal(formatSubagentBadgeText([]), null);

  // running > 0
  assert.deepEqual(formatSubagentBadgeText([
    { status: "running" },
    { status: "starting" },
    { status: "completed" }
  ]), {
    text: "◉ 1 个子 Agent 运行中",
    tone: "running",
    activeCount: 1,
    hasUnknown: false,
  });

  // only starting
  assert.deepEqual(formatSubagentBadgeText([
    { status: "starting" },
    { status: "starting" },
    { status: "completed" }
  ]), {
    text: "2 个子 Agent 启动中",
    tone: "starting",
    activeCount: 2,
    hasUnknown: false,
  });

  // has unknown: must explicitly show unknown / pending check, cannot be accurate 0
  assert.deepEqual(formatSubagentBadgeText([
    { status: "unknown" },
    { status: "completed" }
  ]), {
    text: "子 Agent · 1 待核查",
    tone: "unknown",
    activeCount: 1,
    hasUnknown: true,
  });

  // running + unknown
  assert.deepEqual(formatSubagentBadgeText([
    { status: "running" },
    { status: "unknown" }
  ]), {
    text: "◉ 1 个运行中 (1待核查)",
    tone: "running",
    activeCount: 2,
    hasUnknown: true,
  });

  // history only (all terminal without unknown)
  assert.deepEqual(formatSubagentBadgeText([
    { status: "completed" },
    { status: "failed" },
    { status: "interrupted" }
  ]), {
    text: "子 Agent · 3",
    tone: "history",
    activeCount: 0,
    hasUnknown: false,
  });
});

test("sortSubagentSummaries sorts active runs before history and by time descending", () => {
  const runs: SubagentSummary[] = [
    {
      id: "run-hist-old",
      projectId: "p1",
      conversationId: "c1",
      roundId: "r1",
      parentSessionId: "s1",
      title: "Old History",
      taskSummary: "old",
      status: "completed",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T09:00:00Z",
      updatedAt: "2026-10-18T09:10:00Z",
      version: 1,
      truncated: false,
    },
    {
      id: "run-unknown",
      projectId: "p1",
      conversationId: "c1",
      roundId: "r1",
      parentSessionId: "s1",
      title: "Unknown Run",
      taskSummary: "unknown",
      status: "unknown",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:05:00Z",
      version: 1,
      truncated: false,
    },
    {
      id: "run-running",
      projectId: "p1",
      conversationId: "c1",
      roundId: "r1",
      parentSessionId: "s1",
      title: "Active Run",
      taskSummary: "active",
      status: "running",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:15:00Z",
      version: 2,
      truncated: false,
    },
    {
      id: "run-starting",
      projectId: "p1",
      conversationId: "c1",
      roundId: "r1",
      parentSessionId: "s1",
      title: "Starting Run",
      taskSummary: "starting",
      status: "starting",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:12:00Z",
      version: 1,
      truncated: false,
    },
    {
      id: "run-hist-new",
      projectId: "p1",
      conversationId: "c1",
      roundId: "r1",
      parentSessionId: "s1",
      title: "New History",
      taskSummary: "new",
      status: "failed",
      profile: { provider: "mock", model: "fake" },
      startedAt: "2026-10-18T10:00:00Z",
      updatedAt: "2026-10-18T10:20:00Z",
      version: 2,
      truncated: false,
    },
  ];

  const sorted = sortSubagentSummaries(runs);
  assert.deepEqual(sorted.map(r => r.id), [
    "run-running",
    "run-starting",
    "run-unknown",
    "run-hist-new",
    "run-hist-old",
  ]);
});

test("formatSubagentStatusLabel accurately labels statuses without calling completed 'accepted'", () => {
  assert.equal(formatSubagentStatusLabel("starting"), "启动中");
  assert.equal(formatSubagentStatusLabel("running"), "运行中");
  assert.equal(formatSubagentStatusLabel("completed"), "已结束"); // strictly NOT "验收通过"
  assert.equal(formatSubagentStatusLabel("failed"), "执行失败");
  assert.equal(formatSubagentStatusLabel("interrupted"), "已中断");
  assert.equal(formatSubagentStatusLabel("unknown"), "待核查");
});

test("mergeSubagentEvents deduplicates by sequence and preserves monotonicity", () => {
  const existing: SubagentEvent[] = [
    { id: "e1", sequence: 1, at: "2026-10-18T10:00:00Z", kind: "text", text: "step 1" },
    { id: "e2", sequence: 2, at: "2026-10-18T10:00:01Z", kind: "tool_start", toolName: "read" },
  ];

  const incoming: SubagentEvent[] = [
    { id: "e2", sequence: 2, at: "2026-10-18T10:00:01Z", kind: "tool_start", toolName: "read" }, // duplicate
    { id: "e3", sequence: 3, at: "2026-10-18T10:00:02Z", kind: "tool_end", toolName: "read" },
    { id: "e4", sequence: 4, at: "2026-10-18T10:00:03Z", kind: "text", text: "step 2" },
  ];

  const merged = mergeSubagentEvents(existing, incoming);
  assert.equal(merged.length, 4);
  assert.deepEqual(merged.map(e => e.sequence), [1, 2, 3, 4]);
});

test("shouldFetchSubagentIncremental correctly tracks version increments", () => {
  assert.equal(shouldFetchSubagentIncremental(undefined, 1), true); // initial fetch
  assert.equal(shouldFetchSubagentIncremental(1, 1), false); // same version, no fetch
  assert.equal(shouldFetchSubagentIncremental(1, 2), true); // incremented version
  assert.equal(shouldFetchSubagentIncremental(2, 1), false); // older version, no fetch
});

test("createSubagentQueryState handles generation and late response discard", () => {
  const state = createSubagentQueryState();

  const token1 = state.nextRequest("p1", "c1", "run-1");
  assert.equal(state.isCurrent(token1, "p1", "c1", "run-1"), true);

  // Switch card/run
  const token2 = state.nextRequest("p1", "c2", "run-2");
  assert.equal(state.isCurrent(token2, "p1", "c2", "run-2"), true);
  // Old request for run-1 is now late and must be discarded
  assert.equal(state.isCurrent(token1, "p1", "c1", "run-1"), false);

  // Close/reset
  state.reset();
  assert.equal(state.isCurrent(token2, "p1", "c2", "run-2"), false);
});

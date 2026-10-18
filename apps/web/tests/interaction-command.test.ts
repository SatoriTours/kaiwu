import { test } from "node:test";
import assert from "node:assert/strict";
import type { Conversation, Receipt } from "../src/types.ts";
import {
  dispatchInteractionCommand,
  type DispatchContext,
  type InteractionCommandPayload,
} from "../src/interaction-command.ts";
import { saveInteractionDraft, loadInteractionDraft } from "../src/interaction.ts";

function draftStore() {
  const store = new Map<string, string>();
  (globalThis as any).sessionStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => store.set(k, v),
    removeItem: (k: string) => store.delete(k),
    clear: () => store.clear(),
    get length() {
      return store.size;
    },
    key: (i: number) => Array.from(store.keys())[i],
  };
}

function conversation(): Conversation {
  return {
    id: "c1",
    projectId: "p1",
    taskId: "t1",
    sessionId: "s1",
    runState: "idle",
    profile: { provider: "fixture", model: "m", thinking: "" },
    messages: [],
    interactions: [
      {
        schemaVersion: 1,
        id: "ix_1",
        version: 1,
        projectId: "p1",
        conversationId: "c1",
        taskId: "t1",
        source: "assistant_question",
        title: "问题",
        blocking: true,
        allowCancel: true,
        status: "pending",
        createdAt: "2026-10-18T10:00:00Z",
        fields: [],
      },
    ],
  };
}

const payload: InteractionCommandPayload = {
  type: "answer_interaction",
  projectId: "p1",
  conversationId: "c1",
  taskId: "t1",
  requestId: "ix_1",
  requestVersion: 1,
  answerMode: "form",
  answers: { allow: false },
};

interface Call {
  path: string;
  body?: any;
}

function context(calls: Call[], overrides: Partial<DispatchContext> = {}) {
  const conv = conversation();
  const operations = new Map<string, { id: string; signature: string }>();
  const localPending: Record<string, boolean> = {};
  let next = 0;
  let epoch = 1;
  const ctx: DispatchContext & { bumpEpoch: () => void; operations: typeof operations } = {
    authEpoch: epoch,
    getCurrentEpoch: () => epoch,
    online: true,
    createOperationId: () => "op_" + ++next,
    operations,
    localPending,
    api: async <T>(path: string, body?: unknown) => {
      calls.push({ path, body });
      return undefined as T;
    },
    refresh: async () => {},
    report: () => {},
    setError: () => {},
    getConversation: () => conv,
    bumpEpoch: () => {
      epoch++;
    },
  };
  return Object.assign(ctx, overrides);
}

function statusError(status: number) {
  return Object.assign(new Error("status " + status), { status });
}

test("unknown 404 retry keeps the original operation id (no duplicate identity)", async () => {
  draftStore();
  saveInteractionDraft("c1", "ix_1", 1, {
    mode: "form",
    answers: { allow: false },
    others: {},
    otherFlags: {},
    comment: "",
    text: "",
  });
  const calls: Call[] = [];
  let commandAttempts = 0;
  const ctx = context(calls, {
    api: async <T>(path: string, body?: unknown) => {
      calls.push({ path, body });
      if (path === "commands") {
        commandAttempts++;
        if (commandAttempts === 1) throw statusError(0);
        return {
          revision: 2,
          type: "answer_interaction",
          conversationId: "c1",
          interactionId: "ix_1",
          interactionVersion: 1,
          delivery: "recorded",
          interaction: { ...conversation().interactions![0], status: "answered" },
        } as T;
      }
      throw statusError(404);
    },
  });
  // First submission: network unknown, then the operation query has no record.
  const first = await dispatchInteractionCommand(ctx, payload);
  assert.equal(first.success, false);
  const firstId = [...ctx.operations.values()][0]?.id;
  assert.ok(firstId, "the unconfirmed operation must be retained");
  // The next explicit retry must reuse that exact operation id.
  const second = await dispatchInteractionCommand(ctx, payload);
  assert.equal(second.success, true);
  const commandCalls = calls.filter((c) => c.path === "commands");
  assert.equal(commandCalls.length, 2);
  assert.equal(commandCalls[0].body.operationId, firstId);
  assert.equal(commandCalls[1].body.operationId, firstId, "retry must not mint a new operation id");
  assert.equal(ctx.operations.size, 0);
  assert.equal(loadInteractionDraft("c1", "ix_1", 1), null, "draft cleared only after confirmed success");
});

test("a still-unknown query result keeps the operation and never resends automatically", async () => {
  draftStore();
  const calls: Call[] = [];
  const ctx = context(calls, {
    api: async <T>(path: string, body?: unknown) => {
      calls.push({ path, body });
      if (path === "commands") throw statusError(0);
      throw statusError(0);
    },
  });
  const first = await dispatchInteractionCommand(ctx, payload);
  assert.equal(first.success, false);
  assert.equal(ctx.operations.size, 1);
  const second = await dispatchInteractionCommand(ctx, payload);
  assert.equal(second.success, false);
  assert.equal(calls.filter((c) => c.path === "commands").length, 1, "no automatic resend while unknown");
  assert.equal(ctx.operations.size, 1, "operation identity is preserved");
});

test("a definitive rejection removes the operation and never mints another id", async () => {
  draftStore();
  const calls: Call[] = [];
  let reported = 0;
  const ctx = context(calls, {
    report: () => {
      reported++;
    },
    api: async <T>(path: string, body?: unknown) => {
      calls.push({ path, body });
      if (path === "commands") throw statusError(409);
      throw statusError(404);
    },
  });
  const result = await dispatchInteractionCommand(ctx, payload);
  assert.equal(result.success, false);
  assert.equal(ctx.operations.size, 0);
  assert.equal(reported, 1);
  assert.equal(calls.filter((c) => c.path === "commands").length, 1);
});

test("different content for the same request identity is refused without sending", async () => {
  draftStore();
  const calls: Call[] = [];
  const ctx = context(calls, {
    api: async <T>(path: string, body?: unknown) => {
      calls.push({ path, body });
      if (path === "commands") throw statusError(0);
      throw statusError(0);
    },
  });
  await dispatchInteractionCommand(ctx, payload);
  const changed = { ...payload, answers: { allow: true } };
  const result = await dispatchInteractionCommand(ctx, changed);
  assert.equal(result.success, false);
  assert.equal(calls.filter((c) => c.path === "commands").length, 1, "changed payload must not dispatch");
});

test("epoch change during an in-flight command applies nothing and clears nothing", async () => {
  draftStore();
  saveInteractionDraft("c1", "ix_1", 1, {
    mode: "form",
    answers: { allow: false },
    others: {},
    otherFlags: {},
    comment: "",
    text: "",
  });
  const calls: Call[] = [];
  const ctx: any = context(calls);
  ctx.api = async <T>(path: string, body?: unknown) => {
    calls.push({ path, body });
    ctx.bumpEpoch();
    return {
      revision: 2,
      interactionId: "ix_1",
      interactionVersion: 1,
      conversationId: "c1",
      type: "answer_interaction",
      delivery: "recorded",
      interaction: { ...conversation().interactions![0], status: "answered" },
    } as T;
  };
  const result = await dispatchInteractionCommand(ctx, payload);
  assert.equal(result.success, false);
  assert.notEqual(loadInteractionDraft("c1", "ix_1", 1), null, "draft must survive a logout/identity switch");
  assert.equal(ctx.getConversation("c1")!.interactions![0].status, "pending");
});

test("receipt updates the captured conversation, not whatever is selected later", async () => {
  draftStore();
  const calls: Call[] = [];
  const conv = conversation();
  const other: Conversation = { ...conversation(), id: "c2" };
  const ctx = context(calls, {
    getConversation: (id: string) => (id === "c1" ? conv : other),
    api: async <T>(path: string, body?: unknown) => {
      calls.push({ path, body });
      return {
        revision: 2,
        conversationId: "c1",
        interactionId: "ix_1",
        interactionVersion: 1,
        type: "answer_interaction",
        delivery: "recorded",
        interaction: { ...conversation().interactions![0], status: "answered" },
      } as T;
    },
  });
  const result = await dispatchInteractionCommand(ctx, payload);
  assert.equal(result.success, true);
  assert.equal(conv.interactions![0].status, "answered");
  assert.equal(other.interactions![0].status, "pending", "another conversation must never be mutated");
});

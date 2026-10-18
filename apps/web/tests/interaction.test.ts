import { test } from "node:test";
import assert from "node:assert/strict";
import {
  validateInteractionAnswers,
  validateTextFallback,
  getPendingInteractions,
  getInteractionDraftKey,
  formatAnswerSummary,
  hasBlockingPendingInteraction,
  saveInteractionDraft,
  loadInteractionDraft,
  clearInteractionDrafts,
  type InteractionRequest,
  type InteractionAnswers,
  type InteractionOthers,
} from "../src/interaction.ts";

test("validateInteractionAnswers handles confirm field", () => {
  const req: InteractionRequest = {
    schemaVersion: 1,
    id: "ix_1",
    version: 1,
    projectId: "p1",
    conversationId: "c1",
    source: "assistant_question",
    title: "确认操作",
    blocking: true,
    allowCancel: true,
    status: "pending",
    createdAt: new Date().toISOString(),
    fields: [
      {
        id: "agree",
        type: "confirm",
        label: "是否允许修改配置",
        required: true,
        yesLabel: "允许",
        noLabel: "拒绝",
      },
    ],
  };

  // Required confirm with no answer fails
  let res = validateInteractionAnswers(req, {}, {}, "");
  assert.ok(res.errors.agree, "agree should be required");
  assert.equal(res.valid, false);

  // false is a valid explicit boolean answer!
  res = validateInteractionAnswers(req, { agree: false }, {}, "");
  assert.equal(res.valid, true);
  assert.equal(res.errors.agree, undefined);

  // true is also valid
  res = validateInteractionAnswers(req, { agree: true }, {}, "");
  assert.equal(res.valid, true);
});

test("validateInteractionAnswers handles number field with zero, negative, integer and limits", () => {
  const req: InteractionRequest = {
    schemaVersion: 1,
    id: "ix_2",
    version: 1,
    projectId: "p1",
    conversationId: "c1",
    source: "assistant_question",
    title: "参数配置",
    blocking: true,
    allowCancel: true,
    status: "pending",
    createdAt: new Date().toISOString(),
    fields: [
      {
        id: "port",
        type: "number",
        label: "端口号",
        required: true,
        integer: true,
        min: 1024,
        max: 65535,
      },
      {
        id: "retry_count",
        type: "number",
        label: "重试次数",
        required: true,
        integer: true,
        min: 0,
        max: 10,
      },
    ],
  };

  // 0 is a valid number, not falsy/missing
  let res = validateInteractionAnswers(req, { port: 8080, retry_count: 0 }, {}, "");
  assert.equal(res.valid, true);

  // NaN or non-integer
  res = validateInteractionAnswers(req, { port: 8080.5, retry_count: 0 }, {}, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.port);

  // Min out of range
  res = validateInteractionAnswers(req, { port: 80, retry_count: -1 }, {}, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.port);
  assert.ok(res.errors.retry_count);
});

test("validateInteractionAnswers handles text and textarea fields", () => {
  const req: InteractionRequest = {
    schemaVersion: 1,
    id: "ix_3",
    version: 1,
    projectId: "p1",
    conversationId: "c1",
    source: "assistant_question",
    title: "文本补充",
    blocking: true,
    allowCancel: false,
    status: "pending",
    createdAt: new Date().toISOString(),
    fields: [
      {
        id: "host",
        type: "text",
        label: "主机名",
        required: true,
        minLength: 3,
        maxLength: 20,
      },
      {
        id: "details",
        type: "textarea",
        label: "补充信息",
        required: false,
        maxLength: 100,
      },
    ],
  };

  // Missing required host
  let res = validateInteractionAnswers(req, { host: "" }, {}, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.host);

  // Too short
  res = validateInteractionAnswers(req, { host: "ab" }, {}, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.host);

  // Valid
  res = validateInteractionAnswers(req, { host: "127.0.0.1", details: "ok" }, {}, "");
  assert.equal(res.valid, true);
});

test("validateInteractionAnswers handles single_select and multi_select with other option", () => {
  const req: InteractionRequest = {
    schemaVersion: 1,
    id: "ix_4",
    version: 1,
    projectId: "p1",
    conversationId: "c1",
    source: "assistant_question",
    title: "环境选择",
    blocking: true,
    allowCancel: true,
    status: "pending",
    createdAt: new Date().toISOString(),
    fields: [
      {
        id: "env",
        type: "single_select",
        label: "部署环境",
        required: true,
        allowOther: true,
        options: [
          { id: "lan", label: "局域网" },
          { id: "vpn", label: "VPN" },
        ],
      },
      {
        id: "roles",
        type: "multi_select",
        label: "测试角色",
        required: true,
        allowOther: true,
        minSelections: 1,
        maxSelections: 2,
        options: [
          { id: "dev", label: "开发者" },
          { id: "qa", label: "测试" },
          { id: "ops", label: "运维" },
        ],
      },
    ],
  };

  // Missing single_select
  let res = validateInteractionAnswers(req, {}, {}, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.env);
  assert.ok(res.errors.roles);

  // Select "other" without filling others[fieldId] fails
  res = validateInteractionAnswers(req, { env: "", roles: ["dev"] }, { env: "" }, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.env, "other requires explanation text");

  // Select "other" with explanation succeeds (env is purely in others, roles has option 'dev' in answers and custom text in others)
  res = validateInteractionAnswers(req, { roles: ["dev"] }, { env: "云主机", roles: "安全员" }, "");
  assert.equal(res.valid, true);

  // Multi select exceeding maxSelections
  res = validateInteractionAnswers(req, { env: "lan", roles: ["dev", "qa", "ops"] }, {}, "");
  assert.equal(res.valid, false);
  assert.ok(res.errors.roles);
});

test("validateTextFallback requires non-empty string", () => {
  assert.equal(validateTextFallback("").valid, false);
  assert.equal(validateTextFallback("   ").valid, false);
  assert.equal(validateTextFallback("使用默认配置继续开发").valid, true);
});

test("getPendingInteractions and hasBlockingPendingInteraction filters correctly", () => {
  const interactions: InteractionRequest[] = [
    {
      schemaVersion: 1,
      id: "ix_p1",
      version: 1,
      projectId: "p1",
      conversationId: "c1",
      source: "assistant_question",
      title: "待答问题 1",
      blocking: true,
      allowCancel: true,
      status: "pending",
      createdAt: "2026-10-18T10:00:00Z",
      fields: [],
    },
    {
      schemaVersion: 1,
      id: "ix_a1",
      version: 1,
      projectId: "p1",
      conversationId: "c1",
      source: "assistant_question",
      title: "已答问题",
      blocking: true,
      allowCancel: true,
      status: "answered",
      createdAt: "2026-10-18T09:00:00Z",
      fields: [],
    },
    {
      schemaVersion: 1,
      id: "ix_p2",
      version: 1,
      projectId: "p1",
      conversationId: "c1",
      source: "agent_ui",
      title: "原生询问",
      blocking: false,
      allowCancel: false,
      status: "pending",
      createdAt: "2026-10-18T10:05:00Z",
      fields: [],
    },
  ];

  const pending = getPendingInteractions(interactions);
  assert.equal(pending.length, 2);
  assert.equal(pending[0].id, "ix_p1");
  assert.equal(pending[1].id, "ix_p2");

  assert.equal(hasBlockingPendingInteraction(interactions), true);
  assert.equal(hasBlockingPendingInteraction([interactions[1], interactions[2]]), false);
});

import { taskState } from "../src/task-state.ts";
import type { Task, Conversation, Execution } from "../src/types.ts";

test("taskState correctly identifies waiting_user status and label", () => {
  const task: Task = {
    id: "t1",
    projectId: "p1",
    topicId: "top1",
    title: "任务 1",
    request: "要求",
    status: "waiting_user",
    conversationId: "c1",
    profile: { provider: "pi", model: "model", thinking: "" },
  };

  const conv: Conversation = {
    id: "c1",
    projectId: "p1",
    taskId: "t1",
    sessionId: "s1",
    runState: "idle",
    profile: { provider: "pi", model: "model", thinking: "" },
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
        title: "请选择环境",
        blocking: true,
        allowCancel: true,
        status: "pending",
        createdAt: new Date().toISOString(),
        fields: [],
      },
    ],
  };

  const exec: Execution = {
    id: "e1",
    phase: "awaiting_user",
    tools: [],
  };

  const state = taskState(task, conv, exec);
  assert.equal(state.status, "waiting_user");
  assert.equal(state.label, "待你处理");
  assert.equal(state.confirmRoundId, undefined, "should not prompt for acceptance when waiting for user");
});

test("loadInteractionDraft and saveInteractionDraft works with sessionStorage mock", () => {
  const store = new Map<string, string>();
  (globalThis as any).sessionStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => store.set(k, v),
    removeItem: (k: string) => store.delete(k),
    clear: () => store.clear(),
    get length() { return store.size; },
    key: (i: number) => Array.from(store.keys())[i],
  };

  const draft = {
    mode: "form" as const,
    answers: { agree: true },
    others: {},
    otherFlags: {},
    comment: "ok",
    text: "",
  };

  saveInteractionDraft("c1", "ix_1", 1, draft);
  const loaded = loadInteractionDraft("c1", "ix_1", 1);
  assert.deepEqual(loaded, draft);

  // Different version or request returns null
  assert.equal(loadInteractionDraft("c1", "ix_1", 2), null);
  assert.equal(loadInteractionDraft("c2", "ix_1", 1), null);

  clearInteractionDrafts();
  assert.equal(loadInteractionDraft("c1", "ix_1", 1), null);
});


import {
  splitInteractions,
  effectiveInteractionStatus,
  normalizeSubmissionAnswers,
} from "../src/interaction.ts";

function interaction(overrides: Partial<InteractionRequest> = {}): InteractionRequest {
  return {
    schemaVersion: 1,
    id: "ix_base",
    version: 1,
    projectId: "p1",
    conversationId: "c1",
    source: "assistant_question",
    title: "问题",
    blocking: true,
    allowCancel: false,
    status: "pending",
    createdAt: "2026-10-18T10:00:00Z",
    fields: [],
    ...overrides,
  };
}

test("splitInteractions moves an expired pending request into history", () => {
  const now = Date.parse("2026-10-18T12:00:00Z");
  const items = [
    interaction({ id: "ix_live", expiresAt: "2026-10-18T13:00:00Z" }),
    interaction({ id: "ix_stale", expiresAt: "2026-10-18T11:00:00Z" }),
    interaction({ id: "ix_answered", status: "answered" }),
    interaction({ id: "ix_no_expiry" }),
  ];
  const { pending, history } = splitInteractions(items, now);
  assert.deepEqual(pending.map((x) => x.id), ["ix_live", "ix_no_expiry"]);
  assert.deepEqual(history.map((x) => x.id).sort(), ["ix_answered", "ix_stale"]);
});

test("effectiveInteractionStatus reports an expired pending request as expired", () => {
  const now = Date.parse("2026-10-18T12:00:00Z");
  assert.equal(effectiveInteractionStatus(interaction({ expiresAt: "2026-10-18T11:00:00Z" }), now), "expired");
  assert.equal(effectiveInteractionStatus(interaction(), now), "pending");
  assert.equal(effectiveInteractionStatus(interaction({ status: "answered" }), now), "answered");
});

test("normalizeSubmissionAnswers sends explicit empty strings for required allowEmpty fields", () => {
  const req = interaction({
    fields: [
      { id: "native_input", type: "text", label: "输入", required: true, allowEmpty: true },
      { id: "native_editor", type: "textarea", label: "编辑", required: true, allowEmpty: true },
      { id: "must_fill", type: "text", label: "必填", required: true },
      { id: "optional_text", type: "text", label: "可选" },
      { id: "allow", type: "confirm", label: "允许", required: true },
      { id: "port", type: "number", label: "端口", required: true, min: 0 },
    ],
  });
  const input: InteractionAnswers = { allow: false, port: 0 };
  const out = normalizeSubmissionAnswers(req, input);
  assert.equal(out.native_input, "");
  assert.equal(out.native_editor, "");
  assert.equal("must_fill" in out, false, "a required non-allowEmpty field must stay missing so validation blocks it");
  assert.equal("optional_text" in out, false);
  assert.equal(out.allow, false);
  assert.equal(out.port, 0);
  assert.equal("native_input" in input, false, "input must not be mutated");
});

test("formatAnswerSummary covers false, zero, other-only and text answers", () => {
  const fields = [
    { id: "allow", type: "confirm" as const, label: "是否允许", yesLabel: "允许", noLabel: "不允许" },
    { id: "port", type: "number" as const, label: "端口", unit: "端口" },
    { id: "env", type: "single_select" as const, label: "环境", allowOther: true, options: [{ id: "staging", label: "测试环境" }] },
    { id: "roles", type: "multi_select" as const, label: "角色", allowOther: true, options: [{ id: "dev", label: "开发者" }] },
    { id: "note", type: "text" as const, label: "标题" },
  ];
  const summary = formatAnswerSummary(
    interaction({
      status: "answered",
      fields,
      response: {
        operationId: "op",
        mode: "form",
        delivery: "accepted",
        answers: { allow: false, port: 0, roles: ["dev"], note: "来自浏览器" },
        others: { env: "独立专线", roles: "安全员" },
      },
    }),
  );
  assert.match(summary, /是否允许: 不允许/);
  assert.match(summary, /端口: 0 端口/);
  assert.match(summary, /环境: 其他（独立专线）/);
  assert.match(summary, /角色: 开发者、其他（安全员）/);
  assert.match(summary, /标题: 来自浏览器/);
  // Others-only multi select must not invent an answer id.
  const othersOnly = formatAnswerSummary(
    interaction({
      status: "answered",
      fields,
      response: {
        operationId: "op2",
        mode: "form",
        delivery: "accepted",
        answers: { allow: true, port: 1, roles: [], note: "x" },
        others: { env: "仅其他", roles: "仅自定义" },
      },
    }),
  );
  assert.match(othersOnly, /角色: 其他（仅自定义）/);
  assert.doesNotMatch(othersOnly, /角色: 其他（仅自定义）.*其他（仅自定义）/);
});

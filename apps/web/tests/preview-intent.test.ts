import { test } from "node:test";
import assert from "node:assert/strict";
import {
  createPreviewIntent,
  beginPreviewIntent,
  isPreviewIntentCurrent,
  markPreviewNavigated,
  releasePreviewIntent,
  invalidatePreviewIntent,
  previewIntentKey,
  previewStartOperation,
  settlePreviewStart,
  reconcilePreviewStart,
  peekPreviewStart,
  clearPreviewStarts,
  clearPreviewCompletion,
} from "../src/preview-intent.ts";

const A = { projectId: "p1", taskId: "t1", roundId: "r1" };
const A2 = { projectId: "p1", taskId: "t1", roundId: "r2" };
const B = { projectId: "p1", taskId: "t2", roundId: "r3" };

type FakeWindow = { closed: boolean; close(): void; navigations: string[] };

function harness(opts: { dedupeMs?: number } = {}) {
  let now = 0;
  let ids = 0;
  const blanks: FakeWindow[] = [];
  const state = createPreviewIntent({
    now: () => now,
    completedDedupeMs: opts.dedupeMs ?? 700,
    createOperationId: () => `op-${++ids}`,
    openBlank: () => {
      const w: FakeWindow = {
        closed: false,
        navigations: [],
        close() {
          w.closed = true;
        },
      };
      blanks.push(w);
      return w;
    },
  });
  return {
    state,
    blanks,
    tick(ms: number) {
      now += ms;
    },
    closedCount() {
      return blanks.filter((b) => b.closed).length;
    },
    /** How many new operation ids were minted (i.e. real write attempts). */
    idCount() {
      return ids;
    },
  };
}

test("F2/F3: 每次手势只预留一个空白页，在途重复手势不再开新页", () => {
  const h = harness();
  const first = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(first);
  assert.equal(h.blanks.length, 1);
  // 在途重复点击（无论间隔多长）不产生第二个意图/标签页
  h.tick(80);
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "open" }), undefined);
  h.tick(600);
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "open" }), undefined);
  assert.equal(h.blanks.length, 1);
});

test("F3: 首请求已完成（busy 清空）后 120ms/250ms 二击仍去重，窗口过后可再打开", () => {
  const h = harness();
  const first = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(first);
  markPreviewNavigated(first);
  releasePreviewIntent(h.state, first);
  assert.equal(h.blanks[0].closed, false); // 已导航的用户页面不关

  h.tick(120);
  const second = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.equal(second, undefined);
  h.tick(130); // 累计 250ms
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "open" }), undefined);
  assert.equal(h.blanks.length, 1);

  h.tick(600); // 累计 850ms，超出短手势窗口
  const third = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(third, "明确的新手势在短窗口后必须可用，不能被永久锁死");
  assert.equal(h.blanks.length, 2);
  releasePreviewIntent(h.state, third);
  assert.equal(h.blanks[1].closed, true); // 自己未导航的空白页由自己关闭
});

test("F2/F4: A→B→A 迟到回执不得复活，generation 单调失效且不依赖面板存在", () => {
  const h = harness();
  const first = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(first);
  const generationAtStart = first.epoch;

  // 切到 B（无结果面板也可触发），再切回 A：作用域又相同
  invalidatePreviewIntent(h.state, "switch");
  invalidatePreviewIntent(h.state, "switch-back");
  assert.equal(h.blanks[0].closed, true, "失效时关闭自己未导航的预留空白页");
  assert.ok(h.state.epoch > generationAtStart);

  // 迟到回执按原始 scope 校验：即使当前页面又回到 A，也不得导航
  assert.equal(isPreviewIntentCurrent(h.state, first, A, { taskId: "t1", roundId: "r1" }), false);
  releasePreviewIntent(h.state, first); // finally 再释放一次不得重复关闭/复活
  assert.equal(h.closedCount(), 1);

  // 新的一次明确点击产生新意图，旧回执依旧失效
  const again = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(again);
  assert.notEqual(again.id, first.id);
  assert.equal(isPreviewIntentCurrent(h.state, first, A), false);
  assert.equal(isPreviewIntentCurrent(h.state, again, A), true);
});

test("F2: 同卡新轮次后旧回执失效（当前最新轮次参与比较）", () => {
  const h = harness();
  const first = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(first);
  // 最新轮次已推进到 r2，页面作用域按当前最新轮次计算
  assert.equal(isPreviewIntentCurrent(h.state, first, A2), false);
  // 响应本身轮次不匹配同样失效
  assert.equal(isPreviewIntentCurrent(h.state, first, A, { taskId: "t1", roundId: "r2" }), false);
  assert.equal(isPreviewIntentCurrent(h.state, first, A, { taskId: "t1", roundId: "r1" }), true);
  // 切换任务/登出后作用域不同
  assert.equal(isPreviewIntentCurrent(h.state, first, B), false);
});

test("F5: start 标识稳定，未知回执只读核查且绝不重写（含 status 未运行）", () => {
  const h = harness();
  const key = previewIntentKey(A) + ":m1";
  const first = previewStartOperation(h.state, key);
  assert.equal(first.action, "write");
  const id1 = first.action === "write" ? first.operationId : "";
  assert.ok(id1);

  // 在途重复调用沿用同一 operationId，不产生并联写
  assert.deepEqual(previewStartOperation(h.state, key), { action: "wait", operationId: id1 });

  // 回执丢失：标记未知后不得自动重发写请求
  settlePreviewStart(h.state, key, id1, "unknown");
  assert.equal(peekPreviewStart(h.state, key), "unknown");
  const blocked = previewStartOperation(h.state, key);
  assert.deepEqual(blocked, { action: "blocked", reason: "unknown", operationId: id1 });

  assert.equal(h.idCount(), 1, "unknown 前后不得铸造新 operationId");
  // 只读 Status 未运行**不能**证明原写未执行（服务端 operation 只是内存记录），
  // 因此仍是 blocked，记录保持不变。
  assert.equal(reconcilePreviewStart(h.state, key, false), "blocked");
  assert.equal(peekPreviewStart(h.state, key), "unknown");
  assert.deepEqual(previewStartOperation(h.state, key), {
    action: "blocked",
    reason: "unknown",
    operationId: id1,
  });

  // 反复明确操作只应做 Status 核查，Start 写入次数保持 1，ID 不变。
  assert.equal(reconcilePreviewStart(h.state, key, false), "blocked");
  assert.deepEqual(previewStartOperation(h.state, key), {
    action: "blocked",
    reason: "unknown",
    operationId: id1,
  });
  assert.equal(h.idCount(), 1, "反复明确操作只读核查，写次数保持 1");

  // 只读 Status 显示运行中：只能打开，绝不进入 Start 写分支。
  assert.equal(reconcilePreviewStart(h.state, key, true), "open-only");
  assert.equal(peekPreviewStart(h.state, key), "unknown", "open-only 不结算为成功");
  assert.deepEqual(previewStartOperation(h.state, key), {
    action: "blocked",
    reason: "unknown",
    operationId: id1,
  });
  assert.equal(h.idCount(), 1, "open-only 分支绝不铸造第二个 ID");
});

test("F5: 迟到旧回执不得结算后来或新登录的 operation", () => {
  const h = harness();
  const key = previewIntentKey(A) + ":m1";
  const first = previewStartOperation(h.state, key);
  const id1 = first.action === "write" ? first.operationId : "";
  settlePreviewStart(h.state, key, id1, "succeeded");

  // Stop 后用户再次明确启动 = 新操作，新 ID（不永久锁死）
  const restart = previewStartOperation(h.state, key);
  assert.equal(restart.action, "write");
  const id2 = restart.action === "write" ? restart.operationId : "";
  assert.notEqual(id2, id1);

  // 旧 ID 的迟到回执不得结算当前 entry
  settlePreviewStart(h.state, key, id1, "unknown");
  assert.equal(peekPreviewStart(h.state, key), "inflight");

  // 旧 ID 的迟到“成功”回执同样不得把当前写标记为已成功
  settlePreviewStart(h.state, key, id1, "succeeded");
  assert.equal(peekPreviewStart(h.state, key), "inflight");

  // 重新登录/卸载：该登录的写记录全部清空，后续必须是全新 ID
  clearPreviewStarts(h.state);
  assert.equal(peekPreviewStart(h.state, key), undefined);
  const afterRelogin = previewStartOperation(h.state, key);
  assert.equal(afterRelogin.action, "write");
  assert.notEqual(afterRelogin.action === "write" ? afterRelogin.operationId : "", id2);
});

test("F1/F5: 面板路径不占完成窗口；同 scope 的 auto/explicit Start/打开共享去重", () => {
  const h = harness();
  const open = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(open);
  const openBlank = open.blank as FakeWindow;
  releasePreviewIntent(h.state, open); // 需要授权：关掉自己的 blank，转面板
  assert.equal(openBlank.closed, true);
  // 面板上的 Start 可立即使用：未导航的面板路径本就不占用完成窗口
  const start = beginPreviewIntent(h.state, { scope: A, gesture: "start" });
  assert.ok(start);
  assert.equal(h.blanks.length, 2);

  // 同 scope 的 explicit Start 刚完成后，auto 打开入口在窗口内共享去重
  markPreviewNavigated(start);
  releasePreviewIntent(h.state, start);
  h.tick(120);
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "open" }), undefined);
  h.tick(160); // 280ms
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "open" }), undefined);
  // explicit Start 自己的重复手势同样被吞
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "start" }), undefined);

  // 窗口过期后显式打开仍可用（不永久锁死）
  h.tick(800);
  const reopen = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(reopen);

  // 明确 Stop 成功后只清导航去重，可立即重开
  markPreviewNavigated(reopen!);
  releasePreviewIntent(h.state, reopen);
  assert.equal(beginPreviewIntent(h.state, { scope: A, gesture: "open" }), undefined);
  clearPreviewCompletion(h.state);
  const afterStop = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(afterStop);
});

test("F2: scope 变化单调失效，跨 scope 不共享去重窗口", () => {
  const h = harness();
  const first = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  markPreviewNavigated(first!);
  releasePreviewIntent(h.state, first);
  invalidatePreviewIntent(h.state, "task");
  h.tick(0);
  // 失效后去重窗口被清除，新 scope 立即可用
  const other = beginPreviewIntent(h.state, { scope: B, gesture: "open" });
  assert.ok(other);
});

test("invalidate 与导航状态：不关闭用户已打开的页面，只关自己的 blank", () => {
  const h = harness();
  const first = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(first);
  const navigatedBlank = first.blank as FakeWindow;
  markPreviewNavigated(first);
  invalidatePreviewIntent(h.state, "auth");
  assert.equal(navigatedBlank.closed, false, "已导航的真实页面不得被失效逻辑关闭");
  releasePreviewIntent(h.state, first);
  assert.equal(navigatedBlank.closed, false);

  const second = beginPreviewIntent(h.state, { scope: A, gesture: "open" });
  assert.ok(second);
  const secondBlank = second.blank as FakeWindow;
  markPreviewNavigated(second);
  releasePreviewIntent(h.state, second);
  assert.equal(secondBlank.closed, false);
  assert.equal(h.blanks.length, 2);
});

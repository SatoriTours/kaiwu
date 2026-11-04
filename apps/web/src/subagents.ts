import type { SubagentSummary, SubagentEvent, SubagentStatus } from './types.ts';

export function summarizeSubagents(all: Record<string, SubagentSummary> | undefined, projectId: string, conversationId: string) {
  return sortSubagentSummaries(Object.values(all ?? {}).filter(run => run.projectId === projectId && run.conversationId === conversationId));
}
export function isRunTerminal(status: SubagentStatus) { return !['starting', 'running'].includes(status); }
export function formatSubagentStatusLabel(status: SubagentStatus) {
  return { starting: '启动中', running: '运行中', completed: '已结束', failed: '执行失败', interrupted: '已中断', unknown: '待核查' }[status];
}
export function sortSubagentSummaries(runs: SubagentSummary[]) {
  const rank = (s: SubagentStatus) => ({ running: 0, starting: 1, unknown: 2, completed: 3, failed: 3, interrupted: 3 })[s];
  return [...runs].sort((a, b) => rank(a.status) - rank(b.status) || b.startedAt.localeCompare(a.startedAt) || a.id.localeCompare(b.id));
}
export function formatSubagentBadgeText(runs: Pick<SubagentSummary, 'status'>[]) {
  if (!runs.length) return null;
  const running = runs.filter(r => r.status === 'running').length;
  const starting = runs.filter(r => r.status === 'starting').length;
  const unknown = runs.filter(r => r.status === 'unknown').length;
  const suffix = unknown ? ` (${unknown}待核查)` : '';
  return {
    text: running ? (unknown ? `◉ ${running} 个运行中${suffix}` : `◉ ${running} 个子 Agent 运行中`)
      : starting ? `${starting} 个子 Agent 启动中${suffix}`
      : unknown ? `子 Agent · ${unknown} 待核查` : `子 Agent · ${runs.length}`,
    tone: running ? 'running' : starting ? 'starting' : unknown ? 'unknown' : 'history',
    activeCount: (running || starting) + unknown,
    hasUnknown: unknown > 0,
  };
}
export function mergeSubagentEvents(existing: SubagentEvent[], incoming: SubagentEvent[]) {
  return [...new Map([...existing, ...incoming].map(e => [e.sequence, e])).values()].sort((a, b) => a.sequence - b.sequence);
}
export function shouldFetchSubagentIncremental(loaded: number | undefined, current: number) { return loaded === undefined || current > loaded; }
export function createSubagentQueryState() {
  let generation = 0, key = '';
  return {
    nextRequest(project: string, conversation: string, run: string) { key = JSON.stringify([project, conversation, run]); return ++generation; },
    isCurrent(token: number, project: string, conversation: string, run: string) { return token === generation && key === JSON.stringify([project, conversation, run]); },
    reset() { ++generation; key = ''; },
  };
}

import type { Snapshot } from "./types.ts";
export function isStartRequest(text: string): boolean {
  return text.trim().replace(/[。！!.]+$/, "") === "开始开发";
}
// randomUUID requires HTTPS outside localhost; getRandomValues also works on LAN HTTP.
export function createOperationID(
  random = (bytes: Uint8Array) => crypto.getRandomValues(bytes),
): string {
  return Array.from(random(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
export interface LocalState {
  current: string;
  drafts: Record<string, { text: string; revision: number }>;
  scroll: Record<string, number>;
  pending: Record<string, boolean>;
  snapshot: Snapshot | null;
}
export function createLocalState(): LocalState {
  return { current: "", drafts: {}, scroll: {}, pending: {}, snapshot: null };
}
export function selectConversation(local: LocalState, id: string) {
  local.current = id;
  local.drafts[id] ??= { text: "", revision: 0 };
}
export function setDraft(local: LocalState, text: string) {
  if (!local.current) return;
  const d = (local.drafts[local.current] ??= { text: "", revision: 0 });
  d.text = text;
  d.revision++;
}
export function acceptSent(local: LocalState, id: string, revision: number) {
  const d = local.drafts[id];
  if (d?.revision === revision) {
    d.text = "";
    d.revision++;
  }
}
export function mergeSnapshot(local: LocalState, snapshot: Snapshot) {
  if (!local.snapshot || snapshot.revision >= local.snapshot.revision)
    local.snapshot = snapshot;
}
export function createRefreshQueue(work: () => Promise<void>) {
  let running: Promise<void> | undefined,
    again = false;
  return () => {
    again = true;
    if (!running)
      running = (async () => {
        while (again) {
          again = false;
          await work();
        }
      })().finally(() => {
        running = undefined;
      });
    return running;
  };
}

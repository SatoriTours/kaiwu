// Never embed a raw URL reported by an Agent. Only the server capability route
// may become a frame target; traversal and encoded separators are rejected.
export function safePreviewURL(value?: string): string | undefined {
 if (!value || !/^\/preview\/[A-Za-z0-9_-]{32,128}\//.test(value) || /[\\\x00-\x20]/.test(value)) return;
 try {
  const url = new URL(value, 'http://kaiwu.invalid');
  if (url.origin !== 'http://kaiwu.invalid' || url.hash) return;
  for (const part of value.split(/[/?#]/)) {
   const decoded = decodeURIComponent(part);
   if (decoded === '.' || decoded === '..' || /[/\\]/.test(decoded)) return;
  }
  return url.pathname + url.search;
 } catch { return; }
}

import type { TaskPreview, PreviewArtifact } from './types.ts';

/** Downloads are a narrower route than a webpage: no query, encoding or arbitrary path. */
export function safeArtifactURL(value?: string): string | undefined {
  return value && /^\/preview\/[0-9a-f]{64}\/~download\/[0-9a-f]{64}$/.test(value) ? value : undefined;
}

/** Status has no URLs. Retain only still-identical live capabilities, never mint in background. */
export function preserveArtifactLinks(current: TaskPreview, previous?: TaskPreview): PreviewArtifact[] | undefined {
  const same = current.state === 'running' && !!current.manifestId && current.manifestId === previous?.manifestId && current.taskId === previous.taskId && current.roundId === previous.roundId;
  return current.artifacts?.map(file => {
    const prior = same ? previous?.artifacts?.find(old => old.id === file.id && old.name === file.name && old.size === file.size && old.type === file.type) : undefined;
    const url = safeArtifactURL(file.url ?? prior?.url);
    return { ...file, url: url?.endsWith('/' + file.id) ? url : undefined };
  });
}

export function isWebPreviewable(preview?: TaskPreview): boolean {
  if (!preview || preview.kind === 'files') return false;
  if (preview.kind === 'static' && preview.entry) return true;
  if (preview.state === 'running' && !!preview.url) return true;
  if (Array.isArray(preview.command) && preview.command.length > 0) return true;
  return false;
}

// In-memory set of approved manifestIds for the current browser session.
// Maps "taskId:roundId:manifestId" to true.
const approvedManifests = new Set<string>();

export function isManifestApproved(taskId: string, roundId: string, manifestId?: string): boolean {
  if (!manifestId) return false;
  return approvedManifests.has(`${taskId}:${roundId}:${manifestId}`);
}

export function recordManifestApproval(taskId: string, roundId: string, manifestId?: string): void {
  if (!manifestId) return;
  approvedManifests.add(`${taskId}:${roundId}:${manifestId}`);
}

export function clearManifestApprovals(): void {
  approvedManifests.clear();
}

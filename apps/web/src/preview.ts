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

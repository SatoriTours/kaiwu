export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      /* Private-LAN HTTP or denied clipboard permission: use the user click below. */
    }
  }
  const active = document.activeElement as HTMLElement | null;
  const selection = document.getSelection();
  const ranges = selection
    ? Array.from({ length: selection.rangeCount }, (_, i) =>
        selection.getRangeAt(i).cloneRange(),
      )
    : [];
  const field = document.createElement("textarea");
  field.value = text;
  field.setAttribute("readonly", "");
  field.style.cssText = "position:fixed;left:-9999px;top:0;opacity:0";
  // Modal dialogs make the rest of the document inert, including a body-level copy field.
  (active?.closest('dialog[open]') ?? document.querySelector('dialog:modal') ?? document.body).append(field);
  try {
    field.select();
    if (!document.execCommand("copy")) throw new Error("clipboard unavailable");
  } finally {
    field.remove();
    active?.focus({ preventScroll: true });
    selection?.removeAllRanges();
    for (const range of ranges) selection?.addRange(range);
  }
}

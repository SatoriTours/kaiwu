// Blocking, same-origin bootstrap: apply the preference before CSS/app paint.
// Kept external to preserve the server's script-src 'self' policy.
(() => {
  let preference = 'system';
  try {
    const saved = localStorage.getItem('kaiwu.theme');
    if (saved === 'light' || saved === 'dark') preference = saved;
  } catch { /* Restricted storage still follows the system. */ }
  document.documentElement.dataset.theme = preference;
})();

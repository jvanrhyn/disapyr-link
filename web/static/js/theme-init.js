// theme-init.js — NO defer: runs synchronously before first paint to prevent FOUC.
// Reads localStorage and applies data-theme to <html> immediately.
(function () {
  try {
    var saved = localStorage.getItem('disapyr-theme');
    if (saved === 'light' || saved === 'dark') {
      document.documentElement.setAttribute('data-theme', saved);
    }
  } catch (_) {
    // Storage access denied (privacy mode, sandboxed iframe, etc.) — system default applies.
  }
}());

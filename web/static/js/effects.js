(function () {
  'use strict';

  var CHARS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789!@#$%^&*';

  function scramble(el, duration) {
    var text = el.dataset.original || el.textContent.trim();
    el.dataset.original = text;
    var len = text.length;
    var start = null;

    function tick(ts) {
      if (!start) start = ts;
      var p = Math.min((ts - start) / duration, 1);
      var resolved = Math.floor(p * len);
      var out = '';
      for (var i = 0; i < len; i++) {
        if (i < resolved || text[i] === ' ') {
          out += text[i];
        } else {
          out += CHARS[Math.floor(Math.random() * CHARS.length)];
        }
      }
      el.textContent = out;
      if (p < 1) requestAnimationFrame(tick);
    }

    requestAnimationFrame(tick);
  }

  function initScramble() {
    document.querySelectorAll('.scramble-target').forEach(function (el) {
      setTimeout(function () { scramble(el, 850); }, 120);
    });
  }

  // Re-scramble headings when a hidden card becomes visible
  var observer = new MutationObserver(function (mutations) {
    mutations.forEach(function (m) {
      if (m.type === 'attributes' && m.attributeName === 'class') {
        var el = m.target;
        if (!el.classList.contains('hidden')) {
          var heading = el.querySelector('.scramble-target');
          if (heading) setTimeout(function () { scramble(heading, 600); }, 40);
        }
      }
    });
  });

  document.addEventListener('DOMContentLoaded', function () {
    initScramble();
    initThemeToggle();
    document.querySelectorAll('.card').forEach(function (card) {
      observer.observe(card, { attributes: true });
    });
  });

  // ─── Theme toggle ────────────────────────────────────────────────────────
  // States: 'system' (no attribute) → 'light' → 'dark' → 'system'
  function initThemeToggle() {
    var btn = document.getElementById('theme-toggle');
    if (!btn) return;

    // Determine current state from the data-theme attribute set by theme-init.js
    function getCurrentState() {
      var attr = document.documentElement.getAttribute('data-theme');
      if (attr === 'light') return 'light';
      if (attr === 'dark') return 'dark';
      return 'system';
    }

    function applyState(state) {
      // Always update DOM first so UI is never inconsistent, even if storage fails.
      if (state === 'system') {
        document.documentElement.removeAttribute('data-theme');
        btn.setAttribute('aria-label', 'Currently following system theme — click for light mode');
      } else if (state === 'light') {
        document.documentElement.setAttribute('data-theme', 'light');
        btn.setAttribute('aria-label', 'Currently light mode — click for dark mode');
      } else {
        document.documentElement.setAttribute('data-theme', 'dark');
        btn.setAttribute('aria-label', 'Currently dark mode — click for system theme');
      }
      btn.setAttribute('data-state', state);
      // Persist — best-effort; storage may be unavailable in privacy-restricted environments.
      try {
        if (state === 'system') {
          localStorage.removeItem('disapyr-theme');
        } else {
          localStorage.setItem('disapyr-theme', state);
        }
      } catch (_) { /* storage denied — theme still applied visually */ }
    }

    // Set initial button state without overwriting the theme (theme-init.js already applied it)
    applyState(getCurrentState());

    btn.addEventListener('click', function () {
      var cur = getCurrentState();
      var next = cur === 'system' ? 'light' : cur === 'light' ? 'dark' : 'system';
      applyState(next);
    });
  }
})();

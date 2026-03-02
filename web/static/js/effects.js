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
  // Two buttons: system (monitor) + explicit light/dark toggle (sun/moon).
  // States: 'system' (no data-theme attr) | 'light' | 'dark'
  function initThemeToggle() {
    var sysBtn      = document.getElementById('theme-btn-system');
    var explicitBtn = document.getElementById('theme-btn-explicit');
    if (!sysBtn || !explicitBtn) return;

    var darkMQ = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)');

    function getCurrentState() {
      var attr = document.documentElement.getAttribute('data-theme');
      if (attr === 'light') return 'light';
      if (attr === 'dark')  return 'dark';
      return 'system';
    }

    // Effective theme: what the user actually sees (resolves 'system' via matchMedia).
    function getEffectiveTheme() {
      var state = getCurrentState();
      if (state !== 'system') return state;
      return (darkMQ && darkMQ.matches) ? 'dark' : 'light';
    }

    function updateButtons(state) {
      // System button: active when in system mode.
      if (state === 'system') {
        sysBtn.setAttribute('aria-label', 'System theme active');
        sysBtn.setAttribute('data-active', 'true');
      } else {
        sysBtn.setAttribute('aria-label', 'Follow system theme');
        sysBtn.removeAttribute('data-active');
      }

      // Explicit button reflects the *effective* current theme (what user sees),
      // so clicking it always produces a meaningful visible change.
      var effective = (state === 'system')
        ? ((darkMQ && darkMQ.matches) ? 'dark' : 'light')
        : state;

      if (effective === 'light') {
        explicitBtn.setAttribute('aria-label', 'Switch to dark mode');
        explicitBtn.setAttribute('data-state', 'light');
      } else {
        explicitBtn.setAttribute('aria-label', 'Switch to light mode');
        explicitBtn.setAttribute('data-state', 'dark');
      }
    }

    function applyState(state) {
      // Update data-theme on <html> first (DOM always wins over storage).
      if (state === 'system') {
        document.documentElement.removeAttribute('data-theme');
      } else {
        document.documentElement.setAttribute('data-theme', state);
      }
      updateButtons(state);
      // Persist — best-effort; storage may be unavailable in privacy-restricted environments.
      try {
        if (state === 'system') {
          localStorage.removeItem('disapyr-theme');
        } else {
          localStorage.setItem('disapyr-theme', state);
        }
      } catch (_) { /* storage denied — theme still applied visually */ }
    }

    // Reflect current state (set by theme-init.js) into button UI without re-applying theme.
    updateButtons(getCurrentState());

    // While in system mode, keep button icon in sync with OS preference changes.
    if (darkMQ && darkMQ.addEventListener) {
      darkMQ.addEventListener('change', function () {
        if (getCurrentState() === 'system') updateButtons('system');
      });
    }

    // System button: always sets system mode.
    sysBtn.addEventListener('click', function () {
      applyState('system');
    });

    // Explicit button: toggles away from current *effective* theme.
    explicitBtn.addEventListener('click', function () {
      applyState(getEffectiveTheme() === 'light' ? 'dark' : 'light');
    });
  }
})();

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
    document.querySelectorAll('.card').forEach(function (card) {
      observer.observe(card, { attributes: true });
    });
  });
})();

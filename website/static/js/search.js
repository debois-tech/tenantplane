// Client-side docs search over the Hugo-generated index.json.
// Self-contained: no external services, works offline.
document.addEventListener('DOMContentLoaded', function () {
  var input = document.getElementById('docs-search');
  var results = document.getElementById('search-results');
  if (!input || !results) return;

  var pages = null;

  function load() {
    if (pages) return Promise.resolve(pages);
    return fetch(input.dataset.index)
      .then(function (r) { return r.json(); })
      .then(function (d) { pages = d; return d; });
  }

  function esc(s) {
    return String(s).replace(/[&<>"]/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c];
    });
  }

  function close() {
    results.classList.remove('open');
    results.innerHTML = '';
  }

  function render(items, q) {
    if (!q) { close(); return; }
    var html = items.slice(0, 8).map(function (p) {
      return '<a href="' + esc(p.url) + '">' +
        '<span class="r-title">' + esc(p.title) + '</span>' +
        '<span class="r-desc">' + esc(p.description || '') + '</span></a>';
    }).join('');
    if (!html) html = '<div class="r-empty">No results for “' + esc(q) + '”</div>';
    results.innerHTML = html;
    results.classList.add('open');
  }

  input.addEventListener('input', function () {
    var q = input.value.trim().toLowerCase();
    if (!q) { close(); return; }
    load().then(function (d) {
      var words = q.split(/\s+/);
      var matched = d.map(function (p) {
        var hay = (p.title + ' ' + (p.description || '') + ' ' + (p.content || '')).toLowerCase();
        for (var i = 0; i < words.length; i++) {
          if (hay.indexOf(words[i]) === -1) return null;
        }
        var score = 0;
        for (var j = 0; j < words.length; j++) {
          if (p.title.toLowerCase().indexOf(words[j]) !== -1) score += 10;
          if ((p.description || '').toLowerCase().indexOf(words[j]) !== -1) score += 4;
          score += 1;
        }
        return { p: p, score: score };
      }).filter(Boolean).sort(function (a, b) { return b.score - a.score; })
        .map(function (x) { return x.p; });
      render(matched, q);
    });
  });

  input.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { input.value = ''; close(); input.blur(); }
  });

  document.addEventListener('click', function (e) {
    if (e.target !== input && !results.contains(e.target)) close();
  });

  // Animated "typewriter" placeholder that cycles sample queries while the box
  // is idle. On focus (click) it switches to a stable placeholder; it resumes
  // on blur if the box is still empty. Honors prefers-reduced-motion.
  var STATIC_PH = 'Search tenantplane docs…';
  var SAMPLES = [
    'Search “explain-sync”…',
    'Search “isolation profiles”…',
    'Search “deploy on EKS”…',
    'Search “TenantCluster”…',
    'Search “sync policy”…',
    'Search “quickstart”…'
  ];
  var reduce = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var tSample = 0, tChar = 0, deleting = false, phTimer = null;

  function typeTick() {
    var full = SAMPLES[tSample];
    if (!deleting) {
      tChar++;
      input.placeholder = full.slice(0, tChar);
      if (tChar >= full.length) { deleting = true; phTimer = setTimeout(typeTick, 1500); return; }
      phTimer = setTimeout(typeTick, 55 + Math.random() * 50);
    } else {
      tChar--;
      input.placeholder = full.slice(0, tChar);
      if (tChar <= 0) { deleting = false; tSample = (tSample + 1) % SAMPLES.length; phTimer = setTimeout(typeTick, 400); return; }
      phTimer = setTimeout(typeTick, 28);
    }
  }

  function startPlaceholder() {
    if (reduce || phTimer || document.activeElement === input) return;
    tChar = 0; deleting = false;
    typeTick();
  }

  function stopPlaceholder() { clearTimeout(phTimer); phTimer = null; }

  input.addEventListener('focus', function () {
    stopPlaceholder();
    input.placeholder = STATIC_PH;
  });
  input.addEventListener('blur', function () {
    if (!input.value) startPlaceholder();
  });

  if (reduce) {
    input.placeholder = STATIC_PH;
  } else {
    setTimeout(startPlaceholder, 600); // brief hold so the box isn't blank on load
  }
});

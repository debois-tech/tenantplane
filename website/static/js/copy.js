// Copy-to-clipboard icon buttons on every code block. Self-contained, no deps.
document.addEventListener('DOMContentLoaded', function () {
  var copyIcon =
    '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<rect x="9" y="9" width="13" height="13" rx="2"/>' +
    '<path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>';
  var checkIcon =
    '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
    '<path d="M20 6 9 17l-5-5"/></svg>';

  document.querySelectorAll('pre').forEach(function (pre) {
    // Wrap the <pre> in a non-scrolling container so the button can anchor to
    // it and stay pinned while the code scrolls horizontally underneath.
    var wrap = pre.parentNode;
    if (!wrap || !wrap.classList.contains('code-wrap')) {
      wrap = document.createElement('div');
      wrap.className = 'code-wrap';
      pre.parentNode.insertBefore(wrap, pre);
      wrap.appendChild(pre);
    }

    var btn = document.createElement('button');
    btn.className = 'copy-btn';
    btn.type = 'button';
    btn.innerHTML = copyIcon;
    btn.title = 'Copy to clipboard';
    btn.setAttribute('aria-label', 'Copy code to clipboard');

    btn.addEventListener('click', function () {
      var code = pre.querySelector('code');
      var text = (code || pre).innerText.replace(/\n$/, '');
      navigator.clipboard.writeText(text).then(function () {
        btn.innerHTML = checkIcon;
        btn.classList.add('copied');
        setTimeout(function () {
          btn.innerHTML = copyIcon;
          btn.classList.remove('copied');
        }, 1600);
      }).catch(function () {
        btn.title = 'Copy failed — select the text and press Ctrl+C';
      });
    });

    wrap.appendChild(btn);
  });
});

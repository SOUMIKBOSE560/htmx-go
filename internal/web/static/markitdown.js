// Markitdown converter — streams dummy OpenAI-style SSE deltas from
// GET /markitdown/stream and renders the accumulating markdown live as a
// formatted invoice view in the right panel.
(function () {
  'use strict';

  function esc(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function inline(s) {
    s = esc(s);
    s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    s = s.replace(/_(.+?)_/g, '<em>$1</em>');
    return s;
  }

  // Minimal markdown renderer: headings, hr, blockquote, tables, lists, paragraphs.
  function renderMarkdown(md) {
    var lines = md.split('\n');
    var html = '';
    var inTable = false;
    var inList = false;
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];
      var t = line.trim();
      if (/^\|.*\|$/.test(t)) {
        var cells = t.split('|').slice(1, -1).map(function (c) { return c.trim(); });
        if (/^:?-+:?$/.test(cells[0].replace(/ /g, ''))) continue; // separator row
        if (!inTable) { html += '<table>'; inTable = true; }
        var tag = inTable && html.slice(-7) === '<table>' ? 'th' : 'td';
        html += '<tr>' + cells.map(function (c) { return '<' + tag + '>' + inline(c) + '</' + tag + '>'; }).join('') + '</tr>';
        continue;
      }
      if (inTable) { html += '</table>'; inTable = false; }
      if (t === '---') { html += '<hr>'; continue; }
      if (t.indexOf('# ') === 0) { html += '<h1>' + inline(t.slice(2)) + '</h1>'; continue; }
      if (t.indexOf('## ') === 0) { html += '<h2>' + inline(t.slice(3)) + '</h2>'; continue; }
      if (t.indexOf('> ') === 0) { html += '<blockquote>' + inline(t.slice(2)) + '</blockquote>'; continue; }
      if (t.indexOf('- ') === 0) {
        if (!inList) { html += '<ul>'; inList = true; }
        html += '<li>' + inline(t.slice(2)) + '</li>';
        continue;
      }
      if (inList) { html += '</ul>'; inList = false; }
      if (t === '') continue;
      html += '<p>' + inline(line) + '</p>';
    }
    if (inTable) html += '</table>';
    if (inList) html += '</ul>';
    return html;
  }

  function init() {
    var input = document.getElementById('md-input');
    var btn = document.getElementById('md-convert');
    var clear = document.getElementById('md-clear');
    var out = document.getElementById('md-output');
    var status = document.getElementById('md-status');
    if (!input || !btn || !out) return;

    var es = null;

    function setStatus(t) { if (status) status.textContent = t; }

    function stop() {
      if (es) { es.close(); es = null; }
      btn.disabled = false;
      btn.textContent = 'Convert';
    }

    if (clear) clear.addEventListener('click', function () {
      stop();
      input.value = '';
      out.innerHTML = '<span class="md-empty">Nothing converted yet. Your invoice will appear here.</span>';
      setStatus('Idle.');
      input.focus();
    });

    btn.addEventListener('click', function () {
      stop();
      var text = input.value.trim();
      var md = '';
      btn.disabled = true;
      btn.textContent = 'Streaming…';
      setStatus('Converting… streaming deltas.');
      out.innerHTML = '<span class="md-cursor"></span>';

      es = new EventSource('/markitdown/stream?text=' + encodeURIComponent(text));
      es.onmessage = function (ev) {
        var msg;
        try { msg = JSON.parse(ev.data); } catch (e) { return; }
        if (msg.done) {
          stop();
          setStatus('Done. Invoice ready.');
          return;
        }
        if (msg.delta) {
          md += msg.delta;
          out.innerHTML = renderMarkdown(md) + '<span class="md-cursor"></span>';
          out.scrollTop = out.scrollHeight;
        }
      };
      es.onerror = function () {
        stop();
        setStatus('Stream interrupted. Try again.');
      };
    });
  }

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();

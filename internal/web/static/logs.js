// Live backend log viewer — terminal-style stream.
// Opens an overlay terminal that subscribes to /logs/stream (SSE),
// prepends a prompt + timestamp to each line, and appends new lines
// as they arrive. Reconnects automatically while the modal is open.
(function () {
  'use strict';

  var toggle = document.getElementById('logs-toggle');
  var modal = document.getElementById('logs-modal');
  var close = document.getElementById('logs-close');
  var terminal = document.getElementById('logs-terminal');
  var statusEl = document.getElementById('logs-status');
  if (!toggle || !modal || !close || !terminal || !statusEl) return;

  var MAX_LINES = 500;
  var controller = null;
  var retryTimer = null;
  var backoff = 500;

  function formatTime(iso) {
    try {
      var d = new Date(iso);
      var h = String(d.getHours()).padStart(2, '0');
      var m = String(d.getMinutes()).padStart(2, '0');
      var s = String(d.getSeconds()).padStart(2, '0');
      return h + ':' + m + ':' + s;
    } catch (e) {
      return '';
    }
  }

  function setStatus(text, cls) {
    statusEl.textContent = text;
    statusEl.className = 'logs-status' + (cls ? ' ' + cls : '');
  }

  function append(line) {
    var row = document.createElement('div');
    row.className = 'log-line ' + (line.color || 'default');

    var ts = document.createElement('span');
    ts.className = 'log-ts';
    ts.textContent = formatTime(line.ts);

    var prompt = document.createElement('span');
    prompt.className = 'log-prompt';
    prompt.textContent = '›';

    var msg = document.createElement('span');
    msg.className = 'log-msg';
    msg.textContent = line.text;

    row.appendChild(ts);
    row.appendChild(prompt);
    row.appendChild(msg);
    terminal.appendChild(row);

    while (terminal.children.length > MAX_LINES) terminal.removeChild(terminal.firstChild);
    terminal.scrollTop = terminal.scrollHeight;
  }

  async function readStream(res, onEvent) {
    var reader = res.body.getReader();
    var decoder = new TextDecoder();
    var buffer = '';
    var lastData = Date.now();
    while (true) {
      var chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, { stream: true });
      var idx;
      while ((idx = buffer.indexOf('\n\n')) !== -1) {
        var frame = buffer.slice(0, idx);
        buffer = buffer.slice(idx + 2);
        var dataLine = null;
        frame.split('\n').forEach(function (l) {
          if (l.indexOf('data: ') === 0) dataLine = l.slice(6);
        });
        if (dataLine === null) continue;
        lastData = Date.now();
        try { onEvent(JSON.parse(dataLine)); } catch (e) { /* skip malformed */ }
      }
    }
    return Date.now() - lastData;
  }

  async function connect() {
    controller = new AbortController();
    setStatus('connecting…');
    try {
      var res = await fetch('/logs/stream', { signal: controller.signal });
      if (!res.ok || !res.body) throw new Error('HTTP ' + res.status);
      backoff = 500;
      setStatus('live', 'live');
      var idle = await readStream(res, append);
      if (controller.signal.aborted) return;
      setStatus('reconnecting…');
    } catch (err) {
      if (controller.signal.aborted) return;
      setStatus('unavailable (' + err.message + ')', 'error');
    }
    scheduleReconnect();
  }

  function scheduleReconnect() {
    if (modal.hidden) return;
    clearTimeout(retryTimer);
    retryTimer = setTimeout(function () {
      backoff = Math.min(backoff * 2, 10000);
      connect();
    }, backoff);
  }

  function open() {
    modal.hidden = false;
    terminal.textContent = '';
    setStatus('connecting…');
    connect();
  }

  function closeModal() {
    modal.hidden = true;
    if (controller) controller.abort();
    clearTimeout(retryTimer);
    setStatus('');
  }

  toggle.addEventListener('click', open);
  close.addEventListener('click', closeModal);
  modal.addEventListener('click', function (e) {
    if (e.target === modal) closeModal();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !modal.hidden) closeModal();
  });
})();

'use strict';

const $ = (id) => document.getElementById(id);
const BAUDS = [4800, 9600, 19200, 38400, 57600, 115200];

let state = null;
let mountList = [];
let formDirty = false;
let lastTraffic = { toApp: 0, toMount: 0 };
let flowTimer = null;

// Red-light mode is remembered per browser.
(function initRedLight() {
  let on = false;
  try { on = localStorage.getItem('skybridge.light') === 'red'; } catch (e) { /* storage blocked */ }
  setRedLight(on);
  $('redlight').addEventListener('click', () => setRedLight(document.documentElement.dataset.light !== 'red'));
})();

function setRedLight(on) {
  document.documentElement.dataset.light = on ? 'red' : '';
  $('redlight').setAttribute('aria-pressed', on ? 'true' : 'false');
  try { localStorage.setItem('skybridge.light', on ? 'red' : ''); } catch (e) { /* ignore */ }
}

// shortPath keeps the device name: /dev/cu.usbserial-110 stays as it is,
// a long path elsewhere becomes its last segment.
function shortPath(p) {
  if (!p || p.startsWith('/dev/') || !p.includes('/')) return p || '';
  return p.slice(p.lastIndexOf('/') + 1);
}

// hostOf strips the port from "host:port" or "[v6]:port".
function hostOf(addr) {
  const i = addr.lastIndexOf(':');
  if (i < 0) return addr;
  const host = addr.slice(0, i);
  return host.startsWith('[') ? host.slice(1, -1) : host;
}

function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' kB';
  return (n / (1024 * 1024)).toFixed(2) + ' MB';
}

async function api(path, body) {
  const res = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

// Chain and header

function renderState(s) {
  // A reply to a request can arrive after a newer live event.
  if (state && s.seq < state.seq) return;
  state = s;
  $('version').textContent = s.version && s.version !== 'dev' ? s.version : '';

  const toggle = $('toggle');
  toggle.disabled = false;
  toggle.textContent = s.running ? 'Stop' : 'Start';
  toggle.className = s.running ? 'quiet' : 'primary';

  // SkySafari side
  const client = s.client || {};
  // SkySafari reconnects constantly, so its port number is noise: show the host.
  $('app-sub').textContent = client.connected
    ? 'connected from ' + hostOf(client.addr)
    : (s.running ? 'not connected yet' : 'not connected');
  $('app-sub').title = client.connected ? client.addr : '';
  $('link-app').dataset.live = client.connected ? 'true' : 'false';
  $('link-app-sub').textContent = s.running ? fmtBytes(s.traffic.toMount) + ' sent to the mount' : '';

  // Bridge
  $('bridge-sub').textContent = s.running ? 'listening on ' + s.listen : 'stopped';
  const err = $('bridge-err');
  err.hidden = !s.error;
  err.textContent = s.error || '';

  // Mount side
  const serial = s.serial || {};
  $('mount-name').textContent = s.settings.mount || 'Mount';
  const mountSub = $('mount-sub');
  mountSub.title = '';
  if (serial.status === 'open') {
    mountSub.textContent = shortPath(serial.path) + ', ' + serial.baud + ' baud';
    mountSub.title = serial.path;
  } else if (serial.status === 'waiting') {
    mountSub.textContent = s.settings.port ? 'waiting for ' + shortPath(s.settings.port) : 'waiting for a cable';
    mountSub.title = s.settings.port;
  } else {
    mountSub.textContent = 'not connected';
  }
  $('link-mount').dataset.live = serial.status === 'open' ? 'true' : 'false';
  $('link-mount-sub').textContent = s.running ? fmtBytes(s.traffic.toApp) + ' received from the mount' : '';

  // Show flow while bytes move.
  const t = s.traffic || { toApp: 0, toMount: 0 };
  if (t.toApp !== lastTraffic.toApp || t.toMount !== lastTraffic.toMount) {
    lastTraffic = t;
    for (const id of ['link-app', 'link-mount']) $(id).classList.add('flow');
    clearTimeout(flowTimer);
    flowTimer = setTimeout(() => {
      for (const id of ['link-app', 'link-mount']) $(id).classList.remove('flow');
    }, 700);
  }

  $('identify').disabled = s.running;
  $('identify').title = s.running ? 'Stop the bridge first; the mount can only talk to one program' : '';
  if (s.probe) renderProbe(s.probe);
  if (!formDirty) fillForm(s.settings);
}

$('toggle').addEventListener('click', async () => {
  const b = $('toggle');
  b.disabled = true;
  try {
    renderState(await api(state && state.running ? '/api/stop' : '/api/start', {}));
  } catch (e) {
    note($('setup-msg'), e.message, false);
    b.disabled = false;
  }
});

// Log

const MAX_LOG = 300;
function logLine(l) {
  const li = document.createElement('li');
  if (l.level !== 'info') li.className = l.level;
  const t = document.createElement('span');
  t.className = 't';
  t.textContent = l.time + ' '; // real whitespace, so copied text keeps it
  li.append(t, l.text);
  return li;
}
function appendLog(lines, replace) {
  const ol = $('log');
  const stick = replace || ol.scrollTop + ol.clientHeight >= ol.scrollHeight - 24;
  if (replace) ol.replaceChildren();
  for (const l of lines) ol.appendChild(logLine(l));
  while (ol.childElementCount > MAX_LOG) ol.removeChild(ol.firstElementChild);
  if (stick) ol.scrollTop = ol.scrollHeight;
}

// Setup form

function fillForm(cfg) {
  const port = $('port');
  if (cfg.port && ![...port.options].some((o) => o.value === cfg.port)) {
    port.appendChild(new Option(cfg.port, cfg.port));
  }
  port.value = cfg.port || '';
  if (mountList.length) $('mount').value = cfg.mount;
  fillBauds(cfg.mount, cfg.baud);
  $('listen').value = cfg.listen;
  $('verify').checked = !!cfg.verify;
  mountChanged();
}

function fillBauds(mountName, chosen) {
  const sel = $('baud');
  const m = mountList.find((x) => x.name === mountName);
  sel.replaceChildren();
  if (m && m.baud) sel.appendChild(new Option('Profile default, ' + m.baud + ' baud', '0'));
  else sel.appendChild(new Option('Choose a speed', '0'));
  for (const b of BAUDS) sel.appendChild(new Option(b + ' baud', String(b)));
  sel.value = String(chosen || 0);
}

function mountChanged() {
  const m = mountList.find((x) => x.name === $('mount').value);
  $('mount-hint').textContent = m ? m.covers + (m.notes ? ' ' + m.notes : '') : '';
  $('verify').disabled = !!m && !m.probe;
  if (m && !m.probe) $('verify').checked = false;
}

async function loadPorts() {
  const sel = $('port');
  const before = sel.value;
  try {
    const ports = await api('/api/ports');
    // The state may have arrived while the list was loading; it wins unless
    // the user has been editing.
    const keep = formDirty || !state ? before : (state.settings.port || '');
    sel.replaceChildren(new Option('Auto-detect', ''));
    for (const p of ports) {
      let label = p.path;
      const via = p.adapter || p.product;
      if (via) label += ' (' + via + ')';
      sel.appendChild(new Option(label, p.path));
    }
    if (keep && ![...sel.options].some((o) => o.value === keep)) sel.appendChild(new Option(keep, keep));
    sel.value = keep;
    if (ports.length === 0) note($('setup-msg'), 'No serial ports found. Is the mount on and the cable plugged in?', false);
  } catch (e) {
    note($('setup-msg'), 'Could not list ports: ' + e.message, false);
  }
}

async function loadMounts() {
  mountList = await api('/api/mounts');
  const sel = $('mount');
  sel.replaceChildren();
  for (const m of mountList) sel.appendChild(new Option(m.name, m.name));
}

function note(el, text, ok) {
  el.textContent = text;
  el.className = 'msg ' + (ok ? 'ok' : 'bad');
}

function readForm() {
  return {
    port: $('port').value,
    mount: $('mount').value,
    baud: Number($('baud').value) || 0,
    listen: $('listen').value.trim(),
    verify: $('verify').checked,
  };
}

$('setup').addEventListener('input', () => { formDirty = true; });
$('mount').addEventListener('change', () => { fillBauds($('mount').value, 0); mountChanged(); });
$('refresh').addEventListener('click', loadPorts);

$('setup').addEventListener('submit', async (ev) => {
  ev.preventDefault();
  const btn = $('apply');
  btn.disabled = true;
  try {
    const s = await api('/api/settings', readForm());
    formDirty = false;
    renderState(s);
    note($('setup-msg'), s.running ? 'Applied and restarted.' : 'Saved. Start the bridge to use these settings.', true);
  } catch (e) {
    note($('setup-msg'), e.message, false);
  } finally {
    btn.disabled = false;
  }
});

$('identify').addEventListener('click', async () => {
  const btn = $('identify');
  btn.disabled = true;
  note($('probe-msg'), 'Asking the mount who it is.', true);
  try {
    renderProbe(await api('/api/probe', {}));
  } catch (e) {
    note($('probe-msg'), e.message, false);
  } finally {
    btn.disabled = !!(state && state.running);
  }
});

function renderProbe(p) {
  const el = $('probe-msg');
  if (p.error) {
    note(el, p.at + ' ' + p.error, false);
    return;
  }
  note(el, p.at + ' ' + p.profile + ' answered on ' + p.path + ' at ' + p.baud + ' baud.', true);
  if (p.profile !== $('mount').value) {
    const use = document.createElement('button');
    use.type = 'button';
    use.className = 'quiet';
    use.textContent = 'Use ' + p.profile;
    use.addEventListener('click', () => {
      $('mount').value = p.profile;
      $('port').value = p.path;
      if (![...$('port').options].some((o) => o.value === p.path)) $('port').appendChild(new Option(p.path, p.path));
      $('port').value = p.path;
      fillBauds(p.profile, 0);
      mountChanged();
      formDirty = true;
      use.remove();
    });
    el.appendChild(use);
  }
}

// Live updates

function connect() {
  const es = new EventSource('/api/events');
  es.addEventListener('open', () => { $('offline').hidden = true; });
  es.addEventListener('error', () => { $('offline').hidden = false; $('toggle').disabled = true; });
  es.addEventListener('state', (ev) => renderState(JSON.parse(ev.data)));
  es.addEventListener('logs', (ev) => appendLog(JSON.parse(ev.data), true));
  es.addEventListener('log', (ev) => appendLog([JSON.parse(ev.data)], false));
}

(async function main() {
  try {
    await loadMounts();
  } catch (e) {
    note($('setup-msg'), 'Could not load mount profiles: ' + e.message, false);
  }
  connect();
  loadPorts();
})();

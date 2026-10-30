/* zcode2api dashboard — shared: token gate, header, fetch, toasts, helpers */
const LINKS = { github: 'https://github.com/D3-vin/Zcode2Api', telegram: 'https://t.me/D3_vin' };
var DASH_TOKEN = localStorage.getItem('dash_token') || '';
var authWaiters = [];

function dashHeaders(extra) {
  var h = extra || {};
  if (DASH_TOKEN) h['Authorization'] = 'Bearer ' + DASH_TOKEN;
  return h;
}
var DBG = localStorage.getItem('dash_debug') === '1';
function dbg(...a) { if (DBG) console.log('[dash]', ...a); }

async function j(url, opts) {
  opts = opts || {};
  opts.headers = dashHeaders(opts.headers || {});
  dbg('GET/POST', url);
  var res = await fetch(url, opts);
  dbg('←', url, res.status);
  if (res.status === 401) { dbg('401 → login'); localStorage.removeItem('dash_token'); location.href = '/login.html'; }
  var text = await res.text();
  dbg('body', url, text.slice(0, 300));
  var d;
  try { d = JSON.parse(text); }
  catch (e) {
    console.error('[dash] bad JSON from', url, '→', text.slice(0, 300), e);
    toast('Server returned non-JSON from ' + url, 'err');
    throw e;
  }
  return d;
}
function post(url, body) { return j(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) }); }

var JOB = null;
function jobShow(kind, total) {
  JOB = { kind: kind, total: total || 0, done: 0 };
  var bar = document.getElementById('job-bar');
  if (!bar) return;
  bar.classList.add('on');
  var k = document.getElementById('job-kind');
  if (k) k.textContent = kind;
  jobPaint('starting…');
}
function jobTick(ev) {
  if (!JOB || !ev) return;
  if (ev.event === 'done') { jobPaint(ev.detail || 'done'); return; }
  if (ev.total != null && ev.total > 0) JOB.total = ev.total;
  if (ev.done != null) JOB.done = ev.done;
  jobPaint(ev.detail || '');
}
function jobPaint(detail) {
  if (!JOB) return;
  var total = JOB.total || 0, done = JOB.done || 0;
  var pct = total ? Math.round(done / total * 100) : 0;
  var c = document.getElementById('job-count');
  var p = document.getElementById('job-pct');
  var f = document.getElementById('job-fill');
  var l = document.getElementById('job-label');
  if (c) c.textContent = done + ' / ' + total;
  if (p) p.textContent = pct + '%';
  if (f) f.style.width = pct + '%';
  if (l && detail) l.textContent = detail;
  else if (l && !l.textContent) l.textContent = (JOB.kind || 'job') + '…';
}
function jobHide() {
  JOB = null;
  var bar = document.getElementById('job-bar');
  if (bar) bar.classList.remove('on');
}
async function postProgress(url, body, kind) {
  jobShow(kind, 0);
  try {
    var res = await fetch(url, { method: 'POST', headers: dashHeaders({ 'Content-Type': 'application/json' }), body: JSON.stringify(body || {}) });
    if (res.status === 401) { localStorage.removeItem('dash_token'); location.href = '/login.html'; return; }
    var ct = (res.headers.get('content-type') || '');
    if (ct.indexOf('ndjson') < 0) {
      var text = await res.text();
      var d; try { d = JSON.parse(text); } catch (e) { throw e; }
      if (d && d.error) throw new Error(d.error);
      jobTick({ done: 1, total: 1, detail: 'done' });
      return d;
    }
    var reader = res.body.getReader();
    var dec = new TextDecoder();
    var buf = '';
    for (;;) {
      var chunk = await reader.read();
      if (chunk.done) break;
      buf += dec.decode(chunk.value, { stream: true });
      var lines = buf.split('\n');
      buf = lines.pop();
      for (var i = 0; i < lines.length; i++) {
        if (!lines[i].trim()) continue;
        var ev = JSON.parse(lines[i]);
        if (ev.event === 'start') jobShow(kind, ev.total || 0);
        jobTick(ev);
      }
    }
    if (buf.trim()) { try { jobTick(JSON.parse(buf)); } catch (e) {} }
  } finally {
    if (JOB) jobTick({ done: JOB.total, total: JOB.total, detail: 'done' });
    setTimeout(jobHide, 900);
  }
}
function toast(msg, tone) {
  const c = document.getElementById('toasts'); if (!c) return;
  const t = document.createElement('div'); t.className = 'toast ' + (tone || ''); t.textContent = msg;
  c.appendChild(t); setTimeout(() => t.remove(), 2600);
}
function showLogin(msg) { location.href = '/login.html'; }
function hideLogin() {
  const s = document.getElementById('loginScreen'); if (s) s.style.display = 'none';
  const a = document.getElementById('app'); if (a) a.style.display = '';
}
async function verifyToken(t) {
  const res = await fetch('/api/status', { headers: { 'Authorization': 'Bearer ' + t } });
  return res.status === 200;
}

function doLogout() { DASH_TOKEN = ''; localStorage.removeItem('dash_token'); location.href = '/login.html'; }
function spinBtn(btn) {
  const s = btn && btn.querySelector('svg'); if (!s) return;
  s.classList.add('spin'); setTimeout(() => s.classList.remove('spin'), 1500);
}
function openModal(id) { document.getElementById(id).classList.add('open'); }
function closeModal(id) { document.getElementById(id).classList.remove('open'); }

function renderHeader(active) {
  // header is static HTML now; just mark the active nav link + version
  document.querySelectorAll('.admin-nav-link').forEach(a => a.classList.remove('active'));
  const map = { accounts: '/', proxies: '/proxies.html', settings: '/settings.html' };
  const link = document.querySelector('.admin-nav-link[href="' + (map[active] || '/') + '"]');
  if (link) link.classList.add('active');
  j('/api/status').then(s => { const v = document.getElementById('hdr-version'); if (v && s.version) v.textContent = 'v' + s.version; }).catch(() => {});
}
function fmtNum(v) {
  const n = Number(v); if (!isFinite(n)) return '—';
  if (Math.abs(n) >= 1e9) return (n / 1e9).toFixed(2) + 'B';
  if (Math.abs(n) >= 1e6) return (n / 1e6).toFixed(2) + 'M';
  if (Math.abs(n) >= 1e3) return (n / 1e3).toFixed(2) + 'K';
  return n.toLocaleString('en-US');
}
function fmtDateQ(d) { 
  if (!d) return ''; const dt = new Date(d * 1000); if (isNaN(dt)) return '';
  return dt.toLocaleString('en-US', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}
function fmtDate(ts) {
  if (!ts) return '';
  const d = new Date(ts * 1000); if (isNaN(d)) return String(ts);
  return d.getFullYear() + '-' + String(d.getMonth() + 1).padStart(2, '0') + '-' + String(d.getDate()).padStart(2, '0');
}
function tickClock() {
  const el = document.getElementById('updated-at'); if (!el) return;
  const d = new Date();
  el.textContent = 'Updated at ' + [d.getHours(), d.getMinutes(), d.getSeconds()].map(x => String(x).padStart(2, '0')).join(':');
}
function gate(onAuthed) {
  console.log('[dash] gate start, page=' + PAGE + ', token=' + (DASH_TOKEN ? 'yes' : 'no'));
  renderHeader(PAGE);
  (async () => {
    if (DASH_TOKEN && await verifyToken(DASH_TOKEN)) {
    const app = document.getElementById('app');
    if (app) app.style.display = '';
    boot(onAuthed);
    return;
  }
    DASH_TOKEN = ''; localStorage.removeItem('dash_token'); location.href = '/login.html';
  })();
}

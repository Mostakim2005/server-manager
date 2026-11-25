const ICONS = {
  plus: '<svg viewBox="0 0 24 24"><path d="M12 5v14M5 12h14"/></svg>',
  refresh: '<svg viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/></svg>',
  settings: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.78 1 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
  sun: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/></svg>',
  alert: '<svg viewBox="0 0 24 24"><path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
  search: '<svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>',
  sparkles: '<svg viewBox="0 0 24 24"><path d="m12 3-1.9 5.8a2 2 0 0 1-1.3 1.3L3 12l5.8 1.9a2 2 0 0 1 1.3 1.3L12 21l1.9-5.8a2 2 0 0 1 1.3-1.3L21 12l-5.8-1.9a2 2 0 0 1-1.3-1.3L12 3z"/></svg>',
  play: '<svg viewBox="0 0 24 24"><polygon points="6 3 20 12 6 21 6 3"/></svg>',
  stop: '<svg viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>',
  terminal: '<svg viewBox="0 0 24 24"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>',
  sliders: '<svg viewBox="0 0 24 24"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></svg>',
  external: '<svg viewBox="0 0 24 24"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/><polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/></svg>'
};

const token = document.querySelector('meta[name="sm-session"]')?.content || '';
let state = { services: [], runtime: {}, startup: {}, selfStartup: { supported: true, canToggle: false, enabled: false }, conflicts: [], stacks: [], config: {}, aiReports: {}, aiProxyStatus: { online: false } };
let currentFilter = 'all';
let activeServiceId = null;
let activeDrawerTab = 'ai';
let eventSource = null;

const $ = (id) => document.getElementById(id);

function hydrateIcons(root = document) {
  root.querySelectorAll('[data-ico]').forEach((el) => {
    const key = el.getAttribute('data-ico');
    if (ICONS[key]) el.innerHTML = ICONS[key];
  });
}

async function api(path, opts = {}) {
  const headers = { ...(opts.headers || {}), Authorization: `Bearer ${token}` };
  if (opts.body && !headers['Content-Type']) headers['Content-Type'] = 'application/json';
  const res = await fetch(path, { ...opts, headers });
  const text = await res.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch { data = { raw: text }; }
  if (!res.ok) throw new Error(data.error || text || `HTTP ${res.status}`);
  return data;
}

function showToast(msg) {
  const el = $('toast');
  el.textContent = msg;
  el.classList.remove('hidden');
  clearTimeout(showToast._timer);
  showToast._timer = setTimeout(() => el.classList.add('hidden'), 3200);
}

function applyTheme(pref) {
  const resolved = pref === 'system'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : pref;
  localStorage.setItem('sm-theme', pref);
  document.documentElement.dataset.theme = resolved;
  document.documentElement.dataset.themePref = pref;
  const meta = document.querySelector('meta[name="color-scheme"]');
  if (meta) meta.setAttribute('content', pref === 'system' ? 'light dark' : pref);
  $('btn-theme').title = `Theme: ${pref} (click to switch)`;
}

function cycleTheme() {
  const order = ['system', 'light', 'dark'];
  const cur = document.documentElement.dataset.themePref || 'system';
  const next = order[(order.indexOf(cur) + 1) % order.length];
  applyTheme(next);
}

async function loadState(rescan = false) {
  const data = await api(rescan ? '/api/v1/rescan' : '/api/v1/state', { method: rescan ? 'POST' : 'GET' });
  state = data;
  $('chk-utilities').checked = !!state.config?.showUtilityScripts;
  renderTopStatus();
  renderConflicts();
  renderStacks();
  renderServices();
}

function renderTopStatus() {
  const services = state.services || [];
  const visible = services.filter((s) => $('chk-utilities').checked || !s.isUtility);
  const runningCount = visible.filter((s) => {
    const st = state.runtime?.[s.id]?.state;
    return st === 'running' || st === 'running_external';
  }).length;
  $('summary-badge').textContent = `${runningCount} active · ${visible.length} launchers`;

  const proxy = state.aiProxyStatus || {};
  const pill = $('btn-proxy-status');
  pill.classList.toggle('online', !!proxy.online);
  pill.classList.toggle('offline', !proxy.online);
  $('proxy-label').textContent = proxy.online
    ? `AI Proxy Ready (${(proxy.models || []).length} models)`
    : 'AI Proxy Offline (auto-starts)';
}

function renderConflicts() {
  const conflicts = state.conflicts || [];
  const banner = $('conflicts-banner');
  if (!conflicts.length) {
    banner.classList.add('hidden');
    return;
  }
  banner.classList.remove('hidden');
  $('conflicts-title').textContent = `${conflicts.length} Port / Registry Warning${conflicts.length === 1 ? '' : 's'}`;
  const list = $('conflicts-list');
  list.innerHTML = '';
  for (const c of conflicts) {
    const li = document.createElement('li');
    li.textContent = c.message;
    list.appendChild(li);
  }
}

function renderStacks() {
  const list = $('stacks-list');
  list.innerHTML = '';
  for (const st of state.stacks || []) {
    const wrap = document.createElement('div');
    wrap.className = 'stack-chip';
    const name = document.createElement('span');
    name.textContent = st.name;
    name.title = st.description || `${(st.serviceIds || []).length} services`;

    const btnStart = document.createElement('button');
    btnStart.type = 'button';
    btnStart.className = 'btn btn-success btn-xs';
    btnStart.textContent = 'Start';
    btnStart.onclick = () => runStack(st.id, 'start', st.name);

    const btnStop = document.createElement('button');
    btnStop.type = 'button';
    btnStop.className = 'btn btn-danger btn-xs';
    btnStop.textContent = 'Stop';
    btnStop.onclick = () => runStack(st.id, 'stop', st.name);

    const btnDel = document.createElement('button');
    btnDel.type = 'button';
    btnDel.className = 'btn btn-ghost btn-xs';
    btnDel.textContent = '✕';
    btnDel.title = 'Delete stack';
    btnDel.onclick = async () => {
      await api(`/api/v1/stacks/${encodeURIComponent(st.id)}`, { method: 'DELETE' });
      showToast(`Removed stack "${st.name}"`);
      await loadState(false);
    };

    wrap.append(name, btnStart, btnStop, btnDel);
    list.appendChild(wrap);
  }
}

async function runStack(id, action, name) {
  try {
    showToast(`${action === 'start' ? 'Starting' : 'Stopping'} ${name}…`);
    await api(`/api/v1/stacks/${encodeURIComponent(id)}/action`, {
      method: 'POST',
      body: JSON.stringify({ action }),
    });
    await loadState(false);
  } catch (e) {
    showToast(`Stack ${action} failed: ${e.message}`);
  }
}

function matchesFilters(s) {
  const showUtil = $('chk-utilities').checked;
  if (!showUtil && s.isUtility && currentFilter !== 'utility' && currentFilter !== 'startup') return false;

  const rt = state.runtime?.[s.id]?.state || 'stopped';
  if (currentFilter === 'running' && rt !== 'running' && rt !== 'running_external') return false;
  if (currentFilter === 'startup' && !state.startup?.[s.id]?.enabled) return false;
  if (currentFilter === 'server' && s.category !== 'server' && s.category !== 'registry') return false;
  if (currentFilter === 'custom' && !s.isCustom && !s.addedByAi) return false;
  if (currentFilter === 'utility' && !s.isUtility) return false;

  const q = $('search-input').value.trim().toLowerCase();
  if (!q) return true;
  const hay = [s.name, s.projectName, s.runtime, s.staticSummary, s.userNote, s.startCmd, (s.ports || []).join(' ')].join(' ').toLowerCase();
  return hay.includes(q);
}

function renderServices() {
  const grid = $('services-grid');
  grid.innerHTML = '';
  const items = (state.services || []).filter(matchesFilters);
  if (!items.length) {
    const empty = document.createElement('div');
    empty.className = 'svc-card';
    empty.innerHTML = '<h3>No matching launchers found</h3><p class="muted">Adjust your search/filters, toggle build scripts, or click Add Project.</p>';
    grid.appendChild(empty);
    return;
  }

  for (const s of items) {
    const rt = state.runtime?.[s.id] || { state: 'stopped', logCount: 0 };
    const isActive = rt.state === 'running' || rt.state === 'running_external';
    const card = document.createElement('article');
    card.className = `svc-card ${isActive ? 'is-running' : ''}`;

    const head = document.createElement('div');
    head.className = 'svc-head';
    const titleBox = document.createElement('div');
    const title = document.createElement('h2');
    title.className = 'svc-title';
    title.textContent = s.name;
    const sub = document.createElement('div');
    sub.className = 'svc-sub';
    sub.textContent = `${s.runtime} · ${s.sourceType.toUpperCase()}`;
    titleBox.append(title, sub);

    const badge = document.createElement('span');
    badge.className = `state-badge state-${rt.state}`;
    badge.textContent = rt.state === 'running'
      ? `Running (PID ${rt.pid})`
      : rt.state === 'running_external'
        ? `Running Ext${rt.externalPids?.length ? ' #' + rt.externalPids[0] : ''}`
        : rt.state === 'error' ? 'Error' : 'Stopped';
    head.append(titleBox, badge);

    const meta = document.createElement('div');
    meta.className = 'svc-meta';
    for (const pt of s.ports || []) {
      const pTag = document.createElement('span');
      pTag.className = 'tag mono';
      pTag.textContent = `:${pt}`;
      meta.appendChild(pTag);
    }
    if (s.webUrl) {
      const link = document.createElement('a');
      link.className = 'tag tag-link';
      link.href = s.webUrl;
      link.target = '_blank';
      link.rel = 'noopener';
      link.textContent = 'Web UI ↗';
      meta.appendChild(link);
    }
    if (state.aiReports?.[s.id]?.summary) {
      const aiTag = document.createElement('span');
      aiTag.className = 'tag tag-ai';
      aiTag.textContent = 'AI Explained';
      meta.appendChild(aiTag);
    }
    const stInfo = state.startup?.[s.id];
    if (stInfo?.enabled) {
      const stTag = document.createElement('span');
      stTag.className = stInfo.taskManagerDisabled ? 'tag tag-warn' : 'tag tag-startup';
      stTag.textContent = stInfo.taskManagerDisabled ? 'Startup (Task Mgr Off)' : 'Autostart';
      stTag.title = stInfo.taskManagerDisabled
        ? `Shortcut ${stInfo.shortcutName} exists, but is disabled in Windows Task Manager Startup apps`
        : `Windows Startup shortcut: ${stInfo.shortcutName} (${stInfo.launchMode})`;
      meta.appendChild(stTag);
    }
    if (s.conflicts?.length) {
      const wTag = document.createElement('span');
      wTag.className = 'tag tag-warn';
      wTag.title = s.conflicts.map((c) => c.message).join('\n');
      wTag.textContent = `${s.conflicts.length} Warning${s.conflicts.length > 1 ? 's' : ''}`;
      meta.appendChild(wTag);
    }

    const summary = document.createElement('p');
    summary.className = 'svc-summary';
    summary.textContent = s.userNote || state.aiReports?.[s.id]?.summary || s.staticSummary;

    const actions = document.createElement('div');
    actions.className = 'svc-actions';

    if (!isActive) {
      const bStart = mkBtn('Start', 'btn-primary btn-sm', 'play', () => svcAction(s.id, 'start'));
      actions.appendChild(bStart);
    } else {
      const bStop = mkBtn('Stop', 'btn-danger btn-sm', 'stop', () => svcAction(s.id, 'stop'));
      const bRestart = mkBtn('Restart', 'btn-tonal btn-sm', 'refresh', () => svcAction(s.id, 'restart'));
      actions.append(bStop, bRestart);
    }

    const bLogs = mkBtn(`Logs${rt.logCount ? ` (${rt.logCount})` : ''}`, 'btn-ghost btn-sm', 'terminal', () => openDrawer(s.id, 'logs'));
    const bOpts = mkBtn('Options', 'btn-tonal btn-sm', 'sliders', () => openDrawer(s.id, 'ai'));
    actions.append(bLogs, bOpts);

    card.append(head, meta, summary, actions);
    grid.appendChild(card);
  }
  hydrateIcons(grid);
}

function mkBtn(label, cls, icoName, onClick) {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = `btn ${cls}`;
  b.innerHTML = `<span class="ico" data-ico="${icoName}"></span>${label}`;
  b.onclick = onClick;
  return b;
}

async function svcAction(id, action) {
  try {
    await api(`/api/v1/services/${encodeURIComponent(id)}/${action}`, { method: 'POST' });
    showToast(`${action.toUpperCase()} sent`);
    await loadState(false);
    if (activeServiceId === id && activeDrawerTab === 'logs') await loadLogs(id);
  } catch (e) {
    showToast(`Failed to ${action}: ${e.message}`);
  }
}

async function openDrawer(id, tab = 'ai') {
  activeServiceId = id;
  const svc = state.services.find((s) => s.id === id);
  if (!svc) return;
  $('drawer-title').textContent = svc.name;
  $('drawer-subtitle').textContent = svc.workingDir || svc.scriptPath || svc.startCmd;
  $('service-note-input').value = svc.userNote || '';
  $('btn-delete-custom').classList.toggle('hidden', !svc.isCustom);

  const chips = $('drawer-meta-chips');
  chips.innerHTML = '';
  for (const txt of [svc.runtime, ...(svc.ports || []).map((p) => `Port :${p}`), svc.bindHost ? `Bind ${svc.bindHost}` : ''].filter(Boolean)) {
    const sp = document.createElement('span');
    sp.className = 'tag mono';
    sp.textContent = txt;
    chips.appendChild(sp);
  }

  const cw = $('drawer-conflicts');
  if (svc.conflicts?.length) {
    cw.classList.remove('hidden');
    cw.innerHTML = svc.conflicts.map((c) => `<div class="tag tag-warn">${escapeHtml(c.message)}</div>`).join(' ');
  } else {
    cw.classList.add('hidden');
    cw.innerHTML = '';
  }

  $('options-drawer').classList.remove('hidden');
  renderDrawerStartup(svc);
  switchDrawerTab(tab);

  if (tab === 'ai') {
    await loadOrFetchAIReport(id, false);
  }
}

function renderDrawerStartup(svc) {
  const st = state.startup?.[svc.id] || { supported: false, canToggle: false, enabled: false };
  const toggle = $('service-startup-toggle');
  const badge = $('service-startup-badge');
  const meta = $('service-startup-meta');
  const warn = $('service-startup-warning');

  toggle.checked = !!st.enabled;
  toggle.disabled = !st.canToggle;
  warn.classList.toggle('hidden', !st.taskManagerDisabled);

  if (!st.supported) {
    badge.className = 'tag';
    badge.textContent = 'Windows Only';
    meta.textContent = st.reason || 'Windows Startup shortcuts are only supported on Windows.';
    return;
  }
  if (!st.canToggle) {
    badge.className = 'tag';
    badge.textContent = 'Unavailable';
    meta.textContent = st.reason || 'No local start command configured.';
    return;
  }
  if (st.taskManagerDisabled) {
    badge.className = 'tag tag-warn';
    badge.textContent = 'Task Mgr Off';
    meta.textContent = `Shortcut ${st.shortcutName} exists in Startup folder, but Task Manager disabled it.`;
  } else if (st.enabled) {
    badge.className = 'tag tag-startup';
    badge.textContent = 'Enabled';
    meta.textContent = `Shortcut: ${st.shortcutName} · Mode: ${st.launchMode || 'silent'}`;
  } else {
    badge.className = 'tag';
    badge.textContent = 'Off';
    meta.textContent = `Will create ${st.shortcutName || 'Startup shortcut'} (${st.launchMode || 'server-manager-hidden'}).`;
  }
}

async function toggleServiceStartup(id, enabled) {
  const toggle = $('service-startup-toggle');
  toggle.disabled = true;
  try {
    const res = await api(`/api/v1/services/${encodeURIComponent(id)}/startup`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    });
    state.startup = { ...(state.startup || {}), [id]: res.status };
    const svc = state.services.find((s) => s.id === id);
    if (svc && activeServiceId === id) renderDrawerStartup(svc);
    renderServices();
    showToast(enabled ? `Enabled Windows Startup (${res.status?.shortcutName || 'shortcut'})` : 'Removed from Windows Startup');
  } catch (e) {
    showToast(`Startup update failed: ${e.message}`);
    const svc = state.services.find((s) => s.id === id);
    if (svc && activeServiceId === id) renderDrawerStartup(svc);
  }
}

function switchDrawerTab(tab) {
  activeDrawerTab = tab;
  document.querySelectorAll('.drawer-tabs .tab').forEach((t) => t.classList.toggle('active', t.dataset.dtab === tab));
  $('dtab-ai').classList.toggle('hidden', tab !== 'ai');
  $('dtab-source').classList.toggle('hidden', tab !== 'source');
  $('dtab-logs').classList.toggle('hidden', tab !== 'logs');
  if (!activeServiceId) return;
  if (tab === 'ai') loadOrFetchAIReport(activeServiceId, false);
  if (tab === 'source') loadSource(activeServiceId);
  if (tab === 'logs') loadLogs(activeServiceId);
}

async function loadOrFetchAIReport(id, force) {
  const box = $('ai-report-box');
  const cached = state.aiReports?.[id];
  const svc = state.services.find((s) => s.id === id);
  if (!force && cached && svc && cached.scriptHash === svc.scriptHash) {
    renderAIReportBox(cached, true);
    return;
  }
  box.textContent = 'Checking CLIProxyAPI (:8317) & generating plain-English AI report…';
  try {
    const res = await api(`/api/v1/services/${encodeURIComponent(id)}/ai-report`, {
      method: 'POST',
      body: JSON.stringify({ force }),
    });
    if (res.aiProxyStatus) {
      state.aiProxyStatus = res.aiProxyStatus;
      renderTopStatus();
    }
    if (res.report) {
      state.aiReports[id] = res.report;
      renderAIReportBox(res.report, res.cached);
      renderServices();
    }
  } catch (e) {
    box.innerHTML = `<div class="tag tag-warn">AI report unavailable: ${escapeHtml(e.message)}</div><p class="small muted">Showing static script analysis: ${escapeHtml(svc?.staticSummary || '')}</p>`;
  }
}

function renderAIReportBox(rep, cached) {
  const ul = (arr) => (arr && arr.length ? `<ul>${arr.map((x) => `<li>${escapeHtml(x)}</li>`).join('')}</ul>` : '<span class="muted">None</span>');
  $('ai-report-box').innerHTML = `
    <p><strong>What it does:</strong> ${escapeHtml(rep.summary || '')}</p>
    <div><strong>Commands &amp; Flags:</strong>${ul(rep.commandBreakdown)}</div>
    <div><strong>Ports &amp; URLs:</strong>${ul(rep.ports)}</div>
    <div><strong>Dependencies:</strong>${ul(rep.dependencies)}</div>
    <div><strong>Notes / Risks:</strong>${ul(rep.risksOrNotes)}</div>
    <div class="small muted">Model: <code>${escapeHtml(rep.model || 'cached')}</code>${cached ? ' (cached)' : ' (fresh)'}</div>
  `;
}

async function loadSource(id) {
  const res = await api(`/api/v1/services/${encodeURIComponent(id)}/source`);
  $('source-path-label').textContent = res.scriptPath || res.startCommand || '(no script file)';
  $('source-start-pre').textContent = res.startContent || res.startCommand || '(empty)';
  if (res.stopContent) {
    $('source-stop-wrap').classList.remove('hidden');
    $('stop-path-label').textContent = res.stopScriptPath || res.stopCommand;
    $('source-stop-pre').textContent = res.stopContent;
  } else {
    $('source-stop-wrap').classList.add('hidden');
  }
}

async function loadLogs(id) {
  const res = await api(`/api/v1/services/${encodeURIComponent(id)}/logs`);
  const pre = $('logs-viewer');
  pre.textContent = (res.lines || []).join('\n') || '(No logs captured yet. Click Start to launch under Server Manager.)';
  pre.scrollTop = pre.scrollHeight;
}

function connectSSE() {
  if (eventSource) eventSource.close();
  eventSource = new EventSource(`/api/v1/events?token=${encodeURIComponent(token)}`);
  eventSource.addEventListener('status', () => loadState(false));
  eventSource.addEventListener('log', (ev) => {
    try {
      const payload = JSON.parse(ev.data);
      if (activeServiceId === payload.serviceId && activeDrawerTab === 'logs') {
        const pre = $('logs-viewer');
        if (pre.textContent.startsWith('(No logs captured')) pre.textContent = '';
        pre.textContent += (pre.textContent ? '\n' : '') + payload.line;
        pre.scrollTop = pre.scrollHeight;
      }
    } catch {}
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (m) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[m]));
}

function bindEvents() {
  hydrateIcons();
  applyTheme(localStorage.getItem('sm-theme') || 'system');

  $('btn-theme').onclick = cycleTheme;
  $('btn-rescan').onclick = async () => { await loadState(true); showToast('Rescanned folders & ports'); };
  $('btn-proxy-status').onclick = async () => {
    try {
      showToast('Checking / starting CLIProxyAPI (:8317)…');
      const res = await api('/api/v1/ai/status', { method: 'POST' });
      state.aiProxyStatus = res.status;
      renderTopStatus();
      showToast(res.status?.online ? 'CLIProxyAPI online!' : 'CLIProxyAPI offline');
    } catch (e) {
      showToast(e.message);
    }
  };

  $('btn-toggle-conflicts').onclick = () => $('conflicts-list').classList.toggle('hidden');
  $('search-input').oninput = renderServices;
  $('chk-utilities').onchange = async () => {
    const nextCfg = { ...state.config, showUtilityScripts: $('chk-utilities').checked };
    await api('/api/v1/settings', { method: 'PUT', body: JSON.stringify(nextCfg) });
    await loadState(false);
  };
  document.querySelectorAll('.filter-tabs .tab').forEach((t) => {
    t.onclick = () => {
      currentFilter = t.dataset.filter;
      document.querySelectorAll('.filter-tabs .tab').forEach((x) => x.classList.toggle('active', x === t));
      renderServices();
    };
  });

  // Drawer events
  $('btn-close-drawer').onclick = () => { $('options-drawer').classList.add('hidden'); activeServiceId = null; };
  document.querySelectorAll('.drawer-tabs .tab').forEach((t) => {
    t.onclick = () => switchDrawerTab(t.dataset.dtab);
  });
  $('btn-refresh-ai').onclick = () => activeServiceId && loadOrFetchAIReport(activeServiceId, true);
  $('service-startup-toggle').onchange = () => activeServiceId && toggleServiceStartup(activeServiceId, $('service-startup-toggle').checked);
  $('btn-reenable-startup').onclick = () => activeServiceId && toggleServiceStartup(activeServiceId, true);
  $('btn-save-note').onclick = async () => {
    if (!activeServiceId) return;
    await api(`/api/v1/services/${encodeURIComponent(activeServiceId)}/note`, {
      method: 'POST',
      body: JSON.stringify({ note: $('service-note-input').value }),
    });
    showToast('Saved note');
    await loadState(false);
  };
  $('btn-copy-cmd').onclick = async () => {
    const svc = state.services.find((s) => s.id === activeServiceId);
    if (svc) { await navigator.clipboard.writeText(svc.startCmd); showToast('Copied start command'); }
  };
  $('btn-copy-source').onclick = async () => {
    await navigator.clipboard.writeText($('source-start-pre').textContent);
    showToast('Copied script content');
  };
  $('btn-open-folder').onclick = async () => {
    if (!activeServiceId) return;
    await api('/api/v1/open-folder', { method: 'POST', body: JSON.stringify({ serviceId: activeServiceId }) });
  };
  $('btn-delete-custom').onclick = async () => {
    if (!activeServiceId) return;
    await api(`/api/v1/catalog/custom/${encodeURIComponent(activeServiceId)}`, { method: 'DELETE' });
    $('options-drawer').classList.add('hidden');
    activeServiceId = null;
    await loadState(false);
    showToast('Deleted custom project');
  };
  $('btn-log-start').onclick = () => activeServiceId && svcAction(activeServiceId, 'start');
  $('btn-log-stop').onclick = () => activeServiceId && svcAction(activeServiceId, 'stop');
  $('btn-log-restart').onclick = () => activeServiceId && svcAction(activeServiceId, 'restart');
  $('btn-copy-logs').onclick = async () => {
    await navigator.clipboard.writeText($('logs-viewer').textContent);
    showToast('Copied console logs');
  };

  // Add Project + AI Analyzer
  $('btn-add-project').onclick = () => {
    $('add-project-form').reset();
    $('ai-add-status').textContent = '';
    $('add-modal').classList.remove('hidden');
  };
  $('btn-close-add').onclick = $('btn-cancel-add').onclick = () => $('add-modal').classList.add('hidden');
  $('btn-ai-analyze').onclick = async () => {
    const p = $('ai-folder-input').value.trim();
    if (!p) { showToast('Paste a folder or script path first'); return; }
    $('ai-add-status').textContent = 'Querying CLIProxyAPI (:8317) to inspect folder & scripts…';
    try {
      const res = await api('/api/v1/ai/analyze-project', { method: 'POST', body: JSON.stringify({ path: p }) });
      const prop = res.proposal;
      $('add-name').value = prop.name || '';
      $('add-category').value = prop.category || 'custom';
      $('add-desc').value = prop.description || '';
      $('add-workdir').value = prop.workingDir || p;
      $('add-startcmd').value = prop.startCmd || '';
      $('add-stopcmd').value = prop.stopCmd || '';
      $('add-ports').value = (prop.ports || []).join(', ');
      $('add-weburl').value = prop.webUrl || '';
      $('ai-add-status').textContent = 'AI pre-filled fields below — review and click Save Project.';
    } catch (e) {
      $('ai-add-status').textContent = `AI inspection failed: ${e.message}`;
    }
  };
  $('add-project-form').onsubmit = async (ev) => {
    ev.preventDefault();
    const ports = $('add-ports').value.split(',').map((x) => parseInt(x.trim(), 10)).filter((n) => n > 0);
    const wantStartup = $('add-startup').checked;
    const res = await api('/api/v1/catalog/custom', {
      method: 'POST',
      body: JSON.stringify({
        name: $('add-name').value.trim(),
        category: $('add-category').value,
        description: $('add-desc').value.trim(),
        workingDir: $('add-workdir').value.trim(),
        startCmd: $('add-startcmd').value.trim(),
        stopCmd: $('add-stopcmd').value.trim(),
        ports,
        webUrl: $('add-weburl').value.trim(),
        addedByAi: !!$('ai-add-status').textContent.includes('AI pre-filled'),
      }),
    });
    if (wantStartup && res?.saved?.id) {
      try {
        await api(`/api/v1/services/${encodeURIComponent(res.saved.id)}/startup`, {
          method: 'POST',
          body: JSON.stringify({ enabled: true }),
        });
      } catch (e) {
        showToast(`Project saved, but Windows Startup failed: ${e.message}`);
      }
    }
    $('add-modal').classList.add('hidden');
    await loadState(false);
    showToast(wantStartup ? 'Project saved & added to Windows Startup' : 'Project saved to catalog');
  };

  // New Stack Modal
  $('btn-new-stack').onclick = () => {
    $('stack-form').reset();
    const picker = $('stack-service-picker');
    picker.innerHTML = '';
    for (const s of (state.services || []).filter((x) => !x.isUtility)) {
      const lbl = document.createElement('label');
      lbl.className = 'picker-item';
      lbl.innerHTML = `<input type="checkbox" value="${escapeHtml(s.id)}"> <span>${escapeHtml(s.name)}</span>`;
      picker.appendChild(lbl);
    }
    $('stack-modal').classList.remove('hidden');
  };
  $('btn-close-stack').onclick = $('btn-cancel-stack').onclick = () => $('stack-modal').classList.add('hidden');
  $('stack-form').onsubmit = async (ev) => {
    ev.preventDefault();
    const ids = Array.from($('stack-service-picker').querySelectorAll('input:checked')).map((c) => c.value);
    await api('/api/v1/stacks', {
      method: 'POST',
      body: JSON.stringify({
        name: $('stack-name').value.trim(),
        description: $('stack-desc').value.trim(),
        serviceIds: ids,
      }),
    });
    $('stack-modal').classList.add('hidden');
    await loadState(false);
    showToast('Stack preset saved');
  };

  // Settings Modal
  $('btn-settings').onclick = () => {
    const cfg = state.config || {};
    const selfSt = state.selfStartup || {};
    $('set-autostart-ai').checked = cfg.autoStartCliProxy !== false;
    $('set-self-startup').checked = !!selfSt.enabled;
    $('set-self-startup').disabled = !selfSt.canToggle;
    $('self-startup-warning').classList.toggle('hidden', !selfSt.taskManagerDisabled);
    $('set-scan-d').checked = !!cfg.includeVirtualDriveD;
    $('set-proxy-url').value = cfg.cliProxyUrl || 'http://127.0.0.1:8317';
    $('set-ai-model').value = cfg.preferredAiModel || '';
    renderRootsEditor(cfg.scanRoots || []);
    $('settings-modal').classList.remove('hidden');
  };
  $('btn-reenable-self-startup').onclick = async () => {
    try {
      const res = await api('/api/v1/startup/self', { method: 'POST', body: JSON.stringify({ enabled: true }) });
      state.selfStartup = res.status;
      $('set-self-startup').checked = !!res.status?.enabled;
      $('self-startup-warning').classList.toggle('hidden', !res.status?.taskManagerDisabled);
      showToast('Re-enabled Server Manager Windows Startup');
    } catch (e) {
      showToast(`Re-enable failed: ${e.message}`);
    }
  };
  $('btn-close-settings').onclick = $('btn-cancel-settings').onclick = () => $('settings-modal').classList.add('hidden');
  $('btn-add-root').onclick = () => appendRootRow({ id: `root-${Date.now()}`, label: 'Custom Root', path: '', enabled: true, maxDepth: 3 });
  $('btn-save-settings').onclick = async () => {
    const roots = Array.from($('scan-roots-list').querySelectorAll('.root-row')).map((row, idx) => ({
      id: row.dataset.id || `root-${idx}`,
      label: row.querySelector('.r-label').value.trim() || 'Folder',
      path: row.querySelector('.r-path').value.trim(),
      enabled: row.querySelector('.r-en').checked,
      maxDepth: 3,
    })).filter((r) => r.path);
    const nextCfg = {
      ...state.config,
      autoStartCliProxy: $('set-autostart-ai').checked,
      includeVirtualDriveD: $('set-scan-d').checked,
      cliProxyUrl: $('set-proxy-url').value.trim() || 'http://127.0.0.1:8317',
      preferredAiModel: $('set-ai-model').value.trim(),
      scanRoots: roots,
    };
    await api('/api/v1/settings', { method: 'PUT', body: JSON.stringify(nextCfg) });
    const wantSelfStartup = $('set-self-startup').checked;
    const prevSelfStartup = !!state.selfStartup?.enabled;
    if (state.selfStartup?.canToggle && (wantSelfStartup !== prevSelfStartup || (wantSelfStartup && state.selfStartup?.taskManagerDisabled))) {
      await api('/api/v1/startup/self', { method: 'POST', body: JSON.stringify({ enabled: wantSelfStartup }) });
    }
    $('settings-modal').classList.add('hidden');
    await loadState(false);
    showToast('Settings saved & rescanned');
  };
  $('btn-install-shortcut').onclick = async () => {
    try {
      const res = await api('/api/v1/install-shortcut', { method: 'POST' });
      showToast(`Shortcut created: ${res.shortcut}`);
    } catch (e) {
      showToast(`Shortcut failed: ${e.message}`);
    }
  };
  $('btn-open-startup').onclick = async () => {
    try {
      await api('/api/v1/startup/open-folder', { method: 'POST' });
    } catch (e) {
      showToast(`Open Startup folder failed: ${e.message}`);
    }
  };

  window.addEventListener('keydown', (ev) => {
    if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 'k') {
      ev.preventDefault();
      $('search-input').focus();
    }
    if (ev.key === 'Escape') {
      ['options-drawer', 'add-modal', 'stack-modal', 'settings-modal'].forEach((id) => $(id).classList.add('hidden'));
      activeServiceId = null;
    }
  });
}

function renderRootsEditor(roots) {
  $('scan-roots-list').innerHTML = '';
  roots.forEach(appendRootRow);
}

function appendRootRow(r) {
  const row = document.createElement('div');
  row.className = 'root-row';
  row.dataset.id = r.id || '';
  row.innerHTML = `
    <input class="r-en" type="checkbox" ${r.enabled ? 'checked' : ''} title="Enable root">
    <input class="r-label" type="text" value="${escapeHtml(r.label || '')}" placeholder="Label" style="max-width:140px">
    <input class="r-path" type="text" value="${escapeHtml(r.path || '')}" placeholder="C:\\path\\to\\projects">
    <button type="button" class="btn btn-ghost btn-xs" title="Remove">✕</button>
  `;
  row.querySelector('button').onclick = () => row.remove();
  $('scan-roots-list').appendChild(row);
}

bindEvents();
loadState(false).then(connectSSE).catch((err) => showToast(err.message));

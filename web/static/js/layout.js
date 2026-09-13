/* ============================================================
   layout.js —— 外壳渲染：顶栏 / 侧栏 / 状态栏 + 主渲染 render / softRender
   零构建链原生 JS：由 index.html 按依赖顺序以 <script> 引入，共享全局命名空间
   ============================================================ */
'use strict';

/* ============================================================
   顶栏
   ============================================================ */
function brand(){
  return `<div class="brand">
    <div class="brand-logo">${ICON.logo}</div>
    <span class="brand-name">Multi-Service Deployer</span>
    <span class="brand-ver">CONSOLE</span>
  </div>`;
}
function crumb(parts){
  return `<div class="crumb">` + parts.map((p,i)=>{
    const last = i === parts.length - 1;
    let seg = last
      ? `<span class="cur">${esc(p.t)}</span>`
      : `<span class="up${p.go ? ' link' : ''}"${p.go ? ` data-go="${esc(p.go)}"` : ''}>${esc(p.t)}</span>`;
    if (i > 0) seg = `<span class="sep">/</span>` + seg;
    return seg;
  }).join('') + `</div>`;
}
function renderTopbar(){
  const tb = document.getElementById('topbar');
  const v = app.view;
  let left = brand();
  let right = '';

  if (v === 'spaces') {
    left += `<div class="space-switch">
      <button class="space-btn" id="spaceBtn">${esc(state.activeWs)}<span class="chev"></span></button>
      <div class="space-menu" id="spaceMenu">
        <div class="sw-search">${ICON.search}<input placeholder="搜索空间"></div>
        ${state.workspaces.map(s => `<button class="it" data-space="${esc(s.id)}">
          ${s.id === state.activeWs ? '<span class="dot dot--ok"></span>' : '<span class="dot dot--muted"></span>'}
          <span class="nm">${esc(s.id)}</span>
          <span class="mt">${esc(s.name || '')}${s.id === state.activeWs ? ' · 当前' : ''}</span>
        </button>`).join('')}
        <div class="ft"><button data-act="manage-space">管理空间</button><button data-act="new-space" style="color:var(--accent)">+ 新建空间</button></div>
      </div>
    </div>`;
    right = `<span class="mono" style="font-size:11.5px;color:var(--t3)">${state.workspaces.length} 个空间 · ${cfgServices().length} 个服务</span>
      <button class="btn" data-go="#/settings">平台设置</button>
      <button class="btn btn--primary" data-act="new-space">${ICON.plus}新建空间</button>`;
  } else if (v === 'execution') {
    const running = state.deploy && state.deploy.status === 'running';
    right = running
      ? `<span class="mono" style="font-size:11.5px;color:var(--accent);display:flex;align-items:center;gap:8px">
          <span class="dot dot--run"></span>部署执行中 <span style="color:var(--t3)">${esc(state.deploy.id || '')}</span></span>
        <button class="btn btn--danger" data-act="cancel">中止部署</button>`
      : `<span class="mono" style="font-size:11.5px;color:var(--t2);display:flex;align-items:center;gap:8px">
          <span class="dot ${state.deploy ? 'dot--' + (state.deploy.status === 'success' ? 'ok' : 'err') : 'dot--muted'}"></span>${state.deploy ? (state.deploy.status === 'success' ? '批次成功' : state.deploy.status === 'canceled' ? '批次已中止' : '批次失败') : '暂无批次'}</span>
        <button class="btn" data-go="#/orchestration">返回编排台</button>`;
  } else {
    const CRUMBS = {
      orchestration:[{t:'服务单元'}],
      service:[{t:'服务单元', go:'#/orchestration'},{t:app.arg || ''}],
      hosts:[{t:'配置中心'}],
      keys:[{t:'配置中心', go:'#/hosts'},{t:'SSH 密钥'}],
      hooks:[{t:'批次钩子'}],
      history:[{t:'部署历史'}],
      batch:[{t:'部署历史', go:'#/history'},{t:'批次 #' + (app.arg || '')}],
      settings:[{t:'设置'}]
    };
    left += crumb(CRUMBS[v] || [{t:'控制台'}]);

    const R = {
      orchestration:`<span class="mono" style="font-size:11.5px;color:${state.esConnected ? 'var(--accent)' : 'var(--t3)'};display:flex;align-items:center;gap:8px"><span class="dot ${state.esConnected ? 'dot--ok' : 'dot--muted'}"></span>${state.esConnected ? 'SSE 通道正常' : '离线'}</span>
        <button class="btn" data-act="save">${state.dirty ? '保存配置 ●' : '保存配置'}</button>
        <button class="btn btn--primary" data-act="deploy">${ICON.bolt}一键开始部署</button>`,
      service:`<button class="btn" data-act="duplicate">复制服务</button>
        <button class="btn btn--danger" data-act="delete-service">${ICON.trash}删除服务</button>
        <button class="btn btn--primary" data-act="save">${state.dirty ? '保存更改 ●' : '保存更改'}</button>`,
      hosts:`<span class="mono" style="font-size:11.5px;color:var(--t3)">${deriveHosts().length} 台主机（由 services[].server 派生）</span>`,
      hooks:`<button class="btn btn--primary" data-act="save">${state.dirty ? '保存更改 ●' : '保存更改'}</button>`,
      batch:`<button class="btn" data-act="export-report">导出报告</button>
        <button class="btn btn--primary" data-act="replay">${ICON.play}重放此批次</button>`,
      history:`<button class="btn" data-act="export-report">导出全部记录</button>`,
      keys:`<button class="btn btn--primary" data-act="new-key">${ICON.plus}导入密钥</button>`,
      settings:`<button class="btn btn--primary" data-act="save-settings">保存设置</button>`
    };
    right = R[v] || '';
  }

  tb.innerHTML = `<div style="display:flex;align-items:center;gap:14px;min-width:0">${left}</div><div class="tb-right">${right}</div>`;

  const btn = document.getElementById('spaceBtn');
  if (btn) btn.onclick = e => { e.stopPropagation(); document.getElementById('spaceMenu').classList.toggle('show'); };
}

/* ============================================================
   侧边栏
   ============================================================ */
function renderSide(){
  const side = document.getElementById('side');
  const sep  = document.getElementById('sideSep');
  if (app.view === 'spaces' || app.view === 'execution') {
    side.style.display = 'none'; sep.style.display = 'none';
    return;
  }
  side.style.display = ''; sep.style.display = '';

  const sysActive = { hooks:'hooks', history:'history', batch:'history', settings:'settings', keys:'keys', spaces:'spaces' }[app.view] || '';
  const mainActive = ['orchestration','service'].includes(app.view);
  const tags = cfgTags();
  const tagKeys = Object.keys(tags).filter(t => tags[t] > 0).sort();

  /* cls='filter' 的分组是筛选器而非导航，侧栏折叠为图标条时隐藏，
     由页面内的下拉控件承接（见 .grp-sel） */
  const nav = (label, items, cls) => `<div class="nav-group${cls ? ' nav-group--' + cls : ''}">
    <div class="nav-label">${label}</div>
    ${items.join('')}
  </div>`;

  const item = (t, c, active, attrs, icon) =>
    `<button class="nav-item${active ? ' is-active' : ''}" title="${esc(t)}${c != null ? '（' + c + '）' : ''}" ${attrs || ''}>
      ${icon ? `<span class="ic">${icon}</span>` : ''}
      <span class="nm">${esc(t)}</span>${c != null ? `<span class="ct">${c}</span>` : ''}
    </button>`;

  side.innerHTML =
    nav('标签 / TAGS', [
      item('全部服务', cfgServices().filter(s => s.enabled !== false).length,
        mainActive && app.tagFilter.length === 0, `data-tag-clear="1"`, ICON.layers),
      ...tagKeys.map(k =>
        item(k, tags[k],
          mainActive && app.tagFilter.indexOf(k) >= 0, `data-tag="${esc(k)}"`))
    ], 'filter') +
    nav('主机 / HOSTS', [
      item('主机配置库', ((state.config && state.config.hosts) || []).length + deriveHosts().length, app.view === 'hosts', `data-go="#/hosts"`, ICON.server),
      item('SSH 密钥', state.keysLoaded ? state.keys.length : null, app.view === 'keys', `data-go="#/keys"`, ICON.key)
    ]) +
    `<div class="nav-spacer"></div>` +
    nav('系统 / SYSTEM', [
      item('空间管理', state.workspaces.length, sysActive === 'spaces', `data-go="#/spaces"`, ICON.layers),
      item('批次钩子', null, sysActive === 'hooks', `data-go="#/hooks"`, ICON.hook),
      item('部署历史', null, sysActive === 'history', `data-go="#/history"`, ICON.clock),
      item('设置', null, sysActive === 'settings', `data-go="#/settings"`, ICON.gear)
    ]) +
    `<div class="side-file">
      <div class="p">deploy.json</div>
      <div class="s">空间 ${esc(state.activeWs)}${state.dirty ? ' · 有未保存更改' : ''}</div>
    </div>`;

  side.querySelectorAll('[data-go]').forEach(el => el.onclick = () => go(el.dataset.go));
  side.querySelectorAll('[data-tag]').forEach(el => el.onclick = () => {
    const k = el.dataset.tag;
    const i = app.tagFilter.indexOf(k);
    if (i >= 0) app.tagFilter.splice(i, 1); else app.tagFilter.push(k);
    go('#/orchestration');
  });
  side.querySelectorAll('[data-tag-clear]').forEach(el => el.onclick = () => {
    app.tagFilter = [];
    go('#/orchestration');
  });
}

/* ============================================================
   状态栏
   ============================================================ */
function renderStatus(){
  const sb = document.getElementById('statusbar');
  const v = app.view;
  let L = '', R = '';
  const onHosts = v === 'hosts' || v === 'keys';
  const addr = (state.settings && state.settings.listenAddr) || '127.0.0.1:8080';

  if (v === 'orchestration') {
    const enabled = cfgServices().filter(s => s.enabled !== false).length;
    L = `<span class="hl">READY ${enabled}/${cfgServices().length}</span><span>${esc(addr)}</span>`;
    R = `<span>Go · CGO_ENABLED=0</span><span>空间 ${esc(state.activeWs)}</span>`;
  } else if (v === 'execution') {
    L = state.deploy && state.deploy.status === 'running'
      ? `<span class="hl">RUNNING</span><span class="at">${state.deploy.curStage ? 'S' + String(state.deploy.curStage).padStart(2, '0') + ' 波次' : '调度中'}</span><span>${esc(addr)}</span>`
      : `<span class="hl">${state.deploy ? state.deploy.status.toUpperCase() : 'IDLE'}</span><span>${esc(addr)}</span>`;
    R = `<span>SSE ${state.esConnected ? '已连接' : '未连接'} · 已接收 ${state.logCount} 行</span><span>缓冲 2000</span>`;
  } else if (onHosts) {
    const hosts = deriveHosts();
    const refs = hosts.reduce((n, h) => n + h.refs.length, 0);
    L = `<span class="hl">${hosts.length} HOSTS · ${refs} REFERENCES</span><span>${esc(addr)}</span>`;
    R = `<span>主机由 services[].server 派生</span><span>deploy.json</span>`;
  } else if (v === 'spaces') {
    L = `<span class="hl">${state.workspaces.length} SPACES</span><span>当前空间 ${esc(state.activeWs)}</span>`;
    R = `<span>切换空间时重新加载对应 deploy.json</span><span>workspaces/&lt;空间&gt;.json</span>`;
  } else if (v === 'service') {
    L = `<span>SERVICE</span><span class="hl">${esc(app.arg || '')}</span>`;
    R = state.dirty ? `<span class="wn">未保存更改</span><span>deploy.json › services[]</span>` : `<span>deploy.json › services[]</span>`;
  } else if (v === 'hooks') {
    const hooks = (state.config && state.config.hooks) || {};
    const cnt = (hooks.preDeploy || []).length + (hooks.postDeploy || []).length;
    const tagHookCnt = ((state.config && state.config.tagHooks) || []).length;
    L = `<span>SCOPE</span><span class="at">global + tags</span><span>${2 + tagHookCnt} 个钩子 · 全局 ${cnt} 条 + 标签钩子 ×${tagHookCnt}</span><span>deploy.json</span>`;
    R = `<span>保存后对后续批次生效</span><span>hooks.preDeploy / hooks.postDeploy / tagHooks[]</span>`;
  } else if (v === 'batch') {
    const rec = state.batch;
    L = rec
      ? `<span class="hl">BATCH #${esc(rec.id)}</span><span class="at">${rec.total} NODES · ${rec.failed} FAILED</span><span>${esc(rec.workspace || state.activeWs)}</span>`
      : `<span class="hl">BATCH #${esc(app.arg || '')}</span>`;
    R = rec ? `<span>${esc(rec.status)}</span><span>workspaces/${esc(rec.workspace || state.activeWs)}/history/batch-${esc(rec.id)}.log</span>` : '';
  } else if (v === 'history') {
    L = `<span class="hl">${state.history.length} BATCHES</span><span>空间 ${esc(state.activeWs)}</span>`;
    R = `<span>批次记录与日志归档于 workspaces/&lt;空间&gt;/history/</span>`;
  } else if (v === 'settings') {
    L = `<span class="hl">SECURE</span><span>${esc(addr)}</span>`;
    R = `<span>凭据以 ${'${ENV}'} 插值注入</span><span>settings.json</span>`;
  }
  sb.innerHTML = `<div class="l">${L}</div><div class="r">${R}</div>`;
}

/* ============================================================
   渲染 & 路由
   ============================================================ */
function render(){
  const { view, arg } = app;
  document.getElementById('content').innerHTML = (() => {
    if (!state.ready && view !== 'spaces') return offlinePanel();
    switch (view) {
      case 'orchestration': return viewOrchestration();
      case 'execution':     return viewExecution();
      case 'hosts':         return viewHosts();
      case 'keys':          return viewKeys();
      case 'spaces':        return viewSpaces();
      case 'service':       return viewService();
      case 'hooks':         return viewHooks();
      case 'history':       return viewHistory();
      case 'batch':         return viewBatch();
      case 'settings':      return viewSettings();
      default:              return viewOrchestration();
    }
  })();
  renderTopbar();
  renderSide();
  renderStatus();

  if (view === 'execution') refillLog();

  bind();

  /* 主机编辑抽屉支持深链：#/hosts/<hostKey> */
  if (view === 'hosts' && arg) openHostDrawer(arg);
}

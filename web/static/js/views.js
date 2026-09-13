/* ============================================================
   views.js —— 视图层：编排台 / 执行监控 / SSE 接入 / 主机库 / 密钥 / 空间 / 服务配置 / 批次钩子 / 历史 / 批次详情 / 设置 / 主机编辑抽屉
   零构建链原生 JS：由 index.html 按依赖顺序以 <script> 引入，共享全局命名空间
   ============================================================ */
'use strict';

/* ============================================================
   视图 —— 1. 服务编排台
   ============================================================ */
function viewOrchestration(){
  let list = cfgServices().slice();
  if (app.tagFilter.length) {
    // 标签并集语义：服务携带任一选中标签即命中
    list = list.filter(s => svcTags(s).some(t => app.tagFilter.indexOf(t) >= 0));
  }
  if (app.typeFilter) list = list.filter(s => (s.type || 'standard') === app.typeFilter);
  if (app.search) {
    const q = app.search.toLowerCase();
    list = list.filter(s => (s.name + svcTags(s).join(' ') + (s.server && s.server.host || '') + (s.upload && s.upload.remotePath || '')).toLowerCase().includes(q));
  }

  const tags = cfgTags();
  const tagKeys = Object.keys(tags).filter(t => tags[t] > 0).sort();
  const enabled = cfgServices().filter(s => s.enabled !== false).length;
  const lastRun = state.history[0];
  const hooks = (state.config && state.config.hooks) || {};
  const preCmds = hooks.preDeploy || [];

  const rows = list.map(s => `
    <div class="tbl-row">
      <div class="cell"><span class="nm link" data-svc="${esc(s.name)}">${esc(s.name)}</span></div>
      <div class="cell">
        ${svcTags(s).map(t => `<span class="tag type" style="color:var(--${tagColor(t)})">${esc(t)}</span>`).join(' ')}
        <span class="tag none">·</span><span class="tag">${esc(s.type || 'standard')}</span>
        <span class="tag none">·</span><span class="tag">${esc(svcStage(s))}</span>
      </div>
      <div class="cell"><span class="ref-link">${esc((s.server && s.server.host) || s.hostRef || '—')}${ICON.link}</span></div>
      <div class="cell c-opt">${s.upload && s.upload.remotePath ? `<span class="val">${esc(s.upload.remotePath)}</span>` : `<span class="dim">跳过 SFTP</span>`}</div>
      <div class="cell">
        <span class="st ${stClass(svcSt(s))}"><span class="dot dot--${dotOf(svcSt(s))}"></span>${stLabel(svcSt(s))}</span>
      </div>
      <div class="cell right"><button class="btn btn--sm" data-deploy-svc="${esc(s.name)}">部署</button></div>
    </div>`).join('');

  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">服务单元</div>
        <div class="ph-sub">${list.length} 个服务 · ${tagKeys.length} 个标签 · 空间 ${esc(state.activeWs)}</div>
      </div>
      <div class="ph-right">
        <div class="grp-sel">${ICON.layers}<select id="grpSel" title="标签筛选">
          <option value="">全部标签</option>
          ${tagKeys.map(k =>
            `<option value="${esc(k)}"${app.tagFilter.length === 1 && app.tagFilter[0] === k ? ' selected' : ''}>${esc(k)}（${tags[k]}）</option>`).join('')}
        </select></div>
        <div class="search">${ICON.search}<input id="svcSearch" placeholder="搜索服务 / 标签 / 主机" value="${esc(app.search)}"></div>
        <div class="grp-sel grp-sel--type"><select id="typeSel" title="类型筛选" aria-label="类型筛选">
          <option value=""${app.typeFilter === '' ? ' selected' : ''}>全部类型</option>
          ${['standard','exec_only','sync_only'].map(t => `<option value="${t}"${app.typeFilter === t ? ' selected' : ''}>${t}</option>`).join('')}
        </select></div>
        <button class="btn" data-act="new-service">${ICON.plus}新增服务</button>
      </div>
    </div>

    <div class="metrics">
      <div class="metric"><span class="k">SERVICES</span><span class="v">${cfgServices().length}</span></div>
      <div class="metric"><span class="k">WORKERS</span><span class="v">${(state.settings && state.settings.maxWorkers) || 10}</span></div>
      <div class="metric"><span class="k">READY</span><span class="v">${enabled}/${cfgServices().length}</span></div>
      <div class="metric"><span class="k">LAST RUN</span><span class="v">${lastRun ? fmtMs(lastRun.durationMs) : '—'}</span></div>
    </div>

    <div class="panel">
      <div class="tbl" style="--minw:760px;--cols:minmax(0,1fr) 260px 196px 176px 110px 62px;--cols-l:minmax(0,1fr) 260px 196px 110px 62px">
        <div class="tbl-head">
          <span>服务名称</span><span>标签 / 类型 / 阶段</span><span>主机引用</span><span class="c-opt">远端路径</span><span>状态</span><span style="text-align:right">操作</span>
        </div>
        ${rows || `<div class="tbl-row"><div class="cell"><span class="dim">当前空间暂无服务，请保存配置或切换空间</span></div></div>`}
      </div>
    </div>

    <div class="panel" style="padding:14px 16px;display:flex;flex-direction:column;gap:10px">
      <div style="display:flex;align-items:center;gap:12px">
        <span class="sec-label">批次钩子</span>
        <span class="mono" style="font-size:10.5px;color:var(--t3)">preDeploy ×${preCmds.length} · postDeploy ×${(hooks.postDeploy || []).length} · 标签钩子 ×${((state.config && state.config.tagHooks) || []).length}</span>
        <button class="btn btn--quiet btn--sm" style="margin-left:auto" data-go="#/hooks">编辑批次钩子</button>
      </div>
      ${preCmds.slice(0,2).map((c,i) =>
        `<div class="cmd"><span class="no mono">${i+1}</span><span class="pr">$</span><span class="tx">${esc(c)}</span></div>`).join('') ||
        `<span class="dim" style="font-size:11px">尚未配置批次前置钩子</span>`}
    </div>
  </div>`;
}

/* ============================================================
   视图 —— 2. 执行监控（真实 SSE：文本日志 + 结构化批次事件）
   ============================================================ */
function viewExecution(){
  const d = state.deploy;
  const nodes = (d ? d.nodes : []).map(n => {
    const cls = n.s === 'done' ? 'done' : n.s === 'run' ? 'run' : n.s === 'err' ? 'done' : 'wait';
    const dot = n.s === 'done' ? 'ok' : n.s === 'run' ? 'run' : n.s === 'err' ? 'err' : 'muted';
    return `<div class="node ${cls}"><span class="dot dot--${dot}"></span><span class="nm">${esc(n.name)}</span></div>`;
  }).join('');

  const done = d ? d.done : 0;
  const total = d && d.total ? d.total : 0;
  const pct = total ? Math.round(done / total * 100) : (d && d.status === 'running' ? 0 : 0);
  const headTxt = !d ? '暂无进行中的部署'
    : d.status !== 'running' ? (d.status === 'success' ? '批次成功完成' : d.status === 'canceled' ? '批次已中止' : '批次失败')
    : `部署执行中 · ${d.id || ''}`;

  return `
  <div class="page full" style="display:flex;flex-direction:column">
    <div class="run-head">
      <div class="run-title"><span class="dot dot--${d ? (d.status === 'running' ? 'run' : d.status === 'success' ? 'ok' : 'err') : 'muted'}"></span>${esc(headTxt)}</div>
      <div class="run-pct">
        <span class="v" id="runPct">${total ? pct + '%' : '—'}</span>
        <span class="t">${d ? `节点 ${done}/${total}${d.failed ? ' · 失败 ' + d.failed : ''}` : '触发部署后此处展示实时进度'}</span>
      </div>
    </div>
    <div class="bar"><i id="runBar" style="width:${total ? pct : 0}%"></i></div>
    <div class="nodes" id="runNodes">${nodes || ''}</div>

    <div class="term">
      <div class="term-hd">
        <div class="lights"><i style="background:#FF5D5D"></i><i style="background:#FFB020"></i><i style="background:#00E07A"></i></div>
        <span class="ttl"><b>deploy</b> — 实时日志&nbsp;&nbsp;GET /api/deploy/events</span>
        <span class="rt"><span class="dot ${state.esConnected ? 'dot--ok' : 'dot--muted'}"></span>${state.esConnected ? 'LIVE' : 'OFFLINE'}&nbsp;&nbsp;自动滚动 ON</span>
      </div>
      <div class="term-bd" id="termBd"></div>
    </div>
  </div>`;
}

/* 执行页动态区局部刷新：只更新进度与节点带，不重建终端（保护已追加的日志 DOM） */
function updateExecDynamic(){
  const d = state.deploy;
  const pctEl = document.getElementById('runPct');
  const barEl = document.getElementById('runBar');
  const nodesEl = document.getElementById('runNodes');
  if (!pctEl || !barEl) return;
  const total = d && d.total ? d.total : 0;
  const done = d ? d.done : 0;
  const pct = total ? Math.round(done / total * 100) : 0;
  pctEl.textContent = total ? pct + '%' : '—';
  barEl.style.width = (total ? pct : 0) + '%';
  pctEl.nextElementSibling.innerHTML = d
    ? `节点 ${done}/${total}${d.failed ? ' · 失败 ' + d.failed : ''}`
    : '触发部署后此处展示实时进度';
  if (nodesEl) {
    nodesEl.innerHTML = (d ? d.nodes : []).map(n => {
      const cls = n.s === 'done' ? 'done' : n.s === 'run' ? 'run' : n.s === 'err' ? 'done' : 'wait';
      const dot = n.s === 'done' ? 'ok' : n.s === 'run' ? 'run' : n.s === 'err' ? 'err' : 'muted';
      return `<div class="node ${cls}"><span class="dot dot--${dot}"></span><span class="nm">${esc(n.name)}</span></div>`;
    }).join('');
  }
}

/* 日志行解析：清洗 ANSI 颜色码，按后端 TAG（DEPLOY/SUCCESS/ERROR/服务名）映射级别徽标 */
const ANSI_RE = /\x1b\[[0-9;]*m/g;
function parseLogLine(raw){
  const line = raw.replace(ANSI_RE, '');
  const m = line.match(/^\[(\d{2}:\d{2}:\d{2})\]\s+([\s\S]*)$/);
  if (!m) return { ts: '', lv: 'exec', msg: line };
  const t = m[2].match(/^\[([^\]]+)\]\s*([\s\S]*)$/);
  let lv = 'exec', msg = m[2];
  if (t) {
    const tag = t[1];
    msg = t[2];
    if (tag === 'DEPLOY' || tag === 'SYSTEM') lv = 'batch';
    else if (tag === 'SUCCESS') lv = 'ok';
    else if (tag === 'ERROR') lv = 'warn';
    else if (/sftp|上传|传输|续传/i.test(msg)) lv = 'sftp';
    else if (/ssh|连接|握手|登录/i.test(msg)) lv = 'ssh';
  }
  return { ts: m[1], lv, msg };
}

function logLineHTML(ts, lv, msg){
  const TAG = { batch:'[批次]', ssh:'[SSH]', exec:'[EXEC]', sftp:'[SFTP]', ok:'[ OK ]', warn:'[WARN]' };
  const html = esc(msg).replace(/\$ /g, '<i>$ </i>');
  return `<div class="ln"><span class="ts">${esc(ts)}</span><span class="lv ${lv}">${TAG[lv] || '[LOG]'}</span><span class="msg">${html}</span></div>`;
}

function appendLogLine(raw){
  if (!raw || raw.indexOf('[[') === 0) return;   // 哨兵标记不进终端
  const parsed = parseLogLine(raw);
  state.logBuffer.push(parsed);
  if (state.logBuffer.length > 2000) state.logBuffer.shift();
  state.logCount++;
  const bd = document.getElementById('termBd');
  if (bd) {
    bd.insertAdjacentHTML('beforeend', logLineHTML(parsed.ts, parsed.lv, parsed.msg));
    while (bd.children.length > 2000) bd.removeChild(bd.firstChild);
    bd.scrollTop = bd.scrollHeight;
  }
}

function refillLog(){
  const bd = document.getElementById('termBd');
  if (!bd) return;
  bd.innerHTML = state.logBuffer.map(l => logLineHTML(l.ts, l.lv, l.msg)).join('');
  bd.scrollTop = bd.scrollHeight;
}

/* ============================================================
   SSE 接入 —— EventSource 消费文本日志与结构化批次事件
   ============================================================ */
function ensureSSE(){
  if (state.es) return;
  const es = new EventSource('/api/deploy/events');
  state.es = es;
  es.onopen = () => { state.esConnected = true; renderStatus(); render(); };
  es.onerror = () => { state.esConnected = false; renderStatus(); };
  es.onmessage = ev => {
    const line = ev.data || '';
    if (line === '[[DEPLOY_COMPLETED_SUCCESS]]') return finishDeploy('success');
    if (line === '[[DEPLOY_COMPLETED_FAILED]]')  return finishDeploy('failed');
    if (line === '[[DEPLOY_CANCELED]]')          return finishDeploy('canceled');
    if (line.indexOf('[CONNECTED]') === 0) return;
    appendLogLine(line);
  };
  es.addEventListener('batch_started', ev => {
    const p = safeJSON(ev.data) || {};
    state.deploy = {
      id: p.id, workspace: p.workspace, tags: p.tags || [],
      total: p.total || (p.services ? p.services.length : 0),
      done: 0, ok: 0, failed: 0, status: 'running', curStage: null,
      nodes: (p.services || []).map(s => ({ name: s.name, host: s.host, tags: s.tags || [], s: 'wait' }))
    };
    state.logBuffer = []; state.logCount = 0;
    softRender();
  });
  es.addEventListener('service_started', ev => {
    const p = safeJSON(ev.data) || {};
    if (!state.deploy) return;
    const n = nodeByName(p.name);
    if (n) { n.s = 'run'; }
    state.deploy.curStage = p.stage;
    softRender();
  });
  es.addEventListener('service_finished', ev => {
    const p = safeJSON(ev.data) || {};
    if (!state.deploy) return;
    const n = nodeByName(p.name);
    if (n) {
      n.s = p.status === 'ok' ? 'done' : (p.status || 'err');
      n.dur = p.durationMs;
      n.err = p.error;
    }
    state.deploy.done++;
    if (p.status === 'ok') state.deploy.ok++; else state.deploy.failed++;
    softRender();
  });
  es.addEventListener('batch_finished', ev => {
    const rec = safeJSON(ev.data) || {};
    if (state.deploy && state.deploy.status === 'running') {
      state.deploy.status = rec.status === 'canceled' ? 'canceled' : rec.status === 'success' ? 'success' : 'failed';
      if (rec.total) state.deploy.done = rec.total;
    }
    state.historyLoaded = false;   // 批次落盘后刷新历史指标
    softRender();
  });
}

function safeJSON(s) { try { return JSON.parse(s); } catch (e) { return null; } }
function nodeByName(name) { return state.deploy ? state.deploy.nodes.find(n => n.name === name) : null; }

function finishDeploy(kind){
  if (state.deploy && state.deploy.status === 'running') state.deploy.status = kind;
  toast(kind === 'success' ? '部署批次成功完成' : kind === 'canceled' ? '部署已被中止' : '部署批次失败', kind === 'success' ? 'ok' : undefined);
  softRender();
}

/* 渲染节流：高频事件下合并到下一帧；后台标签页 rAF 被节流时以 setTimeout 兜底 */
let renderQueued = false;
function softRender(){
  if (renderQueued) return;
  renderQueued = true;
  const run = () => {
    if (!renderQueued) return;
    renderQueued = false;
    if (app.view === 'execution') { updateExecDynamic(); renderStatus(); renderTopbar(); }
    else render();
  };
  requestAnimationFrame(run);
  setTimeout(run, 300);
}

/* ============================================================
   视图 —— 3. 主机配置库（主机库 hosts[] 可 CRUD；内联主机由 services[].server 派生）
   ============================================================ */
function viewHosts(){
  const libHosts = (state.config && state.config.hosts) || [];
  const derived = deriveHosts();
  const refs = derived.reduce((n, h) => n + h.refs.length, 0);
  const reused = derived.filter(h => h.refs.length > 1).length;

  const libRows = libHosts.map(h => {
    const sv = h.server || {};
    return `
    <div class="tbl-row is-click" data-lib-host="${esc(h.name)}">
      <div class="cell"><span class="nm">${esc(h.name)}</span></div>
      <div class="cell"><span class="val">${esc(sv.host || '')}:${sv.port || 22}</span></div>
      <div class="cell"><span class="val">${esc(sv.username || '')} · ${sv.password ? '密码' : (sv.privateKeyPath ? '密钥' : '—')}</span></div>
      <div class="cell c-opt"><span class="val">${libRefs(h.name)}</span></div>
      <div class="cell"><span class="st ok"><span class="dot dot--ok"></span>已入库</span>
        <button class="btn btn--sm btn--quiet" data-del-lib-host="${esc(h.name)}" title="移出主机库" aria-label="移出主机库 ${esc(h.name)}">${ICON.trash}</button></div>
    </div>`;
  }).join('');

  const inlineRows = derived.map(h => `
    <div class="tbl-row is-click" data-host="${esc(h.key)}">
      <div class="cell"><span class="nm">${esc(h.user)}@${esc(h.host)}</span></div>
      <div class="cell"><span class="val">${esc(h.host)}:${h.port}</span></div>
      <div class="cell"><span class="val">${esc(h.user)} · ${esc(h.auth)}</span></div>
      <div class="cell c-opt">${h.refs.length
        ? `<span class="val">${esc(h.refs.join('、'))}</span>`
        : `<span class="dim">未引用</span>`}</div>
      <div class="cell"><span class="st ok"><span class="dot dot--ok"></span>已配置</span></div>
    </div>`).join('');

  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">主机配置库</div>
        <div class="ph-sub">${libHosts.length} 台主机入库 · ${derived.length} 台内联主机 · 服务可通过 hostRef 引用库内主机（deploy.json › hosts[]）</div>
      </div>
      <div class="ph-right">
        <div class="search">${ICON.search}<input id="hostSearch" placeholder="搜索主机名称 / 地址"></div>
        <button class="btn" data-act="new-host">${ICON.plus}新增入库主机</button>
      </div>
    </div>

    <div class="metrics">
      <div class="metric"><span class="k">HOSTS</span><span class="v">${libHosts.length + derived.length}</span></div>
      <div class="metric"><span class="k">IN LIBRARY</span><span class="v g">${libHosts.length}</span></div>
      <div class="metric"><span class="k">REFERENCES</span><span class="v">${refs}</span></div>
      <div class="metric"><span class="k">REUSED</span><span class="v">${reused}</span></div>
    </div>

    <div class="panel">
      <div class="panel-head"><span class="panel-title">主机库（hosts[]）</span>
        <span class="panel-sub">点击行编辑 · 保存配置后落盘</span></div>
      <div class="tbl" style="--minw:720px;--cols:minmax(0,1fr) 200px 215px 276px 106px;--cols-l:minmax(0,1fr) 200px 215px 106px">
        <div class="tbl-head">
          <span>主机名称</span><span>地址:端口</span><span>登录用户/认证</span><span class="c-opt">引用服务</span><span>状态</span>
        </div>
        ${libRows || `<div class="tbl-row"><div class="cell"><span class="dim">主机库为空，点击「新增入库主机」创建可复用的连接定义</span></div></div>`}
      </div>
    </div>

    <div class="panel">
      <div class="panel-head"><span class="panel-title">服务内联主机（由 services[].server 派生）</span>
        <span class="panel-sub">点击行编辑，保存将同步回写全部引用服务</span></div>
      <div class="tbl" style="--minw:720px;--cols:minmax(0,1fr) 200px 215px 276px 106px;--cols-l:minmax(0,1fr) 200px 215px 106px">
        <div class="tbl-head">
          <span>主机标识</span><span>地址:端口</span><span>登录用户/认证</span><span class="c-opt">引用服务</span><span>状态</span>
        </div>
        ${inlineRows || `<div class="tbl-row"><div class="cell"><span class="dim">当前空间暂无内联主机配置</span></div></div>`}
      </div>
    </div>
  </div>`;
}

/* 主机库条目被哪些服务通过 hostRef 引用 */
function libRefs(name){
  const used = cfgServices().filter(s => (s.hostRef || '').toLowerCase() === name.toLowerCase()).map(s => s.name);
  return used.length ? esc(used.join('、')) : '<span class="dim">未被引用</span>';
}

/* ============================================================
   视图 —— 4. SSH 密钥（/api/keys，永不展示私钥内容）
   ============================================================ */
function viewKeys(){
  if (!state.keysLoaded) { loadKeys().then(render); return loadingPanel('SSH 密钥'); }
  const rows = state.keys.map(k => `
    <div class="tbl-row">
      <div class="cell"><span class="nm">${esc(k.name)}</span></div>
      <div class="cell"><span class="val">${esc(k.algorithm || 'unknown')}${k.encrypted ? ' · 加密' : ''}</span></div>
      <div class="cell"><span class="val">${esc(k.fingerprint || '—（加密私钥，连接时验证）')}</span></div>
      <div class="cell c-opt"><span class="val">${k.usedBy && k.usedBy.length ? esc(k.usedBy.join('、')) : '<span class="dim">未引用</span>'}</span></div>
      <div class="cell"><span class="st ok"><span class="dot dot--ok"></span>可用</span>
        <button class="btn btn--sm btn--quiet" data-del-key="${esc(k.name)}" title="删除密钥" aria-label="删除密钥 ${esc(k.name)}">${ICON.trash}</button></div>
    </div>`).join('');
  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">SSH 密钥</div>
        <div class="ph-sub">${state.keys.length} 个私钥文件 · 存储于 workspaces/${esc(state.activeWs)}/keys/ · 私钥内容永不回传前端</div>
      </div>
      <div class="ph-right"><div class="search">${ICON.search}<input id="keySearch" placeholder="搜索密钥名 / 指纹"></div></div>
    </div>
    <div class="metrics">
      <div class="metric"><span class="k">KEYS</span><span class="v">${state.keys.length}</span></div>
      <div class="metric"><span class="k">ED25519</span><span class="v">${state.keys.filter(k => (k.algorithm||'').indexOf('ed25519') >= 0).length}</span></div>
      <div class="metric"><span class="k">RSA</span><span class="v">${state.keys.filter(k => (k.algorithm||'').indexOf('rsa') >= 0).length}</span></div>
      <div class="metric"><span class="k">ENCRYPTED</span><span class="v">${state.keys.filter(k => k.encrypted).length}</span></div>
    </div>
    <div class="panel">
      <div class="tbl" style="--minw:700px;--cols:minmax(0,1fr) 140px 250px 200px 106px;--cols-l:minmax(0,1fr) 140px 250px 106px">
        <div class="tbl-head">
          <span>私钥文件</span><span>算法</span><span>SHA256 指纹</span><span class="c-opt">被引用</span><span>状态</span>
        </div>
        ${rows || `<div class="tbl-row"><div class="cell"><span class="dim">密钥库为空，点击右上角「导入密钥」添加私钥文件</span></div></div>`}
      </div>
    </div>
  </div>`;
}

function loadingPanel(title){
  return `<div class="page"><div class="page-head"><div>
    <div class="ph-title">${esc(title)}</div>
    <div class="ph-sub">正在从后端加载数据…</div>
  </div></div></div>`;
}

function offlinePanel(){
  return `<div class="page" style="align-items:center;justify-content:center;display:flex;flex-direction:column;gap:14px">
    <div class="ph-title">无法连接控制台后端</div>
    <div class="ph-sub">请确认 deploy -web 服务进程正在运行，然后重试。</div>
    <button class="btn btn--primary" data-act="retry-init">重新连接</button>
  </div>`;
}

/* ============================================================
   视图 —— 5. 空间管理（/api/workspaces）
   ============================================================ */
function viewSpaces(){
  if (!state.ready) { loadWorkspacesFlow().then(render); return loadingPanel('空间管理'); }
  const cards = state.workspaces.map(s => `
    <div class="space-card${s.id === state.activeWs ? ' cur' : ''}" data-space="${esc(s.id)}">
      <div class="top">
        <span class="nm">${esc(s.id)}</span>
        <span class="right">${s.id === state.activeWs
          ? `<span class="pill pill--ok"><span class="dot dot--ok"></span>当前空间</span>`
          : `<span class="pill pill--dim">活跃</span>`}</span>
      </div>
      <div class="meta">${esc(s.name || s.id)}</div>
      <div class="path">${esc(s.path || '')}</div>
      <div class="foot">
        <span>更新于 ${esc(s.updatedAt ? fmtClock(s.updatedAt) : '—')}</span>
        ${s.id !== state.activeWs && !s.isDefault ? `<button class="btn btn--sm btn--quiet" data-del-space="${esc(s.id)}" title="删除空间" aria-label="删除空间 ${esc(s.id)}">${ICON.trash}</button>` : ''}
      </div>
    </div>`).join('');

  const rows = state.history.map(h => `
    <div class="tbl-row is-click" data-batch="${esc(h.id)}">
      <div class="cell"><span class="val">${esc(fmtClock(h.start))}</span></div>
      <div class="cell"><span class="nm">${esc(h.workspace || state.activeWs)}</span></div>
      <div class="cell"><span class="val">批次 #${esc(h.id)}${h.tags && h.tags.length ? ' · 标签 ' + esc(h.tags.join(',')) : ''} · ${h.total} 节点</span></div>
      <div class="cell right"><span class="st ${h.status === 'success' ? 'ok' : 'err'}" style="margin-left:auto">
        ${h.status === 'success' ? `成功 · ${esc(fmtMs(h.durationMs))}` : `${esc(h.status)} · ${esc(fmtMs(h.durationMs))}`}</span></div>
    </div>`).join('');

  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">空间管理</div>
        <div class="ph-sub">${state.workspaces.length} 个空间 · 点击卡片切换（切换后重新加载对应 deploy.json）</div>
      </div>
      <div class="ph-right">
        <div class="search">${ICON.search}<input id="spaceSearch" placeholder="搜索空间名称"></div>
        <button class="btn" data-act="new-space">${ICON.plus}新建空间</button>
      </div>
    </div>

    <div class="metrics">
      <div class="metric"><span class="k">SPACES</span><span class="v">${state.workspaces.length}</span></div>
      <div class="metric"><span class="k">SERVICES</span><span class="v">${cfgServices().length}</span></div>
      <div class="metric"><span class="k">HOSTS</span><span class="v">${deriveHosts().length}</span></div>
    </div>

    <div class="space-grid">${cards}</div>

    <div style="display:flex;flex-direction:column;gap:8px">
      <div style="display:flex;align-items:center">
        <span class="sec-label">最近部署活动</span>
        <button class="btn btn--quiet btn--sm" style="margin-left:auto" data-go="#/history">查看全部历史</button>
      </div>
      <div class="panel">
        <div class="tbl tbl--compact" style="--minw:600px;--cols:186px 138px minmax(0,1fr) 100px">
          <div class="tbl-head" style="border-bottom:none"></div>
          ${rows || `<div class="tbl-row"><div class="cell"><span class="dim">暂无部署记录</span></div></div>`}
        </div>
      </div>
    </div>
  </div>`;
}

async function loadWorkspacesFlow(){
  try {
    const ws = await API.workspaces();
    state.workspaces = ws.workspaces || [];
    state.activeWs = ws.active || state.activeWs;
    state.ready = true;
  } catch (e) { toast('工作空间加载失败：' + e.message); }
}

/* ============================================================
   视图 —— 6. 服务配置（真实 services[] 条目；保存时整表回传，
   掩码 ****** 原样回传由后端 MergePreservingSecrets 回填真值）
   ============================================================ */
function currentService(){
  const list = cfgServices();
  return list.find(s => s.name === app.arg) || list[0] || null;
}

function viewService(){
  const d = currentService();
  if (!d) return loadingPanel('服务配置');
  const sv = d.server || {};
  const up = d.upload || {};
  const hk = d.hooks || {};
  const hookDefs = [
    { key:'preUploadLocal',   where:'本地 · 上传前' },
    { key:'preUploadRemote',  where:'远端 · 上传前' },
    { key:'postUploadRemote', where:'远端 · 上传后' },
    { key:'postUploadLocal',  where:'本地 · 上传后' }
  ];
  const hookCards = hookDefs.map((h, i) => {
    const cmds = hk[h.key] || [];
    return `
    <div class="hook">
      <div class="hook-hd">
        <span class="id">${String(i+1).padStart(2,'0')}</span>
        <span class="ttl">${esc(h.key)}</span>
        <span class="right"><span class="mono" style="font-size:10.5px;color:var(--t3)">${esc(h.where)}</span></span>
      </div>
      ${cmds.map((c, ci) => `<div class="cmd"><span class="no mono">${ci+1}</span><span class="pr">$</span><span class="tx">${esc(c)}</span><span class="ic" data-del-svc-hook="${h.key}:${ci}" title="删除命令" aria-label="删除命令" style="cursor:pointer">${ICON.trash}</span></div>`).join('') ||
        `<span class="dim" style="font-size:11px">未配置</span>`}
      <div class="cmd-add"><input class="input" id="cmdIn-svc-${h.key}" placeholder="输入命令后回车添加（可连续添加多条）" data-cmd-scope="svc" data-cmd-key="${h.key}" autocomplete="off"></div>
    </div>`;
  }).join('');

  const seg = (v, t) => `<button class="${(d.type || 'standard') === v ? 'on' : ''}" data-svc-type="${v}">${t}</button>`;
  const hostPickOpts = hostStoreOptions();

  return `
  <div class="page" style="padding:0;gap:0">
    <div class="page-head" style="padding:18px 24px 16px">
      <div>
        <div class="h1" style="display:flex;align-items:center;gap:10px">${esc(d.name)}
          <span class="pill pill--${d.enabled === false ? 'dim' : 'ok'}"><span class="dot dot--${d.enabled === false ? 'muted' : 'ok'}"></span>${d.enabled === false ? '已停用' : '就绪'}</span></div>
        <div class="ph-sub mono" style="display:flex;align-items:center;gap:8px;flex-wrap:nowrap;white-space:nowrap">
            <span style="color:var(--t3)">↳</span>${svcTags(d).map(t => `<span class="tag type" style="color:var(--${tagColor(t)})">${esc(t)}</span>`).join(' ')}<span style="color:var(--line2)">·</span>
            <span>${esc(d.type || 'standard')}</span><span style="color:var(--line2)">·</span>
            <span>${esc(svcStage(d))}</span><span style="color:var(--line2)">·</span>
            <span style="color:var(--accent)">${esc(sv.host || '—')}:${sv.port || 22}</span><span style="color:var(--line2)">·</span>
            <span>并发受限于 WORKERS</span>
          </div>
        </div>
        <div class="ph-right" style="padding-top:6px">
          <button class="btn" data-go="#/history">执行历史</button>
          <button class="btn btn--primary" data-act="deploy-service">${ICON.bolt}立即部署此服务</button>
        </div>
      </div>

      <div class="cols">
        <div class="L">

          <div class="form-sec">
            <div class="sec-hd"><span class="no">01 基础信息</span><span class="rule"></span></div>
        <div class="g2" style="display:grid;grid-template-columns:minmax(0,1fr) 300px;gap:16px">
          <div class="field"><label>服务名称</label><input class="input" id="fName" value="${esc(d.name)}"></div>
          <div class="field"><label>阶段号（波次）</label><input class="input" id="fStage" value="${d.stage || 1}"></div>
        </div>
        <div class="field">
          <label>任务类型</label>
          <div class="seg" id="fType">${seg('standard','standard')}${seg('exec_only','exec_only')}${seg('sync_only','sync_only')}</div>
        </div>
        <div class="field"><label>标签 tags（回车 / 逗号 / 失焦添加，输入新标签或从已有标签中选择；点击 × 移除）</label>
          <div class="chips" id="fTagsChips">${(d.tags && d.tags.length ? d.tags : ['default']).map(t =>
            `<span class="chip" data-del-tag="${esc(t)}" style="cursor:pointer" title="移除标签 ${esc(t)}">${esc(t)} ×</span>`).join('')}</div>
          <input class="input" id="fTagInput" list="allTagList" value="" placeholder="输入或选择标签后回车添加" style="margin-top:8px" autocomplete="off">
          <datalist id="allTagList">${Object.keys(cfgTags()).map(t => `<option value="${esc(t)}">`).join('')}</datalist>
          <div class="field-note">多维度归类依据；侧栏与部署均按标签筛选，一次勾选多个标签按并集命中。未打标服务归属默认标签 default</div>
        </div>
        <div class="g2" style="display:grid;grid-template-columns:180px minmax(0,1fr);gap:16px">
          <div class="field"><label>启用状态</label>
            <div class="seg" id="fEnabled"><button class="${d.enabled === false ? '' : 'on'}" data-on="1">启用</button><button class="${d.enabled === false ? 'on' : ''}" data-on="0">停用</button></div>
          </div>
        </div>
        <div class="field"><label>主机库引用 hostRef（可选；可从主机库选择并自动回填，也可手动填写后一键存入主机库）</label>
          <div style="display:flex;gap:8px">
            <select class="input" required data-pick-host="fHostRef" style="width:auto;flex:0 0 210px;min-width:0">
              <option value="" disabled selected>${hostPickOpts ? '从库内 / 内联主机选择' : '主机库为空'}</option>
              ${hostPickOpts}
            </select>
            <input class="input" id="fHostRef" value="${esc(d.hostRef || '')}" placeholder="选择已配置主机，或手动填写" style="width:auto;flex:1 1 auto;min-width:120px">
            <button class="btn btn--sm btn--quiet" type="button" data-act="save-host-lib" title="将当前连接配置存入主机库并通过 hostRef 引用">${ICON.plus}存入主机库</button>
          </div>
          <div class="field-note">命中主机库条目时自动回填其连接配置，凭据改为跟随库内配置；未命中则使用手输内联配置</div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">02 连接与认证</span><span class="rule"></span></div>
        <div class="g2" style="display:grid;grid-template-columns:minmax(0,1fr) 120px;gap:16px">
          <div class="field"><label>主机地址</label><input class="input" id="fHost" value="${esc(sv.host || '')}"></div>
          <div class="field"><label>端口</label><input class="input" id="fPort" value="${sv.port || 22}"></div>
        </div>
        <div class="field"><label>登录用户</label><input class="input" id="fMaskU" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform value="${esc(sv.username || '')}"></div>
        <div class="g2" style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
          <div class="field"><label>密码（${sv.password ? '已配置 · 掩码显示' : '未配置'}）</label>
            <div style="display:flex;gap:8px">
              <input class="input masked" id="fMaskA" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform type="text" value="${sv.password ? '******' : ''}" placeholder="留空保持不变" autocomplete="off">
              <button class="btn btn--sm btn--quiet" type="button" data-mask-toggle="fMaskA">显示</button>
            </div>
          </div>
          <div class="field"><label>私钥口令（${sv.passphrase ? '已配置 · 掩码显示' : '未配置'}）</label>
            <div style="display:flex;gap:8px">
              <input class="input masked" id="fMaskB" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform type="text" value="${sv.passphrase ? '******' : ''}" placeholder="留空保持不变" autocomplete="off">
              <button class="btn btn--sm btn--quiet" type="button" data-mask-toggle="fMaskB">显示</button>
            </div>
          </div>
        </div>
        <div class="field"><label>私钥路径（可选；可从 SSH 密钥库选择，或填写本地路径后一键入库，支持 ${'${ENV}'} 插值）</label>
          <div style="display:flex;gap:8px">
            <select class="input" required data-pick-key="fKeyPath" style="width:auto;flex:0 0 210px;min-width:0">
              <option value="" disabled selected>${(state.keys || []).length ? '从 SSH 密钥库选择' : '密钥库为空'}</option>
              ${keyStoreOptions()}
            </select>
            <input class="input" id="fKeyPath" value="${esc(sv.privateKeyPath || '')}" placeholder="本地私钥文件路径或从密钥库选择" style="width:auto;flex:1 1 auto;min-width:120px">
            <button class="btn btn--sm btn--quiet" type="button" data-act="save-key-lib" title="将该路径的私钥存入工作空间 SSH 密钥库">${ICON.plus}存入密钥库</button>
          </div>
        </div>
        <div class="g2" style="display:grid;grid-template-columns:1fr 1fr;gap:16px">
          <div class="field"><label>SHA256 主机指纹（可选）</label><input class="input" id="fFingerprint" value="${esc(sv.hostKeyFingerprint || '')}" placeholder="SHA256:..."></div>
          <div class="field"><label>连接超时（秒）</label><input class="input" id="fTimeout" value="${sv.connectTimeout || 15}"></div>
        </div>
        <div style="display:flex;align-items:center;gap:12px">
          <button class="btn btn--sm" data-act="test-conn">连通性测试</button>
          <span class="st ok" id="testRes"></span>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">03 上传与排除</span><span class="rule"></span></div>
        <div class="field"><label>本地路径（支持文件或目录）</label>
          <div style="display:flex;gap:10px">
            <input class="input" id="fLocalPath" value="${esc(up.localPath || '')}">
            <button class="btn" data-act="pick-local">浏览</button>
          </div>
        </div>
        <div class="field"><label>远端路径（POSIX，自动创建层级）</label><input class="input" id="fRemotePath" value="${esc(up.remotePath || '')}"></div>
        <div class="field">
          <label>排除规则（点击移除）</label>
          <div class="chips">${(up.exclude || []).map(c => `<span class="chip" data-del-exclude="${esc(c)}" style="cursor:pointer">${esc(c)} ×</span>`).join('')}
            <span class="chip" data-act="add-exclude" style="cursor:pointer;color:var(--accent)">+ 添加</span></div>
        </div>
        <div style="display:flex;flex-direction:column;gap:7px">
          <div class="sw-row">
            <span style="font-size:12.5px;color:var(--text)">上传前清理远端目录</span>
            <button type="button" class="sw${up.cleanRemote ? ' on' : ''}" id="swClean" role="switch" aria-checked="${!!up.cleanRemote}" aria-label="上传前清理远端目录"></button>
          </div>
          <div class="field-note">开启后清空目标目录再上传；/、/root、/etc 等高危路径会被后端强制拦截，无法开启</div>
        </div>
      </div>
    </div>

    <div class="R">
      <div style="display:flex;align-items:center;gap:10px">
        <span style="font-size:13px;font-weight:600;color:var(--text)">生命周期钩子</span>
        <span class="mono" style="font-size:10.5px;color:var(--t3);margin-left:auto">按顺序执行 · 失败即中断</span>
      </div>
      <div style="display:flex;gap:9px;padding:10px 12px;border-radius:var(--r-btn);background:rgba(77,159,255,.08);border:1px solid rgba(77,159,255,.22)">
        <span class="mono" style="font-size:11px;color:var(--blue);flex:0 0 auto">i</span>
        <span style="font-size:10.5px;color:var(--t2);line-height:1.6">批次级 preDeploy / postDeploy 在侧栏「批次钩子」统一配置，此处为本服务的四阶段钩子（点击 + 添加、垃圾桶删除，保存配置后落盘）</span>
      </div>
      ${hookCards}
    </div>
    </div>
  </div>`;
}

/* 服务配置页保存：从表单收集并写回 config.services 条目 */
function applyServiceForm(){
  const d = currentService();
  if (!d) return;
  const val = id => { const el = document.getElementById(id); return el ? el.value : null; };
  const num = (v, def) => { const n = parseInt(v, 10); return isNaN(n) ? def : n; };

  d.name = (val('fName') || d.name).trim();
  // 标签：芯片编辑器实时维护 d.tags；此处仅收集尚未回车确认的待输入标签
  const pendingTag = (val('fTagInput') || '').trim().toLowerCase();
  if (pendingTag) {
    d.tags = d.tags || [];
    if (d.tags.indexOf(pendingTag) < 0) d.tags.push(pendingTag);
  }
  const typeEl = document.querySelector('#fType button.on');
  if (typeEl) d.type = typeEl.dataset.svcType;
  d.stage = num(val('fStage'), d.stage || 1);
  const enEl = document.querySelector('#fEnabled button.on');
  if (enEl) d.enabled = enEl.dataset.on === '1' ? true : (enEl.dataset.on === '0' ? false : undefined);
  const hostRef = (val('fHostRef') || '').trim();
  if (hostRef) { d.hostRef = hostRef; } else { delete d.hostRef; }

  d.server = d.server || {};
  d.server.host = (val('fHost') || '').trim();
  d.server.port = num(val('fPort'), 22);
  d.server.username = (val('fMaskU') || '').trim();
  // 掩码或留空保持原值，由后端 MergePreservingSecrets 语义保证不覆盖真实凭证
  const pwd = val('fMaskA');
  if (pwd && pwd !== '******') d.server.password = pwd;
  const pp = val('fMaskB');
  if (pp && pp !== '******') d.server.passphrase = pp;
  d.server.privateKeyPath = (val('fKeyPath') || '').trim();
  d.server.hostKeyFingerprint = (val('fFingerprint') || '').trim();
  d.server.connectTimeout = num(val('fTimeout'), 15);

  d.upload = d.upload || {};
  d.upload.localPath = (val('fLocalPath') || '').trim();
  d.upload.remotePath = (val('fRemotePath') || '').trim();
}

/* ============================================================
   视图 —— 7. 批次钩子（全局 hooks + 标签钩子 tagHooks）
   ============================================================ */
function hookCard(id, key, cmds, tag, tagClass, tagHookName){
  /* tagHookName 存在时命令增删作用于该标签钩子条目，否则作用于全局 hooks */
  const scopeId = 'cmdIn-' + (tagHookName ? 'th-' + String(tagHookName).replace(/[^a-zA-Z0-9_-]/g, '_') : 'hooks') + '-' + key;
  const scopeAttrs = tagHookName
    ? `data-cmd-scope="tagHooks" data-cmd-tag="${esc(tagHookName)}"`
    : `data-cmd-scope="hooks"`;
  const delAttr = i => tagHookName
    ? `data-tag-cmd-del="${esc(tagHookName)}" data-cmd-key="${esc(key)}" data-cmd-idx="${i}"`
    : `data-del-hook="${esc(key)}:${i}"`;
  return `
  <div class="hook">
    <div class="hook-hd">
      <span class="id">${id}</span>
      <span class="ttl">${esc(key)}</span>
      <span class="right">
        <span class="mono" style="font-size:10.5px;color:var(--t3)">本地 · ${esc(tag)}</span>
        <span class="pill ${tagClass}">${tagClass === 'pill--warn' ? '失败即阻断批次' : '有节点失败则不执行'}</span>
      </span>
    </div>
    ${(cmds || []).map((c,i) => `<div class="cmd"><span class="no mono">${i+1}</span><span class="pr">$</span><span class="tx">${esc(c)}</span><span class="ic" ${delAttr(i)} title="删除命令" aria-label="删除命令" style="cursor:pointer">${ICON.trash}</span></div>`).join('') ||
      `<span class="dim" style="font-size:11px">未配置命令</span>`}
    <div class="cmd-add"><input class="input" id="${scopeId}" placeholder="输入命令后回车添加（可连续添加多条）" ${scopeAttrs} data-cmd-key="${esc(key)}" autocomplete="off"></div>
  </div>`;
}
function viewHooks(){
  const hooks = (state.config && state.config.hooks) || {};
  const pre = hooks.preDeploy || [];
  const post = hooks.postDeploy || [];
  const tagHooks = (state.config && state.config.tagHooks) || [];

  const tagHookBlocks = tagHooks.map(th => {
    const thName = String(th.name || '').trim().toLowerCase();
    const thPre = (th.hooks && th.hooks.preDeploy) || [];
    const thPost = (th.hooks && th.hooks.postDeploy) || [];
    return `
    <div class="panel" style="padding:14px 16px;display:flex;flex-direction:column;gap:10px">
      <div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap">
        <span class="tag type" style="color:var(--${tagColor(thName)})">${esc(thName)}</span>
        <input class="input" style="max-width:340px" value="${esc(th.description || '')}" placeholder="标签描述（可选）" data-tag-desc="${esc(thName)}">
        <button class="btn btn--sm btn--quiet" style="margin-left:auto" data-del-tag-hook="${esc(thName)}" title="删除该标签钩子">删除标签钩子</button>
      </div>
      <div style="display:flex;flex-direction:column;gap:10px">
        ${hookCard('01','preDeploy', thPre, '含该标签的首个波次开始前', 'pill--warn', thName)}
        ${hookCard('02','postDeploy', thPost, '该标签全部节点成功后', 'pill--ok', thName)}
      </div>
    </div>`;
  }).join('');

  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">批次钩子</div>
        <div class="ph-sub">全局 + 标签两级作用域 · 每个批次各执行一次 · 与单个服务的四阶段钩子互不干扰</div>
      </div>
      <div class="ph-right">
        <button class="btn" data-act="add-tag-hook">${ICON.plus}新增标签钩子</button>
        <button class="btn" data-go="#/history">查看最近批次日志</button>
      </div>
    </div>

    <div style="display:flex;gap:9px;padding:10px 12px;border-radius:var(--r-btn);background:rgba(77,159,255,.08);border:1px solid rgba(77,159,255,.22)">
      <span class="mono" style="font-size:11px;color:var(--blue);flex:0 0 auto">i</span>
      <span style="font-size:11px;color:var(--t2);line-height:1.6">
        全局 preDeploy 在所有节点并发启动前于本地执行一次，任一命令失败即终止整个批次，postDeploy 仅在全部选定节点成功后执行一次；
        标签钩子按标签绑定（tagHooks）：该标签的前置钩子在含此标签服务的首个波次开始前执行一次，后置钩子在本批次中该标签的全部节点成功后执行一次。均不参与 Worker Pool 并发限流。
      </span>
    </div>

    <span class="sec-label">全局批次钩子 · hooks.preDeploy / hooks.postDeploy</span>
    ${hookCard('01','preDeploy', pre, '批次开始前', 'pill--warn')}
    ${hookCard('02','postDeploy', post, '全部节点成功后', 'pill--ok')}

    <span class="sec-label">标签钩子 · tagHooks[]（${tagHooks.length}）</span>
    ${tagHookBlocks || `<div class="panel" style="padding:14px 16px"><span class="dim" style="font-size:11.5px">尚未绑定任何标签钩子：点击右上角「新增标签钩子」，为指定标签挂载批次前置/后置命令（如该类服务的本地统一构建）</span></div>`}

    <div class="panel" style="padding:14px 16px;display:flex;flex-direction:column;gap:12px">
      <div style="display:flex;align-items:center;gap:10px">
        <span style="font-size:13px;font-weight:600;color:var(--text)">可用变量</span>
        <span style="font-size:11px;color:var(--t3)">钩子命令中可直接引用 · 执行前由控制机按批次上下文注入环境变量（POSIX shell 用 $SPACE，Windows cmd 用 %SPACE%）</span>
      </div>
      <div class="g2" style="display:grid;grid-template-columns:1fr 1fr;gap:10px">
        ${[
          ['$SPACE','当前空间标识'],
          ['$TAGS','本次部署选中的标签列表（逗号分隔，未筛选时为 none）'],
          ['$BATCH_ID','本次批次编号'],
          ['$CONFIG','配置文件路径'],
          ['$NODE_TOTAL','本批次选中的节点总数'],
          ['$NODE_SUCCESS','部署成功的节点数'],
          ['$NODE_FAILED','部署失败的节点数'],
          ['$DURATION','批次总耗时（毫秒）']
        ].map(v => `
          <div style="display:flex;align-items:center;gap:10px;height:20px">
            <span class="mono" style="font-size:11.5px;font-weight:500;color:var(--cyan);flex:0 0 150px">${esc(v[0])}</span>
            <span style="font-size:11px;color:var(--t3)">${esc(v[1])}</span>
          </div>`).join('')}
      </div>
    </div>
  </div>`;
}

/* ============================================================
   视图 —— 8. 部署历史（GET /api/deploy/history）
   ============================================================ */
function viewHistory(){
  if (!state.historyLoaded) { loadHistory().then(render); return loadingPanel('部署历史'); }
  const rows = state.history.map(h => `
    <div class="tbl-row is-click" data-batch="${esc(h.id)}">
      <div class="cell"><span class="val">${esc(fmtClock(h.start))}</span></div>
      <div class="cell"><span class="nm">${esc(h.workspace || state.activeWs)}</span></div>
      <div class="cell"><span class="val">批次 #${esc(h.id)}${h.tags && h.tags.length ? ' · 标签 ' + esc(h.tags.join(',')) : ''} · ${h.total} 节点 · 成功 ${h.success}</span></div>
      <div class="cell right"><span class="st ${h.status === 'success' ? 'ok' : 'err'}" style="margin-left:auto">
        ${h.status === 'success' ? `成功 · ${esc(fmtMs(h.durationMs))}` : `${esc(h.status)} · ${esc(fmtMs(h.durationMs))}`}</span></div>
    </div>`).join('');
  const failed = state.history.filter(h => h.status !== 'success').length;
  const avg = state.history.length
    ? state.history.reduce((n, h) => n + (h.durationMs || 0), 0) / state.history.length
    : 0;
  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">部署历史</div>
        <div class="ph-sub">空间 ${esc(state.activeWs)} · 最近 ${state.history.length} 批 · 点击任意行查看批次详情</div>
      </div>
      <div class="ph-right">
        <div class="search">${ICON.search}<input id="histSearch" placeholder="搜索批次号 / 标签"></div>
        <button class="btn" data-act="refresh-history">刷新</button>
      </div>
    </div>
    <div class="metrics">
      <div class="metric"><span class="k">BATCHES</span><span class="v">${state.history.length}</span></div>
      <div class="metric"><span class="k">SUCCESS</span><span class="v g">${state.history.length - failed}</span></div>
      <div class="metric"><span class="k">FAILED</span><span class="v${failed ? ' r' : ''}">${failed}</span></div>
      <div class="metric"><span class="k">AVG</span><span class="v">${fmtMs(avg)}</span></div>
    </div>
    <div class="panel">
      <div class="tbl tbl--compact" style="--minw:640px;--cols:186px 138px minmax(0,1fr) 140px">
        <div class="tbl-head">
          <span>时间</span><span>空间</span><span>批次内容</span><span style="text-align:right">结果</span>
        </div>
        ${rows || `<div class="tbl-row"><div class="cell"><span class="dim">暂无部署记录，触发一次部署后此处将展示批次历史</span></div></div>`}
      </div>
    </div>
  </div>`;
}

async function loadHistory(){
  try {
    const res = await API.history(state.activeWs, 30);
    state.history = res.history || [];
    state.historyLoaded = true;
  } catch (e) {
    toast('历史加载失败：' + e.message);
  }
}

async function loadKeys(){
  try {
    const res = await API.keys(state.activeWs);
    state.keys = res.keys || [];
    state.keysDir = res.dir || '';
    state.keysLoaded = true;
  } catch (e) {
    toast('密钥加载失败：' + e.message);
  }
}

/* ============================================================
   视图 —— 9. 批次详情（GET /api/deploy/history/{id}）
   ============================================================ */
function viewBatch(){
  const bid = app.arg;
  if (!state.batch || state.batchId !== bid) {
    loadBatch(bid).then(render);
    return loadingPanel('批次 #' + bid);
  }
  const rec = state.batch;
  const ok = rec.status === 'success';
  const slowest = rec.services.reduce((m, s) => (s.durationMs || 0) > (m ? m.durationMs : -1) ? s : m, null);

  /* 波次耗时聚合：由节点结果按 stage 汇总（后端按 Stage 分波调度） */
  const stageAgg = {};
  for (const s of rec.services) {
    const st = 'S' + String(s.stage || 1).padStart(2, '0');
    stageAgg[st] = (stageAgg[st] || 0) + (s.durationMs || 0);
  }
  const maxStage = Math.max(1, ...Object.values(stageAgg));
  const stageRows = Object.keys(stageAgg).map(st => `
    <div class="stage-row">
      <span class="nm">${esc(st)}</span>
      <span class="track"><i style="width:${(stageAgg[st] / maxStage * 100).toFixed(1)}%;background:var(--cyan)"></i></span>
      <span class="du">${esc(fmtMs(stageAgg[st]))}</span>
    </div>`).join('');

  const nodeCards = rec.services.map(s => `
    <div class="node-card">
      <span class="dot dot--${s.status === 'ok' ? 'ok' : s.status === 'skipped' ? 'muted' : 'err'}"></span>
      <span class="nm">${esc(s.name)}</span>
      <span class="host">${esc(s.host || '')}</span>
      <span class="du" title="${esc(s.error || '')}">${s.status === 'ok' ? esc(fmtMs(s.durationMs)) : s.status === 'skipped' ? '跳过' : esc((s.error || '失败').slice(0, 40))}</span>
    </div>`).join('');

  const failedNodes = rec.services.filter(s => s.status !== 'ok' && s.status !== 'skipped');

  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="h1" style="display:flex;align-items:center;gap:10px">批次 #${esc(rec.id)}
          ${ok
            ? `<span class="pill pill--ok"><span class="dot dot--ok"></span>SUCCESS</span>`
            : rec.status === 'canceled'
              ? `<span class="pill pill--warn"><span class="dot dot--warn"></span>CANCELED</span>`
              : `<span class="pill pill--err"><span class="dot dot--err"></span>FAILED</span>`}</div>
        <div class="ph-sub mono">${esc(rec.workspace || state.activeWs)}${rec.tags && rec.tags.length ? ' · 标签 ' + esc(rec.tags.join(',')) : ''} · ${esc(fmtDateTime(rec.start))} → ${esc(fmtDateTime(rec.end))} · 耗时 ${esc(fmtMs(rec.durationMs))}</div>
      </div>
      <div class="ph-right" style="padding-top:6px"><button class="btn" data-act="compare">与上批次对比</button></div>
    </div>

    <div class="metrics">
      <div class="metric"><span class="k">NODES</span><span class="v">${rec.total}</span></div>
      <div class="metric"><span class="k">SUCCESS</span><span class="v g">${rec.success}</span></div>
      <div class="metric"><span class="k">FAILED</span><span class="v${rec.failed ? ' r' : ''}">${rec.failed}</span></div>
      <div class="metric"><span class="k">DURATION</span><span class="v">${esc(fmtMs(rec.durationMs))}</span></div>
    </div>

    <div style="display:flex;flex-direction:column;gap:12px">
      <div style="display:flex;align-items:center;gap:12px">
        <span style="font-size:13px;font-weight:600;color:var(--text)">节点执行结果</span>
        <span style="font-size:11px;color:var(--t3)">${ok
          ? `${rec.total} 个节点全部成功${slowest ? ` · 最慢节点 ${esc(slowest.name)} ${esc(fmtMs(slowest.durationMs))}` : ''}`
          : `${rec.total} 个节点调度 · ${rec.failed} 个节点失败`}</span>
      </div>
      <div class="node-grid" style="flex:1">${nodeCards}</div>
    </div>

    <div class="g2" style="display:grid;grid-template-columns:660px minmax(0,1fr);gap:12px">
      <div class="panel" style="padding:14px 16px;display:flex;flex-direction:column;gap:12px">
        <div style="display:flex;flex-direction:column;gap:5px">
          <span style="font-size:13px;font-weight:600;color:var(--text)">波次耗时聚合</span>
          <span style="font-size:11px;color:var(--t3)">按 Stage 分波调度 · 波次串行 · 波内按 Worker Pool 并发</span>
        </div>
        <div style="display:flex;flex-direction:column;gap:10px">${stageRows || '<span class="dim" style="font-size:11px">无节点数据</span>'}</div>
        <div style="flex:1"></div>
        <div class="foot-note">数据来源 workspaces/${esc(rec.workspace || state.activeWs)}/history/batch-${esc(rec.id)}.json</div>
      </div>

      <div class="panel" style="padding:14px 16px;display:flex;flex-direction:column;gap:12px">
        <div style="display:flex;flex-direction:column;gap:5px">
          <span style="font-size:13px;font-weight:600;color:var(--text)">失败明细</span>
          <span style="font-size:11px;color:var(--t3)">${failedNodes.length ? failedNodes.length + ' 个节点存在失败原因记录' : '本批次无失败节点'}</span>
        </div>
        ${failedNodes.map(s => `
          <div style="display:flex;flex-direction:column;gap:3px;padding:8px 10px;border-radius:var(--r-cmd);background:rgba(255,93,93,.06);border:1px solid rgba(255,93,93,.18)">
            <span class="mono" style="font-size:11.5px;color:var(--red)">${esc(s.name)} · ${esc(s.host || '')}</span>
            <span style="font-size:10.5px;color:var(--t2);line-height:1.5;word-break:break-all">${esc(s.error || '未知错误')}</span>
          </div>`).join('') || '<span class="dim" style="font-size:11px">全部节点流水线执行成功，无中断</span>'}
        <div style="flex:1"></div>
        <div class="foot-note">完整日志归档于 workspaces/${esc(rec.workspace || state.activeWs)}/history/batch-${esc(rec.id)}.log</div>
      </div>
    </div>
  </div>`;
}

async function loadBatch(id){
  try {
    state.batch = await API.batch(id, state.activeWs);
    state.batchId = id;
  } catch (e) {
    state.batch = null; state.batchId = null;
    toast('批次详情加载失败：' + e.message);
  }
}

/* 导出批次报告：由详情数据本地生成 JSON 下载 */
function exportBatchReport(){
  const rec = state.batch;
  if (!rec) { toast('请先打开批次详情'); return; }
  downloadJSON(rec, 'batch-' + rec.id + '.json');
}
function exportAllHistory(){
  if (!state.history.length) { toast('暂无历史记录可导出'); return; }
  downloadJSON(state.history, 'deploy-history-' + state.activeWs + '.json');
}
function downloadJSON(data, filename){
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = filename;
  a.click();
  URL.revokeObjectURL(a.href);
}

/* 与上批次对比：取历史列表中紧邻的上一条做摘要 diff */
function compareWithPrev(){
  const cur = state.batch;
  if (!cur) { toast('请先打开批次详情'); return; }
  const idx = state.history.findIndex(h => h.id === cur.id);
  const prev = idx >= 0 && idx + 1 < state.history.length ? state.history[idx + 1] : null;
  if (!prev) { toast('没有更早的批次可对比'); return; }
  const dDur = (cur.durationMs || 0) - (prev.durationMs || 0);
  const dNodes = (cur.total || 0) - (prev.total || 0);
  toast(`对比批次 #${prev.id}：耗时 ${dDur >= 0 ? '+' : ''}${fmtMs(Math.abs(dDur))} · 节点 ${dNodes >= 0 ? '+' : ''}${dNodes}`);
}

/* ============================================================
   视图 —— 10. 设置（GET/POST /api/settings）
   ============================================================ */
function viewSettings(){
  if (!state.settingsLoaded) {
    API.settings().then(st => { state.settings = st; state.settingsLoaded = true; render(); })
      .catch(e => toast('设置加载失败：' + e.message));
    return loadingPanel('设置');
  }
  const st = state.settings;
  /* 安全红线项为恒定生效的后端硬校验，以只读徽标呈现（非开关，避免误导可关闭） */
  const badge = (t, d) => `
    <div class="sw-row"><div><div style="font-size:12.5px;color:var(--text)">${esc(t)}</div>
      <div class="field-note">${esc(d)}</div></div>
      <span class="pill pill--ok" style="margin-left:auto"><span class="dot dot--ok"></span>恒定生效</span></div>`;
  return `
  <div class="page">
    <div class="page-head">
      <div>
        <div class="ph-title">设置</div>
        <div class="ph-sub">控制台运行时参数 · 保存后写入 settings.json</div>
      </div>
    </div>
    <div class="form-sec" style="max-width:720px">
      <div class="sec-hd"><span class="no">01 并发</span><span class="rule"></span></div>
      <div class="field" style="max-width:360px">
        <label>默认最大并发度（Worker Pool 容量）</label>
        <input class="input" id="setMaxWorkers" value="${st.maxWorkers}">
        <div class="field-note">超过 10 可能导致本地文件描述符枯竭或对端 SSH 防刷流控触发</div>
      </div>

      <div class="sec-hd"><span class="no">02 安全红线</span><span class="rule"></span></div>
      <div style="display:flex;flex-direction:column;gap:14px">
        ${badge('强制校验主机指纹', '配置指纹后变更即阻断该节点部署，防止中间人攻击（架构红线，不可关闭）')}
        ${badge('高危远端路径拦截', '拦截 /、/root、/etc、/bin 等系统关键路径及向上逃逸路径的递归删除（架构红线，不可关闭）')}
      </div>

      <div class="sec-hd"><span class="no">03 启动与监听</span><span class="rule"></span></div>
      <div style="display:flex;flex-direction:column;gap:14px">
        <div class="sw-row"><div><div style="font-size:12.5px;color:var(--text)">启动时自动打开浏览器</div>
          <div class="field-note">关闭后仅输出访问地址；环境变量 DEPLOY_NO_OPEN=1 可强制关闭</div></div>
          <button type="button" class="sw${st.autoOpenBrowser ? ' on' : ''}" id="swAutoOpen" role="switch" aria-checked="${!!st.autoOpenBrowser}" aria-label="启动时自动打开浏览器" style="margin-left:auto"></button></div>
        <div class="field" style="max-width:360px">
          <label>Web 控制台绑定地址（当前生效）</label>
          <input class="input" id="setListenAddr" value="${esc(st.listenAddrSetting || st.listenAddr)}">
          <div class="field-note">默认仅监听本地回环 ${esc(st.listenAddrDefault)}；修改保存后需重启进程生效${st.listenAddrRestart ? ' · <span style="color:var(--amber)">当前已保存的地址与生效地址不一致</span>' : ''}</div>
        </div>
      </div>
    </div>
  </div>`;
}

/* 设置保存：收集表单 → POST /api/settings */
async function saveSettingsFlow(){
  const num = (id, def) => { const el = document.getElementById(id); const n = parseInt(el && el.value, 10); return isNaN(n) ? def : n; };
  const addr = (document.getElementById('setListenAddr') || {}).value || '';
  const swAuto = document.getElementById('swAutoOpen');
  try {
    const res = await API.saveSettings({
      maxWorkers: num('setMaxWorkers', 10),
      listenAddr: addr.trim(),
      autoOpen: swAuto ? swAuto.classList.contains('on') : undefined
    });
    state.settingsLoaded = false;
    toast(res && res.restartRequired ? '设置已保存 · 监听地址需重启生效' : '设置已保存', 'ok');
    render();
  } catch (e) {
    toast(e.status === 409 ? '部署运行中，禁止修改设置' : ('保存失败：' + e.message));
  }
}

/* ============================================================
   抽屉 —— 主机编辑（双模式：library 编辑 hosts[] 条目；inline 回写引用服务）
   ============================================================ */
let drawerCtx = null;   // { mode:'library'|'inline', name/host, services }

function openHostDrawer(key){
  const hosts = deriveHosts();
  const h = hosts.find(x => x.key === key) || hosts[0];
  if (!h) { toast('当前空间暂无主机配置'); return; }
  drawerCtx = { mode: 'inline', host: h };
  const ref = h.services[0] || {};
  const sv = ref.server || {};
  const drawer = document.getElementById('drawer');
  drawer.innerHTML = `
    <div class="drawer-hd">
      <div>
        <div class="t">编辑内联主机</div>
        <div class="s">${esc(h.user)}@${esc(h.host)}:${h.port} · 被 ${h.refs.length} 个服务引用（保存将同步回写全部引用服务）</div>
      </div>
      <button class="x" data-act="close-drawer" aria-label="关闭">${ICON.close}</button>
    </div>
    <div class="drawer-bd">
      <div class="form-sec">
        <div class="sec-hd"><span class="no">01 基础信息</span><span class="rule"></span></div>
        <div style="display:grid;grid-template-columns:minmax(0,1fr) 92px;gap:14px">
          <div class="field"><label>地址</label><input class="input" id="dHost" value="${esc(h.host)}"></div>
          <div class="field"><label>端口</label><input class="input" id="dPort" value="${h.port}"></div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">02 认证方式</span><span class="rule"></span></div>
        <div class="field"><label>登录用户</label><input class="input" id="dMaskU" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform value="${esc(h.user)}"></div>
        <div class="field"><label>密码（${sv.password ? '已配置 · 掩码显示' : '未配置'}）</label>
          <div style="display:flex;gap:8px">
            <input class="input masked" id="dMaskA" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform type="text" value="${sv.password ? '******' : ''}" placeholder="留空保持不变" autocomplete="off">
            <button class="btn btn--sm btn--quiet" type="button" data-mask-toggle="dMaskA">显示</button>
          </div>
        </div>
        <div class="field"><label>私钥口令（${sv.passphrase ? '已配置 · 掩码显示' : '未配置'}）</label>
          <div style="display:flex;gap:8px">
            <input class="input masked" id="dMaskB" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform type="text" value="${sv.passphrase ? '******' : ''}" placeholder="留空保持不变" autocomplete="off">
            <button class="btn btn--sm btn--quiet" type="button" data-mask-toggle="dMaskB">显示</button>
          </div>
        </div>
        <div class="field"><label>私钥路径（可选；可从 SSH 密钥库选择，或填写本地路径后一键入库）</label>
          <div style="display:flex;gap:6px">
            <select class="input" required data-pick-key="dKeyPath" style="width:auto;flex:0 0 118px;min-width:0;padding:9px 6px">
              <option value="" disabled selected>${(state.keys || []).length ? '选择密钥' : '库为空'}</option>
              ${keyStoreOptions()}
            </select>
            <input class="input" id="dKeyPath" value="${esc(sv.privateKeyPath || '')}" placeholder="本地路径或从密钥库选择" style="width:auto;flex:1 1 auto;min-width:80px">
            <button class="btn btn--sm btn--quiet" type="button" data-act="save-key-lib" title="将该路径的私钥存入工作空间 SSH 密钥库">${ICON.plus}</button>
          </div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">03 安全校验</span><span class="rule"></span></div>
        <div class="field">
          <label>SHA256 主机指纹（可选）</label>
          <input class="input" id="dFingerprint" value="${esc(sv.hostKeyFingerprint || '')}" placeholder="SHA256:...">
          <div class="field-note">配置后首次连接即强校验；指纹变更将直接阻断该节点部署，防止中间人攻击。</div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">04 连接策略</span><span class="rule"></span></div>
        <div class="field"><label>连接超时（秒）</label><input class="input" id="dTimeout" value="${sv.connectTimeout || 15}"></div>
      </div>
    </div>
    <div class="drawer-ft">
      <button class="btn" data-act="test-conn">连通性测试</button>
      <span class="ok" id="testRes"></span>
      <div class="acts">
        <button class="btn" data-act="close-drawer">取消</button>
        <button class="btn btn--primary" data-act="save-drawer">保存更改</button>
      </div>
    </div>`;
  document.getElementById('backdrop').classList.add('show');
  drawer.classList.add('show');
  bindDrawerActions();
}

/* 编辑主机库条目抽屉（hosts[]，保存进 config 待落盘） */
function openLibHostDrawer(name){
  const lib = (state.config && state.config.hosts) || [];
  const h = lib.find(x => x.name === name);
  if (!h) { toast('主机库条目不存在'); return; }
  drawerCtx = { mode: 'library', name: h.name };
  const sv = h.server || {};
  const drawer = document.getElementById('drawer');
  drawer.innerHTML = `
    <div class="drawer-hd">
      <div>
        <div class="t">编辑库内主机</div>
        <div class="s">deploy.json › hosts[] · 被 ${cfgServices().filter(s => (s.hostRef||'').toLowerCase() === h.name.toLowerCase()).length} 个服务通过 hostRef 引用</div>
      </div>
      <button class="x" data-act="close-drawer" aria-label="关闭">${ICON.close}</button>
    </div>
    <div class="drawer-bd">
      <div class="form-sec">
        <div class="sec-hd"><span class="no">01 基础信息</span><span class="rule"></span></div>
        <div class="field"><label>主机名称（服务 hostRef 引用名）</label><input class="input" id="dName" value="${esc(h.name)}"></div>
        <div style="display:grid;grid-template-columns:minmax(0,1fr) 92px;gap:14px">
          <div class="field"><label>地址</label><input class="input" id="dHost" value="${esc(sv.host || '')}"></div>
          <div class="field"><label>端口</label><input class="input" id="dPort" value="${sv.port || 22}"></div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">02 认证方式</span><span class="rule"></span></div>
        <div class="field"><label>登录用户</label><input class="input" id="dMaskU" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform value="${esc(sv.username || '')}"></div>
        <div class="field"><label>密码（${sv.password ? '已配置 · 掩码显示' : '未配置'}）</label>
          <div style="display:flex;gap:8px">
            <input class="input masked" id="dMaskA" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform type="text" value="${sv.password ? '******' : ''}" placeholder="留空保持不变" autocomplete="off">
            <button class="btn btn--sm btn--quiet" type="button" data-mask-toggle="dMaskA">显示</button>
          </div>
        </div>
        <div class="field"><label>私钥口令（${sv.passphrase ? '已配置 · 掩码显示' : '未配置'}）</label>
          <div style="display:flex;gap:8px">
            <input class="input masked" id="dMaskB" data-1p-ignore data-lpignore="true" data-bwignore data-form-type="other" data-kwpform type="text" value="${sv.passphrase ? '******' : ''}" placeholder="留空保持不变" autocomplete="off">
            <button class="btn btn--sm btn--quiet" type="button" data-mask-toggle="dMaskB">显示</button>
          </div>
        </div>
        <div class="field"><label>私钥路径（可选；可从 SSH 密钥库选择，或填写本地路径后一键入库，支持 ${'${ENV}'} 插值）</label>
          <select class="input" required data-pick-key="dKeyPath">
            <option value="" disabled selected>${(state.keys || []).length ? '从 SSH 密钥库选择…' : 'SSH 密钥库为空：可填路径后点「存入密钥库」，或到 SSH 密钥页导入'}</option>
            ${keyStoreOptions()}
          </select>
          <div style="display:flex;gap:8px">
            <input class="input" id="dKeyPath" value="${esc(sv.privateKeyPath || '')}" placeholder="本地私钥文件路径或从密钥库选择">
            <button class="btn btn--sm btn--quiet" type="button" data-act="save-key-lib" title="将该路径的私钥存入工作空间 SSH 密钥库">${ICON.plus}存入密钥库</button>
          </div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">03 安全校验</span><span class="rule"></span></div>
        <div class="field">
          <label>SHA256 主机指纹（可选）</label>
          <input class="input" id="dFingerprint" value="${esc(sv.hostKeyFingerprint || '')}" placeholder="SHA256:...">
          <div class="field-note">配置后首次连接即强校验；指纹变更将直接阻断该节点部署，防止中间人攻击。</div>
        </div>
      </div>

      <div class="form-sec">
        <div class="sec-hd"><span class="no">04 连接策略</span><span class="rule"></span></div>
        <div class="field"><label>连接超时（秒）</label><input class="input" id="dTimeout" value="${sv.connectTimeout || 15}"></div>
      </div>
    </div>
    <div class="drawer-ft">
      <button class="btn" data-act="test-conn">连通性测试</button>
      <span class="ok" id="testRes"></span>
      <div class="acts">
        <button class="btn" data-act="close-drawer">取消</button>
        <button class="btn btn--primary" data-act="save-drawer">保存更改</button>
      </div>
    </div>`;
  document.getElementById('backdrop').classList.add('show');
  drawer.classList.add('show');
  bindDrawerActions();
}

/* 从抽屉收集当前表单值为 ServerConfig（用于连通性测试与回写） */
function collectDrawerServer(){
  const val = id => { const el = document.getElementById(id); return el ? el.value : null; };
  const num = (v, def) => { const n = parseInt(v, 10); return isNaN(n) ? def : n; };
  const server = {
    host: (val('dHost') || '').trim(),
    port: num(val('dPort'), 22),
    username: (val('dMaskU') || '').trim(),
    privateKeyPath: (val('dKeyPath') || '').trim(),
    hostKeyFingerprint: (val('dFingerprint') || '').trim(),
    connectTimeout: num(val('dTimeout'), 15)
  };
  const pwd = val('dMaskA');
  if (pwd && pwd !== '******') server.password = pwd;
  const pp = val('dMaskB');
  if (pp && pp !== '******') server.passphrase = pp;
  return server;
}

/* 抽屉内容为动态注入，打开后需单独绑定其动作按钮（bind() 仅覆盖渲染时就存在的 DOM） */
function bindDrawerActions(){
  const drawer = document.getElementById('drawer');
  drawer.querySelectorAll('[data-act]').forEach(el => {
    const fn = ACTIONS[el.dataset.act];
    if (fn) el.onclick = fn;
  });
  bindMaskToggles(drawer);
}

/* 保存抽屉：library 模式写回 hosts[] 条目；inline 模式同步回写全部引用服务 */
function applyDrawer(){
  if (!drawerCtx) return false;
  const server = collectDrawerServer();
  const num = (v, def) => { const n = parseInt(v, 10); return isNaN(n) ? def : n; };

  if (drawerCtx.mode === 'library') {
    const lib = state.config.hosts || (state.config.hosts = []);
    const h = lib.find(x => x.name === drawerCtx.name);
    if (!h) return false;
    const keepPwd = h.server ? h.server.password : '';
    const keepPP = h.server ? h.server.passphrase : '';
    const nameInput = document.getElementById('dName');
    h.name = (nameInput && nameInput.value.trim()) || h.name;
    h.server = h.server || {};
    h.server.host = server.host;
    h.server.port = server.port;
    h.server.username = server.username;
    // 密码/口令留空或掩码时保持原值（掩码由后端 MergePreservingSecrets 兜底）
    h.server.password = server.password !== undefined ? server.password : keepPwd;
    h.server.passphrase = server.passphrase !== undefined ? server.passphrase : keepPP;
    h.server.privateKeyPath = server.privateKeyPath;
    h.server.hostKeyFingerprint = server.hostKeyFingerprint;
    h.server.connectTimeout = server.connectTimeout;
    markDirty();
    return true;
  }

  for (const svc of drawerCtx.host.services) {
    const keepPwd = svc.server.password;
    const keepPP = svc.server.passphrase;
    svc.server.host = server.host;
    svc.server.port = server.port;
    svc.server.username = server.username;
    svc.server.password = server.password !== undefined ? server.password : keepPwd;
    svc.server.passphrase = server.passphrase !== undefined ? server.passphrase : keepPP;
    svc.server.privateKeyPath = server.privateKeyPath;
    svc.server.hostKeyFingerprint = server.hostKeyFingerprint;
    svc.server.connectTimeout = server.connectTimeout;
  }
  markDirty();
  return true;
}

async function testConnFlow(){
  const resEl = document.getElementById('testRes');
  let server, serviceName = null;
  if (drawerCtx && document.getElementById('dHost')) {
    server = collectDrawerServer();
  } else {
    applyServiceForm();
    const d = currentService();
    if (!d) return;
    server = JSON.parse(JSON.stringify(d.server || {}));
    serviceName = d.name;
    // hostRef 引用库内主机时以前端掌握的库值补齐探测参数
    if (d.hostRef && state.config) {
      const h = (state.config.hosts || []).find(x => x.name.toLowerCase() === d.hostRef.toLowerCase());
      if (h && h.server) {
        server.host = server.host || h.server.host;
        server.port = server.port || h.server.port;
        server.username = server.username || h.server.username;
        server.privateKeyPath = server.privateKeyPath || h.server.privateKeyPath;
        server.hostKeyFingerprint = server.hostKeyFingerprint || h.server.hostKeyFingerprint;
        if (!server.password) server.password = h.server.password;
        if (!server.passphrase) server.passphrase = h.server.passphrase;
      }
    }
  }
  if (!server.host) { toast('请先填写主机地址'); return; }
  if (resEl) resEl.innerHTML = '<span class="dot dot--run"></span> 测试中…';
  try {
    const res = await API.testConnect(serviceName, server);
    if (resEl) resEl.innerHTML = `<span class="dot dot--ok"></span> 通过 · ${res.latencyMs}ms`;
  } catch (e) {
    // test-connect 端点对不可达主机返回 200 + status:error，此处处理后端异常
    if (resEl) resEl.innerHTML = `<span class="dot dot--err"></span> ${esc(e.message).slice(0, 60)}`;
    toast('连通性测试失败：' + e.message);
  }
}

function closeDrawer(){
  document.getElementById('backdrop').classList.remove('show');
  document.getElementById('drawer').classList.remove('show');
  /* 同步回收深链，避免后续重渲染时抽屉自行弹回 */
  if (app.view === 'hosts' && app.arg) {
    app.arg = null;
    if (location.hash.indexOf('#/hosts/') === 0) {
      history.replaceState(null, '', location.pathname + location.search + '#/hosts');
    }
  }
}

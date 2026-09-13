/* ============================================================
   core.js —— 基础层：图标、API 客户端、全局状态、派生模型（标签/主机）、格式化工具、路由状态与通用工具
   零构建链原生 JS：由 index.html 按依赖顺序以 <script> 引入，共享全局命名空间
   ============================================================ */
'use strict';

/* ============================================================
   图标（内联 SVG，避免任何图标字体/外部依赖）
   ============================================================ */
const ICON = {
  logo: `<svg viewBox="0 0 14 14" fill="none"><path d="M7 11V3.2M7 3.2L3.6 6.6M7 3.2l3.4 3.4" stroke="#00E07A" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/><path d="M2.4 11.6h9.2" stroke="#00E07A" stroke-width="1.6" stroke-linecap="round"/></svg>`,
  search: `<svg viewBox="0 0 14 14" fill="none"><circle cx="6.2" cy="6.2" r="4.2" stroke-width="1.5"/><path d="M9.4 9.4L12.4 12.4" stroke-width="1.5" stroke-linecap="round"/></svg>`,
  link: `<svg viewBox="0 0 12 12" fill="none"><path d="M5 7a2 2 0 0 0 2.8 0l1.6-1.6a2 2 0 1 0-2.8-2.8l-.7.7" stroke-width="1.3" stroke-linecap="round"/><path d="M7 5a2 2 0 0 0-2.8 0L2.6 6.6a2 2 0 1 0 2.8 2.8l.7-.7" stroke-width="1.3" stroke-linecap="round"/></svg>`,
  trash: `<svg viewBox="0 0 12 12" fill="none"><path d="M2 3.4h8M4.6 3.4V2.2h2.8v1.2M3.2 3.4l.4 6.4h5.2l.4-6.4" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  plus: `<svg viewBox="0 0 12 12" fill="none"><path d="M6 2.4v7.2M2.4 6h7.2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>`,
  bolt: `<svg viewBox="0 0 12 12" fill="none"><path d="M6.8 1.2L2.6 6.8h3l-.8 4 4.2-5.6h-3l.8-4z" fill="currentColor"/></svg>`,
  play: `<svg viewBox="0 0 12 12" fill="none"><path d="M3.4 2.2l6 3.8-6 3.8V2.2z" fill="currentColor"/></svg>`,
  check: `<svg viewBox="0 0 11 11" fill="none"><path d="M1.6 5.9L4.3 8.6L9.4 2.6" stroke="#00E07A" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  close: `<svg viewBox="0 0 12 12" fill="none"><path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"/></svg>`,
  grip: `<svg viewBox="0 0 12 12" fill="none"><path d="M3.4 4.6h5.2M3.4 7.4h5.2" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>`,
  /* 侧栏折叠为图标条时使用的导航图标 */
  server: `<svg viewBox="0 0 14 14" fill="none"><rect x="1.6" y="2.2" width="10.8" height="4.2" rx="1.2" stroke="currentColor" stroke-width="1.3"/><rect x="1.6" y="7.6" width="10.8" height="4.2" rx="1.2" stroke="currentColor" stroke-width="1.3"/><path d="M9.9 4.3h.01M9.9 9.7h.01" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>`,
  key: `<svg viewBox="0 0 14 14" fill="none"><circle cx="4.4" cy="9.6" r="2.8" stroke="currentColor" stroke-width="1.3"/><path d="M6.4 7.6l5-5M9.6 4.4l1.6 1.6M8.2 5.8l1.6 1.6" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>`,
  hook: `<svg viewBox="0 0 14 14" fill="none"><path d="M7 2.4v4.2" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/><path d="M7 6.6a3 3 0 1 0 3 3" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/><path d="M2.6 3.2h8.8" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>`,
  clock: `<svg viewBox="0 0 14 14" fill="none"><circle cx="7" cy="7" r="5.2" stroke="currentColor" stroke-width="1.3"/><path d="M7 4.2V7l2.2 1.6" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  gear: `<svg viewBox="0 0 14 14" fill="none"><circle cx="7" cy="7" r="2.1" stroke="currentColor" stroke-width="1.3"/><path d="M7 1.6v1.6M7 10.8v1.6M1.6 7h1.6M10.8 7h1.6M3.2 3.2l1.1 1.1M9.7 9.7l1.1 1.1M10.8 3.2L9.7 4.3M4.3 9.7L3.2 10.8" stroke="currentColor" stroke-width="1.3" stroke-linecap="round"/></svg>`,
  layers: `<svg viewBox="0 0 14 14" fill="none"><path d="M7 1.8L12.2 4.6 7 7.4 1.8 4.6 7 1.8z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/><path d="M1.8 8l5.2 2.8L12.2 8" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/></svg>`
};

/* ============================================================
   API 客户端 —— 后端 REST 契约统一封装（单一出口，便于统一错误处理）
   ============================================================ */
const API = {
  qs(params) {
    const p = new URLSearchParams();
    for (const k in params) {
      const v = params[k];
      if (v !== undefined && v !== null && v !== '') p.set(k, v);
    }
    const s = p.toString();
    return s ? '?' + s : '';
  },
  async req(method, url, body) {
    const opt = { method, headers: {} };
    if (body !== undefined) {
      opt.headers['Content-Type'] = 'application/json';
      opt.body = JSON.stringify(body);
    }
    let res;
    try { res = await fetch(url, opt); }
    catch (e) { throw new Error('无法连接控制台后端'); }
    const text = await res.text();
    let data = null;
    try { data = text ? JSON.parse(text) : null; } catch (e) { data = null; }
    if (!res.ok) {
      const err = new Error(data && data.error ? data.error : (text || ('HTTP ' + res.status)));
      err.status = res.status;
      throw err;
    }
    return data;
  },
  config: ws => API.req('GET', '/api/config' + API.qs({ workspace: ws })),
  saveConfig: (cfg, ws) => API.req('POST', '/api/config' + API.qs({ workspace: ws }), cfg),
  workspaces: () => API.req('GET', '/api/workspaces'),
  selectWorkspace: id => API.req('POST', '/api/workspaces/select', { workspace: id }),
  createWorkspace: (id, name, from) => API.req('POST', '/api/workspaces/create', { id, name, from }),
  deleteWorkspace: id => API.req('DELETE', '/api/workspaces?id=' + encodeURIComponent(id)),
  deploy: opts => API.req('POST', '/api/deploy', opts),
  cancel: () => API.req('POST', '/api/deploy/cancel', {}),
  testConnect: (serviceName, server) => API.req('POST', '/api/server/test-connect', { serviceName, server }),
  pickPath: mode => API.req('GET', '/api/system/pick-path' + API.qs({ mode })),
  history: (ws, limit) => API.req('GET', '/api/deploy/history' + API.qs({ workspace: ws, limit })),
  batch: (id, ws) => API.req('GET', '/api/deploy/history/' + encodeURIComponent(id) + API.qs({ workspace: ws })),
  keys: ws => API.req('GET', '/api/keys' + API.qs({ workspace: ws })),
  importKey: (name, content, passphrase, ws) => API.req('POST', '/api/keys' + API.qs({ workspace: ws }), { name, content, passphrase }),
  importKeyPath: (path, name, passphrase, ws) => API.req('POST', '/api/keys' + API.qs({ workspace: ws }), { path, name, passphrase }),
  deleteKey: (name, ws) => API.req('DELETE', '/api/keys' + API.qs({ id: name, workspace: ws })),
  settings: () => API.req('GET', '/api/settings'),
  saveSettings: st => API.req('POST', '/api/settings', st)
};

/* ============================================================
   全局状态 —— 单一数据源：视图只读 state，动作经 API 写回后置 dirty
   ============================================================ */
const state = {
  ready: false,          // 后端引导（workspaces + config）是否完成
  config: null,          // 当前活动空间的 deploy.json（脱敏视图；保存时原样回传，后端回填掩码真值）
  workspaces: [],
  activeWs: '',
  dirty: false,          // 相对磁盘 deploy.json 的未保存更改
  deploy: null,          // 实时批次状态（由 SSE 结构化事件驱动）
  logBuffer: [],         // 日志环形缓冲（重渲染时回填终端）
  logCount: 0,
  es: null,              // EventSource
  esConnected: false,
  history: [],
  historyLoaded: false,
  batch: null,           // 批次详情
  batchId: null,
  keys: [],
  keysLoaded: false,
  keysDir: '',           // 工作空间私钥库存储目录（GET /api/keys 返回，用于拼装引用路径）
  settings: null,
  settingsLoaded: false
};

/* 未保存更改标记：任何就地编辑（服务表单/主机抽屉/钩子/排除规则）都必须先经过它 */
function markDirty() {
  state.dirty = true;
  const btn = document.querySelector('#topbar [data-act="save"]');
  if (btn && !btn.textContent.includes('●')) btn.textContent = btn.textContent + ' ●';
}

/* ============================================================
   派生模型 —— 标签体系：服务打标（services[].tags）→ 筛选/批量部署；
   标签无需预声明，tagHooks 中声明的标签钩子同样纳入标签建议
   ============================================================ */

/* 标签色板：按标签名 hash 稳定取色（替代旧版写死的分组名映射） */
const TAG_COLORS = ['blue', 'accent', 'violet', 'amber', 'cyan'];
function tagColor(tag) {
  let h = 0;
  const t = String(tag || '');
  for (let i = 0; i < t.length; i++) h = (h * 31 + t.charCodeAt(i)) >>> 0;
  return TAG_COLORS[h % TAG_COLORS.length];
}

function cfgServices() { return (state.config && state.config.services) || []; }

/* 服务标签视图：旧配置的 group 字段视为单标签（后端加载时已自动迁移，此处仅兜底）；
   未打标服务归属默认标签 default */
function svcTags(s) {
  const raw = (s && s.tags) || [];
  if (raw.length) return raw.map(t => String(t).trim().toLowerCase()).filter(Boolean);
  return [s && s.group ? String(s.group).trim().toLowerCase() : 'default'];
}

/* 标签→服务计数（含 tagHooks 声明但暂无服务的标签，计数为 0，供输入建议） */
function cfgTags() {
  const counts = {};
  for (const s of cfgServices()) {
    for (const t of svcTags(s)) counts[t] = (counts[t] || 0) + 1;
  }
  for (const th of ((state.config && state.config.tagHooks) || [])) {
    const n = String(th.name || '').trim().toLowerCase();
    if (n && !(n in counts)) counts[n] = 0;
  }
  return counts;
}

/* 按标签名查找 tagHooks 条目（大小写不敏感） */
function findTagHookEntry(name) {
  const target = String(name || '').trim().toLowerCase();
  if (!target) return null;
  return ((state.config && state.config.tagHooks) || []).find(
    th => String(th.name || '').trim().toLowerCase() === target) || null;
}

function svcStage(svc) { return 'S' + String(svc.stage || 1).padStart(2, '0'); }
function svcSt(svc) { return svc.enabled === false ? 'off' : 'ok'; }

/* ============================================================
   流水线步骤模型 —— 服务 steps 字段（缺省=执行，显式 false=跳过）；
   旧版 type 字段已由后端迁移为步骤开关，此处仅做展示口径推导
   ============================================================ */
const PIPELINE_STEPS = [
  { key: 'preUploadLocal',   where: '本地 · 上传前' },
  { key: 'preUploadRemote',  where: '远端 · 上传前' },
  { key: 'upload',           where: '本地 → 远端 · SFTP' },
  { key: 'postUploadRemote', where: '远端 · 上传后' },
  { key: 'postUploadLocal',  where: '本地 · 上传后' }
];

function stepEnabled(svc, key) {
  const st = svc && svc.steps;
  return !(st && st[key] === false);
}

function disabledStepsCount(svc) {
  return PIPELINE_STEPS.filter(st => !stepEnabled(svc, st.key)).length;
}

function deriveHosts() {
  const map = new Map();
  for (const s of cfgServices()) {
    const sv = s.server || {};
    const key = (sv.host || '') + ':' + (sv.port || 22) + '@' + (sv.username || '');
    if (!key || key === ':22@') continue;
    if (!map.has(key)) {
      map.set(key, {
        key, host: sv.host || '', port: sv.port || 22, user: sv.username || '',
        auth: sv.password ? '密码' : (sv.privateKeyPath ? '密钥' + (sv.hostKeyFingerprint ? ' + 指纹' : '') : '—'),
        refs: [], services: []
      });
    }
    const h = map.get(key);
    h.services.push(s);
    h.refs.push(s.name);
  }
  return [...map.values()];
}

/* ============================================================
   格式化工具 —— 耗时/时间统一由毫秒计算（契约修正：不再硬编码人读字符串）
   ============================================================ */
function fmtMs(ms) {
  if (ms == null || isNaN(ms)) return '—';
  if (ms < 1000) return ms + 'ms';
  const s = ms / 1000;
  if (s < 60) return (Math.round(s * 10) / 10) + 's';
  const m = Math.floor(s / 60);
  const rs = Math.round(s - m * 60);
  return m + 'm' + String(rs).padStart(2, '0') + 's';
}
function fmtClock(ts) {
  const d = ts instanceof Date ? ts : new Date(ts);
  if (isNaN(d.getTime())) return '—';
  const p = n => String(n).padStart(2, '0');
  return p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes());
}
function fmtDateTime(ts) {
  const d = ts instanceof Date ? ts : new Date(ts);
  if (isNaN(d.getTime())) return '—';
  const p = n => String(n).padStart(2, '0');
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate()) + ' ' + p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
}

/* ============================================================
   路由
   ============================================================ */
const app = {
  view:'orchestration', arg:null,
  tagFilter:[],   // 选中标签集合（多选，并集语义；空数组=全部服务）
  search:'',
  hookUnlockGlobal:false,  // 服务配置页内全局批次钩子的编辑解锁标记（会话级）
  hookUnlockTags:[]        // 已解锁编辑的标签批次钩子名列表（会话级）
};

function parseHash(){
  const raw = (location.hash || '#/orchestration').replace(/^#\/?/,'');
  const p = raw.split('/').filter(Boolean);
  return { view:p[0] || 'orchestration', arg:p[1] ? decodeURIComponent(p[1]) : null };
}
function go(hash){
  if (location.hash === hash) { route(); } else { location.hash = hash; }
}

/* ============================================================
   工具
   ============================================================ */
function esc(s){
  return String(s == null ? '' : s)
    .replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')
    .replace(/"/g,'&quot;').replace(/'/g,'&#39;');
}
function dotOf(st){
  return st === 'ok' ? 'ok' : st === 'warn' ? 'warn' : st === 'err' ? 'err' : 'muted';
}
function stLabel(st){
  return st === 'ok' ? '就绪' : st === 'warn' ? '变更待保存' : st === 'err' ? '失败' : st === 'off' ? '已停用' : '未引用';
}
function stClass(st){
  return st === 'ok' ? 'ok' : st === 'warn' ? 'warn' : st === 'err' ? 'err' : 'dim';
}
let toastTimer = null;
function toast(msg, kind){
  const el = document.getElementById('toast');
  el.textContent = msg;
  el.className = 'toast show' + (kind === 'ok' ? ' ok' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'toast'; }, 2200);
}

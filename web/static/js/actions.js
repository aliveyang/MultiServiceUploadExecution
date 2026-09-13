/* ============================================================
   actions.js —— 交互流程与动作表：保存 / 部署 / 空间 / 密钥库联动 / 主机库 / 服务 / 标签钩子，全部经 API 落到后端
   零构建链原生 JS：由 index.html 按依赖顺序以 <script> 引入，共享全局命名空间
   ============================================================ */
'use strict';

/* ============================================================
   动作表 —— 全部动作经 API 落到后端，不再有纯 toast 假动作
   ============================================================ */
async function saveConfigFlow(){
  if (app.view === 'service') applyServiceForm();
  try {
    await API.saveConfig(state.config, state.activeWs);
    state.dirty = false;
    toast('配置已保存到 deploy.json', 'ok');
    render();
  } catch (e) {
    toast('保存失败：' + e.message);
  }
}

async function startDeployFlow(extra){
  ensureSSE();
  const opts = Object.assign({ workspace: state.activeWs }, extra || {});
  try {
    // 在发起请求前清理上一批次状态，避免与 batch_started 事件产生先后竞态
    if (!state.deploy || state.deploy.status !== 'running') {
      state.deploy = null;
      state.logBuffer = [];
      state.logCount = 0;
    }
    await API.deploy(opts);
    go('#/execution');
  } catch (e) {
    if (e.status === 409) {
      toast('已有部署任务正在运行，已跳转执行监控');
      ensureSSE();
      go('#/execution');
    } else {
      toast('部署触发失败：' + e.message);
    }
  }
}

async function cancelDeployFlow(){
  try {
    await API.cancel();
    toast('已发送中止信号，正在回收远端会话');
  } catch (e) {
    toast(e.status === 400 ? '当前没有运行中的部署任务' : ('中止失败：' + e.message));
  }
}

async function switchSpaceFlow(id){
  if (id === state.activeWs) { toast('已处于该空间'); return; }
  try {
    await API.selectWorkspace(id);
    state.activeWs = id;
    state.config = await API.config();
    state.dirty = false;
    state.historyLoaded = false;
    state.keysLoaded = false;
    toast('已切换到空间 ' + id + '，已重新加载 deploy.json', 'ok');
    render();
  } catch (e) {
    toast(e.status === 409 ? '部署运行中，禁止切换空间' : ('切换失败：' + e.message));
  }
}

async function createSpaceFlow(){
  const id = (prompt('新空间 ID（字母 / 数字 / 中划线 / 下划线）') || '').trim();
  if (!id) return;
  const name = prompt('显示名称（可留空）') || '';
  const from = (prompt('复制自空间 ID（留空=复制当前空间，输入 empty 表示空白空间）') || '').trim();
  try {
    await API.createWorkspace(id, name, from || undefined);
    const ws = await API.workspaces();
    state.workspaces = ws.workspaces || [];
    state.activeWs = ws.active || id;
    state.config = await API.config();
    state.dirty = false;
    toast('空间 ' + id + ' 已创建并切换', 'ok');
    go('#/spaces');
  } catch (e) {
    toast('创建失败：' + e.message);
  }
}

async function deleteSpaceFlow(id){
  if (!confirm('确认删除空间 ' + id + '？该操作不可恢复。')) return;
  try {
    await API.deleteWorkspace(id);
    const ws = await API.workspaces();
    state.workspaces = ws.workspaces || [];
    state.activeWs = ws.active || state.activeWs;
    toast('空间 ' + id + ' 已删除', 'ok');
    render();
  } catch (e) {
    toast('删除失败：' + e.message);
  }
}

/* 导入私钥：本地读取文件内容，可选 passphrase（仅用于解析指纹，不存储） */
function importKeyFlow(){
  const input = document.createElement('input');
  input.type = 'file';
  input.onchange = () => {
    const file = input.files && input.files[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async () => {
      const content = String(reader.result || '');
      const passphrase = prompt('私钥口令（无口令请留空；仅用于解析指纹，不会被存储）');
      if (passphrase === null) return;
      try {
        await API.importKey(file.name, content, passphrase, state.activeWs);
        state.keysLoaded = false;
        toast('私钥 ' + file.name + ' 已导入', 'ok');
        render();
      } catch (e) {
        toast('导入失败：' + e.message);
      }
    };
    reader.readAsText(file);
  };
  input.click();
}

async function deleteKeyFlow(name){
  if (!confirm('确认删除私钥 ' + name + '？')) return;
  try {
    await API.deleteKey(name, state.activeWs);
    state.keysLoaded = false;
    toast('私钥 ' + name + ' 已删除', 'ok');
    render();
  } catch (e) {
    toast('删除失败：' + e.message);
  }
}

/* 新增入库主机：追加 hosts[] 条目并打开库内编辑抽屉 */
function createLibraryHostFlow(){
  if (!state.config) return;
  state.config.hosts = state.config.hosts || [];
  let i = state.config.hosts.length + 1;
  let name = 'host-' + String(i).padStart(2, '0');
  while (state.config.hosts.some(h => h.name === name)) { i++; name = 'host-' + String(i).padStart(2, '0'); }
  state.config.hosts.push({ name, server: { host: '', port: 22, username: '', connectTimeout: 15 } });
  markDirty();
  openLibHostDrawer(name);
}

/* 移出主机库（被 hostRef 引用时拒绝，后端校验同样兜底） */
function deleteLibraryHostFlow(name){
  const referenced = cfgServices().some(s => (s.hostRef || '').toLowerCase() === name.toLowerCase());
  if (referenced) { toast('主机 ' + name + ' 正被服务通过 hostRef 引用，请先解除引用'); return; }
  if (!confirm('确认将主机 ' + name + ' 移出主机库？')) return;
  state.config.hosts = (state.config.hosts || []).filter(h => h.name !== name);
  markDirty();
  toast('主机 ' + name + ' 已移出主机库，点击「保存配置」落盘', 'ok');
  render();
}

function newServiceFlow(){
  const list = cfgServices();
  const base = JSON.parse(JSON.stringify(list[0] || null));
  if (!base) { toast('请先在 deploy.json 中配置首个服务'); return; }
  let i = 1;
  while (list.some(s => s.name === base.name + '-copy' + (i > 1 ? i : ''))) i++;
  base.name = base.name + '-copy' + (i > 1 ? i : '');
  state.config.services.push(base);
  markDirty();
  toast('已复制 ' + list[0].name + ' → ' + base.name + '，保存前请修改主机与服务名', 'ok');
  go('#/services/' + encodeURIComponent(base.name));
}

function deleteServiceFlow(){
  const d = currentService();
  if (!d) return;
  if (!confirm('确认删除服务 ' + d.name + '？保存配置后生效。')) return;
  state.config.services = state.config.services.filter(s => s !== d);
  markDirty();
  toast('服务 ' + d.name + ' 已移除，点击「保存更改」落盘', 'ok');
  go('#/orchestration');
}

/* 新增标签钩子：以标签名创建 tagHooks 条目并绑定批次前后置钩子 */
function addTagHookFlow(){
  const name = (prompt('新标签名（为携带该标签的服务绑定批次前后置钩子）') || '').trim().toLowerCase();
  if (!name) return;
  if (findTagHookEntry(name)) { toast('标签 "' + name + '" 已绑定批次钩子'); return; }
  state.config.tagHooks = (state.config && state.config.tagHooks) || [];
  state.config.tagHooks.push({ name: name, description: '', hooks: { preDeploy: [], postDeploy: [] } });
  markDirty();
  toast('标签钩子 "' + name + '" 已创建，点击「保存更改」落盘', 'ok');
  render();
}

const ACTIONS = {
  'save':            () => saveConfigFlow(),
  'save-settings':   () => saveSettingsFlow(),
  'deploy':          () => startDeployFlow(app.tagFilter.length ? { tags: app.tagFilter.slice() } : {}),
  'deploy-service':  () => { const d = currentService(); startDeployFlow(d ? { targetServices: [d.name] } : {}); },
  'cancel':          () => cancelDeployFlow(),
  'replay':          () => { const rec = state.batch; startDeployFlow(rec && rec.tags && rec.tags.length ? { tags: rec.tags.slice() } : {}); },
  'add-tag-hook':    () => addTagHookFlow(),
  'export-report':   () => { if (app.view === 'history') exportAllHistory(); else exportBatchReport(); },
  'compare':         () => compareWithPrev(),
  'new-service':     () => newServiceFlow(),
  'new-host':        () => createLibraryHostFlow(),
  'new-key':         () => importKeyFlow(),
  'save-key-lib':    () => saveKeyFromPathFlow(),
  'save-host-lib':   () => saveServiceHostToLibrary(),
  'refresh-history': () => { state.historyLoaded = false; render(); },
  'retry-init':      () => bootstrap(),
  'delete-service':  () => deleteServiceFlow(),
  'duplicate':       () => newServiceFlow(),
  'close-drawer':    closeDrawer,
  'save-drawer':     () => {
    if (applyDrawer()) {
      closeDrawer();
      toast(drawerCtx && drawerCtx.mode === 'library'
        ? '主机库已更新，点击「保存配置」落盘'
        : '主机配置已回写引用服务，点击「保存配置」落盘', 'ok');
    }
  },
  'test-conn':       () => testConnFlow(),
  'pick-local':      async () => {
    try {
      const res = await API.pickPath('folder');
      if (res && res.path) { const el = document.getElementById('fLocalPath'); if (el) el.value = res.path; }
      else if (res && res.status === 'canceled') toast('已取消选择');
    } catch (e) { toast('路径选择失败：' + e.message); }
  },
  'add-exclude':     () => {
    const d = currentService();
    if (!d) return;
    const rule = (prompt('新增排除规则（支持通配符，如 *.log）') || '').trim();
    if (!rule) return;
    d.upload = d.upload || {};
    d.upload.exclude = d.upload.exclude || [];
    d.upload.exclude.push(rule);
    markDirty();
    render();
  },
  'manage-space':    () => { toggleSpaceMenu(false); go('#/spaces'); },
  'new-space':       () => { toggleSpaceMenu(false); createSpaceFlow(); }
};

/* ============================================================
   密钥库联动 —— 表单中的私钥路径可选择库内密钥，也可将本地路径一键入库
   ============================================================ */
function keyStoreBaseName(p){ return String(p || '').split(/[\\/]/).pop() || ''; }

/* 密钥库条目的引用路径（部署时 deployer 以控制机进程工作目录解析相对路径） */
function keyStorePathOf(name){
  const dir = (state.keysDir || ('workspaces/' + (state.activeWs || 'default') + '/keys')).replace(/[\\/]+$/, '');
  return dir + '/' + name;
}

/* 密钥库下拉选项：值 = 库内引用路径，标签 = 名称与算法摘要 */
function keyStoreOptions(){
  return (state.keys || []).map(k =>
    `<option value="${esc(keyStorePathOf(k.name))}">${esc(k.name)}（${esc(k.algorithm || 'unknown')}${k.encrypted ? ' · 已加密' : ''}）</option>`).join('');
}

/* 主机库下拉选项：与「主机配置库」页面同口径 = hosts[] 显式条目 + 服务内联派生主机。
   库条目值为 hostRef 名称（选中即建立引用回填）；内联主机值为 __inline__N 令牌，
   经 inlineHostPickMap 命中后仅回填连接配置（无库条目可引用）。 */
let inlineHostPickMap = {};
function hostStoreOptions(){
  const lib = (state.config && state.config.hosts) || [];
  const opts = lib.map(h =>
    `<option value="${esc(h.name)}">${esc(h.name)}${h.server && h.server.host ? `（${esc(h.server.username || '')}@${esc(h.server.host)}）` : ''}</option>`);
  inlineHostPickMap = {};
  deriveHosts().forEach((h, i) => {
    // 已被库条目覆盖的 host:port@user 不重复列出
    const dup = lib.some(x => x.server && (x.server.host + ':' + (x.server.port || 22) + '@' + x.server.username) === h.key);
    if (dup) return;
    const v = '__inline__' + i;
    inlineHostPickMap[v] = h;
    opts.push(`<option value="${esc(v)}">${esc((h.user ? h.user + '@' : '') + h.host + ':' + h.port)}（内联 · ${h.refs.length} 个服务引用）</option>`);
  });
  return opts.join('');
}

/* 将表单中手填的本地私钥路径读取入库，成功后把字段切换为库内引用路径 */
async function saveKeyFromPathFlow(){
  const el = document.getElementById('dKeyPath') || document.getElementById('fKeyPath');
  const p = el ? (el.value || '').trim() : '';
  if (!p) { toast('请先填写本地私钥文件路径'); return; }
  try {
    await API.importKeyPath(p, '', undefined, state.activeWs);
    await loadKeys();
    const storePath = keyStorePathOf(keyStoreBaseName(p));
    if (el) el.value = storePath;
    if (el && el.id === 'fKeyPath') {
      const d = currentService();
      if (d) { d.server = d.server || {}; d.server.privateKeyPath = storePath; }
    }
    markDirty();
    toast('私钥已存入 SSH 密钥库：' + storePath, 'ok');
  } catch (e) {
    toast('存入密钥库失败：' + e.message);
  }
}

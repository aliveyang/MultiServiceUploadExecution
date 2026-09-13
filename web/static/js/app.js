/* ============================================================
   app.js —— 路由、全局监听（Esc / Ctrl+S / 哈希）与启动引导 bootstrap
   零构建链原生 JS：由 index.html 按依赖顺序以 <script> 引入，共享全局命名空间
   ============================================================ */
'use strict';

/* ============================================================
   路由
   ============================================================ */
function route(){
  const p = parseHash();
  let view = p.view;

  if (view === 'services') view = 'service';
  if (view === 'history' && p.arg) view = 'batch';
  if (!['orchestration','execution','hosts','keys','spaces','service','hooks','history','batch','settings'].includes(view)) {
    view = 'orchestration';
  }

  /* 先清理上一层视图留下的浮层状态，再写入新的 view/arg —— 顺序颠倒会把刚解析出的深链清掉 */
  closeDrawer();
  toggleSpaceMenu(false);

  app.view = view;
  /* 直接进入详情路由但缺 id 时回落到首条记录，避免出现「批次 #」这种空面包屑 */
  if (view === 'batch')        app.arg = p.arg || (state.history[0] && state.history[0].id) || null;
  else if (view === 'service') app.arg = p.arg || (cfgServices()[0] && cfgServices()[0].name) || null;
  else if (view === 'hosts')   app.arg = p.arg || null;   /* #/hosts/<hostKey> 深链直达编辑抽屉 */
  else                         app.arg = null;

  const c = document.getElementById('content');
  if (c) c.scrollTop = 0;

  render();
}

/* 点击空白处关闭浮层 */
document.addEventListener('click', e => {
  if (!e.target.closest('.space-switch')) toggleSpaceMenu(false);
});
document.getElementById('backdrop').onclick = closeDrawer;
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') { closeDrawer(); toggleSpaceMenu(false); }
});

/* Ctrl/Cmd+S 保存配置更改（拦截浏览器默认保存行为） */
document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && String(e.key).toLowerCase() === 's') {
    e.preventDefault();
    if (state.ready) saveConfigFlow();
  }
});

window.addEventListener('hashchange', route);

/* ============================================================
   启动引导：拉取工作空间与配置 → 首次渲染 → 建立 SSE → 预热指标
   ============================================================ */
async function bootstrap(){
  try {
    const ws = await API.workspaces();
    state.workspaces = ws.workspaces || [];
    state.activeWs = ws.active || 'default';
    state.config = await API.config(state.activeWs);
    state.ready = true;
  } catch (e) {
    state.ready = false;
  }
  route();
  if (state.ready) {
    ensureSSE();
    loadHistory().then(() => { if (app.view === 'orchestration') renderStatus(); });
    API.settings().then(st => { state.settings = st; state.settingsLoaded = true; renderStatus(); }).catch(() => {});
    loadKeys().then(() => {
      // 密钥库晚于页面加载完成时，刷新相关视图的库选择下拉（焦点在输入框时跳过，避免覆盖输入）
      if (['service','hosts'].includes(app.view) && !['INPUT','SELECT','TEXTAREA'].includes(document.activeElement.tagName)) render();
    });   // 预热密钥库列表，供服务表单与主机抽屉的私钥路径选择
  }
}

if (!location.hash) {
  location.replace(location.pathname + '#/orchestration');
}
bootstrap();

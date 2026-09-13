/* ============================================================
   bind.js —— DOM 绑定层：事件绑定、库选择下拉、凭据掩码切换、行内命令输入、主机回填、本地过滤与浮层开关
   零构建链原生 JS：由 index.html 按依赖顺序以 <script> 引入，共享全局命名空间
   ============================================================ */
'use strict';

function bind(){
  const c = document.getElementById('content');

  /* 搜索框（编排台）—— 行级过滤，不重渲染避免丢焦点 */
  const search = document.getElementById('svcSearch');
  if (search) {
    search.oninput = () => {
      app.search = search.value;
      filterTableRows(app.search);
    };
    /* 重渲染后恢复输入框值对应的过滤状态 */
    if (app.search) filterTableRows(app.search);
  }

  /* 标签下拉（编排台）—— 侧栏折叠后接管筛选（单选快捷入口） */
  const grpSel = document.getElementById('grpSel');
  if (grpSel) {
    grpSel.onchange = () => {
      app.tagFilter = grpSel.value ? [grpSel.value] : [];
      render();
    };
  }

  /* 类型筛选下拉（编排台） */
  const typeSel = document.getElementById('typeSel');
  if (typeSel) {
    typeSel.onchange = () => { app.typeFilter = typeSel.value; render(); };
  }

  /* 自动打开浏览器开关（设置页，内存态，保存时统一收集） */
  const swAuto = document.getElementById('swAutoOpen');
  if (swAuto) swAuto.onclick = () => {
    const on = !swAuto.classList.contains('on');
    swAuto.classList.toggle('on', on);
    swAuto.setAttribute('aria-checked', String(on));
  };

  /* 服务名 → 服务配置页 */
  c.querySelectorAll('[data-svc]').forEach(el => {
    el.onclick = () => go('#/services/' + encodeURIComponent(el.dataset.svc));
  });

  /* 表格行 → 批次详情 */
  c.querySelectorAll('[data-batch]').forEach(el => {
    el.onclick = () => go('#/history/' + encodeURIComponent(el.dataset.batch));
  });

  /* 表格行 → 主机编辑抽屉（走哈希，保持可深链） */
  c.querySelectorAll('[data-host]').forEach(el => {
    el.onclick = () => go('#/hosts/' + encodeURIComponent(el.dataset.host));
  });

  /* 内部跳转 */
  c.querySelectorAll('[data-go]').forEach(el => {
    el.onclick = () => go(el.dataset.go);
  });
  document.getElementById('topbar').querySelectorAll('[data-go]').forEach(el => {
    el.onclick = () => go(el.dataset.go);
  });

  /* 单行「部署」 */
  c.querySelectorAll('[data-deploy-svc]').forEach(el => {
    el.onclick = ev => { ev.stopPropagation(); startDeployFlow({ targetServices: [el.dataset.deploySvc] }); };
  });

  /* 空间卡片：切换 / 删除 */
  c.querySelectorAll('.space-card').forEach(el => {
    el.onclick = () => switchSpaceFlow(el.dataset.space);
  });
  c.querySelectorAll('[data-del-space]').forEach(el => {
    el.onclick = ev => { ev.stopPropagation(); deleteSpaceFlow(el.dataset.delSpace); };
  });

  /* 主机库行 → 库内编辑抽屉；移出主机库 */
  c.querySelectorAll('[data-lib-host]').forEach(el => {
    el.onclick = () => openLibHostDrawer(el.dataset.libHost);
  });
  c.querySelectorAll('[data-del-lib-host]').forEach(el => {
    el.onclick = ev => { ev.stopPropagation(); deleteLibraryHostFlow(el.dataset.delLibHost); };
  });

  /* 密钥删除 */
  c.querySelectorAll('[data-del-key]').forEach(el => {
    el.onclick = ev => { ev.stopPropagation(); deleteKeyFlow(el.dataset.delKey); };
  });

  /* 任务类型 / 启用状态分段（服务配置页，内存态 + 脏标记，保存时统一收集） */
  c.querySelectorAll('[data-svc-type]').forEach(el => {
    el.onclick = () => {
      c.querySelectorAll('[data-svc-type]').forEach(b => b.classList.remove('on'));
      el.classList.add('on');
      markDirty();
    };
  });
  c.querySelectorAll('#fEnabled [data-on]').forEach(el => {
    el.onclick = () => {
      c.querySelectorAll('#fEnabled [data-on]').forEach(b => b.classList.remove('on'));
      el.classList.add('on');
      markDirty();
    };
  });

  /* 排除规则删除 */
  c.querySelectorAll('[data-del-exclude]').forEach(el => {
    el.onclick = () => {
      const d = currentService();
      if (!d || !d.upload || !d.upload.exclude) return;
      d.upload.exclude = d.upload.exclude.filter(x => x !== el.dataset.delExclude);
      markDirty();
      render();
    };
  });

  /* 标签芯片删除（服务配置页）：移除后空列表回退默认标签 default */
  c.querySelectorAll('[data-del-tag]').forEach(el => {
    el.onclick = () => {
      const d = currentService();
      if (!d) return;
      d.tags = (d.tags || []).filter(t => t !== el.dataset.delTag);
      markDirty();
      render();
    };
  });

  /* 标签输入（服务配置页）：回车 / 逗号 / 失焦添加，自动小写去重 */
  const tagInput = document.getElementById('fTagInput');
  if (tagInput) {
    const addTag = () => {
      const d = currentService();
      if (!d) return;
      const t = (tagInput.value || '').trim().toLowerCase();
      if (!t) return;
      d.tags = d.tags || [];
      if (d.tags.indexOf(t) < 0) d.tags.push(t);
      markDirty();
      render();
      const el = document.getElementById('fTagInput');
      if (el) el.focus();
    };
    tagInput.onkeydown = ev => {
      if (ev.key === 'Enter' || ev.key === ',') { ev.preventDefault(); addTag(); }
    };
    tagInput.onchange = addTag;
  }

  /* 清理远端目录开关 */
  const sw = document.getElementById('swClean');
  if (sw) sw.onclick = () => {
    const d = currentService();
    if (!d) return;
    d.upload = d.upload || {};
    d.upload.cleanRemote = !d.upload.cleanRemote;
    markDirty();
    render();
  };

  /* 标签钩子命令删除 / 描述编辑 / 条目删除（批次钩子页；命令新增走行内输入框） */
  c.querySelectorAll('[data-tag-cmd-del]').forEach(el => {
    el.onclick = () => {
      const th = findTagHookEntry(el.dataset.tagCmdDel);
      const key = el.dataset.cmdKey;
      const idx = parseInt(el.dataset.cmdIdx, 10);
      if (th && th.hooks && th.hooks[key] && th.hooks[key][idx] != null) {
        th.hooks[key].splice(idx, 1);
        markDirty();
        render();
      }
    };
  });
  c.querySelectorAll('[data-tag-desc]').forEach(el => {
    el.oninput = () => {
      const th = findTagHookEntry(el.dataset.tagDesc);
      if (!th) return;
      th.description = el.value;
      markDirty();
    };
  });
  c.querySelectorAll('[data-del-tag-hook]').forEach(el => {
    el.onclick = () => {
      const name = String(el.dataset.delTagHook || '').trim().toLowerCase();
      if (!confirm('确认删除标签 "' + name + '" 的批次钩子？保存配置后生效。')) return;
      state.config.tagHooks = ((state.config && state.config.tagHooks) || [])
        .filter(th => String(th.name || '').trim().toLowerCase() !== name);
      markDirty();
      render();
    };
  });

  /* 钩子命令行内输入（全局 / 标签 / 服务级三个作用域统一处理：回车即添加一条命令） */
  c.querySelectorAll('[data-cmd-scope]').forEach(el => {
    el.onkeydown = ev => {
      if (ev.key !== 'Enter') return;
      ev.preventDefault();
      const cmd = (el.value || '').trim();
      if (!cmd) return;
      const key = el.dataset.cmdKey;
      const scope = el.dataset.cmdScope;
      if (scope === 'hooks') {
        state.config.hooks = state.config.hooks || {};
        state.config.hooks[key] = state.config.hooks[key] || [];
        state.config.hooks[key].push(cmd);
      } else if (scope === 'tagHooks') {
        const th = findTagHookEntry(el.dataset.cmdTag);
        if (!th) return;
        th.hooks = th.hooks || {};
        th.hooks[key] = th.hooks[key] || [];
        th.hooks[key].push(cmd);
      } else {
        const d = currentService();
        if (!d) return;
        d.hooks = d.hooks || {};
        d.hooks[key] = d.hooks[key] || [];
        d.hooks[key].push(cmd);
      }
      markDirty();
      // 重渲染后把焦点还给当前输入框，支持连续回车录入多条命令
      window.__focusCmd = el.id;
      render();
    };
  });
  c.querySelectorAll('[data-del-hook]').forEach(el => {
    el.onclick = () => {
      const parts = el.dataset.delHook.split(':');
      const key = parts[0], idx = parseInt(parts[1], 10);
      const hooks = state.config && state.config.hooks;
      if (hooks && hooks[key] && hooks[key][idx] != null) {
        hooks[key].splice(idx, 1);
        markDirty();
        render();
      }
    };
  });

  /* 服务级四阶段钩子命令删除（服务配置页，内存态 + 脏标记，保存时随整表落盘） */
  c.querySelectorAll('[data-del-svc-hook]').forEach(el => {
    el.onclick = () => {
      const d = currentService();
      if (!d) return;
      const parts = el.dataset.delSvcHook.split(':');
      const key = parts[0], idx = parseInt(parts[1], 10);
      if (d.hooks && d.hooks[key] && d.hooks[key][idx] != null) {
        d.hooks[key].splice(idx, 1);
        markDirty();
        render();
      }
    };
  });

  /* 各视图本地搜索（纯前端过滤） */
  const bindLocalSearch = (id, fn) => {
    const el = document.getElementById(id);
    if (el) el.oninput = () => fn(el.value);
  };
  /* 各视图本地搜索（纯前端过滤，不重渲染避免丢焦点） */
  bindLocalSearch('hostSearch', v => filterTableRows(v));
  bindLocalSearch('keySearch', v => filterTableRows(v));
  bindLocalSearch('spaceSearch', v => filterSpaceCards(v));
  bindLocalSearch('histSearch', v => filterTableRows(v));

  /* 动作按钮 */
  c.querySelectorAll('[data-act]').forEach(el => {
    const fn = ACTIONS[el.dataset.act];
    if (fn) el.onclick = fn;
  });

  /* 凭据掩码切换（不使用 type=password，浏览器密码插件不会探测这些字段） */
  bindMaskToggles(c);

  /* 库选择下拉（SSH 密钥库 / 主机库）：选中后回填对应输入框 */
  c.querySelectorAll('[data-pick-key]').forEach(el => {
    el.onchange = () => {
      const v = el.value;
      el.selectedIndex = 0;
      if (!v) return;
      const target = document.getElementById(el.dataset.pickKey);
      if (target) target.value = v;
      if (el.dataset.pickKey === 'fKeyPath') {
        const d = currentService();
        if (d) { d.server = d.server || {}; d.server.privateKeyPath = v; }
      }
      markDirty();
    };
  });
  c.querySelectorAll('[data-pick-host]').forEach(el => {
    el.onchange = () => {
      const v = el.value;
      el.selectedIndex = 0;
      if (!v) return;
      // 内联派生主机（__inline__N 令牌）：仅回填连接配置，无库条目可引用
      if (inlineHostPickMap[v]) {
        fillServiceFromInlineHost(inlineHostPickMap[v]);
        return;
      }
      const target = document.getElementById(el.dataset.pickHost);
      if (target) target.value = v;
      fillServiceFromLibrary(v);
    };
  });

  /* 服务表单：hostRef 选中库内主机时回填其连接配置（凭据以库为准） */
  const hostRefEl = document.getElementById('fHostRef');
  if (hostRefEl) {
    hostRefEl.onchange = () => fillServiceFromLibrary(hostRefEl.value);
  }

  /* 抽屉动作按钮在其打开时由 bindDrawerActions() 绑定（内容为动态注入） */

  /* 顶栏动作 */
  document.getElementById('topbar').querySelectorAll('[data-act]').forEach(el => {
    const fn = ACTIONS[el.dataset.act];
    if (fn) el.onclick = fn;
  });

  /* 重渲染后恢复钩子命令输入框焦点（支持连续回车录入多条命令） */
  if (window.__focusCmd) {
    const el = document.getElementById(window.__focusCmd);
    if (el) el.focus();
    window.__focusCmd = null;
  }
}

/* 凭据显示/隐藏切换按钮（作用于同容器内的 masked 类切换） */
function bindMaskToggles(root){
  root.querySelectorAll('[data-mask-toggle]').forEach(el => {
    el.onclick = () => {
      const t = document.getElementById(el.dataset.maskToggle);
      if (!t) return;
      const masked = t.classList.toggle('masked');
      el.textContent = masked ? '显示' : '隐藏';
    };
  });
}

/* hostRef 命中主机库条目时回填连接配置；凭据字段清空以库值生效（后端内联空值回退库值） */
function fillServiceFromLibrary(name){
  const key = String(name || '').trim().toLowerCase();
  const h = ((state.config && state.config.hosts) || []).find(x => String(x.name || '').toLowerCase() === key);
  if (!h || !h.server) return;
  const d = currentService();
  if (!d) return;
  const set = (id, v) => { const el = document.getElementById(id); if (el) el.value = v; };
  set('fHost', h.server.host || '');
  set('fPort', h.server.port || 22);
  set('fUsername', h.server.username || '');
  set('fKeyPath', h.server.privateKeyPath || '');
  set('fFingerprint', h.server.hostKeyFingerprint || '');
  set('fTimeout', h.server.connectTimeout || 15);
  set('fPassword', '');
  set('fPassphrase', '');
  // 凭据改为跟随库配置：清空服务内联凭据，后端对空内联值自动回退库值
  d.server = d.server || {};
  d.server.password = '';
  d.server.passphrase = '';
  markDirty();
  toast('已回填主机库配置「' + h.name + '」' + (h.server.password ? '（凭据使用库内配置）' : ''), 'ok');
}

/* 选中服务内联派生主机：回填其连接配置（不建立 hostRef，凭据保持本服务原值；需要复用可「存入主机库」） */
function fillServiceFromInlineHost(h){
  const d = currentService();
  if (!d) return;
  const ref = (h.services && h.services[0]) || {};
  const sv = ref.server || {};
  const set = (id, v) => { const el = document.getElementById(id); if (el) el.value = v; };
  set('fHost', sv.host || h.host || '');
  set('fPort', sv.port || h.port || 22);
  set('fUsername', sv.username || h.user || '');
  set('fKeyPath', sv.privateKeyPath || '');
  set('fFingerprint', sv.hostKeyFingerprint || '');
  set('fTimeout', sv.connectTimeout || 15);
  set('fHostRef', '');
  markDirty();
  toast('已回填内联主机 ' + (h.user ? h.user + '@' : '') + h.host + ':' + h.port + '；需要复用可「存入主机库」', 'ok');
}

/* 服务表单「存入主机库」：以当前内联连接配置创建 hosts[] 条目并建立 hostRef 引用 */
function saveServiceHostToLibrary(){
  const d = currentService();
  if (!d) return;
  applyServiceForm();
  const sv = d.server || {};
  if (!sv.host || !sv.username) { toast('请先填写主机地址与登录用户'); return; }
  if (!sv.password && !sv.privateKeyPath) { toast('主机库条目需要密码或私钥（当前服务缺少凭据）'); return; }
  state.config.hosts = (state.config && state.config.hosts) || [];
  let name = (sv.username + '@' + sv.host).replace(/\s+/g, '');
  let i = 2;
  while (((state.config.hosts).some(h => String(h.name || '').toLowerCase() === name.toLowerCase()))) {
    name = (sv.username + '@' + sv.host).replace(/\s+/g, '') + '-' + i;
    i++;
  }
  state.config.hosts.push({ name: name, server: JSON.parse(JSON.stringify(sv)) });
  d.hostRef = name;
  markDirty();
  toast('已存入主机库「' + name + '」，点击「保存更改」落盘', 'ok');
  render();
}

/* 表格本地过滤：隐藏不匹配的行（不重渲染，避免丢焦点） */
function filterTableRows(q, col){
  const kw = (q || '').toLowerCase();
  document.querySelectorAll('#content .tbl-row').forEach(row => {
    if (row.querySelector('.tbl-head')) return;
    const txt = row.textContent.toLowerCase();
    row.style.display = (!kw || txt.includes(kw)) ? '' : 'none';
  });
}
function filterSpaceCards(q){
  const kw = (q || '').toLowerCase();
  document.querySelectorAll('.space-card').forEach(card => {
    card.style.display = (!kw || card.textContent.toLowerCase().includes(kw)) ? '' : 'none';
  });
}

function toggleSpaceMenu(force){
  const m = document.getElementById('spaceMenu');
  if (!m) return;
  const on = typeof force === 'boolean' ? force : !m.classList.contains('show');
  m.classList.toggle('show', on);
}

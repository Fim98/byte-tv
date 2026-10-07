import { $, element, button, sourceLabel, normalizeSource, readPreference, savePreference, populateIcons } from './ui-core.js';

export function createShell(app) {
  const main = $('workspaceMain');
  const positions = {library: 0, following: 0, downloads: 0, users: 0};
  let current = '', sourceStates = {}, sourceLoading = false, sourcePhase = '', sourceRequest = '';
  let sourceNotice;
  const sourceRows = new Map(), sourceErrors = new Map();

  function setPage(page) {
    if (!app.viewer && page === 'users') return;
    if (app.viewer && (page === 'users' && !app.viewer.account?.admin || page === 'downloads' && app.viewer.onlineOnly)) {
      window.JukuDialogs.navigate('library');
      page = 'library';
    }
    if (page === current) return;
    if (current) positions[current] = main.scrollTop;
    current = page;
    document.body.dataset.page = page;
    document.querySelectorAll('[data-view]').forEach(view => view.hidden = view.dataset.view !== page);
    document.querySelectorAll('button[data-page]').forEach(control => {
      if (control.dataset.page === page) control.setAttribute('aria-current', 'page');
      else control.removeAttribute('aria-current');
    });
    if (page === 'users') app.users?.load();
    if (page === 'following') app.following?.render();
    if (page === 'downloads') app.downloads?.render();
    if (page === 'library') app.library?.layout();
    requestAnimationFrame(() => {main.scrollTop = positions[page] || 0;});
  }

  function activity(visible) {
    visible = visible && !app.viewer?.onlineOnly;
    $('activityPanel').hidden = !visible;
    $('catalogLayout').classList.toggle('with-activity', visible);
    $('activityToggle').setAttribute('aria-pressed', String(visible));
    $('activityToggleLabel').textContent = visible ? '收起任务' : '任务侧栏';
    if (!app.viewer?.onlineOnly) savePreference('activitySidebar', visible);
    if (visible) app.downloads?.render();
  }

  function info(title, build) {
    sourceRows.clear();
    sourceNotice = null;
    $('infoTitle').textContent = title;
    $('infoContent').replaceChildren();
    if (typeof build === 'function') build($('infoContent'));
    else $('infoContent').appendChild(element('p', '', build));
    window.JukuDialogs.open('infoPanel');
  }

  function updateSourceRows() {
    const statuses = {loading: '正在获取', canceled: '已停止', ready: '已就绪', success: '已更新', done: '已更新', failed: '暂不可用', error: '暂不可用', partial: '部分更新', cached: '使用缓存', pending: '等待更新'};
    for (const [id, controls] of sourceRows) {
      const state = sourceStates[id];
      const allowed = !app.viewer?.sources || app.viewer.sources.includes(normalizeSource(id));
      const busy = sourceRequest === id || sourceLoading && state?.status === 'loading';
      const stopping = sourceRequest === 'cancel';
      controls.row.hidden = !allowed;
      controls.status.textContent = sourceRequest === id ? '正在提交' : state ? statuses[state.status] || (state.error ? '暂不可用' : '已有缓存') : '未检查';
      const date = state?.updatedAt && !state.updatedAt.startsWith('0001') ? new Date(state.updatedAt).toLocaleString() : '';
      controls.detail.textContent = [Number.isFinite(state?.count) ? state.count + ' 部' : '', date].filter(Boolean).join(' · ');
      controls.error.textContent = sourceErrors.get(id) || state?.error || state?.phase || (busy ? sourcePhase : '') || '';
      controls.action.disabled = !allowed || stopping || Boolean(sourceRequest) || sourceLoading && !busy;
      controls.action.textContent = busy ? '停止' : '更新';
      controls.action.setAttribute('aria-busy', String(busy));
      controls.action.title = busy ? '停止当前剧库更新，已入库内容会保留，稍后可继续更新' : sourceLoading || sourceRequest ? '正在更新其他站源，完成后可再次更新' : '只更新' + controls.label + '的目录和资料';
    }
    if (sourceNotice) sourceNotice.textContent = sourceLoading || sourceRequest ? '正在更新剧库' + (sourcePhase ? '：' + sourcePhase : '') + '，已有内容可继续使用；可停止后稍后继续。' : '点击对应站源的“更新”可查新、续载和补齐资料，失败时保留已有内容。';
  }

  async function cancelSourceUpdate(id) {
    if (sourceRequest || !sourceLoading) return;
    sourceRequest = 'cancel';
    updateSourceRows();
    try {
      const result = await app.api('/api/ui/dramas/cancel', {method: 'POST', body: '{}'});
      sourceStates = result.sources || sourceStates;
      sourceLoading = Boolean(result.loading);
      sourcePhase = result.loadingPhase || '';
      if (!result.canceled) sourceErrors.set(id, '当前没有正在执行的更新。');
    } catch (error) {
      sourceErrors.set(id, '停止失败：' + error.message);
    } finally {
      sourceRequest = '';
      updateSourceRows();
      void app.library.refresh();
    }
  }

  async function updateSource(id) {
    if (sourceRequest || app.viewer?.sources && !app.viewer.sources.includes(normalizeSource(id))) return;
    if (sourceLoading) {await cancelSourceUpdate(id); return;}
    sourceRequest = id;
    sourceErrors.delete(id);
    updateSourceRows();
    try {
      const result = await app.api('/api/ui/dramas?update=1&source=' + encodeURIComponent(id));
      sourceStates = result.sources || {};
      sourceLoading = Boolean(result.loading);
      sourcePhase = result.loadingPhase || '';
      if (result.updateAccepted === false) sourceErrors.set(id, '本次未提交：已有更新任务，可在完成后重试。');
    } catch (error) {
      sourceErrors.set(id, '更新请求失败：' + error.message);
    } finally {
      sourceRequest = '';
      updateSourceRows();
      void app.library.refresh();
    }
  }

  function showSources() {
    const names = {hongguo: '红果'};
    info('站源状态', content => {
      for (const [id, label] of Object.entries(names)) {
        if (app.viewer?.sources && !app.viewer.sources.includes(normalizeSource(id))) continue;
        const row = element('section', 'source-status-row');
        row.dataset.source = id;
        const heading = element('div', 'heading source-status-heading');
        const actions = element('div', 'source-status-actions');
        const status = element('span', 'tag');
        status.setAttribute('aria-live', 'polite');
        const action = button('更新', () => updateSource(id), false, 'secondary source-update');
        action.setAttribute('aria-label', '更新' + label);
        actions.append(status, action);
        heading.append(element('h3', '', label), actions);
        const detail = element('p', 'small'), error = element('p', 'error');
        error.setAttribute('role', 'status');
        row.append(heading, detail, error);
        sourceRows.set(id, {row, label, status, action, detail, error});
        content.appendChild(row);
      }
      sourceNotice = element('p', 'small notice');
      content.appendChild(sourceNotice);
      updateSourceRows();
    });
    void app.library.refresh();
  }

  function positionMenu(menu) {
    const popover = menu.querySelector('.action-popover');
    if (!menu.open || !popover) return;
    popover.style.maxHeight = '';
    const anchor = menu.getBoundingClientRect();
    const viewport = main.getBoundingClientRect();
    const width = popover.getBoundingClientRect().width;
    const left = Math.max(12, Math.min(anchor.left, window.innerWidth - width - 12));
    const below = Math.max(0, Math.min(window.innerHeight, viewport.bottom) - anchor.bottom - 12);
    const above = Math.max(0, anchor.top - Math.max(0, viewport.top) - 12);
    const height = popover.scrollHeight + 2;
    const upward = height > below && above > below;
    popover.style.left = left - anchor.left + 'px';
    popover.style.right = 'auto';
    popover.style.maxHeight = Math.max(80, upward ? above : below) + 'px';
    popover.style.overflowY = 'auto';
    popover.style.top = upward ? -(Math.min(height, above) + 6) + 'px' : anchor.height + 6 + 'px';
  }

  function access() {
    updateSourceRows();
    document.body.classList.toggle('online-only', Boolean(app.viewer?.onlineOnly));
    document.querySelectorAll('[data-admin-only]').forEach(control => {control.hidden = !app.viewer?.account?.admin;});
    document.querySelectorAll('button[data-page=downloads], [data-go=downloads]').forEach(control => {control.hidden = Boolean(app.viewer?.onlineOnly);});
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.JukuDialogs.page());
  }

  function init() {
    populateIcons();
    window.JukuDialogs.listen(setPage);
    document.querySelectorAll('button[data-page], [data-go]').forEach(control => control.addEventListener('click', () => {
      if (control.dataset.go === 'following') app.following.showTab('watching');
      window.JukuDialogs.navigate(control.dataset.page || control.dataset.go);
    }));
    document.querySelector('.brand').addEventListener('click', event => {event.preventDefault(); window.JukuDialogs.navigate('library');});
    document.querySelector('.skip-link').addEventListener('click', event => {event.preventDefault(); main.focus();});
    $('headerMenu').addEventListener('click', event => {
      if (event.target.closest('button')) {
        $('headerMenu').open = false;
        $('moreButton').focus({preventScroll: true});
      }
    }, true);
    $('headerMenu').addEventListener('toggle', () => $('moreButton').setAttribute('aria-expanded', String($('headerMenu').open)));
    $('menuHistoryBtn').addEventListener('click', () => $('openHistoryBtn').click());
    $('sourceStatusBtn').addEventListener('click', showSources);
    $('aboutBtn').addEventListener('click', () => info('使用提示', content => {
      for (const text of ['输入关键词先筛选本地剧库，点击“联网搜索”补充红果结果。', '更新剧库会查新、继续加载目录，并在后台补齐历史资料。', '登录账号后，观看进度和追剧清单可跨设备接续；允许匿名访问时，匿名记录仍按浏览器独立保存。', '下载保存到运行果果剧库的电脑；手机浏览器不会把下载任务保存到手机相册。']) content.appendChild(element('p', 'notice', text));
    }));
    $('activityToggle').addEventListener('click', () => activity($('activityPanel').hidden));
    $('closeActivityBtn').addEventListener('click', () => activity(false));
    $('downloadLocationBtn').addEventListener('click', () => {
      window.JukuDialogs.open('settingsPanel');
      $('downloadDirectory').focus();
    });
    document.addEventListener('click', event => {
      for (const menu of document.querySelectorAll('.action-menu[open]')) {
        if (!menu.contains(event.target) || event.target.closest('button')) menu.open = false;
      }
    });
    document.addEventListener('toggle', event => {
      if (event.target.matches?.('.action-menu')) positionMenu(event.target);
    }, true);
    document.addEventListener('keydown', event => {
      if (event.key !== 'Escape' || document.querySelector('dialog[open]')) return;
      const menu = document.querySelector('.action-menu[open]');
      if (!menu) return;
      event.preventDefault();
      menu.open = false;
      menu.querySelector('summary').focus({preventScroll: true});
    });
    const positionMenus = () => document.querySelectorAll('.action-menu[open]').forEach(positionMenu);
    window.addEventListener('resize', positionMenus);
    main.addEventListener('scroll', positionMenus, {passive: true});
    activity(readPreference('activitySidebar', false) === true);
    setPage(window.JukuDialogs.page());
  }

  return {init, access, info, page: () => current, resetScroll: () => {main.scrollTop = 0;}, sourceStates: (value, loading, phase = '') => {sourceStates = value || {}; sourceLoading = Boolean(loading); sourcePhase = phase || ''; updateSourceRows();}};
}

import { $, element } from './ui-core.js';

export function createEmby(app) {
  let state, timer, loading = false, saving = false;
  const panel = $('embyPanel');
  const date = value => value && !value.startsWith('0001') ? new Date(value).toLocaleString() : '尚未同步';
  const posterStates = {pending: '海报待同步', waiting: '海报等待扫描', synced: '海报已同步', local: '海报已保存到输出目录', existing: '保留已有海报', missing: '暂无海报地址', failed: '海报同步失败，将重试'};

  function fields() {
    const settings = state.settings;
    $('embyEnabled').checked = settings.enabled;
    $('embyOutputDir').value = settings.outputDir;
    $('embyBaseURL').value = settings.baseUrl || location.origin;
    $('embyInterval').value = settings.intervalMinutes;
    $('embyFollowing').checked = settings.following;
    $('embyDownloads').checked = settings.downloads;
    $('embyServerURL').value = settings.serverUrl || '';
    $('embyAPIKey').value = '';
    $('embyAPIKey').placeholder = state.hasAPIKey ? '已保存，留空保留' : '在 Emby 控制台创建 API Key';
    $('embyBaseURL').required = settings.enabled;
  }

  function render() {
    if (!state) return;
    const entries = state.entries || [];
    const imported = entries.filter(entry => entry.episodes > 0 || entry.merged).length;
    const merged = entries.filter(entry => entry.merged).length;
    $('embyRunBtn').disabled = saving || !state.settings.enabled || state.running;
    $('embyRunBtn').textContent = state.running ? '正在同步…' : '立即同步';
    $('embyOwner').textContent = state.owner ? '追剧账号：' + state.owner : '保存后使用当前管理员的追剧清单。';
    $('embySummary').textContent = '已导入 ' + imported + ' 部 · ' + (state.episodes || 0) + ' 集' + (merged ? ' · 合并版 ' + merged + ' 部' : '') + (state.pending ? ' · 待处理 ' + state.pending + ' 部' : '');
    $('embyPosterSummary').textContent = (state.settings.serverUrl ? '海报已同步 ' : '海报已保存 ') + (state.postersSynced || 0) + ' 部' + (state.postersPending ? ' · 等待 ' + state.postersPending + ' 部' : '') + (state.postersFailed ? ' · 失败待重试 ' + state.postersFailed + ' 部' : '');
    $('embySchedule').textContent = state.running ? '正在更新分集文件和海报状态…' : state.settings.enabled ? '最近完成：' + date(state.lastFinishedAt) + ' · 下次检查：' + date(state.nextRunAt) : '自动同步已关闭，已导入的文件保留。';
    $('embyResult').textContent = state.error || (state.lastFinishedAt && !state.lastFinishedAt.startsWith('0001') ? '最近一轮：成功 ' + state.succeeded + ' 部，失败 ' + state.failed + ' 部，更新 ' + state.writtenFiles + ' 个文件。' : '');
    $('embyResult').classList.toggle('error', Boolean(state.error));
    const fragment = document.createDocumentFragment();
    for (const entry of entries.slice(0, 100)) {
      const row = element('li', 'emby-entry');
      row.append(element('strong', '', entry.title || entry.dramaId), element('span', 'small', entry.episodes + ' 集' + (entry.merged ? ' · 合并版（特别篇）' : '') + ' · ' + date(entry.syncedAt)));
      if (posterStates[entry.posterStatus]) row.append(element('p', 'small', posterStates[entry.posterStatus]));
      if (entry.posterFile && entry.posterStatus !== 'local') row.append(element('p', 'small', '本地海报：' + entry.posterFile));
      if (entry.error) row.append(element('p', 'error', entry.error));
      if (entry.posterError) row.append(element('p', entry.posterStatus === 'failed' ? 'error' : 'small', entry.posterError));
      if (entry.posterLocalError && entry.posterLocalError !== entry.posterError) row.append(element('p', 'error', '海报保存失败：' + entry.posterLocalError));
      fragment.appendChild(row);
    }
    if (entries.length > 100) fragment.appendChild(element('li', 'small', '显示最近处理的 100 部，其余内容仍会自动同步。'));
    $('embyEntries').replaceChildren(fragment);
    $('embyEmpty').hidden = entries.length > 0;
  }

  function poll(delay) {
    clearTimeout(timer);
    if (panel.open) timer = setTimeout(() => load(false), delay ?? (state?.running ? 1500 : 10000));
  }

  async function load(fill = true) {
    if (loading) return;
    loading = true;
    try {
      state = await app.api('/api/ui/admin/emby');
      if (fill) fields();
      render();
    } catch (error) {
      $('embyFeedback').textContent = '读取失败：' + error.message;
    } finally {
      loading = false;
      poll();
    }
  }

  async function save(event) {
    event.preventDefault();
    if (saving || !state) return;
    saving = true;
    $('embySaveBtn').disabled = true;
    const input = {enabled: $('embyEnabled').checked, outputDir: $('embyOutputDir').value.trim(), baseUrl: $('embyBaseURL').value.trim(), intervalMinutes: Number($('embyInterval').value), following: $('embyFollowing').checked, downloads: $('embyDownloads').checked, serverUrl: $('embyServerURL').value.trim()};
    if ($('embyAPIKey').value) input.apiKey = $('embyAPIKey').value;
    try {
      state = await app.post('/api/ui/admin/emby', input);
      fields();
      $('embyFeedback').textContent = input.enabled ? '设置已保存，正在安排同步。Emby 媒体库请使用上方的输出目录。' : '设置已保存，自动同步已关闭。';
    } catch (error) {
      $('embyFeedback').textContent = '保存失败：' + error.message;
    } finally {
      saving = false;
      $('embySaveBtn').disabled = false;
      render();
      poll(500);
    }
  }

  async function run() {
    $('embyRunBtn').disabled = true;
    try {
      state = await app.post('/api/ui/admin/emby/sync', {});
      $('embyFeedback').textContent = '同步已安排，关闭此窗口也会继续。';
      render();
    } catch (error) {
      $('embyFeedback').textContent = error.message;
      render();
    }
    poll(500);
  }

  function init() {
    $('embyForm').addEventListener('submit', save);
    $('embyEnabled').addEventListener('change', () => {$('embyBaseURL').required = $('embyEnabled').checked;});
    $('embyRunBtn').addEventListener('click', run);
    $('openEmbyBtn').addEventListener('click', () => {
      if (!app.viewer?.account?.admin) return;
      $('embyFeedback').textContent = '';
      window.JukuDialogs.open('embyPanel');
      load();
    });
    panel.addEventListener('close', () => clearTimeout(timer));
  }
  return {init};
}

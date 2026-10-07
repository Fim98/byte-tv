import { $, api, post } from './ui-core.js';

export function createPlaybackSettings() {
  let settings = null;
  const fields = ['maxSessions', 'maxMediaRequests', 'maxRemuxJobs', 'maxAudioTranscodes', 'maxVideoTranscodes', 'maxPrefetchJobs', 'cacheMB'];
  function updateDirect() {
    const enabled = $('originalPlaybackEnabled').checked;
    $('playbackDirectSources').querySelectorAll('input').forEach(input => {input.disabled = !enabled;});
    $('playbackDirectHint').textContent = enabled
      ? '允许尝试直连，不保证每集生效。需要服务器解密、转码，或源站鉴权、HTTPS、跨域检查未通过时仍走中转；客户端连接失败也会自动回退。'
      : '原始 MP4 / HLS 播放已关闭，已勾选的直连站源也不会生效。先开启上方开关，再保存播放设置。';
  }
  function render(result) {
    settings = result.settings;
    $('originalPlaybackEnabled').checked = settings.enabled;
    for (const name of fields) $('playback-' + name).value = settings[name];
    const sources = $('playbackDirectSources');
    sources.replaceChildren();
    for (const source of result.sources || []) {
      const label = document.createElement('label');
      const input = document.createElement('input');
      input.type = 'checkbox';
      input.value = source.id;
      input.checked = (settings.directSources || []).includes(source.id);
      label.append(input, document.createTextNode(source.name));
      sources.appendChild(label);
    }
    updateDirect();
  }
  async function load() {
    try {render(await api('/api/ui/admin/playback'));}
    catch (error) {$('playbackSettingsStatus').textContent = error.message;}
  }
  async function save() {
    if (!settings) return;
    const button = $('savePlaybackSettings');
    button.disabled = true;
    try {
      const value = {...settings, enabled: $('originalPlaybackEnabled').checked,
        directSources: [...$('playbackDirectSources').querySelectorAll('input:checked')].map(input => input.value)};
      for (const name of fields) value[name] = Number($('playback-' + name).value);
      render(await post('/api/ui/admin/playback', value));
      $('playbackSettingsStatus').textContent = '播放设置已保存，新任务按当前限制执行。' + (!value.enabled && value.directSources.length ? '原始播放已关闭，直连暂不生效。' : '');
    } catch (error) {$('playbackSettingsStatus').textContent = '保存失败：' + error.message;}
    finally {button.disabled = false;}
  }
  function init() {$('savePlaybackSettings').onclick = save; $('originalPlaybackEnabled').onchange = updateDirect; return load();}
  return {init};
}

(() => {
  const endpoint = '/api/ui/preferences';
  const legacyKeys = ['activitySidebar', 'downloadQuality', 'downloadFilters', 'libraryFilters', 'recentSearches', 'theme', 'playback.autoNext', 'playback.rate'];
  const legacyAliases = {'juku.playback.quality': 'playback.quality', 'juku.playback.prefetchNext': 'playback.prefetchNext', 'juku.playback.autoRotate': 'playback.autoRotate', 'juku-danmaku-enabled': 'danmaku.enabled', 'juku.vip.show': 'vip.show'};
  let cache = {}, ready = false, loading = null, pending = {}, saveTimer = 0;
  const listeners = new Set();

  function validKey(key) {return typeof key === 'string' && key.length > 0 && key.length <= 80 && key.trim() === key && /^[\x21-\x7e]+$/.test(key);}
  function copy(value) {try {return value === undefined ? undefined : JSON.parse(JSON.stringify(value));} catch (_) {return undefined;}}
  function decode(value) {try {return JSON.parse(value);} catch (_) {return undefined;}}
  function readLocal(key) {
    try {
      const raw = localStorage.getItem(key);
      return raw === null ? undefined : decode(raw);
    } catch (_) {return undefined;}
  }
  function loadLegacy() {
    const values = {};
    for (const key of legacyKeys) {
      let value;
      try {
        const raw = localStorage.getItem('duanju.' + key);
        if (raw === null) continue;
        if (key === 'theme' || key === 'playback.rate') value = raw;
        else if (key === 'playback.autoNext') value = raw === 'true';
        else value = decode(raw);
      } catch (_) {continue;}
      if (value !== undefined) values[key] = value;
    }
    for (const [oldKey, key] of Object.entries(legacyAliases)) {
      let value;
      try {
        const raw = localStorage.getItem(oldKey);
        if (raw === null) continue;
        if (key === 'playback.quality') value = Number(raw) || 0;
        else if (['playback.prefetchNext', 'playback.autoRotate', 'danmaku.enabled', 'vip.show'].includes(key)) value = raw === 'true';
        else value = decode(raw);
      } catch (_) {continue;}
      if (value !== undefined) values[key] = value;
    }
    return values;
  }
  function notify(keys) {
    const unique = [...new Set(keys.filter(validKey))];
    if (!unique.length) return;
    const detail = {keys: unique, values: snapshot()};
    for (const listener of listeners) {try {listener(detail);} catch (_) {}}
    document.dispatchEvent(new CustomEvent('jukupreferencechange', {detail}));
  }
  function snapshot() {return copy(cache) || {};}
  function read(key, fallback) {
    if (!validKey(key)) return fallback;
    return Object.prototype.hasOwnProperty.call(cache, key) ? copy(cache[key]) : fallback;
  }
  function merge(values) {
    const changed = [];
    for (const [key, value] of Object.entries(values || {})) {
      if (!validKey(key)) continue;
      const next = copy(value);
      if (next === undefined) continue;
      if (JSON.stringify(cache[key]) !== JSON.stringify(next)) changed.push(key);
      cache[key] = next;
    }
    return changed;
  }
  function request(values) {
    return fetch(endpoint, {method: 'POST', credentials: 'same-origin', headers: {'Accept': 'application/json', 'Content-Type': 'application/json', ...window.JukuViewer?.headers?.()}, body: JSON.stringify({values})})
      .then(async response => {
        const result = await response.json();
        window.JukuViewer?.checkResponse?.(result);
        if (!response.ok) throw new Error(result.error || 'HTTP ' + response.status);
        return result;
      });
  }
  function flush() {
    saveTimer = 0;
    const values = pending;
    pending = {};
    if (!Object.keys(values).length) return;
    request(values).then(result => {
      const changed = merge(result.values || {});
      notify(changed);
    }).catch(() => {
      pending = {...values, ...pending};
      if (!saveTimer) saveTimer = setTimeout(flush, 5000);
    });
  }
  function save(key, value) {
    if (!validKey(key)) return;
    const next = copy(value);
    if (next === undefined) return;
    const changed = merge({[key]: next});
    pending[key] = next;
    if (ready && !saveTimer) saveTimer = setTimeout(flush, 250);
    notify(changed);
  }
  function onChange(listener) {listeners.add(listener); return () => listeners.delete(listener);}
  async function init() {
    if (loading) return loading;
    loading = (async () => {
      const legacy = loadLegacy();
      merge(legacy);
      try {
        const response = await fetch(endpoint, {credentials: 'same-origin', cache: 'no-store', headers: {'Accept': 'application/json', ...window.JukuViewer?.headers?.()}});
        const result = await response.json();
        window.JukuViewer?.checkResponse?.(result);
        if (!response.ok) throw new Error(result.error || 'HTTP ' + response.status);
        const server = result.values && typeof result.values === 'object' ? result.values : {};
        const missing = {};
        for (const [key, value] of Object.entries(legacy)) if (!Object.prototype.hasOwnProperty.call(server, key)) missing[key] = value;
        cache = {};
        const changed = merge({...legacy, ...server});
        ready = true;
        if (Object.keys(missing).length) {
          try {
            const saved = await request(missing);
            changed.push(...merge(saved.values || {}));
          } catch (_) {
            pending = {...missing, ...pending};
          }
        }
        if (Object.keys(pending).length && !saveTimer) saveTimer = setTimeout(flush, 250);
        notify(changed.length ? changed : Object.keys(cache));
      } catch (error) {
        ready = true;
        if (Object.keys(pending).length && !saveTimer) saveTimer = setTimeout(flush, 1000);
        notify(Object.keys(cache));
      }
      return snapshot();
    })();
    return loading;
  }
  cache = loadLegacy();
  window.JukuPreferences = {init, read, save, onChange, snapshot, get ready() {return ready;}};
})();

(() => {
  const defaults = {forwardSeconds: 90, maxForwardSeconds: 180, maxBytes: 48 * 1024 * 1024, backSeconds: 20};
  const abortError = () => new DOMException('播放已停止', 'AbortError');

  function snapshot(video, duration = video.duration) {
    const position = Math.max(0, Number(video.currentTime) || 0);
    const total = Number.isFinite(duration) && duration > 0 ? duration : 0;
    const ranges = [];
    let ahead = 0, end = position;
    try {
      const buffered = video.buffered;
      for (let index = 0; index < buffered.length; index++) {
        const start = Math.max(0, buffered.start(index)), stop = total ? Math.min(buffered.end(index), total) : buffered.end(index);
        if (!Number.isFinite(start) || !Number.isFinite(stop) || stop <= start) continue;
        ranges.push({start, end: total ? Math.min(stop, total) : stop});
        if (start <= position && stop >= position) {
          ahead = Math.max(0, (total ? Math.min(stop, total) : stop) - position);
          end = position + ahead;
        }
      }
    } catch (_) {}
    return {position, duration: total, ahead, end, ranges};
  }

  function gradient(ranges, duration) {
    const base = '#ffffff45', filled = '#ffffff90', stops = [base + ' 0%'];
    if (duration > 0 && Number.isFinite(duration)) {
      for (const range of ranges) {
        const start = Math.max(0, Math.min(100, range.start / duration * 100));
        const end = Math.max(start, Math.min(100, range.end / duration * 100));
        stops.push(base + ' ' + start + '%', filled + ' ' + start + '%', filled + ' ' + end + '%', base + ' ' + end + '%');
      }
    }
    stops.push(base + ' 100%');
    return 'linear-gradient(to right,' + stops.join(',') + ')';
  }

  function prefetchReady({duration, position, rate = 1, ahead, complete, metadataOnly}) {
    const remaining = duration - position;
    rate = Math.max(0.25, Number(rate) || 1);
    if (!Number.isFinite(remaining) || remaining <= 0 || remaining / rate > (metadataOnly ? 60 : 45)) return false;
    return metadataOnly ? ahead >= Math.min(remaining, 15 * rate) - 0.25 : complete && ahead >= remaining - 0.25;
  }

  function pause(signal) {
    return new Promise((resolve, reject) => {
      let timer;
      const abort = () => {clearTimeout(timer); signal.removeEventListener('abort', abort); reject(abortError());};
      if (signal.aborted) {abort(); return;}
      signal.addEventListener('abort', abort, {once: true});
      timer = setTimeout(() => {signal.removeEventListener('abort', abort); resolve();}, 200);
    });
  }

  function create(video, waitForEvent, offset = 0, options = {}) {
    const settings = {...defaults, ...options};
    let bytes = 0, seconds = 0, ceiling = settings.maxForwardSeconds, budget = settings.maxBytes, keepBehind = settings.backSeconds;
    function record(size, buffer) {
      bytes += size;
      if (buffer.buffered.length) seconds = Math.max(seconds, buffer.buffered.end(buffer.buffered.length - 1) - offset);
    }
    function goal() {
      const desired = settings.forwardSeconds * Math.max(1, Number(video.playbackRate) || 1);
      const behind = Math.min(keepBehind, Math.max(0, video.currentTime - offset));
      const fromBytes = seconds >= 1 && bytes > 0 ? budget * seconds / bytes - behind : ceiling;
      return Math.max(4, Math.min(desired, ceiling, fromBytes));
    }
    async function trim(buffer, signal, urgent = false) {
      const cutoff = video.currentTime - (urgent ? Math.min(keepBehind, 10) : keepBehind);
      if (cutoff > 0 && buffer.buffered.length && cutoff - buffer.buffered.start(0) > (urgent ? 0.1 : 5)) {
        await waitForEvent(buffer, 'updateend', signal, () => buffer.remove(0, cutoff));
        return true;
      }
      return false;
    }
    async function waitForSpace(signal) {
      while (!signal.aborted && snapshot(video).ahead > goal()) await pause(signal);
      if (signal.aborted) throw abortError();
    }
    async function append(buffer, data, signal) {
      await trim(buffer, signal);
      let retries = 0;
      for (;;) {
        if (signal.aborted) throw abortError();
        try {
          await waitForEvent(buffer, 'updateend', signal, () => buffer.appendBuffer(data));
          record(data.byteLength, buffer);
          return;
        } catch (error) {
          if (error.name !== 'QuotaExceededError') throw error;


          ceiling = Math.max(4, Math.min(ceiling / 2, snapshot(video).ahead * 0.75 || ceiling / 2));
          budget = Math.max(4 * 1024 * 1024, budget / 2);
          keepBehind = Math.min(keepBehind, 10);
          if (++retries > 3) {
            const failure = new Error('浏览器可用视频缓存不足，请关闭其他视频页面后重试');
            failure.playbackBuffer = true;
            throw failure;
          }
          let removed = await trim(buffer, signal, true);
          while (!removed && snapshot(video).ahead > 0.5) {
            await pause(signal);
            removed = await trim(buffer, signal, true);
          }
          if (!removed) {
            const failure = new Error('浏览器没有足够空间缓冲此视频，请降低清晰度后重试');
            failure.playbackBuffer = true;
            throw failure;
          }
        }
      }
    }
    return {goal, append, waitForSpace};
  }

  const api = {create, snapshot, gradient, prefetchReady};
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
  else window.JukuPlaybackBuffer = api;
})();

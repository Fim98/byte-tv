(() => {
  function createMediaLoader(environment, video, onError) {
    let hls = null;
    let generation = 0;
    let cancelPending = null;
    function destroy() {
      generation++;
      if (cancelPending) cancelPending();
      cancelPending = null;
      if (hls) hls.destroy();
      hls = null;
    }
    function failure(message, network) {
      const error = new Error(message);
      error.playbackNetwork = network;
      error.playbackDecode = !network;
      return error;
    }
    async function load(plan, nativeHls, signal, offset = 0) {
      destroy();
      const current = generation;
      if (signal.aborted) throw new DOMException('已取消', 'AbortError');
      if (plan.player === 'mp4' && plan.mime && !video.canPlayType(plan.mime)) throw failure('浏览器不支持此媒体编码，正在尝试兼容播放', false);
      await new Promise((resolve, reject) => {
        let settled = false;
        const timer = setTimeout(() => finish(failure('媒体加载超时，请重试', true)), 60000);
        const abort = () => finish(new DOMException('已取消', 'AbortError'));
        const metadata = () => finish();
        const failed = () => finish(failure('浏览器无法读取此视频', video.error?.code === 2));
        function finish(error) {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
          signal.removeEventListener('abort', abort);
          video.removeEventListener('loadedmetadata', metadata);
          video.removeEventListener('error', failed);
          cancelPending = null;
          if (error && current === generation) {
            generation++;
            if (hls) hls.destroy();
            hls = null;
          }
          if (error) reject(error); else resolve();
        }
        cancelPending = abort;
        signal.addEventListener('abort', abort, {once: true});
        video.addEventListener('loadedmetadata', metadata, {once: true});
        video.addEventListener('error', failed, {once: true});
        try {
          if (plan.player === 'hls' && !nativeHls) {
            const Hls = environment.Hls;
            if (!Hls?.isSupported()) {finish(failure('此浏览器没有可用的 HLS 播放通道', false)); return;}


            hls = new Hls({enableWorker: true, lowLatencyMode: false, maxBufferLength: 60, maxMaxBufferLength: 180,
              maxBufferSize: 48 * 1024 * 1024, backBufferLength: 20, startPosition: Math.max(0, Number(offset) || 0)});
            hls.on(Hls.Events.ERROR, (_, data) => {
              if (!data.fatal || current !== generation || signal.aborted) return;
              const error = failure(data.type === Hls.ErrorTypes.NETWORK_ERROR ? '视频连接失败，请重试' : '此媒体需要兼容播放', data.type === Hls.ErrorTypes.NETWORK_ERROR);
              if (!settled) finish(error); else onError(error);
            });
            hls.on(Hls.Events.MEDIA_ATTACHED, () => {
              if (current !== generation || signal.aborted) return;
              try {hls.loadSource(plan.url);} catch (error) {finish(failure(error.message || '无法加载 HLS 媒体', false));}
            });
            hls.attachMedia(video);
          } else {
            video.src = plan.url;
            video.load();
          }
        } catch (error) {finish(failure(error.message || '无法初始化播放器', false));}
      });
    }
    return {load, destroy};
  }
  if (typeof module !== 'undefined' && module.exports) module.exports = createMediaLoader;
  else window.JukuPlayerMedia = createMediaLoader;
})();

(() => {
  function createPlaybackCapabilities(environment, video, agent) {
    const touchApple = /iPad|iPhone|iPod/i.test(agent.userAgent || '') || /Mac/i.test(agent.platform || '') && Number(agent.maxTouchPoints) > 1;
    function native() {
      if (touchApple) return true;
      try {return ['application/vnd.apple.mpegurl', 'application/x-mpegURL'].some(type => ['maybe', 'probably'].includes(video.canPlayType(type)));}
      catch (_) {return false;}
    }
    function mse(type) {
      try {return typeof type === 'string' && Boolean(type) && typeof environment.MediaSource === 'function' && typeof environment.MediaSource.isTypeSupported === 'function' && environment.MediaSource.isTypeSupported(type);}
      catch (_) {return false;}
    }
    function choose(type) {
      if (touchApple && native()) return 'hls';
      if (mse(type)) return 'mse';
      return native() ? 'hls' : '';
    }
    function remux() {
      return !touchApple && ['42E02A', '4D002A', '64002A'].every(profile => mse('video/mp4; codecs="avc1.' + profile + ', mp4a.40.2"'));
    }
    function playable(type) {
      try {return ['maybe', 'probably'].includes(video.canPlayType(type));} catch (_) {return false;}
    }
    function describe() {
      const supported = type => playable(type) || mse(type);
      const videoCodecs = {h264: 'avc1.42E01E', hevc: 'hvc1.1.6.L120.B0', av1: 'av01.0.08M.08', vp9: 'vp09.00.10.08'};
      const audioCodecs = {aac: 'mp4a.40.2', mp3: 'mp4a.69', ac3: 'ac-3', eac3: 'ec-3', opus: 'opus'};
      return {mp4: playable('video/mp4'), nativeHls: native(), hlsjs: Boolean(environment.Hls?.isSupported()),
        video: Object.keys(videoCodecs).filter(key => supported('video/mp4; codecs="' + videoCodecs[key] + '"')),
        audio: Object.keys(audioCodecs).filter(key => supported('audio/mp4; codecs="' + audioCodecs[key] + '"'))};
    }
    return {native, mse, choose, remux, playable, describe};
  }
  if (typeof module !== 'undefined' && module.exports) module.exports = createPlaybackCapabilities;
  else window.JukuPlaybackCapabilities = createPlaybackCapabilities;
})();

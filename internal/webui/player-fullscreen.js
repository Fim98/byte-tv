(() => {
  window.JukuPlayerFullscreen = ({panel, video, ready, beforeEnter = () => {}}) => {
    const node = id => document.getElementById(id);
    const stage = node('playbackStage'), frame = node('playerFullscreenRoot');
    const status = node('playbackFullscreenStatus');
    const buttons = ['playerFullscreenBtn', 'mobileFullscreenBtn', 'mobileVideoFullscreenBtn'].map(node);
    let nativeActive = null, entering = false, exiting = false, preferNative = false, messageTimer = null;

    const element = () => document.fullscreenElement || document.webkitFullscreenElement;
    const native = () => nativeActive ?? (Boolean(video.webkitDisplayingFullscreen) || video.webkitPresentationMode === 'fullscreen');
    const active = () => native() || Boolean(element() && panel.contains(element()));
    function elementRequest() {
      const target = panel.classList.contains('mobile-player') ? frame : stage;
      if (document.fullscreenEnabled !== false && typeof target.requestFullscreen === 'function') return () => target.requestFullscreen();
      if (document.webkitFullscreenEnabled !== false && typeof target.webkitRequestFullscreen === 'function') return () => target.webkitRequestFullscreen();
      return null;
    }
    function nativeRequest() {
      if (typeof video.webkitEnterFullscreen === 'function') return () => video.webkitEnterFullscreen();
      if (typeof video.webkitEnterFullScreen === 'function') return () => video.webkitEnterFullScreen();
      if (typeof video.webkitSetPresentationMode === 'function') {
        try {
          if (typeof video.webkitSupportsPresentationMode !== 'function' || video.webkitSupportsPresentationMode('fullscreen')) return () => video.webkitSetPresentationMode('fullscreen');
        } catch (_) {}
      }
      return null;
    }
    function message(text = '') {
      clearTimeout(messageTimer);
      status.textContent = text;
      status.hidden = !text;
      if (text) messageTimer = setTimeout(() => message(), 5500);
    }
    function update() {
      const on = active(), supported = Boolean(elementRequest() || nativeRequest());
      const label = on ? '退出全屏' : '全屏播放';
      for (const button of buttons) {
        button.hidden = !supported && !on;
        button.disabled = entering || exiting || !on && !ready();
        button.title = label;
        button.setAttribute('aria-label', label);
        button.setAttribute('aria-pressed', String(on));
      }
      node('mobileFullscreenBtn').textContent = label;
      node('mobileBackBtn').setAttribute('aria-label', on ? '退出全屏' : panel.classList.contains('player-landscape') ? '返回竖屏播放' : '返回剧库');
      const tokens = new Set((video.getAttribute('controlslist') || '').split(/\s+/).filter(Boolean));
      if (elementRequest() && !preferNative && !native()) tokens.add('nofullscreen');
      else tokens.delete('nofullscreen');
      const value = Array.from(tokens).join(' ');
      if (value) video.setAttribute('controlslist', value); else video.removeAttribute('controlslist');
    }
    function changed() {
      update();
      panel.dispatchEvent(new Event('jukufullscreenchange'));
    }
    function enterNative(request) {
      const tokens = (video.getAttribute('controlslist') || '').split(/\s+/).filter(token => token && token !== 'nofullscreen');
      if (tokens.length) video.setAttribute('controlslist', tokens.join(' ')); else video.removeAttribute('controlslist');
      return request();
    }
    async function enter() {
      if (!panel.open || entering || exiting || active()) return;
      if (!ready()) {message('请等待视频准备好后再点全屏'); return;}
      const fallback = nativeRequest(), request = preferNative && fallback ? null : elementRequest();
      if (!request && !fallback) {message('当前浏览器未开放全屏播放'); return;}
      message();
      beforeEnter();
      entering = true;
      update();
      try {
        if (request) {
          try {await request();}
          catch (error) {
            if (!fallback) throw error;
            preferNative = true;
            if (!panel.open) return;
            if (navigator.userActivation?.isActive === false) {message('请再次点击全屏播放'); return;}
            await enterNative(fallback);
          }
        } else await enterNative(fallback);
      } catch (_) {
        if (panel.open) message('全屏未能打开，请点播放后重试');
      } finally {
        entering = false;
        changed();
        if (!panel.open) exit(false);
      }
    }
    async function exit(report = true) {
      if (exiting) return;
      exiting = true;
      update();
      try {
        if (native()) {
          if (typeof video.webkitExitFullscreen === 'function') await video.webkitExitFullscreen();
          else if (typeof video.webkitExitFullScreen === 'function') await video.webkitExitFullScreen();
          else if (typeof video.webkitSetPresentationMode === 'function') await video.webkitSetPresentationMode('inline');
          else throw new Error('fullscreen exit unavailable');
        } else if (element() && panel.contains(element())) {
          if (typeof document.exitFullscreen === 'function') await document.exitFullscreen();
          else if (typeof document.webkitExitFullscreen === 'function') await document.webkitExitFullscreen();
          else throw new Error('fullscreen exit unavailable');
        }
      } catch (_) {
        if (report && panel.open) message('暂时无法退出全屏，请使用系统返回');
      } finally {
        exiting = false;
        changed();
      }
    }
    function toggle() {return active() ? exit() : enter();}
    function browserChanged() {
      changed();
      if (!panel.open && active()) exit(false);
    }
    for (const button of buttons) button.addEventListener('click', toggle);
    node('exitPlayerFullscreenBtn').addEventListener('click', () => exit());
    for (const type of ['fullscreenchange', 'webkitfullscreenchange']) document.addEventListener(type, browserChanged);
    video.addEventListener('webkitbeginfullscreen', () => {nativeActive = true; browserChanged();});
    video.addEventListener('webkitendfullscreen', () => {nativeActive = false; browserChanged();});
    video.addEventListener('webkitpresentationmodechanged', () => {nativeActive = video.webkitPresentationMode === 'fullscreen'; browserChanged();});
    for (const type of ['loadedmetadata', 'canplay', 'emptied', 'error']) video.addEventListener(type, update);
    video.addEventListener('emptied', () => message());
    panel.addEventListener('close', () => {message(); exit(false);});
    update();
    return {enter, exit, toggle, active, native, update};
  };
})();

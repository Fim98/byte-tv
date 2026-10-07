(() => {
  const system = window.matchMedia('(prefers-color-scheme: dark)');
  let preference = 'system';
  preference = window.JukuPreferences?.read('theme', 'system') || 'system';
  if (!['system', 'light', 'dark'].includes(preference)) preference = 'system';
  function apply() {
    const theme = preference === 'system' ? (system.matches ? 'dark' : 'light') : preference;
    document.documentElement.dataset.theme = theme;
    const browserColor = document.querySelector('meta[name="theme-color"]');
    if (browserColor) browserColor.content = theme === 'dark' ? '#161b19' : '#f7f7f2';
    document.dispatchEvent(new CustomEvent('themechange', {detail: preference}));
  }
  function set(value) {
    if (!['system', 'light', 'dark'].includes(value)) return;
    preference = value;
    window.JukuPreferences?.save('theme', value);
    apply();
  }
  if (system.addEventListener) system.addEventListener('change', apply);
  else system.addListener(apply);
  window.JukuTheme = {set, get: () => preference};
  window.JukuPreferences?.onChange(event => {
    if (event.keys.includes('theme')) {
      const value = event.values.theme;
      if (['system', 'light', 'dark'].includes(value) && value !== preference) {
        preference = value;
        apply();
      }
    }
  });
  apply();
})();

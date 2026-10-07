const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const {chromium, webkit} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');

const project = path.resolve(process.argv[2] || path.join(__dirname, '..'));
const id = process.argv[3] || 'hongguo:7000000000000000001';
const source = id.split(':')[0];
const group = process.argv[4] || 'hongguo';
const controlID = source + ':7000000000000000002';
const origin = 'http://127.0.0.1:27831';
const remote = 'https://covers.example.org/text-fixture?auth=a%2Fb%2B%3D&expires=1900000000';
const cover = '/api/ui/image?url=' + encodeURIComponent(remote) + '&dramaId=' + encodeURIComponent(id);
const controlCover = '/api/ui/image?url=' + encodeURIComponent('https://covers.example.org/control-text') + '&dramaId=' + encodeURIComponent(controlID);
const ui = path.join(project, 'internal/webui');
let dramas = [
  {id, source, title: '海报重试文字测试', cover, coverUrl: cover, totalEpisode: 3},
  {id: controlID, source, title: '无关条目文字测试', cover: controlCover, coverUrl: controlCover, totalEpisode: 3},
];

async function until(predicate, message) {
  const end = Date.now() + 5000;
  while (!predicate()) {
    if (Date.now() >= end) throw new Error(message);
    await new Promise(resolve => setTimeout(resolve, 20));
  }
}

(async () => {
  const engine = process.env.JUKU_BROWSER_ENGINE === 'webkit' ? webkit : chromium;
  const browser = await engine.launch({headless: true, ...(process.env.JUKU_BROWSER_EXECUTABLE ? {executablePath: process.env.JUKU_BROWSER_EXECUTABLE} : {})});
  const context = await browser.newContext({serviceWorkers: 'block', viewport: process.env.JUKU_MOBILE ? {width: 390, height: 844} : {width: 1280, height: 900}});
  context.setDefaultTimeout(7000);
  const imageAttempts = [], unexpected = [], pageErrors = [];
  let refreshes = 0, repairs = 0, releaseRefresh;
  const count = target => imageAttempts.filter(address => new URL(address).searchParams.get('url') === target).length;
  try {
    await context.route('**/*', async route => {
      const request = route.request(), address = new URL(request.url());
      if (request.resourceType() === 'image' || address.pathname === '/api/ui/image') {
        if (address.origin === origin && address.pathname === '/api/ui/image') imageAttempts.push(address.href);
        else unexpected.push('image:' + address.origin + address.pathname);
        return route.abort();
      }
      if (address.origin !== origin) {
        unexpected.push('external:' + address.origin + address.pathname);
        return route.abort();
      }
      if (address.pathname === '/') return route.fulfill({contentType: 'text/html', body: fs.readFileSync(path.join(ui, 'index.html'), 'utf8')});
      if (/^\/assets\/[a-zA-Z0-9_.-]+\.(?:js|css|txt)$/.test(address.pathname)) {
        const name = path.basename(address.pathname);
        let body = fs.readFileSync(path.join(ui, name), 'utf8');
        if (name === 'main.js') body = body.replace('const app = {api, post};', 'const app = {api, post}; window.__coverTestApp = app;');
        return route.fulfill({contentType: name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : 'text/plain', body});
      }
      let result;
      switch (address.pathname) {
        case '/api/ui/viewer': result = {id: 'a'.repeat(64), ready: true, sources: [group], account: {username: 'fixture', admin: false}}; break;
        case '/api/ui/dramas': result = {data: dramas, revision: 1, loadedAt: '2026-09-20T00:00:00Z', sources: {}}; break;
        case '/api/ui/tasks': result = {data: [], merges: {}}; break;
        case '/api/ui/playback/history': result = {data: []}; break;
        case '/api/ui/following': result = {data: dramas.map(drama => ({dramaId: drama.id, source, title: drama.title, saved: true, knownEpisodes: 3, updatedAt: '2026-09-20T00:00:00Z'}))}; break;
        case '/api/ui/dramas/refresh': {
          refreshes++;
          assert.equal(request.postDataJSON().dramaId, id);
          await new Promise(resolve => {releaseRefresh = resolve;});
          dramas[0] = {...dramas[0], title: '资料已更新文字测试', sortMetadata: {version: 2, checkedAt: '2026-09-20T01:00:00Z'}};
          result = {dramaId: id, drama: dramas[0], retryAfter: 300};
          break;
        }
        case '/api/ui/cover/repair': repairs++; result = {dramaId: id, cover, retryAfter: 300}; break;
        default: unexpected.push('local:' + address.pathname); return route.abort();
      }
      return route.fulfill({contentType: 'application/json', body: JSON.stringify(result)});
    });
    const page = await context.newPage();
    page.on('pageerror', error => pageErrors.push(error.message));
    await page.goto(origin);
    await page.waitForFunction(() => document.documentElement.dataset.ready === 'true');
    await page.evaluate(() => {
      window.__plays = [];
      window.dramaPlayer.open = (id, title) => window.__plays.push({id, title});
      window.dramaPlayer.openHistory = window.dramaPlayer.open;
    });
    const card = page.locator(`.card[data-drama-id="${id}"]`);
    await card.locator('.cover.placeholder').waitFor();
    assert.equal(refreshes, 0, 'ordinary library reads must not refresh metadata');
    const unrelated = count('https://covers.example.org/control-text');
    let before = count(remote);
    await card.locator('.poster').click();
    await until(() => refreshes === 1 && count(remote) > before, 'first click did not retry the failed cover before metadata returned');
    assert.equal(await page.evaluate(() => window.__plays.length), 1, 'playback waited for metadata');
    await card.locator('.cover.placeholder').waitFor();
    before = count(remote);
    await card.locator('.poster').click();
    await until(() => count(remote) > before, 'pending metadata suppressed the second cover retry');
    assert.equal(refreshes, 1);
    releaseRefresh();
    releaseRefresh = null;
    await page.waitForFunction(id => window.__coverTestApp.library.get(id)?.title === '资料已更新文字测试', id);
    await card.locator('.cover.placeholder').waitFor();
    before = count(remote);
    await card.locator('.poster').click();
    await until(() => count(remote) > before, 'metadata cooldown suppressed the cover retry');
    assert.equal(refreshes, 1);
    assert.equal(count('https://covers.example.org/control-text'), unrelated, 'clicking one drama retried an unrelated card');

    await card.locator('.card-title').click();
    await page.waitForFunction(() => document.querySelector('.detail-cover-status')?.textContent === '海报加载失败');
    await page.locator('#detailPanel [data-close]').click();
    before = count(remote);
    await card.locator('.card-title').click();
    await until(() => count(remote) > before, 'reopening details did not retry its poster');
    await page.waitForFunction(() => document.querySelector('.detail-cover-status')?.textContent === '海报加载失败');
    before = count(remote);
    await page.locator('#detailPanel .primary-action').click();
    await until(() => count(remote) > before, 'playing from details did not retry its poster');
    await page.locator('#detailPanel [data-close]').click();

    await page.locator('[data-page="following"]:visible').click();
    await page.locator('[data-follow-tab="saved"]').click();
    const following = page.locator(`.following-row[data-drama-id="${id}"]`);
    await following.waitFor();
    await page.waitForFunction(id => !document.querySelector(`.following-row[data-drama-id="${id}"] .following-cover img`), id);
    before = count(remote);
    await following.locator(`[data-focus-key="following-play-${id}"]`).click();
    await until(() => count(remote) > before, 'playing from the following list did not retry its poster');
    assert.equal(refreshes, 1);
    assert.equal(repairs, 0, 'cover retries created redundant metadata repairs');
    assert.equal(await page.evaluate(id => window.__coverTestApp.library.get(id).cover, id), cover, 'a transient retry URL replaced the canonical cover');
    for (const address of imageAttempts.filter(address => new URL(address).searchParams.has('_retry'))) {
      const target = new URL(address);
      assert.equal(target.searchParams.get('url'), remote, 'source signature changed');
      assert.equal(target.searchParams.get('dramaId'), id, 'drama identity changed');
    }
    assert.deepEqual(unexpected, []);
    assert.deepEqual(pageErrors, []);
    console.log(JSON.stringify({project, source, engine: process.env.JUKU_BROWSER_ENGINE || 'chromium', mobile: Boolean(process.env.JUKU_MOBILE), refreshes, repairs, blockedImageAttempts: imageAttempts.length, upstreamRequests: 0, screenshots: 0, result: 'passed'}));
  } finally {
    releaseRefresh?.();
    await context.close();
    await browser.close();
  }
})().catch(error => {console.error(error); process.exitCode = 1;});

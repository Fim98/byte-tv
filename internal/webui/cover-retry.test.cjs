const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const source = fs.readFileSync(path.join(__dirname, 'cover-retry.js'), 'utf8');
const modulePromise = import('data:text/javascript;base64,' + Buffer.from(source).toString('base64'));

test('cover retries preserve the exact signed upstream URL and use fresh local requests', async () => {
  const {retryCoverURL} = await modulePromise;
  const upstream = 'https://covers.example.org/text-fixture.jpg?auth=a%2Fb%2B%3D&expires=1900000000&name=a+b';
  const id = 'hongguo:7000000000000000001';
  const canonical = '/api/ui/image?url=' + encodeURIComponent(upstream) + '&dramaId=' + encodeURIComponent(id);
  const requests = Array.from({length: 20}, () => retryCoverURL(canonical));
  assert.equal(new Set(requests).size, requests.length);
  for (const address of requests) {
    assert.ok(address.startsWith(canonical + '&_retry='));
    const target = new URL(address, 'http://localhost');
    assert.equal(target.pathname, '/api/ui/image');
    assert.equal(target.searchParams.get('url'), upstream);
    assert.equal(target.searchParams.get('dramaId'), id);
    assert.equal(target.searchParams.getAll('_retry').length, 1);
  }
});

test('cover retries leave remote addresses and unrelated endpoints unchanged', async () => {
  const {retryCoverURL} = await modulePromise;
  for (const address of ['', undefined, null, '/api/ui/dramas?url=x', '/api/ui/image-other?url=x', 'https://covers.example.org/text?auth=keep', '//covers.example.org/text', 'data:text/plain,fixture']) {
    assert.equal(retryCoverURL(address), address);
  }
});

test('cover retries retain local path parameters and fragments', async () => {
  const {retryCoverURL} = await modulePromise;
  const canonical = '/api/ui/image?path=upload%2Ftext-fixture&source=fixture';
  const target = new URL(retryCoverURL(canonical + '#poster'), 'http://localhost');
  assert.equal(target.hash, '#poster');
  assert.equal(target.searchParams.get('path'), 'upload/text-fixture');
  assert.equal(target.searchParams.get('source'), 'fixture');
  assert.ok(target.searchParams.get('_retry'));
});

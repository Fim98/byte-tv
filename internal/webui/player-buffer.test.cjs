const assert = require('node:assert/strict');
const {test} = require('node:test');
const buffering = require('./player-buffer.js');

const ranges = values => ({length: values.length, start: i => values[i][0], end: i => values[i][1]});
const update = async (_target, _event, signal, action) => {
  if (signal.aborted) throw new DOMException('canceled', 'AbortError');
  action();
};
const signal = () => new AbortController().signal;

test('buffer display measures the current playable range and preserves gaps after seeking', () => {
  const video = {currentTime: 10, duration: 100, buffered: ranges([[0, 25], [50, 90]])};
  assert.equal(buffering.snapshot(video).ahead, 15);
  video.currentTime = 35;
  assert.equal(buffering.snapshot(video).ahead, 0);
  video.currentTime = 60;
  assert.equal(buffering.snapshot(video).ahead, 30);
  const gradient = buffering.gradient(buffering.snapshot(video).ranges, video.duration);
  assert.match(gradient, /#ffffff45 25%/);
  assert.match(gradient, /#ffffff45 50%/);
  assert.equal(buffering.snapshot({currentTime: 0, duration: NaN, buffered: ranges([])}).ahead, 0);
});

test('paused playback can fill a useful buffer; higher rates increase it within a finite window', async () => {
  const video = {currentTime: 0, duration: 240, playbackRate: 1, paused: true, buffered: ranges([[0, 60]])};
  const policy = buffering.create(video, update);
  await policy.waitForSpace(signal());
  assert.ok(policy.goal() > 60);
  const ordinary = policy.goal();
  video.playbackRate = 2;
  assert.ok(policy.goal() > ordinary);
  video.playbackRate = 3;
  assert.ok(policy.goal() <= 180);
});

test('compressed-byte budget reduces buffering for high bitrate media', async () => {
  const low = {currentTime: 0, duration: 240, playbackRate: 1, buffered: ranges([[0, 30]])};
  const high = {...low};
  const lowPolicy = buffering.create(low, update), highPolicy = buffering.create(high, update);
  const buffer = video => ({get buffered() {return video.buffered;}, appendBuffer() {}});
  await lowPolicy.append(buffer(low), {byteLength: 1024 * 1024}, signal());
  await highPolicy.append(buffer(high), {byteLength: 40 * 1024 * 1024}, signal());
  assert.ok(highPolicy.goal() < lowPolicy.goal());
  assert.ok(highPolicy.goal() <= 48 / 40 * 30);
});

test('quota recovery releases played media and retries the exact pending chunk', async () => {
  const video = {currentTime: 35, duration: 240, playbackRate: 1, buffered: ranges([[0, 70]])};
  const policy = buffering.create(video, update);
  const originalGoal = policy.goal(), chunk = new Uint8Array([1, 2, 3]);
  const attempts = [], removed = [];
  let full = true;
  const buffer = {
    get buffered() {return video.buffered;},
    appendBuffer(data) {
      attempts.push(data);
      if (full) {full = false; throw new DOMException('full', 'QuotaExceededError');}
      video.buffered = ranges([[33, 75]]);
    },
    remove(start, end) {removed.push([start, end]); video.buffered = ranges([[end, 70]]);}
  };
  await policy.append(buffer, chunk, signal());
  assert.deepEqual(attempts, [chunk, chunk]);
  assert.ok(removed.every(([, end]) => end < video.currentTime));
  assert.ok(policy.goal() < originalGoal);
});

test('switching episodes cancels a quota wait while paused without appending stale bytes', async () => {
  const video = {currentTime: 0, duration: 240, playbackRate: 1, paused: true, buffered: ranges([[0, 40]])};
  const policy = buffering.create(video, update), controller = new AbortController();
  let attempts = 0;
  const buffer = {get buffered() {return video.buffered;}, appendBuffer() {attempts++; throw new DOMException('full', 'QuotaExceededError');}};
  const pending = policy.append(buffer, new Uint8Array([1]), controller.signal);
  const rejected = assert.rejects(pending, {name: 'AbortError'});
  await new Promise(resolve => setTimeout(resolve, 10));
  controller.abort();
  await rejected;
  assert.equal(attempts, 1);
});

test('closing a fully buffered player aborts backpressure immediately', async () => {
  const video = {currentTime: 0, duration: 240, playbackRate: 1, buffered: ranges([[0, 150]])};
  const controller = new AbortController();
  const pending = buffering.create(video, update).waitForSpace(controller.signal);
  const rejected = assert.rejects(pending, {name: 'AbortError'});
  controller.abort();
  await rejected;
});

test('media prefetch waits for a complete current episode; metadata can prepare earlier', () => {
  const state = {duration: 120, position: 80, rate: 1, ahead: 40, complete: true};
  assert.equal(buffering.prefetchReady(state), true);
  assert.equal(buffering.prefetchReady({...state, ahead: 10}), false);
  assert.equal(buffering.prefetchReady({...state, complete: false}), false);
  assert.equal(buffering.prefetchReady({...state, position: 60, ahead: 15, complete: false, metadataOnly: true}), true);
  assert.equal(buffering.prefetchReady({...state, position: 60, ahead: 5, complete: false, metadataOnly: true}), false);
  assert.equal(buffering.prefetchReady({...state, position: 30, ahead: 90, rate: 2}), true);
});

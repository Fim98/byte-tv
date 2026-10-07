const assert = require('node:assert/strict');
const {test} = require('node:test');
const createLoader = require('./player-media.js');

function fixture() {
  class Video extends EventTarget {
    src = '';
    error = null;
    canPlayType() {return 'probably';}
    load() {}
  }
  const instances = [], failures = [];
  class Hls {
    static Events = {ERROR:'error',MEDIA_ATTACHED:'attached'};
    static ErrorTypes = {NETWORK_ERROR:'network'};
    static isSupported() {return true;}
    handlers = new Map();
    destroyed = 0;
    constructor(options) {this.options=options;instances.push(this);}
    on(type,handler) {this.handlers.set(type,handler);}
    attachMedia(video) {this.video=video;}
    loadSource(url) {this.url=url;}
    destroy() {this.destroyed++;}
    emit(type,data) {this.handlers.get(type)?.(type,data);}
  }
  const video=new Video();
  return {Hls,video,instances,failures,loader:createLoader({Hls},video,error=>failures.push(error))};
}

test('changing the media cancels a pending load and ignores the old HLS callbacks', async () => {
  const f=fixture(), signal=new AbortController().signal;
  const first=f.loader.load({player:'hls',url:'/old.m3u8'},false,signal);
  const canceled=assert.rejects(first,{name:'AbortError'});
  const old=f.instances[0];
  const next=f.loader.load({player:'hls',url:'/new.m3u8'},false,signal,7.25);
  old.emit('error',{fatal:true,type:'network'});
  old.emit('attached');
  const current=f.instances[1];
  current.emit('attached');
  assert.equal(current.url,'/new.m3u8');
  assert.equal(current.options.startPosition,7.25);
  assert.equal(old.url,undefined);
  f.video.dispatchEvent(new Event('loadedmetadata'));
  await Promise.all([next,canceled]);
  assert.equal(old.destroyed,1);
  assert.deepEqual(f.failures,[]);
  f.loader.destroy();
});

test('synchronous HLS initialization failures clean up and a later load can succeed', async () => {
  const f=fixture(), signal=new AbortController().signal;
  f.Hls.prototype.attachMedia=function(){throw new Error('fixture attach failure');};
  await assert.rejects(f.loader.load({player:'hls',url:'/failed.m3u8'},false,signal),error=>error.playbackDecode===true);
  assert.equal(f.instances[0].destroyed,1);
  const next=f.loader.load({player:'mp4',url:'/ready.mp4',mime:'video/mp4'},false,signal);
  f.video.dispatchEvent(new Event('loadedmetadata'));
  await next;
  assert.equal(f.video.src,'/ready.mp4');
  f.loader.destroy();
});

test('aborting an in-flight load destroys its HLS session without reporting a playback failure', async () => {
  const f=fixture(), abort=new AbortController();
  const pending=f.loader.load({player:'hls',url:'/canceled.m3u8'},false,abort.signal);
  const rejected=assert.rejects(pending,{name:'AbortError'});
  abort.abort();
  await rejected;
  const old=f.instances[0];
  old.emit('error',{fatal:true,type:'network'});
  assert.equal(old.destroyed,1);
  assert.deepEqual(f.failures,[]);
  f.loader.destroy();
});

test('fatal HLS errors after metadata retain the network versus decode distinction', async () => {
  const f=fixture();
  const pending=f.loader.load({player:'hls',url:'/ready.m3u8'},false,new AbortController().signal);
  f.video.dispatchEvent(new Event('loadedmetadata'));
  await pending;
  f.instances[0].emit('error',{fatal:false,type:'network'});
  assert.deepEqual(f.failures,[]);
  f.instances[0].emit('error',{fatal:true,type:'network'});
  assert.equal(f.failures[0].playbackNetwork,true);
  assert.equal(f.failures[0].playbackDecode,false);
  f.loader.destroy();
});

'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const settle = async () => { for (let n = 0; n < 12; n++) await Promise.resolve(); };
function player(metadata = {}) {
  let now = 0, nextTick, active = 0, maximum = 0;
  const pending = [], painted = [], revoked = [], elements = new Map();
  class Element {
    constructor() { this.value = ''; this.handlers = {}; this.paused = true; this.currentTime = 0; this.duration = 10; }
    addEventListener(name, handler) { this.handlers[name] = handler; }
    removeAttribute(name) { delete this[name]; }
    replaceChildren() {}
    add() {}
    pause() { this.paused = true; }
    play() { this.paused = false; return Promise.resolve(); }
    load() {}
    set src(value) { this.source = value; if (this === elements.get('frame')) painted.push(value); }
    get src() { return this.source; }
  }
  for (const id of ['segments', 'frame', 'play', 'seek', 'audio', 'status', 'position', 'download']) elements.set(id, new Element());
  const context = vm.createContext({
    document: {getElementById: id => elements.get(id)},
    window: {addEventListener() {}},
    Option: class {}, AbortController,
    performance: {now: () => now}, requestAnimationFrame: fn => { nextTick = fn; },
    URL: {createObjectURL: blob => blob.url, revokeObjectURL: url => revoked.push(url)},
    fetch: async (url, options) => {
      if (url === '/recordings/list') return {ok: true, json: async () => []};
      if (url.endsWith('/index.json')) return {ok: true, json: async () => ({id: url.split('/')[2], audio: false, audio_samples: 0, video_ms: Array.from({length: 21}, (_, i) => i * 100), ...metadata})};
      active++; maximum = Math.max(maximum, active);
      let resolveBlob;
      const blob = new Promise(resolve => { resolveBlob = resolve; });
      pending.push({url, signal: options.signal, finish() { active--; resolveBlob({url}); }});
      // Fetch headers arrive immediately; decoding may still finish after an abort.
      return {ok: true, blob: () => blob};
    },
  });
  vm.runInContext(fs.readFileSync(__dirname + '/recordings.js', 'utf8'), context);
  return {
    elements, pending, painted, revoked,
    get maximum() { return maximum; },
    async select(id) { elements.get('segments').value = id; await elements.get('segments').handlers.change(); await settle(); },
    async tick(ms) { now = ms; nextTick(); await settle(); },
    async finish() { const request = pending.shift(); assert.ok(request, 'a frame request is pending'); request.finish(); await settle(); return request; },
    async seek(ms) { elements.get('seek').value = String(ms); elements.get('seek').handlers.input(); await settle(); },
    play() { elements.get('play').handlers.click(); },
  };
}

test('150ms frame downloads keep painting during 100ms frame playback', async () => {
  const p = player(); await p.select('first'); p.play();
  for (let t = 150; t <= 900; t += 150) { await p.tick(t); await p.finish(); await p.tick(t); }
  assert.ok(p.painted.length >= 5, `painted ${p.painted.length} frames`);
  const indices = p.painted.map(url => Number(url.split('/').at(-1)));
  assert.deepEqual(indices, [...indices].sort((a, b) => a - b));
  assert.equal(p.maximum, 1);
  assert.equal(p.revoked.length, p.painted.length - 1);
});

test('segment change rejects old delayed blobs and keeps one request in flight', async () => {
  const p = player(); await p.select('old'); const old = p.pending[0];
  await p.select('new'); assert.equal(old.signal.aborted, true);
  assert.equal(p.pending.length, 1);
  await p.finish(); assert.deepEqual(p.painted, []);
  await p.tick(1); await p.finish();
  assert.deepEqual(p.painted, ['/recordings/new/frame/0']); assert.equal(p.maximum, 1);
});

test('backward seek rejects delayed pre-seek blobs and paints the requested position', async () => {
  const p = player(); await p.select('first'); await p.finish();
  await p.seek(1000); await p.tick(1); const old = p.pending[0];
  await p.seek(200); assert.equal(old.signal.aborted, true);
  await p.finish(); assert.equal(p.painted.length, 1);
  await p.tick(2); await p.finish();
  assert.equal(p.painted.at(-1), '/recordings/first/frame/2'); assert.equal(p.maximum, 1);
});

test('audio enabled archive with zero samples hides empty WAV controls', async () => {
  const p = player({audio: true, audio_samples: 0}); await p.select('silent');
  assert.equal(p.elements.get('audio').hidden, true);
  assert.equal(p.elements.get('download').hidden, true);
  assert.equal(p.elements.get('audio').src, undefined);
});

const {test} = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');

function setup() {
  const timers = new Map(); let timerId = 0, urlId = 0;
  const sockets = [], decodes = [], revoked = [], requests = [];
  const listeners = {};
  const elements = Object.fromEntries(['audio','audio-state','gain','gain-value','connection','video'].map(id => [id, {value:'1', textContent:'', addEventListener(){}}]));
  const document = {hidden:false, getElementById:id => elements[id], addEventListener:(name,fn) => listeners[name] = fn};
  class WS { static OPEN = 1; constructor(url) {this.url=url; this.readyState=1; this.sent=[]; sockets.push(this)} send(x){this.sent.push(x)} close(){this.readyState=3} }
  class Image { decode(){return new Promise((resolve,reject)=>decodes.push({resolve,reject,src:this.src}))} }
  const URL = {createObjectURL:()=>`blob:${++urlId}`, revokeObjectURL:x=>revoked.push(x)};
  const fetch = (url, options) => {if(url==='/health') return Promise.resolve({ok:true,json:async()=>({online:true})}); return new Promise((resolve,reject)=>{requests.push({resolve,reject,signal:options.signal}); options.signal.addEventListener('abort',()=>reject(new Error('abort')))})};
  const ctx = {document, window:{addEventListener:(name,fn)=>listeners[name]=fn}, location:{protocol:'http:',host:'localhost:8080'}, WebSocket:WS, Image, URL, Blob, ArrayBuffer, DataView, BigInt, AbortController, fetch, setTimeout:(fn,ms)=>{const id=++timerId; timers.set(id,{fn,ms}); return id}, clearTimeout:id=>timers.delete(id)};
  vm.runInNewContext(fs.readFileSync(__dirname+'/viewer.js','utf8'),ctx);
  const fire = ms => {for(const [id,t] of [...timers]) if(t.ms===ms){timers.delete(id);t.fn()}};
  const frame = n => {const a=new ArrayBuffer(10); const d=new DataView(a);d.setUint8(0,1);d.setBigUint64(1,BigInt(n),true);d.setUint8(9,255);return a};
  const flush = async()=>{for(let i=0;i<8;i++) await Promise.resolve()};
  return {sockets,decodes,revoked,requests,elements,document,listeners,fire,frame,flush};
}
test('queues latest frame during decode and acknowledges painted generations', async()=>{const h=setup(), s=h.sockets[0]; s.onmessage({data:h.frame(1)});s.onmessage({data:h.frame(2)});s.onmessage({data:h.frame(3)});assert.equal(h.decodes.length,1);h.decodes[0].resolve();await h.flush();assert.equal(h.elements.video.src,'blob:1');assert.deepEqual(s.sent,['1']);assert.equal(h.decodes.length,2);h.decodes[1].resolve();await h.flush();assert.deepEqual(s.sent,['1','3']);});
test('stale decode cannot paint after reconnect',async()=>{const h=setup(),s=h.sockets[0];s.onmessage({data:h.frame(1)});s.onclose();h.fire(5000);h.decodes[0].resolve();await h.flush();assert.equal(h.elements.video.src,undefined);});
test('fallback request is aborted when websocket frame arrives',async()=>{const h=setup();h.fire(5000);assert.equal(h.requests.length,1);h.fire(5000);const s=h.sockets[1];s.onmessage({data:h.frame(1)});assert.equal(h.requests[0].signal.aborted,true);});
test('visibility reconnects and invalid payload does not paint',async()=>{const h=setup();h.document.hidden=true;h.listeners.visibilitychange();assert.equal(h.sockets[0].readyState,3);h.document.hidden=false;h.listeners.visibilitychange();const s=h.sockets[1];s.onmessage({data:new ArrayBuffer(9)});await h.flush();assert.equal(h.decodes.length,0);assert.equal(h.elements.video.src,undefined);});
test('page restored from back-forward cache reconnects', async()=>{const h=setup();h.listeners.pagehide();h.listeners.pageshow({persisted:true});assert.equal(h.sockets.length,2);h.sockets[1].onmessage({data:h.frame(1)});h.decodes[0].resolve();await h.flush();assert.equal(h.elements.video.src,'blob:1');});
test('repeated interruptions release pending decode URLs once', async()=>{const h=setup();h.sockets[0].onmessage({data:h.frame(1)});h.document.hidden=true;h.listeners.visibilitychange();assert.deepEqual(h.revoked,['blob:1']);h.document.hidden=false;h.listeners.visibilitychange();h.sockets[1].onmessage({data:h.frame(2)});h.listeners.pagehide();assert.deepEqual(h.revoked,['blob:1','blob:2']);h.decodes[0].resolve();h.decodes[1].resolve();await h.flush();assert.deepEqual(h.revoked,['blob:1','blob:2']);assert.equal(h.elements.video.src,undefined);});

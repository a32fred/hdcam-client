const button = document.getElementById('audio');
const state = document.getElementById('audio-state');
const gainControl = document.getElementById('gain');
const gainValue = document.getElementById('gain-value');
let activeGain;
gainControl.addEventListener('input', () => {
  gainValue.textContent = gainControl.value + '×';
  if (activeGain) activeGain.gain.value = Number(gainControl.value);
});
let stop;
button.addEventListener('click', async () => {
  if (stop) { stop(); return; }
  const abort = new AbortController();
  const context = new AudioContext();
  const gain = context.createGain(); gain.gain.value = Number(gainControl.value); gain.connect(context.destination); activeGain = gain;
  stop = () => { abort.abort(); context.close(); stop = undefined; button.textContent = 'Ouvir áudio'; };
  const finish = stop;
  button.textContent = 'Parar áudio';
  state.textContent = 'Conectando…';
  try {
    await context.resume();
    const response = await fetch('/audio', {signal: abort.signal, cache: 'no-store'});
    if (!response.ok) throw new Error('Áudio indisponível; o servidor precisa iniciar com --audio.');
    const reader = response.body.getReader();
    let next = 0, pending, lastLevel = 0;
    state.textContent = 'Áudio ao vivo; sem sincronização garantida com o vídeo.';
    while (!abort.signal.aborted) {
      const {value, done} = await reader.read();
      if (done) break;
      let bytes = value;
      if (pending !== undefined) { const combined = new Uint8Array(value.length + 1); combined[0] = pending; combined.set(value, 1); bytes = combined; pending = undefined; }
      if (bytes.length % 2) { pending = bytes[bytes.length - 1]; bytes = bytes.subarray(0, bytes.length - 1); }
      if (!bytes.length) continue;
      // Bound each scheduled buffer and skip delayed batches after a network stall.
      if (next > context.currentTime + 0.5) continue;
      if (bytes.length > 8000) bytes = bytes.subarray(bytes.length - 8000);
      const buffer = context.createBuffer(1, bytes.length / 2, 8000);
      const samples = buffer.getChannelData(0);
      const data = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
      let peak = 0;
      for (let i = 0; i < samples.length; i++) { samples[i] = data.getInt16(i * 2, true) / 32768; peak = Math.max(peak, Math.abs(samples[i])); }
      if (context.currentTime - lastLevel > 0.3) {
        state.textContent = 'Áudio recebido: ' + (peak ? Math.round(20 * Math.log10(peak)) + ' dBFS' : 'silêncio digital') + '; sem sincronização garantida com o vídeo.';
        lastLevel = context.currentTime;
      }
      const source = context.createBufferSource(); source.buffer = buffer; source.connect(gain);
      next = Math.max(next, context.currentTime + 0.03); source.start(next); next += buffer.duration;
    }
    if (!abort.signal.aborted) state.textContent = 'Áudio desconectado; clique para reconectar.';
  } catch (error) {
    if (!abort.signal.aborted) state.textContent = error.message;
  } finally { if (stop === finish) finish(); }
});

const connection = document.getElementById('connection');
const video = document.getElementById('video');
async function health() {
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), 4000);
  try {
    const response = await fetch('/health', {cache: 'no-store', signal: abort.signal});
    const data = await response.json();
    const online = response.ok && data.online;
    connection.textContent = online ? 'Câmera online' : 'Câmera sem vídeo recente; a última imagem pode estar congelada.';
  } catch (_) {
    connection.textContent = 'Sem conexão com o visualizador; tentando novamente…';
  } finally { clearTimeout(timer); setTimeout(health, 5000); }
}
let epoch = 0, socket, startupTimer, retryTimer, pollTimer, pollAbort, lastImage, etag;
let stopped = false, fallback = false, received = 0n, pending, decoding = false;
const activeDecodes = new Map();
function cancelDecodes() {
  for (const [image, detached] of activeDecodes) {
    detached.src = '';
    URL.revokeObjectURL(image);
  }
  activeDecodes.clear();
}

function cancelTimers() {
  clearTimeout(startupTimer); clearTimeout(retryTimer); clearTimeout(pollTimer);
}
function cancelPoll() {
  clearTimeout(pollTimer);
  if (pollAbort) { pollAbort.abort(); pollAbort = undefined; }
}
function invalidate() {
  epoch++;
  cancelDecodes();
  pending = undefined; decoding = false; received = 0n;
  cancelTimers(); cancelPoll();
  const old = socket; socket = undefined;
  if (old) old.close();
}
function replaceImage(image) {
  video.src = image;
  if (lastImage) URL.revokeObjectURL(lastImage);
  lastImage = image;
}
async function paint(blob, generation, current, ws) {
  const image = URL.createObjectURL(blob);
  const detached = new Image(); activeDecodes.set(image, detached); detached.src = image;
  try {
    await detached.decode();
    if (current !== epoch || stopped || document.hidden || (ws && socket !== ws)) return;
    replaceImage(image);
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(generation.toString());
    return true;
  } catch (_) {
    return false;
  } finally {
    if (activeDecodes.delete(image) && lastImage !== image) URL.revokeObjectURL(image);
  }
}
async function decodeFrame(data, generation, current, ws) {
  decoding = true;
  await paint(new Blob([data.slice(9)], {type: 'image/jpeg'}), generation, current, ws);
  if (current !== epoch || socket !== ws) return;
  decoding = false;
  if (pending) {
    const next = pending; pending = undefined;
    decodeFrame(next.data, next.generation, current, ws);
  }
}
async function poll(current) {
  if (!fallback || current !== epoch || stopped || document.hidden) return;
  const abort = new AbortController(); pollAbort = abort;
  const timeout = setTimeout(() => abort.abort(), 4000);
  let delay = 500;
  try {
    const headers = etag ? {'If-None-Match': etag} : {};
    const response = await fetch('/snapshot', {cache:'no-store', headers, signal:abort.signal});
    if (response.status === 304) delay = 60;
    if (response.ok) {
      const blob = await response.blob();
      if (current !== epoch || abort.signal.aborted || !fallback) return;
      if (await paint(blob, undefined, current)) {
        etag = response.headers.get('ETag'); delay = 0;
      }
    }
  } catch (_) {
  } finally {
    clearTimeout(timeout);
    if (pollAbort === abort) pollAbort = undefined;
    if (fallback && current === epoch && !stopped && !document.hidden) pollTimer = setTimeout(() => poll(current), delay);
  }
}
function startFallback() {
  if (stopped || document.hidden) return;
  invalidate(); fallback = true;
  const current = epoch;
  poll(current);
  retryTimer = setTimeout(connect, 5000);
}
function connect() {
  if (stopped || document.hidden) return;
  clearTimeout(retryTimer);
  const current = epoch;
  let ws;
  try { ws = new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/live'); }
  catch (_) { if (!fallback) startFallback(); else retryTimer = setTimeout(connect, 5000); return; }
  socket = ws; ws.binaryType = 'arraybuffer';
  startupTimer = setTimeout(() => {
    if (socket !== ws) return;
    if (fallback) { socket = undefined; ws.close(); retryTimer = setTimeout(connect, 5000); }
    else startFallback();
  }, 5000);
  ws.onmessage = event => {
    const data = event.data;
    if (socket !== ws || !(data instanceof ArrayBuffer) || data.byteLength <= 9 || data.byteLength > 4 * 1024 * 1024 + 9) return;
    const view = new DataView(data);
    if (view.getUint8(0) !== 1) return;
    const generation = view.getBigUint64(1, true);
    if (generation <= received) return;
    if (fallback) {
      cancelPoll(); cancelDecodes(); fallback = false; epoch++;
      pending = undefined; decoding = false; received = 0n;
    }
    clearTimeout(startupTimer);
    received = generation;
    if (decoding) pending = {data, generation};
    else decodeFrame(data, generation, epoch, ws);
  };
  ws.onerror = ws.onclose = () => {
    if (socket !== ws) return;
    if (fallback) {
      socket = undefined; clearTimeout(startupTimer); ws.close();
      retryTimer = setTimeout(connect, 5000);
    } else startFallback();
  };
}
document.addEventListener('visibilitychange', () => {
  if (stopped) return;
  invalidate(); fallback = false;
  if (!document.hidden) connect();
});
window.addEventListener('pagehide', () => {
  stopped = true; invalidate();
  if (lastImage) { URL.revokeObjectURL(lastImage); lastImage = undefined; }
});
window.addEventListener('pageshow', event => {
  if (!event.persisted) return;
  stopped = false; invalidate(); fallback = false;
  if (!document.hidden) connect();
});
health();
connect();

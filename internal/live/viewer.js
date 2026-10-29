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
let lastImage, etag;
async function frame() {
  const abort = new AbortController();
  const timer = setTimeout(() => abort.abort(), 4000);
  let retry = 500;
  try {
    const headers = etag ? {'If-None-Match': etag} : {};
    const response = await fetch('/snapshot', {cache: 'no-store', headers, signal: abort.signal});
    if (response.status === 304) retry = 60;
    if (response.ok) {
      const blob = await response.blob();
      const image = URL.createObjectURL(blob);
      video.src = image;
      try { await video.decode(); } catch (_) { URL.revokeObjectURL(image); return; }
      if (lastImage) URL.revokeObjectURL(lastImage);
      lastImage = image;
      etag = response.headers.get('ETag');
      retry = 0;
    }
  } catch (_) { /* Retry one request at a time; never queue stale frames. */ }
  finally { clearTimeout(timer); setTimeout(frame, retry); }
}
health();
frame();

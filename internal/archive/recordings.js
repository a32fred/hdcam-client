'use strict';
const byID = id => document.getElementById(id);
const select = byID('segments'), image = byID('frame'), play = byID('play'), seek = byID('seek'), audio = byID('audio'), status = byID('status');
let meta = null, generation = 0, controller = new AbortController(), frameController = null, objectURL = null;
let running = false, position = 0, origin = 0, duration = 0, requested = -1, lastPaintedFrame = -1, inflight = false;
function invalidateFrames() { generation++; requested = -1; lastPaintedFrame = -1; frameController?.abort(); }
const clock = ms => `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}`;
function stop() { running = false; audio.pause(); play.textContent = 'Reproduzir'; }
function paintPosition() { seek.value = String(position); byID('position').textContent = `${clock(position)} / ${clock(duration)}`; }
function syncAudio() {
  if (!meta?.audio || !running || position < meta.audio_start_ms) { audio.pause(); return; }
  const target = (position - (meta.audio_start_ms || 0)) / 1000;
  if (target >= audio.duration) { audio.pause(); return; }
  if (Math.abs(audio.currentTime - target) > .4) audio.currentTime = target;
  if (audio.paused) audio.play().catch(() => {});
}
function frameAt(ms) {
  let lo = 0, hi = meta.video_ms.length;
  while (lo < hi) { const mid = (lo + hi) >>> 1; if (meta.video_ms[mid] <= ms) lo = mid + 1; else hi = mid; }
  return Math.max(0, lo - 1);
}
async function loadFrame() {
  if (!meta || inflight) return;
  const index = frameAt(position), token = generation;
  if (requested === index) return;
  requested = index; inflight = true;
  const localController = new AbortController(); frameController = localController;
  try {
    const response = await fetch(`/recordings/${encodeURIComponent(meta.id)}/frame/${index}`, {signal: localController.signal});
    if (!response.ok) throw new Error('Quadro indisponível; a gravação pode ter sido removida.');
    const blob = await response.blob();
    if (token !== generation || index < lastPaintedFrame) return;
    if (objectURL) URL.revokeObjectURL(objectURL);
    objectURL = URL.createObjectURL(blob); image.src = objectURL; image.hidden = false; lastPaintedFrame = index;
  } catch (error) { if (token === generation && error.name !== 'AbortError') { status.textContent = error.message; stop(); } }
  finally { if (frameController === localController) { inflight = false; frameController = null; } }
}
select.addEventListener('change', async () => {
  stop(); invalidateFrames(); controller.abort(); controller = new AbortController();
  meta = null; position = 0; duration = 0; requested = -1; play.disabled = seek.disabled = true;
  audio.removeAttribute('src'); audio.load(); audio.hidden = true; byID('download').hidden = true;
  image.removeAttribute('src'); image.hidden = true; if (objectURL) URL.revokeObjectURL(objectURL); objectURL = null;
  paintPosition(); const token = generation; if (!select.value) return;
  try {
    const response = await fetch(`/recordings/${encodeURIComponent(select.value)}/index.json`, {signal: controller.signal});
    if (!response.ok) throw new Error('Gravação indisponível.');
    const data = await response.json(); if (token !== generation) return;
    if (!Array.isArray(data.video_ms) || !data.video_ms.length || data.video_ms.length > 100000) throw new Error('Índice inválido.');
    meta = data; meta.audio_start_ms ||= 0; meta.audio = meta.audio && meta.audio_samples > 0;
    duration = Math.max(meta.video_ms.at(-1), meta.audio_start_ms + meta.audio_samples / 8);
    seek.max = String(duration); play.disabled = seek.disabled = false; status.textContent = 'Pronta para reproduzir.';
    if (meta.audio) { audio.src = `/recordings/${encodeURIComponent(meta.id)}/audio.wav`; audio.hidden = false; byID('download').href = audio.src; byID('download').hidden = false; }
    paintPosition(); loadFrame();
  } catch (error) { if (token === generation && error.name !== 'AbortError') status.textContent = error.message; }
});
play.addEventListener('click', () => { if (running) stop(); else { if (position >= duration) { position = 0; invalidateFrames(); } origin = performance.now() - position; running = true; play.textContent = 'Pausar'; syncAudio(); } });
seek.addEventListener('input', () => { invalidateFrames(); position = Number(seek.value); origin = performance.now() - position; paintPosition(); syncAudio(); loadFrame(); });
function tick() { if (running && meta) { position = Math.min(duration, performance.now() - origin); paintPosition(); syncAudio(); if (position >= duration) stop(); } loadFrame(); requestAnimationFrame(tick); }
requestAnimationFrame(tick);
fetch('/recordings/list').then(response => { if (!response.ok) throw new Error('Não foi possível listar as gravações.'); return response.json(); }).then(list => {
  select.replaceChildren(new Option('Selecione uma gravação', ''));
  for (const item of list) { const ms = Math.max(0, Date.parse(item.ended) - Date.parse(item.started)); select.add(new Option(`${new Date(item.started).toLocaleString('pt-BR')} · ${clock(ms)} · ${(item.bytes / 1048576).toFixed(2)} MB`, item.id)); }
  status.textContent = list.length ? 'Selecione um segmento concluído.' : 'Nenhuma gravação concluída.';
}).catch(error => { status.textContent = error.message; });
window.addEventListener('pagehide', () => { stop(); controller.abort(); frameController?.abort(); if (objectURL) URL.revokeObjectURL(objectURL); });

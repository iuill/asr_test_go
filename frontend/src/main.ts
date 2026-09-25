import type { main } from '../wailsjs/go/models';
import { AutoSaveSession } from './autosave-session';
import { TranscriptStore, formatEntry } from './transcript-store';
import { encodeWav, encodeLivePCM, encodeGooglePCM } from './audio-encoding';
import { microphoneConstraints, SpeechSegmenter, type CaptureMode } from './speech-segmenter';
import './style.css';
import appIcon from './assets/app-icon.svg';
import { AppendGoogleStream, AppendLive, BeginAutoSave, CommitLive, EndAutoSave, EndGoogleStream, GetInfo, LogDiagnostic, SaveTranscript, SetAutoSave, SetDebugLogging, SetShowTimestamps, StartGoogleStream, StartLive, StopLive, Transcribe, WriteAutoSave } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

type Info = main.AppInfo;
const root = document.querySelector<HTMLDivElement>('#app')!;
root.innerHTML = `
  <header><div class="brand"><img src="${appIcon}" alt=""><h1>ASR Studio</h1></div><span id="version" class="badge">v0.0.1</span></header>
  <main>
    <section class="panel intro"><div><h2>マイクから文字起こし</h2><p>GPT LiveとGoogle V1は発話中に途中結果を表示します。Chirp 3は途中結果が返った場合に表示します。OpenAI GPT TranscribeとAzureは発話後に送信します。APIの利用料金が発生します。</p></div></section>
    <section class="panel"><div class="section-head"><h2>入力マイク</h2><button id="refreshMics" class="quiet">マイク一覧を更新</button></div><div class="mic-row"><div class="mic-control"><select id="microphone" aria-label="入力マイク"><option value="">システム既定のマイク</option></select><span id="activeMic" class="hint">録音開始後に使用マイクを表示します</span></div></div><label class="mic-mode"><input id="distantVoices" type="checkbox"> 離れた声を拾う <span class="hint">周囲の音も拾いやすくなります</span></label></section>
    <details id="modelsPanel" class="panel" open><summary><strong>モデル</strong><span id="modelSummary" class="hint"></span></summary><div class="model-detail"><div class="section-head"><span class="hint">利用するモデルを選択</span><button id="reload" class="quiet">設定を再読込</button></div><p id="configMessage" class="hint"></p><div id="models" class="models"></div></div></details>
    <div class="diagnostics" aria-label="表示と保存の設定">
      <div class="diagnostic-option"><label><input id="showTimestamps" type="checkbox"> 文字起こしに時刻を表示</label><span class="hint">結果と保存ファイルに反映</span></div>
      <div class="diagnostic-option"><label><input id="autoSave" type="checkbox"> 文字起こし結果を自動保存</label><span id="autoSaveStatus" class="hint"></span></div>
      <div class="diagnostic-option"><label><input id="debugLogging" type="checkbox"> デバッグログを保存</label><span id="logStatus" class="hint"></span></div>
    </div>
    <section class="toolbar"><button id="start" class="primary">録音を開始</button><button id="stop" disabled>停止</button><canvas id="spectrum" class="spectrum" width="176" height="42" aria-label="マイク入力のスペクトル"></canvas><button id="clear" class="quiet">結果を消去</button><button id="save" class="quiet">テキスト保存</button><span id="status">待機中</span></section>
    <section id="results" class="results"></section>
  </main><footer>音声は選択したクラウドAPIへ送信されます。認証情報はEXE横の設定ファイルから読み込みます。</footer>`;

const $ = (id: string) => document.getElementById(id)!;
let info: Info;
let stream: MediaStream | null = null;
let context: AudioContext | null = null;
let processor: ScriptProcessorNode | null = null;
let source: MediaStreamAudioSourceNode | null = null;
let analyser: AnalyserNode | null = null;
let silentGain: GainNode | null = null;
let animationFrame = 0;
let segmenter: SpeechSegmenter | null = null;
let requestChains = new Map<string, Promise<void>>();
let liveQueue: Promise<void> = Promise.resolve();
let liveActive = false;
let liveFailed = false;
let liveRotationTimer: number | null = null;
type GoogleModelID = 'google-v1' | 'google-chirp-3';
type GoogleSession = { id: string; queue: Promise<void>; failed: boolean };
const googleActive = new Map<GoogleModelID, GoogleSession>();
const googlePending = new Set<Promise<void>>();
const googlePartial = new Map<GoogleModelID, Map<string, string>>();
const googleFinals = new Map<string, Set<number>>();
let googleAudioFrames: Float32Array[] = [];
let googleAudioSamples = 0;
let googleRotationTimer: number | null = null;
let recordingSerial = 0;
let googleTurnSerial = 0;
type RecordingPhase = 'idle' | 'starting' | 'recording' | 'stopping';
let phase: RecordingPhase = 'idle';
const selected = new Set<string>();
const transcripts = new TranscriptStore();
const autoSave = new AutoSaveSession({ begin: BeginAutoSave, write: WriteAutoSave, end: EndAutoSave }, event => {
  if (event.type === 'error') setAutoSaveStatus(`自動保存エラー: ${String(event.error)}`);
  else if (event.type === 'started') setAutoSaveStatus(`保存先: transcripts/${event.path.split(/[\\/]/).pop()}`);
  else setAutoSaveStatus(`自動保存済み ${new Date().toLocaleTimeString()} · ${event.path.split(/[\\/]/).pop()}`);
});
const livePartial = new Map<string, string>();
const highlightUntil = new Map<string, number>();

async function refreshMicrophones(requestPermission: boolean) {
  if (!navigator.mediaDevices?.enumerateDevices) { $('activeMic').textContent = 'この環境ではマイク一覧を取得できません'; return; }
  if (requestPermission && !stream) {
    const probe = await navigator.mediaDevices.getUserMedia({ audio: true });
    probe.getTracks().forEach(track => track.stop());
  }
  const devices = (await navigator.mediaDevices.enumerateDevices()).filter(device => device.kind === 'audioinput');
  const dropdown = $('microphone') as HTMLSelectElement;
  const preferred = dropdown.value || localStorage.getItem('asr-microphone-id') || '';
  dropdown.replaceChildren();
  const fallback = new Option('システム既定のマイク', ''); dropdown.add(fallback);
  devices.filter(device => device.deviceId !== 'default' && device.deviceId !== 'communications').forEach((device, index) => {
    dropdown.add(new Option(device.label || `マイク ${index + 1}（権限許可後に名前を表示）`, device.deviceId));
  });
  dropdown.value = Array.from(dropdown.options).some(option => option.value === preferred) ? preferred : '';
}

function showTranscript(id: string, highlightNew = false, emptyMessage = '結果がここに表示されます') {
  const target = document.getElementById(`text-${id}`);
  if (!target) return;
  const followTail = target.scrollHeight - target.scrollTop - target.clientHeight <= 24;
  const entries = transcripts.entries(id);
  const partial = id === 'gpt-live-transcribe'
    ? Array.from(livePartial.values()).join(' ')
    : Array.from(googlePartial.get(id as GoogleModelID)?.values() || []).join(' ');
  if (highlightNew) {
    const until = Date.now() + 3000;
    highlightUntil.set(id, until);
    window.setTimeout(() => {
      if (highlightUntil.get(id) !== until) return;
      highlightUntil.delete(id);
      document.getElementById(`text-${id}`)?.querySelector('.transcript-line-new')?.classList.remove('transcript-line-new');
    }, 3000);
  }
  const lines = entries.map((entry, index) => {
    const line = document.createElement('div');
    line.className = 'transcript-line' + (highlightUntil.has(id) && index === entries.length - 1 ? ' transcript-line-new' : '');
    line.textContent = formatEntry(entry, info.showTimestamps);
    return line;
  });
  if (partial) lines.push(createPartialLine(partial));
  if (!lines.length) target.textContent = emptyMessage;
  else target.replaceChildren(...lines);
  if (followTail) target.scrollTop = target.scrollHeight;
  else if (highlightNew) $(`jump-${id}`).hidden = false;
}

function createPartialLine(text: string): HTMLDivElement {
  const line = document.createElement('div'); line.className = 'transcript-partial';
  const meta = document.createElement('div'); meta.className = 'transcript-partial-meta';
  const label = document.createElement('span'); label.className = 'transcript-partial-label'; label.textContent = '聞き取り中';
  const note = document.createElement('small'); note.textContent = 'あとで変わることがあります';
  meta.append(label, note);
  const content = document.createElement('div'); content.className = 'transcript-partial-text'; content.textContent = text;
  line.append(meta, content);
  return line;
}

function showLivePartial() {
  const target = document.getElementById('text-gpt-live-transcribe');
  if (!target) return;
  const followTail = target.scrollHeight - target.scrollTop - target.clientHeight <= 24;
  const text = Array.from(livePartial.values()).join(' ');
  let line = target.querySelector<HTMLElement>('.transcript-partial');
  if (!line) {
    if (!target.children.length) target.replaceChildren();
    line = createPartialLine(text);
    target.append(line);
  } else {
    line.querySelector<HTMLElement>('.transcript-partial-text')!.textContent = text;
  }
  if (followTail) target.scrollTop = target.scrollHeight;
}

function transcriptContent(currentRecordingOnly = false): string {
  return transcripts.content(info.models, info.showTimestamps, currentRecordingOnly);
}

function setAutoSaveStatus(message: string) {
  $('autoSaveStatus').textContent = message;
  const error = message.includes('エラー') || message.includes('できません');
  $('autoSaveStatus').title = error ? message : autoSave.path || info.autoSaveDir;
  $('autoSaveStatus').classList.toggle('error', error);
}

function setLogStatus(message: string, error = false) {
  $('logStatus').textContent = message;
  $('logStatus').title = error ? message : info.logPath;
  $('logStatus').classList.toggle('error', error);
}

async function beginAutoSave() {
  if (!info.autoSave) return;
  await autoSave.begin();
  await autoSave.write(() => transcriptContent(true));
}

function finishAutoSave(): Promise<void> {
  return autoSave.end(() => transcriptContent(true));
}

function addTranscript(id: string, text: string) {
  transcripts.append(id, text);
  void autoSave.write(() => transcriptContent(true));
}

function flashResult(id: string) {
  const card = $(`text-${id}`)?.closest('.result');
  if (!card) return;
  card.classList.remove('result-updated');
  void (card as HTMLElement).offsetWidth;
  card.classList.add('result-updated');
}

EventsOn('live-transcript', (event: {type: string; item_id?: string; delta?: string; transcript?: string; message?: string}) => {
  const id = 'gpt-live-transcribe';
  const state = $(`state-${id}`);
  if (event.type === 'delta') {
    const item = event.item_id || 'current';
    livePartial.set(item, (livePartial.get(item) || '') + (event.delta || ''));
    showLivePartial();
    if (state) state.textContent = '認識中';
  } else if (event.type === 'completed') {
    livePartial.delete(event.item_id || 'current');
    if (event.transcript?.trim()) addTranscript(id, event.transcript.trim());
    showTranscript(id, Boolean(event.transcript?.trim()));
    if (event.transcript?.trim()) flashResult(id);
    if (state) state.textContent = '完了';
  } else if (event.type === 'error') {
    liveFailed = true;
    if (state) state.textContent = `エラー: ${event.message || 'Live接続に失敗しました'}`;
  }
});

function recordGoogleFinal(model: GoogleModelID, sessionID: string, index: number, text: string) {
  if (!sessionID.startsWith(`r${recordingSerial}-`) || !text.trim()) return;
  let seen = googleFinals.get(sessionID);
  if (!seen) { seen = new Set(); googleFinals.set(sessionID, seen); }
  if (seen.has(index)) return;
  seen.add(index);
  googlePartial.get(model)?.delete(sessionID);
  addTranscript(model, text.trim());
  showTranscript(model, true);
  flashResult(model);
  const state = document.getElementById(`state-${model}`);
  if (state) state.textContent = '認識中';
}

EventsOn('google-transcript', (event: {type: string; model_id: GoogleModelID; turn_id: string; index?: number; text?: string; message?: string}) => {
  if (event.model_id !== 'google-v1' && event.model_id !== 'google-chirp-3') return;
  if (!event.turn_id.startsWith(`r${recordingSerial}-`)) return;
  const partials = googlePartial.get(event.model_id) || new Map<string, string>();
  googlePartial.set(event.model_id, partials);
  const state = document.getElementById(`state-${event.model_id}`);
  if (event.type === 'partial') {
    if (googleActive.get(event.model_id)?.id !== event.turn_id) return;
    partials.set(event.turn_id, event.text || '');
    showTranscript(event.model_id);
    if (state) state.textContent = '認識中';
  } else if (event.type === 'completed') {
    recordGoogleFinal(event.model_id, event.turn_id, event.index ?? 0, event.text || '');
  } else if (event.type === 'error') {
    partials.delete(event.turn_id);
    showTranscript(event.model_id);
    if (state && googleActive.get(event.model_id)?.id === event.turn_id) {
      state.textContent = `エラー: ${event.message || 'Google接続に失敗しました'}`;
    }
  } else if (event.type === 'ended') {
    partials.delete(event.turn_id);
    showTranscript(event.model_id);
  }
});

function modelTrait(id: string): HTMLElement | null {
  if (id !== 'gpt-transcribe' && id !== 'gpt-live-transcribe') return null;
  const live = id === 'gpt-live-transcribe';
  const trait = document.createElement('div');
  trait.className = `model-trait ${live ? 'model-trait-live' : 'model-trait-accuracy'}`;
  const badge = document.createElement('span'); badge.className = 'trait-badge';
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  icon.setAttribute('viewBox', '0 0 24 24'); icon.setAttribute('aria-hidden', 'true');
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
  path.setAttribute('d', live ? 'M13.2 2 5 13h6l-1 9 9-12h-6l.2-8Z' : 'M8 12.5 10.8 15l5.2-6M12 2.5a9.5 9.5 0 1 0 0 19 9.5 9.5 0 0 0 0-19Z');
  path.setAttribute('fill', live ? 'currentColor' : 'none');
  path.setAttribute('stroke', live ? 'none' : 'currentColor');
  path.setAttribute('stroke-width', '2');
  path.setAttribute('stroke-linecap', 'round'); path.setAttribute('stroke-linejoin', 'round');
  icon.append(path);
  const badgeText = document.createElement('span'); badgeText.textContent = live ? '速報重視' : '精度重視';
  badge.append(icon, badgeText);
  const timing = document.createElement('span'); timing.className = 'trait-timing'; timing.textContent = live ? '発話中に表示' : '発話後に表示';
  trait.append(badge, timing);
  if (live) {
    const caution = document.createElement('span'); caution.className = 'trait-caution'; caution.textContent = '誤認識に注意';
    trait.append(caution);
  }
  return trait;
}

function renderModels() {
  $('models').replaceChildren(...info.models.map(model => {
    const label = document.createElement('label');
    label.className = 'model' + (model.available ? '' : ' unavailable');
    const box = document.createElement('input');
    box.type = 'checkbox'; box.disabled = !model.available || phase !== 'idle'; box.checked = selected.has(model.id);
    box.addEventListener('change', () => { if (box.checked) selected.add(model.id); else selected.delete(model.id); renderModels(); renderResults(); });
    const copy = document.createElement('span');
    const strong = document.createElement('strong'); strong.textContent = model.name;
    const small = document.createElement('small'); small.textContent = model.provider + (model.available ? ' · 使用可能' : ` · ${model.reason || '設定を確認してください'}`);
    copy.append(strong, small);
    const trait = modelTrait(model.id);
    if (trait) copy.append(trait);
    label.append(box, copy); return label;
  }));
  const allUnavailable = info.models.every(model => !model.available);
  $('configMessage').textContent = info.error
    ? `設定エラー: ${info.error} (${info.configPath})`
    : `設定ファイル: ${info.configPath}${allUnavailable ? '。利用するサービスの認証情報を入力して「設定を再読込」を押してください。' : ''}`;
  $('modelSummary').textContent = selected.size ? `${selected.size}モデル選択中` : 'モデル未選択';
  $('modelSummary').title = info.models.filter(model => selected.has(model.id)).map(model => model.name).join('、');
}

function drawSpectrum() {
  const canvas = $('spectrum') as HTMLCanvasElement;
  const graphics = canvas.getContext('2d');
  if (!graphics) return;
  const { width, height } = canvas;
  graphics.clearRect(0, 0, width, height);
  graphics.fillStyle = '#eaf0f7'; graphics.fillRect(0, 0, width, height);
  if (analyser) {
    const bins = new Uint8Array(analyser.frequencyBinCount);
    analyser.getByteFrequencyData(bins);
    const bars = Math.floor(width / 8), step = Math.max(1, Math.floor(bins.length / 3 / bars));
    for (let i = 0; i < bars; i++) {
      let level = 0;
      for (let j = 0; j < step; j++) level = Math.max(level, bins[i * step + j]);
      const barHeight = Math.max(3, level / 255 * (height - 12));
      graphics.fillStyle = level > 150 ? '#2066d1' : '#55a2ee';
      graphics.fillRect(i * 8 + 1, height - barHeight - 4, 5, barHeight);
    }
    animationFrame = requestAnimationFrame(drawSpectrum);
  } else {
    graphics.fillStyle = '#b8c9dd';
    for (let i = 0; i < Math.floor(width / 8); i++) graphics.fillRect(i * 8 + 1, height - 7, 5, 3);
  }
}
function renderResults() {
  $('results').replaceChildren(...info.models.filter(m => selected.has(m.id)).map(m => {
    const article = document.createElement('article'); article.className = 'panel result';
    const heading = document.createElement('div'); heading.className = 'section-head';
    const titleGroup = document.createElement('div'); titleGroup.className = 'result-title';
    const title = document.createElement('h2'); title.textContent = m.name;
    titleGroup.append(title);
    const trait = modelTrait(m.id);
    if (trait) titleGroup.append(trait);
    const state = document.createElement('span'); state.id = `state-${m.id}`; state.className = 'hint'; state.textContent = '待機中';
    heading.append(titleGroup, state);
    const transcript = document.createElement('div'); transcript.className = 'transcript'; transcript.id = `text-${m.id}`;
    transcript.addEventListener('scroll', () => {
      if (transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight <= 24) jump.hidden = true;
    });
    const jump = document.createElement('button'); jump.id = `jump-${m.id}`; jump.className = 'jump-latest';
    jump.textContent = '新しい結果 ↓'; jump.hidden = true;
    jump.addEventListener('click', () => { transcript.scrollTop = transcript.scrollHeight; jump.hidden = true; });
    article.append(heading, transcript, jump); return article;
  }));
  for (const id of selected) showTranscript(id);
}
async function reload() {
  if (phase !== 'idle') return;
  const loaded = await GetInfo();
  if (phase !== 'idle') return;
  info = loaded;
  $('version').textContent = `v${info.version}`;
  ($('showTimestamps') as HTMLInputElement).checked = info.showTimestamps;
  ($('autoSave') as HTMLInputElement).checked = info.autoSave;
  if (!autoSave.path) setAutoSaveStatus(info.autoSave ? '保存先: transcripts/' : '');
  ($('debugLogging') as HTMLInputElement).checked = info.debugLogging;
  setLogStatus(info.logError ? `画面設定エラー: ${info.logError}` : info.debugLogging ? '保存先: logs/asr-studio.log' : '', Boolean(info.logError));
  for (const id of Array.from(selected)) if (!info.models.find(m => m.id === id && m.available)) selected.delete(id);
  if (!selected.size) { const first = info.models.find(m => m.available); if (first) selected.add(first.id); }
  renderModels(); renderResults();
}
function queueLiveFrame(frame: Float32Array) {
  if (!liveActive || liveFailed || !context) return;
  const encoded = encodeLivePCM(frame, context.sampleRate);
  liveQueue = liveQueue.then(() => AppendLive(encoded)).catch(error => {
    liveFailed = true;
    const state = $('state-gpt-live-transcribe'); if (state) state.textContent = `エラー: ${String(error)}`;
  });
}
function queueLiveCommit() {
  if (!liveActive || liveFailed) return;
  liveQueue = liveQueue.then(() => CommitLive()).catch(error => {
    liveFailed = true;
    const state = $('state-gpt-live-transcribe'); if (state) state.textContent = `エラー: ${String(error)}`;
  });
}
function startGoogleStreams() {
  for (const model of ['google-v1', 'google-chirp-3'] as GoogleModelID[]) {
    if (!selected.has(model)) continue;
    const id = `r${recordingSerial}-t${++googleTurnSerial}`;
    const session: GoogleSession = { id, queue: Promise.resolve(), failed: false };
    session.queue = StartGoogleStream(model, id).catch(error => googleStreamFailure(model, session, error));
    googleActive.set(model, session);
  }
}
function googleStreamFailure(model: GoogleModelID, session: GoogleSession, error: unknown) {
  session.failed = true;
  const state = document.getElementById(`state-${model}`);
  if (state && googleActive.get(model) === session) state.textContent = `エラー: ${String(error)}`;
}
function appendGoogleAudio(frame: Float32Array, sampleRate: number) {
  if (!googleActive.size) return;
  googleAudioFrames.push(frame);
  googleAudioSamples += frame.length;
  if (googleAudioSamples >= sampleRate * 0.1) flushGoogleAudio(sampleRate);
}
function flushGoogleAudio(sampleRate: number) {
  if (!googleAudioSamples) return;
  const audio = new Float32Array(googleAudioSamples);
  let offset = 0;
  for (const frame of googleAudioFrames) { audio.set(frame, offset); offset += frame.length; }
  googleAudioFrames = [];
  googleAudioSamples = 0;
  const encoded = encodeGooglePCM(audio, sampleRate);
  for (const [model, session] of googleActive) {
    session.queue = session.queue.then(() => {
      if (session.failed) return;
      return AppendGoogleStream(model, session.id, encoded);
    }).catch(error => googleStreamFailure(model, session, error));
  }
}
function finishGoogleStreams() {
  for (const [model, session] of googleActive) {
    googleActive.delete(model);
    const finishing = session.queue.then(async () => {
      const finals = await EndGoogleStream(model, session.id);
      finals?.forEach((text, index) => recordGoogleFinal(model, session.id, index, text));
      googlePartial.get(model)?.delete(session.id);
      showTranscript(model);
      const state = document.getElementById(`state-${model}`);
      if (state && !googleActive.has(model) && !session.failed) state.textContent = '完了';
    }).catch(async error => {
      try { await EndGoogleStream(model, session.id); } catch { /* close a failed stream */ }
      googlePartial.get(model)?.delete(session.id);
      showTranscript(model);
      const state = document.getElementById(`state-${model}`);
      if (state && !googleActive.has(model)) state.textContent = `エラー: ${String(error)}`;
    });
    googlePending.add(finishing);
    void finishing.finally(() => googlePending.delete(finishing));
  }
}
function rotateGoogleStreams() {
  if (phase !== 'recording' || !context || !googleActive.size) return;
  flushGoogleAudio(context.sampleRate);
  finishGoogleStreams();
  startGoogleStreams();
}
function rotateLiveConnection() {
  if (phase !== 'recording' || !liveActive || liveFailed) return;
  // Realtime sessions end after 60 minutes. Finish the current turn before reconnecting.
  flush();
  const state = document.getElementById('state-gpt-live-transcribe');
  if (state) state.textContent = 'Live接続を更新中…';
  liveQueue = liveQueue.then(async () => {
    await StopLive();
    await StartLive();
    if (state) state.textContent = '認識中';
  }).catch(error => {
    liveFailed = true;
    if (state) state.textContent = `Live再接続エラー: ${String(error)}`;
  });
}
function flush() {
  const audio = segmenter?.finish();
  if (!context || !audio) return;
  submitAudio(audio, context.sampleRate);
}
function submitAudio(audio: Float32Array, sampleRate: number) {
  queueLiveCommit();
  if (audio.length < sampleRate * 0.35) return;
  const wav = encodeWav(audio, sampleRate);
  for (const id of selected) {
    if (id === 'gpt-live-transcribe' || id === 'google-v1' || id === 'google-chirp-3') continue;
    const previous = requestChains.get(id) || Promise.resolve();
    const next = previous.catch(() => {}).then(async () => {
      const state = $(`state-${id}`); if (state) state.textContent = '送信中…';
      try {
        const result = await Transcribe(id, wav);
        if (result.text.trim()) addTranscript(id, result.text.trim());
        showTranscript(id, Boolean(result.text.trim()), '認識結果はありません');
        if (result.text.trim()) flashResult(id);
        if (state) state.textContent = '完了';
      } catch (error) { if (state) state.textContent = `エラー: ${String(error)}`; }
    });
    requestChains.set(id, next);
  }
}
async function start() {
  if (phase !== 'idle') return;
  if (!selected.size) { $('status').textContent = 'モデルを選択してください'; return; }
  setRecordingPhase('starting');
  try {
    transcripts.beginRecording();
    livePartial.clear();
    googlePartial.clear();
    googleFinals.clear();
    recordingSerial++;
    const deviceId = ($('microphone') as HTMLSelectElement).value;
    const mode: CaptureMode = ($('distantVoices') as HTMLInputElement).checked ? 'distant' : 'normal';
    stream = await navigator.mediaDevices.getUserMedia({ audio: microphoneConstraints(mode, deviceId), video: false });
    void LogDiagnostic('microphone_started');
    const track = stream.getAudioTracks()[0];
    $('activeMic').textContent = `使用中: ${track.label || '名称不明のマイク'}${mode === 'distant' ? ' · 離れた声を拾う' : ''}`;
    await refreshMicrophones(false);
    if (selected.has('gpt-live-transcribe')) { await StartLive(); liveActive = true; liveFailed = false; liveQueue = Promise.resolve(); }
    await beginAutoSave();
    startGoogleStreams();
    if (track.readyState === 'ended') throw new Error('マイクが切断されました');
    track.addEventListener('ended', () => { if (phase === 'recording') void stop(); });
    segmenter = new SpeechSegmenter(mode);
    context = new AudioContext(); source = context.createMediaStreamSource(stream);
    analyser = context.createAnalyser(); analyser.fftSize = 1024; analyser.smoothingTimeConstant = 0.72;
    silentGain = context.createGain(); silentGain.gain.value = 0;
    source.connect(analyser); analyser.connect(silentGain); silentGain.connect(context.destination);
    drawSpectrum();
    processor = context.createScriptProcessor(2048, 1, 1);
    processor.onaudioprocess = event => {
      const frame = new Float32Array(event.inputBuffer.getChannelData(0));
      const { frames, completed } = segmenter!.push(frame, context!.sampleRate);
      for (const chunk of frames) queueLiveFrame(chunk);
      appendGoogleAudio(frame, context!.sampleRate);
      if (completed) submitAudio(completed, context!.sampleRate);
    };
    source.connect(processor); processor.connect(context.destination);
    setRecordingPhase('recording');
    if (liveActive) liveRotationTimer = window.setInterval(rotateLiveConnection, 50 * 60 * 1000);
    if (googleActive.size) googleRotationTimer = window.setInterval(rotateGoogleStreams, 4 * 60 * 1000);
    ($('modelsPanel') as HTMLDetailsElement).open = false;
    document.body.classList.add('recording');
    $('status').textContent = '録音中';
  } catch (error) { void LogDiagnostic('microphone_error'); await stop(); $('status').textContent = `録音を開始できません: ${String(error)}`; }
}
function setRecordingPhase(next: RecordingPhase) {
  phase = next;
  ($('start') as HTMLButtonElement).disabled = next !== 'idle';
  ($('stop') as HTMLButtonElement).disabled = next !== 'recording';
  ($('microphone') as HTMLSelectElement).disabled = next !== 'idle';
  ($('distantVoices') as HTMLInputElement).disabled = next !== 'idle';
  ($('reload') as HTMLButtonElement).disabled = next !== 'idle';
  ($('autoSave') as HTMLInputElement).disabled = next === 'starting' || next === 'stopping';
  renderModels();
}

async function stop() {
  if (phase === 'idle' || phase === 'stopping') return;
  void LogDiagnostic('microphone_stopped');
  setRecordingPhase('stopping');
  $('status').textContent = '残りの文字起こしを処理中…';
  const errors: string[] = [];
  if (liveRotationTimer !== null) { window.clearInterval(liveRotationTimer); liveRotationTimer = null; }
  if (googleRotationTimer !== null) { window.clearInterval(googleRotationTimer); googleRotationTimer = null; }
  flush();
  if (processor) processor.onaudioprocess = null;
  if (context) flushGoogleAudio(context.sampleRate);
  finishGoogleStreams();
  processor?.disconnect(); source?.disconnect(); analyser?.disconnect(); silentGain?.disconnect();
  stream?.getTracks().forEach(track => track.stop());
  cancelAnimationFrame(animationFrame); analyser = null; silentGain = null; drawSpectrum();
  try { await context?.close(); } catch (error) { errors.push(String(error)); }
  context = null; processor = null; source = null; stream = null; segmenter = null;
  if (liveActive) {
    await liveQueue;
    try { await StopLive(); } catch (error) { errors.push(`Live停止エラー: ${String(error)}`); }
    liveActive = false;
  }
  // Drain every recording, even with autosave off, before allowing the next one.
  await Promise.allSettled(Array.from(requestChains.values()));
  requestChains.clear();
  await Promise.allSettled(Array.from(googlePending));
  await finishAutoSave();
  livePartial.clear();
  googlePartial.clear();
  if (selected.has('gpt-live-transcribe')) showTranscript('gpt-live-transcribe');
  setRecordingPhase('idle');
  ($('modelsPanel') as HTMLDetailsElement).open = true;
  document.body.classList.remove('recording');
  $('status').textContent = errors.length ? errors.join(' / ') : '停止';
  $('activeMic').textContent = '録音停止中';
}
$('microphone').addEventListener('change', () => localStorage.setItem('asr-microphone-id', ($('microphone') as HTMLSelectElement).value));
($('distantVoices') as HTMLInputElement).checked = localStorage.getItem('asr-distant-voices') === 'true';
$('distantVoices').addEventListener('change', () => localStorage.setItem('asr-distant-voices', String(($('distantVoices') as HTMLInputElement).checked)));
$('refreshMics').addEventListener('click', () => refreshMicrophones(true).catch(error => $('activeMic').textContent = `マイク一覧を取得できません: ${String(error)}`));
navigator.mediaDevices?.addEventListener?.('devicechange', () => { void refreshMicrophones(false); });
void refreshMicrophones(false);
drawSpectrum();
$('reload').addEventListener('click', () => { reload().catch(error => $('status').textContent = String(error)); });
$('debugLogging').addEventListener('change', async () => {
  const toggle = $('debugLogging') as HTMLInputElement;
  toggle.disabled = true;
  try {
    await SetDebugLogging(toggle.checked);
    info.debugLogging = toggle.checked;
    setLogStatus(toggle.checked ? '保存先: logs/asr-studio.log' : '');
  }
  catch (error) { setLogStatus(`ログ設定エラー: ${String(error)}`, true); }
  finally { toggle.checked = info.debugLogging; toggle.disabled = false; }
});
$('showTimestamps').addEventListener('change', async () => {
  const toggle = $('showTimestamps') as HTMLInputElement;
  toggle.disabled = true;
  try {
    await SetShowTimestamps(toggle.checked);
    info.showTimestamps = toggle.checked;
    for (const id of selected) showTranscript(id);
  } catch (error) {
    toggle.checked = info.showTimestamps;
    $('status').textContent = `時刻表示を変更できません: ${String(error)}`;
  } finally { toggle.disabled = false; }
});
$('autoSave').addEventListener('change', async () => {
  const toggle = $('autoSave') as HTMLInputElement;
  toggle.disabled = true;
  try {
    await SetAutoSave(toggle.checked);
    info.autoSave = toggle.checked;
    if (phase === 'recording' && toggle.checked) await beginAutoSave();
    if (!toggle.checked) {
      await finishAutoSave();
    }
  } catch (error) {
    toggle.checked = info.autoSave;
    setAutoSaveStatus(`自動保存設定エラー: ${String(error)}`);
  } finally { toggle.disabled = phase === 'starting' || phase === 'stopping'; }
});
$('start').addEventListener('click', start); $('stop').addEventListener('click', stop);
$('clear').addEventListener('click', () => { transcripts.clearDisplay(); livePartial.clear(); googlePartial.clear(); renderResults(); });
$('save').addEventListener('click', async () => {
  const content = transcriptContent();
  if (!content) { $('status').textContent = '保存する結果がありません'; return; }
  try { if (await SaveTranscript(content)) $('status').textContent = 'テキストを保存しました'; }
  catch (error) { $('status').textContent = `保存できません: ${String(error)}`; }
});
reload().catch(error => $('status').textContent = String(error));

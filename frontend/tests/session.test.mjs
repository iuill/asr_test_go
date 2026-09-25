import { test } from 'node:test';
import assert from 'node:assert/strict';
import { AutoSaveSession } from '../src/autosave-session.ts';
import { TranscriptStore } from '../src/transcript-store.ts';
import { encodeWav, encodeLivePCM, encodeGooglePCM } from '../src/audio-encoding.ts';

test('stopping during save initialization closes after the final snapshot', async () => {
  const calls = [];
  let release;
  const pending = new Promise(resolve => { release = resolve; });
  const session = new AutoSaveSession({
    begin: async () => { calls.push('begin'); await pending; return 'one.txt'; },
    write: async content => { calls.push(`write:${content}`); return 'one.txt'; },
    end: async content => { calls.push(`end:${content}`); return 'one.txt'; },
  }, () => {});
  const begin = session.begin();
  session.write(() => 'first');
  const end = session.end(() => 'final');
  session.write(() => 'must not write after end');
  release();
  await Promise.all([begin, end]);
  assert.deepEqual(calls, ['begin', 'write:first', 'end:final']);
  assert.equal(session.path, '');
});

test('a failed write does not prevent later snapshots or a new session', async () => {
  const events = [];
  let fail = true;
  let count = 0;
  const session = new AutoSaveSession({
    begin: async () => `${++count}.txt`,
    write: async () => { if (fail) { fail = false; throw new Error('disk full'); } return `${count}.txt`; },
    end: async () => `${count}.txt`,
  }, event => events.push(event.type));
  await session.begin();
  await session.write(() => 'one');
  await session.write(() => 'two');
  await session.end(() => 'final');
  await session.begin();
  assert.equal(session.path, '2.txt');
  assert.deepEqual(events, ['started', 'error', 'saved', 'saved', 'started']);
});

test('clearing display retains saved results and recording boundaries isolate exports', () => {
  const store = new TranscriptStore();
  const models = [{ id: 'live', name: 'Live' }];
  store.beginRecording();
  store.append('live', 'first');
  store.clearDisplay();
  assert.equal(store.content(models, false), '');
  assert.equal(store.content(models, false, true), 'Live\nfirst');
  store.beginRecording();
  store.append('live', 'second');
  assert.equal(store.content(models, false, true), 'Live\nsecond');
});

test('audio encoders retain expected sample rates, PCM format and duration', () => {
  const input = new Float32Array(48000).fill(0.5);
  const wav = Buffer.from(encodeWav(input, 48000), 'base64');
  assert.equal(wav.toString('ascii', 0, 4), 'RIFF');
  assert.equal(wav.readUInt32LE(24), 16000);
  assert.equal(wav.readUInt32LE(40), 32000);
  assert.equal(wav.length, 32044);
  assert.equal(wav.readInt16LE(44), 16383);
  const live = Buffer.from(encodeLivePCM(input, 48000), 'base64');
  assert.equal(live.length, 48000);
  assert.equal(live.readInt16LE(0), 16383);
  const google = Buffer.from(encodeGooglePCM(input, 48000), 'base64');
  assert.equal(google.length, 32000);
  assert.equal(google.readInt16LE(0), 16383);
});

test('failed initialization does not save into a previous recording', async () => {
  let attempts = 0;
  const saved = [];
  const session = new AutoSaveSession({
    begin: async () => { if (++attempts === 1) throw new Error('no access'); return 'new.txt'; },
    write: async content => { saved.push(content); return 'new.txt'; },
    end: async content => { saved.push(content); return 'new.txt'; },
  }, () => {});
  await session.begin();
  await session.write(() => 'failed recording');
  await session.end(() => 'failed recording');
  await session.begin();
  await session.write(() => 'new recording');
  assert.deepEqual(saved, ['new recording']);
});

test('distant mode includes the quiet lead-in and completes after silence', async () => {
  const { SpeechSegmenter, microphoneConstraints } = await import('../src/speech-segmenter.ts');
  const normal = new SpeechSegmenter('normal');
  const distant = new SpeechSegmenter('distant');
  const quiet = new Float32Array(160).fill(0.002);
  const distantVoice = new Float32Array(160).fill(0.006);
  for (let i = 0; i < 40; i++) {
    normal.push(quiet, 16000);
    distant.push(quiet, 16000);
  }
  const heard = distant.push(distantVoice, 16000);
  assert.ok(heard.frames.length >= 30, 'start of the utterance should retain 0.3s of pre-roll');
  assert.equal(normal.push(distantVoice, 16000).frames.length, 0);
  let completed = null;
  for (let i = 0; i < 81; i++) completed = distant.push(new Float32Array(160), 16000).completed || completed;
  assert.ok(completed && completed.length > 16000, 'quiet voice and trailing silence should be sent');
  assert.equal(distant.finish(), null);
  assert.equal(microphoneConstraints('normal', '').noiseSuppression, true);
  assert.equal(microphoneConstraints('distant', '').noiseSuppression, false);
  assert.equal(microphoneConstraints('distant', '').echoCancellation, false);
});

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { AutoSaveSession } from '../src/autosave-session.ts';
import { TranscriptStore } from '../src/transcript-store.ts';
import { encodeWav, encodeLivePCM } from '../src/audio-encoding.ts';

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

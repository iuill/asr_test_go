export type CaptureMode = 'normal' | 'distant';

export function microphoneConstraints(mode: CaptureMode, deviceId: string): MediaTrackConstraints {
  return {
    deviceId: deviceId ? { exact: deviceId } : undefined,
    echoCancellation: mode === 'normal',
    noiseSuppression: mode === 'normal',
    autoGainControl: mode === 'distant' ? true : undefined,
  };
}

export class SpeechSegmenter {
  private chunks: Float32Array[] = [];
  private samples = 0;
  private silenceSamples = 0;
  private preRoll: Float32Array[] = [];
  private preRollSamples = 0;
  private mode: CaptureMode;

  constructor(mode: CaptureMode) { this.mode = mode; }

  push(frame: Float32Array, sampleRate: number): { frames: Float32Array[]; completed: Float32Array | null } {
    let energy = 0;
    for (const sample of frame) energy += sample * sample;
    const rms = Math.sqrt(energy / frame.length);
    const speaking = rms > (this.mode === 'distant' ? 0.004 : 0.012);
    if (!this.samples && !speaking) {
      this.preRoll.push(frame);
      this.preRollSamples += frame.length;
      while (this.preRollSamples > sampleRate * 0.3 && this.preRoll.length > 1) {
        this.preRollSamples -= this.preRoll.shift()!.length;
      }
      return { frames: [], completed: null };
    }

    const frames = this.samples ? [frame] : [...this.preRoll, frame];
    this.preRoll = []; this.preRollSamples = 0;
    for (const chunk of frames) { this.chunks.push(chunk); this.samples += chunk.length; }
    this.silenceSamples = speaking ? 0 : this.silenceSamples + frame.length;
    const completed = this.silenceSamples >= sampleRate * 0.8 || this.samples >= sampleRate * 15 ? this.finish() : null;
    return { frames, completed };
  }

  finish(): Float32Array | null {
    if (!this.samples) { this.preRoll = []; this.preRollSamples = 0; return null; }
    const audio = new Float32Array(this.samples);
    let offset = 0;
    for (const chunk of this.chunks) { audio.set(chunk, offset); offset += chunk.length; }
    this.chunks = []; this.samples = 0; this.silenceSamples = 0;
    this.preRoll = []; this.preRollSamples = 0;
    return audio;
  }
}

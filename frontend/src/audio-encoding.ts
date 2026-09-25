export function encodeWav(input: Float32Array, originalRate: number): string {
  const targetRate = 16000;
  const count = Math.floor(input.length * targetRate / originalRate);
  const wav = new ArrayBuffer(44 + count * 2);
  const view = new DataView(wav);
  const ascii = (offset: number, s: string) => { for (let i = 0; i < s.length; i++) view.setUint8(offset + i, s.charCodeAt(i)); };
  ascii(0, 'RIFF'); view.setUint32(4, wav.byteLength - 8, true); ascii(8, 'WAVE'); ascii(12, 'fmt ');
  view.setUint32(16, 16, true); view.setUint16(20, 1, true); view.setUint16(22, 1, true);
  view.setUint32(24, targetRate, true); view.setUint32(28, targetRate * 2, true); view.setUint16(32, 2, true); view.setUint16(34, 16, true);
  ascii(36, 'data'); view.setUint32(40, count * 2, true);
  for (let i = 0; i < count; i++) {
    const pos = i * originalRate / targetRate;
    const low = Math.floor(pos), frac = pos - low;
    const value = Math.max(-1, Math.min(1, (input[low] || 0) * (1 - frac) + (input[low + 1] || 0) * frac));
    view.setInt16(44 + i * 2, value < 0 ? value * 32768 : value * 32767, true);
  }
  const bytes = new Uint8Array(wav); let binary = '';
  for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(binary);
}
function encodePCM(input: Float32Array, originalRate: number, rate: number): string {
  const count = Math.floor(input.length * rate / originalRate);
  const pcm = new Int16Array(count);
  for (let i = 0; i < count; i++) {
    const pos = i * originalRate / rate, low = Math.floor(pos), frac = pos - low;
    const value = Math.max(-1, Math.min(1, (input[low] || 0) * (1 - frac) + (input[low + 1] || 0) * frac));
    pcm[i] = value < 0 ? value * 32768 : value * 32767;
  }
  const bytes = new Uint8Array(pcm.buffer); let binary = '';
  for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(binary);
}

export function encodeLivePCM(input: Float32Array, originalRate: number): string {
  return encodePCM(input, originalRate, 24000);
}

export function encodeGooglePCM(input: Float32Array, originalRate: number): string {
  return encodePCM(input, originalRate, 16000);
}

/**
 * 把浏览器录音（webm/ogg/mp4 等）解码后重新编码成 16 位单声道 WAV。
 * 各浏览器 MediaRecorder 产出的格式不一，统一成 WAV 保证上游能识别。
 */
export async function toWav(blob) {
  const ctx = new (window.AudioContext || window.webkitAudioContext)()
  try {
    const buffer = await ctx.decodeAudioData(await blob.arrayBuffer())
    return encodeWav(mixdown(buffer), buffer.sampleRate)
  } finally {
    ctx.close()
  }
}

// 多声道取平均合成单声道
function mixdown(buffer) {
  const { numberOfChannels, length } = buffer
  if (numberOfChannels === 1) return buffer.getChannelData(0)

  const out = new Float32Array(length)
  for (let c = 0; c < numberOfChannels; c++) {
    const data = buffer.getChannelData(c)
    for (let i = 0; i < length; i++) out[i] += data[i] / numberOfChannels
  }
  return out
}

function encodeWav(samples, sampleRate) {
  const view = new DataView(new ArrayBuffer(44 + samples.length * 2))
  const writeString = (offset, s) => {
    for (let i = 0; i < s.length; i++) view.setUint8(offset + i, s.charCodeAt(i))
  }

  writeString(0, 'RIFF')
  view.setUint32(4, 36 + samples.length * 2, true)
  writeString(8, 'WAVE')
  writeString(12, 'fmt ')
  view.setUint32(16, 16, true) // fmt 块长度
  view.setUint16(20, 1, true) // PCM
  view.setUint16(22, 1, true) // 单声道
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, sampleRate * 2, true) // 字节率
  view.setUint16(32, 2, true) // 块对齐
  view.setUint16(34, 16, true) // 位深
  writeString(36, 'data')
  view.setUint32(40, samples.length * 2, true)

  let offset = 44
  for (let i = 0; i < samples.length; i++, offset += 2) {
    const s = Math.max(-1, Math.min(1, samples[i]))
    view.setInt16(offset, s < 0 ? s * 0x8000 : s * 0x7fff, true)
  }
  return new Blob([view], { type: 'audio/wav' })
}

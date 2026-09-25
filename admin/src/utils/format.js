/** 任务状态 → Element 标签类型与文案 */
export const TASK_STATUS = {
  pending: { label: '排队中', type: 'info' },
  processing: { label: '生成中', type: 'warning' },
  completed: { label: '已完成', type: 'success' },
  failed: { label: '已失败', type: 'danger' }
}

/** 积分流水类型 */
export const POINT_TYPES = {
  1: { label: '消耗', type: 'warning' },
  2: { label: '充值', type: 'success' },
  3: { label: '手动调整', type: 'info' },
  4: { label: '退还', type: 'primary' }
}

/** 任务类型下拉项，与后端 model.KindLabels 对应 */
export const TASK_KINDS = [
  { value: 'generate', label: '生成音乐' },
  { value: 'sound', label: '生成音效' },
  { value: 'upload', label: '上传参考音频' },
  { value: 'whole_song', label: '获取整首歌' },
  { value: 'aligned_lyrics', label: '获取歌词时间戳' },
  { value: 'upsample', label: 'Remaster 音乐' },
  { value: 'video', label: '生成音乐视频' },
  { value: 'crop', label: '裁剪音乐' },
  { value: 'speed', label: '调整音乐速度' },
  { value: 'download_wav', label: '下载 WAV' },
  { value: 'download_mp3', label: '下载 MP3' },
  { value: 'download_m4a', label: '下载 M4A' },
  { value: 'voice_train', label: '训练音色' },
  { value: 'voice_cover', label: '音色翻唱' }
]

/** 后端返回 RFC3339 时间，统一格式化为 YYYY-MM-DD HH:mm:ss */
export function formatTime(value) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n) => String(n).padStart(2, '0')
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  )
}

/** 积分转金额（1 积分 = 0.01 元） */
export function toAmount(points) {
  return `¥${(Number(points || 0) / 100).toFixed(2)}`
}

/** 千分位 */
export function thousands(value) {
  return Number(value || 0).toLocaleString('zh-CN')
}

/** 把 Element 日期范围转成后端需要的起止字符串 */
export function rangeToParams(range) {
  if (!range || range.length !== 2) return { start: '', end: '' }
  return { start: `${range[0]} 00:00:00`, end: `${range[1]} 23:59:59` }
}

/**
 * 上游签名代理地址（proxy_url）带 expires 秒级时间戳，有效期约一天，过期后返回 403。
 * 没有 expires 参数的地址视为长期有效。
 */
export function proxyAlive(url) {
  if (!url) return false
  try {
    const expires = Number(new URL(url).searchParams.get('expires'))
    // 留一分钟余量，避免刚取到就过期
    return !expires || Date.now() / 1000 < expires - 60
  } catch {
    return false
  }
}

// 上游原始音频是 http://，页面走 https 时会被按混合内容拦截；其 CDN 支持 https，直接升级
const secure = (url) => (url ? url.replace(/^http:\/\//i, 'https://') : '')

/**
 * 选出最适合直接播放的地址：代理地址未过期时优先用它，否则回落到原始文件地址。
 */
export function playableUrl(task) {
  if (!task) return ''
  const file = task.fileInfo || {}
  if (proxyAlive(task.proxy_url)) return task.proxy_url
  return secure(file.mp3Url || file.m4aUrl || file.wavUrl) || task.proxy_url || ''
}

/** 视频同理：代理过期后改用 mp4 原始地址 */
export function playableVideoUrl(task) {
  if (!task) return ''
  const file = task.fileInfo || {}
  if (proxyAlive(task.proxy_url)) return task.proxy_url
  return file.mp4Url || task.proxy_url || ''
}

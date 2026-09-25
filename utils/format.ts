/** 把任意值格式化为可读 JSON 文本 */
export function toPrettyJson(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

/** 给 JSON 文本上色（返回 HTML，输入已做转义） */
export function highlightJson(json: string): string {
  const escaped = json.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  return escaped.replace(
    /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g,
    (match) => {
      let cls = 'text-emerald-300' // 数字
      if (/^"/.test(match)) {
        cls = /:$/.test(match) ? 'text-suno-yellow' : 'text-sky-300'
      } else if (/true|false/.test(match)) {
        cls = 'text-purple-300'
      } else if (/null/.test(match)) {
        cls = 'text-gray-500'
      }
      return `<span class="${cls}">${match}</span>`
    }
  )
}

/** 秒数格式化为 mm:ss */
export function formatDuration(seconds?: number): string {
  if (!seconds && seconds !== 0) return '--:--'
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}

/** 从任意响应对象里递归挖出媒体链接，便于测试页直接试听 */
export function collectMediaUrls(value: unknown, depth = 0): { audio: string[]; video: string[]; image: string[] } {
  const out = { audio: [] as string[], video: [] as string[], image: [] as string[] }
  if (depth > 6 || value === null || value === undefined) return out

  const push = (url: string) => {
    const clean = url.split('?')[0].toLowerCase()
    if (/\.(mp3|wav|m4a|flac|ogg)$/.test(clean)) out.audio.push(url)
    else if (/\.(mp4|webm|mov)$/.test(clean)) out.video.push(url)
    else if (/\.(png|jpe?g|webp|gif)$/.test(clean)) out.image.push(url)
  }

  if (typeof value === 'string') {
    if (/^https?:\/\//.test(value)) push(value)
    // extend 等字段是 JSON 字符串，尝试展开
    else if (/^[[{]/.test(value.trim())) {
      try {
        return collectMediaUrls(JSON.parse(value), depth + 1)
      } catch {
        /* 不是合法 JSON，忽略 */
      }
    }
    return out
  }

  if (Array.isArray(value)) {
    for (const item of value) {
      const sub = collectMediaUrls(item, depth + 1)
      out.audio.push(...sub.audio)
      out.video.push(...sub.video)
      out.image.push(...sub.image)
    }
    return out
  }

  if (typeof value === 'object') {
    for (const item of Object.values(value as Record<string, unknown>)) {
      const sub = collectMediaUrls(item, depth + 1)
      out.audio.push(...sub.audio)
      out.video.push(...sub.video)
      out.image.push(...sub.image)
    }
  }

  return {
    audio: [...new Set(out.audio)],
    video: [...new Set(out.video)],
    image: [...new Set(out.image)]
  }
}

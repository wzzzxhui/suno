import type { ApiResponse, TaskStatus } from '~/types/api'

export interface ProxyResult<T = unknown> {
  ok: boolean
  status: number
  duration: number
  requestUrl: string
  data: ApiResponse<T>
}

export interface GenerateMusicPayload {
  /** 灵感模式：一句话描述 */
  gpt_description_prompt?: string
  /** 自定义模式：歌词 */
  prompt?: string
  /** 自定义模式：风格标签 */
  tags?: string
  negative_tags?: string
  mv: 'chirp-hawk' | 'chirp-hawk-wild' | 'chirp-goose' | string
  title: string
  make_instrumental: boolean
  task?: 'extend' | 'cover'
  continue_clip_id?: string
  continue_at?: number
  cover_clip_id?: string
  metadata?: Record<string, unknown>
}

export interface SoundPayload {
  title: string
  tags: string
  mv: 'chirp-crow' | 'chirp-fenix' | string
  tempo?: number
  key?: string
  loop?: boolean
}

export interface MusicTaskResult {
  id: number
  status: TaskStatus | number
  custom_id?: string | null
  proxy_url?: string
  extend?: string
  points_refunded?: boolean
  fileInfo?: {
    mp3Url?: string
    mp4Url?: string
    coverUrl?: string
    cosUrl?: string
    duration?: number
  }
  [key: string]: unknown
}

/**
 * 全量接口客户端。所有请求都走本站 /api/proxy 转发到上游，
 * 以便在浏览器里直接带 access_key 调用而不受 CORS 限制。
 */
export function useSunoApi() {
  const { apiKey } = useApiKey()

  const request = <T = unknown>(
    endpoint: string,
    method: 'GET' | 'POST',
    payload?: Record<string, unknown>,
    accessKey?: string
  ) =>
    $fetch<ProxyResult<T>>('/api/proxy', {
      method: 'POST',
      body: {
        endpoint,
        method,
        accessKey: accessKey ?? apiKey.value,
        query: method === 'GET' ? payload : undefined,
        body: method === 'POST' ? payload : undefined
      }
    })

  /* ---------------- 系统通用 ---------------- */

  /** 查询积分余额 */
  const getBalance = () => request<{ remaining_points: number }>('/api/v1/points/balance', 'GET')

  /** 查询积分流水 */
  const getPointLogs = (page = 1, limit = 20) => request('/api/v1/points/logs', 'GET', { page, limit })

  /* ---------------- 音乐生成 ---------------- */

  /** 生成音乐（灵感 / 自定义 / 延长 / 翻唱） */
  const generateMusic = (payload: GenerateMusicPayload) =>
    request<number[]>('/api/v1/music/generate', 'POST', payload as unknown as Record<string, unknown>)

  /** 生成音效 */
  const generateSound = (payload: SoundPayload) =>
    request<{ task_ids: string[] }>('/api/v1/music/sound', 'POST', payload as unknown as Record<string, unknown>)

  /** 查询单个任务 */
  const getTask = (id: number | string) => request<MusicTaskResult>('/api/v1/music/task', 'GET', { id })

  /** 批量查询任务 */
  const getTasks = (ids: Array<number | string> | string, page = 1, size = 10) =>
    request('/api/v1/music/tasks', 'GET', {
      ids: Array.isArray(ids) ? ids.join(',') : ids,
      page,
      size
    })

  /** 上传参考音频 */
  const uploadAudio = (audioUrl: string, copyrightAudio = false) =>
    request('/api/v1/music/upload', 'POST', { audio_url: audioUrl, copyrightAudio })

  /** 合成整首歌 */
  const wholeSong = (clipId: string) => request('/api/v1/music/whole-song', 'POST', { clip_id: clipId })

  /** 歌词时间戳对齐 */
  const alignedLyrics = (lyrics: string, sunoId: string) =>
    request('/api/v1/music/aligned-lyrics', 'POST', { lyrics, suno_id: sunoId })

  /** Remaster 升采样 */
  const upsample = (clipId: string, modelName: string, variationCategory?: string) =>
    request<number[]>('/api/v1/music/upsample', 'POST', {
      clip_id: clipId,
      model_name: modelName,
      ...(variationCategory ? { variation_category: variationCategory } : {})
    })

  /** 生成音乐视频 */
  const generateVideo = (taskId: number, sunoId: string) =>
    request('/api/v1/music/video', 'POST', { task_id: taskId, suno_id: sunoId })

  /** 裁剪音乐 */
  const cropMusic = (clipId: string, startSeconds: number, endSeconds: number) =>
    request('/api/v1/music/crop', 'POST', {
      clip_id: clipId,
      crop_start_s: startSeconds,
      crop_end_s: endSeconds
    })

  /** 调整播放速度 */
  const changeSpeed = (clipId: string, multiplier: number, keepPitch = false, title?: string) =>
    request('/api/v1/music/speed', 'POST', {
      clip_id: clipId,
      speed_multiplier: multiplier,
      keep_pitch: keepPitch,
      ...(title ? { title } : {})
    })

  /** 创建格式下载任务（v2 路径） */
  const createDownload = (format: 'wav' | 'mp3' | 'm4a', sunoId: string) =>
    request<{ task_id: string; status: string }>(`/api/v2/music/download-${format}`, 'POST', { suno_id: sunoId })

  /* ---------------- 轮询辅助 ---------------- */

  const normalizeStatus = (raw: unknown): TaskStatus => {
    if (typeof raw === 'string') {
      const s = raw.toLowerCase()
      if (['completed', 'complete', 'success'].includes(s)) return 'completed'
      if (['failed', 'fail', 'error'].includes(s)) return 'failed'
      if (['processing', 'running'].includes(s)) return 'processing'
      return 'pending'
    }
    // 上游数字状态：3 = 完成，4 = 失败
    if (raw === 3) return 'completed'
    if (raw === 4) return 'failed'
    if (raw === 2) return 'processing'
    return 'pending'
  }

  /**
   * 轮询任务直到完成或失败。
   * @param onTick 每次查询后的回调，便于界面展示进度
   */
  const pollTask = async (
    id: number | string,
    options: { interval?: number; maxAttempts?: number; onTick?: (result: MusicTaskResult, attempt: number) => void } = {}
  ) => {
    const { interval = 5000, maxAttempts = 60, onTick } = options

    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
      const res = await getTask(id)
      const result = (res.data?.data ?? {}) as MusicTaskResult
      onTick?.(result, attempt)

      const status = normalizeStatus(result.status)
      if (status === 'completed' || status === 'failed') {
        return { status, result, attempts: attempt }
      }
      await new Promise((resolve) => setTimeout(resolve, interval))
    }

    return { status: 'processing' as TaskStatus, result: {} as MusicTaskResult, attempts: maxAttempts }
  }

  return {
    request,
    getBalance,
    getPointLogs,
    generateMusic,
    generateSound,
    getTask,
    getTasks,
    uploadAudio,
    wholeSong,
    alignedLyrics,
    upsample,
    generateVideo,
    cropMusic,
    changeSpeed,
    createDownload,
    pollTask,
    normalizeStatus
  }
}

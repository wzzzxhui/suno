import axios from 'axios'
import { ElMessage } from 'element-plus'

const TOKEN_KEY = 'suno_admin_token'

export const getToken = () => localStorage.getItem(TOKEN_KEY) || ''
export const setToken = (token) => localStorage.setItem(TOKEN_KEY, token)
export const clearToken = () => localStorage.removeItem(TOKEN_KEY)

const http = axios.create({
  baseURL: '/admin/api',
  timeout: 30000
})

http.interceptors.request.use((config) => {
  const token = getToken()
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

http.interceptors.response.use(
  (response) => {
    const body = response.data
    if (body && body.success === false) {
      ElMessage.error(body.message || '请求失败')
      return Promise.reject(new Error(body.message))
    }
    return body?.data
  },
  (error) => {
    const status = error.response?.status
    const message = error.response?.data?.message || error.message || '网络异常'

    if (status === 401) {
      clearToken()
      // 登录态失效，回到登录页
      if (!location.hash.startsWith('#/login')) location.hash = '#/login'
      ElMessage.warning(message)
    } else {
      ElMessage.error(message)
    }
    return Promise.reject(error)
  }
)

/* ---------------------------------- 账号 ---------------------------------- */

export const login = (payload) => http.post('/login', payload)
export const fetchProfile = () => http.get('/profile')
export const changePassword = (payload) => http.post('/password', payload)

/* ---------------------------------- 统计 ---------------------------------- */

export const fetchOverview = () => http.get('/overview')
export const fetchTrend = (days = 7) => http.get('/trend', { params: { days } })
export const fetchKindStats = (days = 30) => http.get('/kind-stats', { params: { days } })
export const fetchSystem = () => http.get('/system')
export const fetchModels = () => http.get('/models')
export const fetchUpstream = () => http.get('/upstream')
export const refreshUpstream = () => http.post('/upstream/refresh')

/* ---------------------------------- 商户 ---------------------------------- */

export const fetchMerchants = (params) => http.get('/merchants', { params })
export const fetchMerchantOptions = () => http.get('/merchants/options')
export const createMerchant = (payload) => http.post('/merchants/create', payload)
export const setMerchantStatus = (payload) => http.post('/merchants/status', payload)
export const renameMerchant = (payload) => http.post('/merchants/rename', payload)

/* ---------------------------------- 密钥 ---------------------------------- */

export const fetchKeys = (params) => http.get('/keys', { params })
export const createKey = (payload) => http.post('/keys/create', payload)
export const setKeyStatus = (payload) => http.post('/keys/status', payload)
export const deleteKey = (payload) => http.post('/keys/delete', payload)

/* ---------------------------------- 音乐创作 ---------------------------------- */

export const generateMusic = (payload) => http.post('/music/generate', payload)
export const generateVideo = (payload) => http.post('/music/video', payload)
export const uploadMusic = (payload) => http.post('/music/upload', payload)
export const fetchSongs = (params) => http.get('/songs', { params })
export const deleteSong = (id) => http.post('/songs/delete', { id })
export const fetchCertificate = (taskId) => http.get('/songs/certificate', { params: { task_id: taskId } })
// 下载音频、算指纹需要时间，放宽超时
export const issueCertificate = (payload) => http.post('/songs/certificate', payload, { timeout: 3 * 60 * 1000 })

/** 下载文件：接口要带登录令牌，不能直接用链接打开，这里取回文件后触发浏览器下载 */
async function downloadFile(path, fallbackName) {
  const res = await fetch(`/admin/api${path}`, { headers: { Authorization: `Bearer ${getToken()}` } })
  if (!res.ok) {
    let message = '下载失败'
    try {
      message = (await res.json()).message || message
    } catch {
      // 非 JSON 错误体，用默认提示
    }
    ElMessage.error(message)
    throw new Error(message)
  }

  const disposition = res.headers.get('Content-Disposition') || ''
  const match = disposition.match(/filename\*=UTF-8''([^;]+)/i)
  const name = match ? decodeURIComponent(match[1]) : fallbackName

  const url = URL.createObjectURL(await res.blob())
  const link = document.createElement('a')
  link.href = url
  link.download = name
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10000)
}

export const downloadCertificate = (no) =>
  downloadFile(`/songs/certificate/file?no=${encodeURIComponent(no)}`, `创作证明-${no}.pdf`)

/** 以 MP3 格式下载作品（上游多为 M4A，由服务端转码） */
export const downloadSongMp3 = (taskId) => downloadFile(`/songs/mp3?task_id=${taskId}`, `作品-${taskId}.mp3`)
export const fetchVoices = (params) => http.get('/voices', { params })
// 演唱音色（Mureka）：创作时直接用这个声音演唱
export const fetchVocals = (params) => http.get('/vocals', { params })
export const createVocal = (payload) => http.post('/vocals/create', payload, { timeout: 3 * 60 * 1000 })
export const deleteVocal = (id) => http.post('/vocals/delete', { id })
export const trainVoice = (payload) => http.post('/voices/train', payload)
export const deleteVoice = (id) => http.post('/voices/delete', { id })
// 翻唱作品库里的歌要先转 MP3，首次可能要几十秒
export const coverWithVoice = (payload) => http.post('/voices/cover', payload, { timeout: 3 * 60 * 1000 })
// 音频较大，放宽超时并透出上传进度
export const uploadSample = (form, onUploadProgress) =>
  http.post('/voices/sample', form, { timeout: 5 * 60 * 1000, onUploadProgress })
export const fetchCapabilities = (group) => http.get('/capabilities', { params: { group } })
// 长 MV：写分镜可能要一两分钟，放宽超时
export const fetchMvProjects = (params) => http.get('/mv/projects', { params })
export const fetchMvProject = (id) => http.get('/mv/project', { params: { id } })
export const quoteMv = (payload) => http.post('/mv/quote', payload)
export const createMv = (payload) => http.post('/mv/create', payload, { timeout: 5 * 60 * 1000 })
export const rewriteMv = (payload) => http.post('/mv/rewrite', payload, { timeout: 5 * 60 * 1000 })
export const saveMvStoryboard = (payload) => http.post('/mv/storyboard', payload)
export const startMv = (id) => http.post('/mv/start', { id })
export const retryMv = (id) => http.post('/mv/retry', { id })
export const deleteMv = (id) => http.post('/mv/delete', { id })
export const uploadMvRef = (form) => http.post('/mv/ref', form, { timeout: 2 * 60 * 1000 })
export const createMvPortrait = (payload) => http.post('/mv/portrait', payload)
export const fetchMvPortrait = (taskId) => http.get('/mv/portrait', { params: { task_id: taskId }, timeout: 2 * 60 * 1000 })
export const studioGenerate = (payload) => http.post('/studio/generate', payload)

/* ---------------------------------- 任务 ---------------------------------- */

export const fetchTasks = (params) => http.get('/tasks', { params })
export const fetchTaskDetail = (id) => http.get('/tasks/detail', { params: { id } })
export const refundTask = (payload) => http.post('/tasks/refund', payload)
// 失败任务不退积分，可免费重试
export const retryTask = (id) => http.post('/tasks/retry', { id }, { timeout: 3 * 60 * 1000 })

/* ---------------------------------- 积分 ---------------------------------- */

export const fetchPointLogs = (params) => http.get('/points/logs', { params })
export const adjustPoints = (payload) => http.post('/points/adjust', payload)

export default http

/**
 * 浏览器 → 本站服务端 → 上游 SUNO API 的转发代理。
 *
 * 在线测试工具需要带 Authorization 头跨域请求上游，浏览器会被 CORS 拦下，
 * 因此统一由服务端转发。access_key 只在本次请求中透传，不落盘、不记录。
 */
interface ProxyPayload {
  endpoint: string
  method: 'GET' | 'POST'
  accessKey: string
  query?: Record<string, unknown>
  body?: Record<string, unknown>
}

export default defineEventHandler(async (event) => {
  const payload = await readBody<ProxyPayload>(event)
  const { upstreamBase } = useRuntimeConfig(event)

  if (!payload?.endpoint || !payload.endpoint.startsWith('/api/')) {
    setResponseStatus(event, 400)
    return { code: 400, message: '非法的 endpoint', data: null }
  }

  if (!payload.accessKey) {
    setResponseStatus(event, 401)
    return { code: 401, message: '缺少 access_key，请在右侧填写后再测试', data: null }
  }

  const method = payload.method === 'GET' ? 'GET' : 'POST'
  const url = new URL(payload.endpoint, upstreamBase)

  if (method === 'GET' && payload.query) {
    for (const [key, value] of Object.entries(payload.query)) {
      if (value === undefined || value === null || value === '') continue
      url.searchParams.set(key, String(value))
    }
  }

  const startedAt = Date.now()

  try {
    const response = await $fetch.raw(url.toString(), {
      method,
      headers: {
        Authorization: `Bearer ${payload.accessKey}`,
        'Content-Type': 'application/json',
        Accept: 'application/json'
      },
      body: method === 'POST' ? payload.body ?? {} : undefined,
      // 交由前端展示上游的错误码与错误体，不在此处抛异常
      ignoreResponseError: true,
      timeout: 60_000
    })

    // 下载创作证明等接口成功时返回文件，不是 JSON；这里只回传文件概况，避免把二进制塞进测试结果
    const contentType = response.headers.get('content-type') || ''
    let data = response._data
    if (response.ok && !contentType.includes('json')) {
      const size = data instanceof Blob ? data.size : Number(response.headers.get('content-length') || 0)
      data = {
        code: 200,
        message: '接口返回了文件，测试工具不展示文件内容，请用代码示例下载',
        success: true,
        data: {
          content_type: contentType,
          size,
          content_disposition: response.headers.get('content-disposition') || ''
        }
      }
    }

    return {
      ok: response.status >= 200 && response.status < 300,
      status: response.status,
      duration: Date.now() - startedAt,
      requestUrl: url.toString(),
      data
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : '请求上游服务失败'
    setResponseStatus(event, 502)
    return {
      ok: false,
      status: 502,
      duration: Date.now() - startedAt,
      requestUrl: url.toString(),
      data: { code: 502, message, data: null }
    }
  }
})

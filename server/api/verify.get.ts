/**
 * 创作证明核验：浏览器 → 本站服务端 → 后端公开核验接口。
 *
 * 核验接口免鉴权、按来源 IP 限流；这里把访客的真实 IP 放进 X-Real-IP，
 * 避免所有访客都被算成本站服务器同一个 IP 而互相挤占额度。
 */
export default defineEventHandler(async (event) => {
  const { no } = getQuery(event)
  const certificateNo = String(no ?? '').trim()
  if (!certificateNo) {
    setResponseStatus(event, 400)
    return { code: 400, message: '请输入证书编号', data: null, success: false }
  }

  const { upstreamBase } = useRuntimeConfig(event)
  const url = new URL('/api/v1/certificate/verify', upstreamBase)
  url.searchParams.set('certificate_no', certificateNo)

  try {
    const response = await $fetch.raw(url.toString(), {
      headers: { Accept: 'application/json', 'X-Real-IP': getRequestIP(event, { xForwardedFor: true }) || '' },
      ignoreResponseError: true,
      timeout: 15_000
    })
    setResponseStatus(event, response.status)
    return response._data
  } catch {
    setResponseStatus(event, 502)
    return { code: 502, message: '核验服务暂时不可用，请稍后重试', data: null, success: false }
  }
})

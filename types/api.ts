export type ParamType = 'string' | 'number' | 'boolean' | 'object'

export interface ApiParam {
  name: string
  type: ParamType
  required: boolean
  description: string
  /** 枚举可选值，渲染为下拉框 */
  options?: string[]
  /** 默认值，用于预填测试表单 */
  default?: unknown
  /** 使用多行文本框 */
  multiline?: boolean
  /** 表单占位提示 */
  placeholder?: string
}

export interface ApiPricing {
  /** 官方参考价 */
  official: number
  /** 本平台价格 */
  our: number
  unit: string
}

export interface ApiDefinition {
  id: string
  name: string
  provider: string
  category: 'system' | 'music'
  description: string
  endpoint: string
  method: 'GET' | 'POST'
  params: ApiParam[]
  pricing: ApiPricing
  responseExample?: Record<string, unknown>
  /** 接口详细说明（HTML） */
  guide?: string
  /** 调用提示（要点列表） */
  notes?: string[]
}

export interface ApiCategory {
  key: string
  title: string
  apis: ApiDefinition[]
}

/** 统一响应结构 */
export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
  success?: boolean
}

/** 任务状态 */
export type TaskStatus = 'pending' | 'processing' | 'completed' | 'failed'

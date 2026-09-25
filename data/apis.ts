import type { ApiCategory, ApiDefinition } from '~/types/api'

/** 异步任务通用流程说明 */
const ASYNC_FLOW = `
<p><strong>调用流程：</strong>提交请求 → 拿到 <code>task_id</code> → 轮询「查询音乐任务」→ <code>status</code> 变为
<code>completed</code> 后读取结果。建议轮询间隔 3~5 秒。</p>
<p>接口返回的 <code>task_id</code> 只是任务编号，<strong>不是最终结果</strong>。</p>
`

/** 音频类任务的结果字段说明 */
const AUDIO_RESULT_TABLE = `
<p><strong>任务完成后 result 中的关键字段：</strong></p>
<table>
  <thead><tr><th>字段路径</th><th>类型</th><th>说明</th></tr></thead>
  <tbody>
    <tr><td><code>custom_id</code></td><td>string</td><td>Suno 音乐 ID（UUID），后续所有加工操作都用它</td></tr>
    <tr><td><code>fileInfo.mp3Url</code></td><td>string</td><td>音频地址，有效期 1 小时，请及时转存</td></tr>
    <tr><td><code>fileInfo.coverUrl</code></td><td>string</td><td>封面图地址</td></tr>
    <tr><td><code>fileInfo.duration</code></td><td>number</td><td>时长（秒）</td></tr>
    <tr><td><code>extend</code></td><td>string (JSON)</td><td>完整歌曲信息，需 <code>JSON.parse</code> 后使用</td></tr>
  </tbody>
</table>
`

export const apiList: ApiDefinition[] = [
  {
    id: 'points-balance',
    name: '查询积分余额',
    provider: '系统通用',
    category: 'system',
    description: '查询当前商户的积分余额。该接口不消耗积分，可随时调用。',
    endpoint: '/api/v1/points/balance',
    method: 'GET',
    params: [],
    pricing: { official: 0, our: 0, unit: '免费' },
    responseExample: { code: 200, message: '查询成功', data: { remaining_points: 1000 } },
    guide: `
      <p><strong>响应说明：</strong></p>
      <table>
        <thead><tr><th>字段</th><th>类型</th><th>说明</th></tr></thead>
        <tbody>
          <tr><td><code>code</code></td><td>number</td><td>状态码，200 表示成功</td></tr>
          <tr><td><code>message</code></td><td>string</td><td>响应消息</td></tr>
          <tr><td><code>data.remaining_points</code></td><td>number</td><td>剩余积分数量</td></tr>
        </tbody>
      </table>
    `,
    notes: ['调用本接口不消耗积分', '建议在提交生成任务前先确认余额充足，避免 402 错误']
  },
  {
    id: 'points-logs',
    name: '查询积分流水',
    provider: '系统通用',
    category: 'system',
    description: '分页查询积分变动记录，包含消耗、充值、手动调整与退还。',
    endpoint: '/api/v1/points/logs',
    method: 'GET',
    params: [
      { name: 'page', type: 'number', required: false, description: '页码', default: 1 },
      { name: 'limit', type: 'number', required: false, description: '每页数量', default: 20 }
    ],
    pricing: { official: 0, our: 0, unit: '免费' },
    responseExample: {
      code: 200,
      message: 'success',
      data: {
        total: 2,
        list: [
          { id: 9001, type: 1, points: -36, balance: 964, remark: '生成音乐', created_at: '2026-09-21 10:22:11' },
          { id: 9002, type: 4, points: 36, balance: 1000, remark: '任务失败退还', created_at: '2026-09-21 10:31:02' }
        ]
      }
    },
    guide: `
      <p><strong>type 字段取值：</strong></p>
      <table>
        <thead><tr><th>值</th><th>含义</th></tr></thead>
        <tbody>
          <tr><td><code>1</code></td><td>消耗</td></tr>
          <tr><td><code>2</code></td><td>充值</td></tr>
          <tr><td><code>3</code></td><td>手动调整</td></tr>
          <tr><td><code>4</code></td><td>退还（任务未能提交上游时自动退还，或客服人工退款）</td></tr>
        </tbody>
      </table>
    `
  },
  {
    id: 'music-generate',
    name: '生成音乐',
    provider: 'SUNO',
    category: 'music',
    description:
      '核心接口。支持灵感模式（仅需描述）与自定义模式（歌词 / 风格 / 标题），并包含延长、翻唱能力。一次提交生成两首歌。',
    endpoint: '/api/v1/music/generate',
    method: 'POST',
    params: [
      {
        name: 'gpt_description_prompt',
        type: 'string',
        required: false,
        multiline: true,
        description: '【灵感模式】音乐描述，与 prompt 二选一',
        placeholder: '一首欢快的流行歌，关于夏天的美好回忆'
      },
      {
        name: 'prompt',
        type: 'string',
        required: false,
        multiline: true,
        description: '【自定义 / 延长 / 翻唱】歌词内容，延长与翻唱时必填（或改填 gpt_description_prompt）。V4 限 3000 字，V5 限 5000 字',
        placeholder: '[Verse]\n第一段歌词\n[Chorus]\n副歌歌词'
      },
      {
        name: 'tags',
        type: 'string',
        required: false,
        description: '【自定义模式】音乐风格。V4 限 200 字，V5 限 1000 字',
        placeholder: 'pop, acoustic, female vocals'
      },
      { name: 'negative_tags', type: 'string', required: false, description: '排除的风格提示词' },
      {
        name: 'mv',
        type: 'string',
        required: true,
        description:
          '模型版本。V6：chirp-hawk（旗舰）/ chirp-hawk-wild（更外放）/ chirp-goose（V6-mini）；' +
          '旧版本：chirp-fenix(V5.5)、chirp-crow(V5)、chirp-bluejay(V4.5+)、chirp-auk(V4.5)、chirp-v4(V4)；' +
          'chirp-v3-5 / chirp-v3-0 已下线，传入会被自动定向到较新版本',
        options: [
          'chirp-hawk',
          'chirp-hawk-wild',
          'chirp-goose',
          'chirp-fenix',
          'chirp-crow',
          'chirp-bluejay',
          'chirp-auk',
          'chirp-v4',
          'chirp-v3-5',
          'chirp-v3-0'
        ],
        default: 'chirp-hawk'
      },
      {
        name: 'title',
        type: 'string',
        required: true,
        description: '歌名。V4 限 80 字，V5 限 100 字',
        default: '夏日回忆'
      },
      { name: 'make_instrumental', type: 'boolean', required: true, description: '是否纯音乐（无歌词）', default: false },
      {
        name: 'task',
        type: 'string',
        required: false,
        description: '操作类型：不传为默认生成；extend = 延长，cover = 翻唱',
        options: ['extend', 'cover']
      },
      { name: 'continue_clip_id', type: 'string', required: false, description: '【延长模式】被延长歌曲的 custom_id' },
      { name: 'continue_at', type: 'number', required: false, description: '【延长模式】从第几秒开始延长' },
      { name: 'cover_clip_id', type: 'string', required: false, description: '【翻唱模式】原歌曲的 custom_id' },
      {
        name: 'metadata',
        type: 'object',
        required: false,
        multiline: true,
        description: '高级参数，如 vocal_gender、control_sliders 等',
        default: { vocal_gender: 'f', control_sliders: { style_weight: 0.87, weirdness_constraint: 0.75 } }
      }
    ],
    pricing: { official: 1, our: 0.36, unit: '¥/次' },
    responseExample: { code: 200, data: [199824, 199825], success: true, message: '请求成功' },
    guide: `
      ${ASYNC_FLOW}
      <p><strong>四种创作模式：</strong></p>
      <table>
        <thead><tr><th>模式</th><th>必填参数</th><th>说明</th></tr></thead>
        <tbody>
          <tr><td>灵感模式</td><td><code>gpt_description_prompt</code> + <code>mv</code> + <code>title</code></td><td>给一句描述，模型自由发挥</td></tr>
          <tr><td>自定义模式</td><td><code>prompt</code> + <code>tags</code> + <code>mv</code> + <code>title</code></td><td>自己写歌词与风格标签</td></tr>
          <tr><td>延长</td><td><code>task=extend</code> + <code>continue_clip_id</code> + <code>prompt</code></td><td>把已有歌曲续写下去</td></tr>
          <tr><td>翻唱</td><td><code>task=cover</code> + <code>cover_clip_id</code> + <code>prompt</code></td><td>用新风格重唱已有歌曲</td></tr>
        </tbody>
      </table>
      ${AUDIO_RESULT_TABLE}
    `,
    notes: [
      '一次生成两首歌，因此返回两个 task_id，需要分别查询',
      'task_id 是数字任务编号，custom_id 是 Suno 音乐 UUID，后续加工一律使用 custom_id',
      '任务失败不退积分，可调用「重试失败任务」接口免费重新生成，任务 ID 不变'
    ]
  },
  {
    id: 'music-sound',
    name: '生成音效',
    provider: 'SUNO',
    category: 'music',
    description: '生成短音效（如雨声、脚步声），支持 BPM、音调与循环设置。',
    endpoint: '/api/v1/music/sound',
    method: 'POST',
    params: [
      { name: 'title', type: 'string', required: true, description: '音效标题，最多 100 字符', default: 'Rain' },
      { name: 'tags', type: 'string', required: true, description: '音效风格，最多 1000 字符', default: 'rain' },
      {
        name: 'mv',
        type: 'string',
        required: true,
        description: '模型版本，音效生成仅支持 chirp-crow 或 chirp-fenix',
        options: ['chirp-crow', 'chirp-fenix'],
        default: 'chirp-crow'
      },
      { name: 'tempo', type: 'number', required: false, description: 'BPM', default: 99 },
      { name: 'key', type: 'string', required: false, description: '音调', default: 'D' },
      {
        name: 'loop',
        type: 'boolean',
        required: false,
        description: '是否循环：true = 循环，false = 单次',
        default: true
      }
    ],
    pricing: { official: 0, our: 0.1, unit: '¥/次' },
    responseExample: { code: 200, message: 'success', data: { task_ids: ['205041', '205042'] } },
    guide: `
      ${ASYNC_FLOW}
      <p>服务端会自动固定 <code>task=sound</code>、<code>generation_type=TEXT</code>、<code>make_instrumental=true</code>；
      <code>tempo</code> / <code>key</code> / <code>loop</code> 会映射到上游的 <code>metadata.sound_configs</code>。</p>
      ${AUDIO_RESULT_TABLE}
    `,
    notes: ['查询方式与生成音乐一致：GET /api/v1/music/task?id=xxx']
  },
  {
    id: 'music-query-task',
    name: '查询音乐任务',
    provider: 'SUNO',
    category: 'music',
    description: '查询单个音乐任务的状态与结果。免费调用。',
    endpoint: '/api/v1/music/task',
    method: 'GET',
    params: [{ name: 'id', type: 'number', required: true, description: '任务 ID（数字），由生成类接口返回' }],
    pricing: { official: 0, our: 0, unit: '免费' },
    responseExample: {
      code: 200,
      data: {
        id: 199824,
        status: 3,
        fileInfo: {
          mp4Url: 'https://cdn.example.com/ai/xxxx.mp4',
          mp3Url: 'https://cdn.example.com/ai/xxxx.mp3',
          coverUrl: 'https://cdn.example.com/ai/xxxx.png'
        },
        custom_id: '0f65a62b-3c55-4af6-8353-56be806b93d2',
        extend: '[...]'
      },
      success: true,
      message: '请求成功'
    },
    guide: `
      <p><strong>任务状态：</strong></p>
      <table>
        <thead><tr><th>status</th><th>含义</th></tr></thead>
        <tbody>
          <tr><td><code>pending</code></td><td>排队中</td></tr>
          <tr><td><code>processing</code></td><td>生成中</td></tr>
          <tr><td><code>completed</code></td><td>已完成，可读取结果</td></tr>
          <tr><td><code>failed</code></td><td>失败，不退积分，可调用重试接口免费重新生成</td></tr>
        </tbody>
      </table>
      ${AUDIO_RESULT_TABLE}
      <p><code>retry_count</code> 为已免费重试的次数；失败任务默认不退积分，<code>points_refunded</code> 仅在客服人工退款后为 <code>true</code>。</p>
    `,
    notes: ['轮询间隔建议 3~5 秒，过于频繁会触发 429 限流', '音频与视频链接有效期为 1 小时，请及时转存']
  },
  {
    id: 'music-batch-query',
    name: '批量查询音乐任务',
    provider: 'SUNO',
    category: 'music',
    description: '一次查询多个任务状态，适合生成音乐后同时查两个 task_id。',
    endpoint: '/api/v1/music/tasks',
    method: 'GET',
    params: [
      { name: 'ids', type: 'string', required: true, description: '任务 ID 列表，逗号分隔，如：199824,199825' },
      { name: 'page', type: 'number', required: false, description: '页码', default: 1 },
      { name: 'size', type: 'number', required: false, description: '每页数量', default: 10 }
    ],
    pricing: { official: 0, our: 0, unit: '免费' },
    responseExample: {
      code: 200,
      message: '请求成功',
      data: { total: 2, list: [{ id: 199824, status: 3, custom_id: '0f65a62b-3c55-4af6-8353-56be806b93d2' }] }
    },
    guide: `<p>返回结构与单个查询一致，逐条包含在列表中。免费调用，是同时轮询多首歌时的推荐方式。</p>`
  },
  {
    id: 'music-retry',
    name: '重试失败任务',
    provider: 'SUNO',
    category: 'music',
    description: '任务失败不退积分，可用原参数免费重新生成。任务 ID 不变，重试后继续用查询接口轮询。',
    endpoint: '/api/v1/music/retry',
    method: 'POST',
    params: [{ name: 'id', type: 'number', required: true, description: '失败任务的 ID（数字）' }],
    pricing: { official: 0, our: 0, unit: '免费' },
    responseExample: {
      code: 200,
      message: '请求成功',
      data: { id: 199824, status: 'pending', status_code: 1, retry_count: 1, points_refunded: false }
    },
    guide: `<p>只有 <code>status</code> 为 <code>failed</code> 的任务可以重试。重试不扣积分，每个任务最多免费重试 3 次，
      返回中的 <code>retry_count</code> 为已重试次数。重试后任务回到 <code>pending</code>，按原方式轮询即可。</p>`,
    notes: ['只有上游当场拒收、任务未建立时才会立即退还积分，这种情况不会产生任务 ID', '超过重试次数请联系客服']
  },
  {
    id: 'v2-music-download-wav',
    name: '下载 WAV',
    provider: 'SUNO',
    category: 'music',
    description: '通过 Suno 音乐 custom_id 创建 WAV 无损下载任务。',
    endpoint: '/api/v2/music/download-wav',
    method: 'POST',
    params: [
      { name: 'suno_id', type: 'string', required: true, description: 'Suno 音乐 ID，即查询结果中的 custom_id' }
    ],
    pricing: { official: 0.05, our: 0.05, unit: '¥/次' },
    responseExample: { code: 200, message: 'success', data: { task_id: '207031', status: 'pending' } },
    guide: ASYNC_FLOW,
    notes: ['注意该接口为 /api/v2 路径', '完成后从任务结果中获取 WAV 文件地址，链接 1 小时内有效']
  },
  {
    id: 'v2-music-download-mp3',
    name: '下载 MP3',
    provider: 'SUNO',
    category: 'music',
    description: '通过 Suno 音乐 custom_id 创建 MP3 下载任务。',
    endpoint: '/api/v2/music/download-mp3',
    method: 'POST',
    params: [
      { name: 'suno_id', type: 'string', required: true, description: 'Suno 音乐 ID，即查询结果中的 custom_id' }
    ],
    pricing: { official: 0.05, our: 0.05, unit: '¥/次' },
    responseExample: { code: 200, message: 'success', data: { task_id: '207031', status: 'pending' } },
    guide: ASYNC_FLOW,
    notes: ['注意该接口为 /api/v2 路径']
  },
  {
    id: 'v2-music-download-m4a',
    name: '下载 M4A',
    provider: 'SUNO',
    category: 'music',
    description: '通过 Suno 音乐 custom_id 创建 M4A 下载任务。',
    endpoint: '/api/v2/music/download-m4a',
    method: 'POST',
    params: [
      { name: 'suno_id', type: 'string', required: true, description: 'Suno 音乐 ID，即查询结果中的 custom_id' }
    ],
    pricing: { official: 0.05, our: 0.05, unit: '¥/次' },
    responseExample: { code: 200, message: 'success', data: { task_id: '207031', status: 'pending' } },
    guide: ASYNC_FLOW,
    notes: ['注意该接口为 /api/v2 路径']
  },
  {
    id: 'music-certificate',
    name: '签发创作证明',
    provider: '本平台',
    category: 'music',
    description: '为已完成的作品签发 PDF 创作证明，登记作品信息与音频指纹，可凭证书编号在线核验。',
    endpoint: '/api/v1/music/certificate',
    method: 'POST',
    params: [
      { name: 'suno_id', type: 'string', required: true, description: 'Suno 音乐 ID，即查询结果中的 custom_id' },
      {
        name: 'author',
        type: 'string',
        required: false,
        description: '印在证书上的署名作者，最多 50 字；不传则用商户名称。签发后不可修改',
        placeholder: '安沐心音乐工作室'
      }
    ],
    pricing: { official: 0, our: 1, unit: '¥/份' },
    responseExample: {
      code: 200,
      message: '请求成功',
      success: true,
      data: {
        certificate_no: 'SC20260925-7K3QM9XA',
        task_id: 199824,
        suno_id: '4852c1a7-cc06-4540-9e88-377ee9ebdefa',
        title: 'Summer Breeze',
        author: '安沐心音乐工作室',
        duration: 152.4,
        audio_sha256: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08',
        song_created_at: '2026-09-22T11:52:50+08:00',
        issued_at: '2026-09-25T16:30:00+08:00',
        points_cost: 100,
        charged: true,
        remaining_points: 900,
        download_path: '/api/v1/music/certificate/download?certificate_no=SC20260925-7K3QM9XA',
        verify_url: 'https://open.suno.cn/verify?no=SC20260925-7K3QM9XA'
      }
    },
    guide: `<p>同步接口，直接返回证书信息，无需轮询。证书包含作品名称、署名作者、作品 ID、创作完成时间、时长、风格、模型、歌词，
      以及作品 MP3 文件的 <code>SHA-256</code> 指纹（与平台下载的 MP3 为同一文件），并附在线核验二维码。</p>
      <p><strong>计费：</strong>每首作品只签发一份，首次签发扣 100 积分；之后重复调用返回同一份证书，
      <code>charged</code> 为 <code>false</code>，不再扣费。音频已失效等原因签发失败时不扣费。</p>
      <p>拿到 <code>certificate_no</code> 后，调用「下载创作证明」获取 PDF 文件。</p>`,
    notes: [
      '作品需已生成完成；音频链接失效后无法再签发',
      '证明用于证明创作时间、来源与音频文件未被改动，不替代著作权登记',
      '删除作品不影响已签发的证明，仍可下载与核验'
    ]
  },
  {
    id: 'music-certificate-download',
    name: '下载创作证明',
    provider: '本平台',
    category: 'music',
    description: '按证书编号下载创作证明 PDF，不扣费。',
    endpoint: '/api/v1/music/certificate/download',
    method: 'GET',
    params: [
      { name: 'certificate_no', type: 'string', required: true, description: '证书编号，由「签发创作证明」返回' }
    ],
    pricing: { official: 0, our: 0, unit: '¥/次' },
    guide: `<p>成功时直接返回 PDF 文件（<code>Content-Type: application/pdf</code>），不是 JSON；
      失败时仍返回统一的 JSON 错误结构。只能下载本商户签发的证书。</p>
      <p>示例：<code>curl -H "Authorization: Bearer $ACCESS_KEY" -o certificate.pdf "$BASE/api/v1/music/certificate/download?certificate_no=SC20260925-7K3QM9XA"</code></p>`,
    notes: ['在线测试工具不展示 PDF 内容，请用代码示例下载', '同一份证书重复下载内容一致']
  },
  {
    id: 'certificate-verify',
    name: '核验创作证明',
    provider: '本平台',
    category: 'music',
    description: '按证书编号核验创作证明，返回登记的作品信息与音频指纹。',
    endpoint: '/api/v1/certificate/verify',
    method: 'GET',
    params: [{ name: 'certificate_no', type: 'string', required: true, description: '证书编号' }],
    pricing: { official: 0, our: 0, unit: '¥/次' },
    responseExample: {
      code: 200,
      message: '请求成功',
      success: true,
      data: {
        certificate_no: 'SC20260925-7K3QM9XA',
        title: 'Summer Breeze',
        author: '安沐心音乐工作室',
        suno_id: '4852c1a7-cc06-4540-9e88-377ee9ebdefa',
        duration: 152.4,
        audio_sha256: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08',
        song_created_at: '2026-09-22T11:52:50+08:00',
        issued_at: '2026-09-25T16:30:00+08:00',
        issuer: 'SUNO API 开放平台'
      }
    },
    guide: `<p>公开接口，<strong>无需 access_key</strong>，按来源 IP 限流（每分钟 30 次）。证书编号不存在时返回 404。
      不返回歌词与商户信息。</p>
      <p>面向普通用户的核验页面见本站「证书核验」，证书上的二维码也指向那里。</p>`,
    notes: ['证书编号不区分大小写']
  },
  {
    id: 'music-upload',
    name: '上传参考音频',
    provider: 'SUNO',
    category: 'music',
    description: '上传一首已有歌曲，拿到 custom_id 后可用于延长、翻唱等操作。',
    endpoint: '/api/v1/music/upload',
    method: 'POST',
    params: [
      {
        name: 'audio_url',
        type: 'string',
        required: true,
        description: '音频文件的公开 URL（MP3）',
        placeholder: 'https://example.com/song.mp3'
      },
      {
        name: 'copyrightAudio',
        type: 'boolean',
        required: false,
        description: '是否使用受版权保护的音频，传 true 额外扣除 15 积分',
        default: false
      }
    ],
    pricing: { official: 0.1, our: 0.01, unit: '¥/次' },
    responseExample: { code: 200, data: 1344761, success: true, message: '请求成功' },
    guide: `
      ${ASYNC_FLOW}
      <p>任务完成后从 <code>result.custom_id</code> 取得音乐 ID，即可作为
      <code>continue_clip_id</code>（延长）或 <code>cover_clip_id</code>（翻唱）使用。</p>
    `,
    notes: ['音频 URL 必须可公开访问，且能被上游直接下载']
  },
  {
    id: 'music-whole-song',
    name: '获取整首歌',
    provider: 'SUNO',
    category: 'music',
    description: '把多段续写片段合并成一首完整歌曲。',
    endpoint: '/api/v1/music/whole-song',
    method: 'POST',
    params: [
      {
        name: 'clip_id',
        type: 'string',
        required: true,
        description: '最后一次生成的 Suno 音乐 ID（custom_id，非数字 task_id）'
      }
    ],
    pricing: { official: 0.3, our: 0.01, unit: '¥/次' },
    responseExample: { code: 200, data: 1344761, success: true, message: '请求成功' },
    guide: `${ASYNC_FLOW}${AUDIO_RESULT_TABLE}`,
    notes: ['需传入最后一次延长得到的 custom_id，系统会回溯合并前序片段']
  },
  {
    id: 'music-aligned-lyrics',
    name: '获取歌词时间戳',
    provider: 'SUNO',
    category: 'music',
    description: '为歌词生成逐字时间戳对齐信息，可用于字幕、卡拉 OK 效果。',
    endpoint: '/api/v1/music/aligned-lyrics',
    method: 'POST',
    params: [
      { name: 'lyrics', type: 'string', required: true, multiline: true, description: '歌曲的完整歌词文本' },
      {
        name: 'suno_id',
        type: 'string',
        required: true,
        description: 'Suno 音乐 ID（custom_id），必须是已生成完成的歌曲'
      }
    ],
    pricing: { official: 0.2, our: 0.01, unit: '¥/次' },
    responseExample: { code: 200, data: 1344761, success: true, message: '请求成功' },
    guide: `
      ${ASYNC_FLOW}
      <p><strong>result.extend 解析后 alignment 数组的字段：</strong></p>
      <table>
        <thead><tr><th>字段</th><th>类型</th><th>说明</th></tr></thead>
        <tbody>
          <tr><td><code>word</code></td><td>string</td><td>歌词文字，可能包含换行符与段落标记</td></tr>
          <tr><td><code>start_s</code></td><td>number</td><td>该词开始时间（秒）</td></tr>
          <tr><td><code>end_s</code></td><td>number</td><td>该词结束时间（秒）</td></tr>
          <tr><td><code>success</code></td><td>boolean</td><td>该词是否对齐成功</td></tr>
          <tr><td><code>p_align</code></td><td>number</td><td>对齐置信度，1 表示高置信</td></tr>
        </tbody>
      </table>
    `,
    notes: ['未完成的歌曲会导致对齐失败', 'extend 为 JSON 字符串，需 JSON.parse 后读取 alignment']
  },
  {
    id: 'music-upsample',
    name: 'Remaster 音乐',
    provider: 'SUNO',
    category: 'music',
    description: '对已有歌曲做升采样重制，提升音质。一次生成两个版本。',
    endpoint: '/api/v1/music/upsample',
    method: 'POST',
    params: [
      { name: 'clip_id', type: 'string', required: true, description: 'Suno 音乐 ID（custom_id，非数字 task_id）' },
      {
        name: 'model_name',
        type: 'string',
        required: true,
        description:
          '需与原歌曲版本匹配：V6 → chirp-halibut，V5.5 → chirp-flounder，' +
          'V5 → chirp-carp，V4.5 → chirp-bass，V4 → chirp-up',
        options: ['chirp-halibut', 'chirp-flounder', 'chirp-carp', 'chirp-bass', 'chirp-up'],
        default: 'chirp-halibut'
      },
      {
        name: 'variation_category',
        type: 'string',
        required: false,
        description: '仅 V5 支持：变化幅度',
        options: ['subtle', 'normal', 'high']
      }
    ],
    pricing: { official: 1, our: 0.36, unit: '¥/次' },
    responseExample: { code: 200, data: [199824, 199825], success: true, message: '请求成功' },
    guide: `${ASYNC_FLOW}${AUDIO_RESULT_TABLE}`,
    notes: ['model_name 与原歌曲版本不匹配会导致任务失败']
  },
  {
    id: 'music-video',
    name: '生成音乐视频',
    provider: 'SUNO',
    category: 'music',
    description: '为已生成的歌曲制作一段 MV 视频。',
    endpoint: '/api/v1/music/video',
    method: 'POST',
    params: [
      { name: 'task_id', type: 'number', required: true, description: '任务 ID（数字，即查询结果中的 id 字段）' },
      { name: 'suno_id', type: 'string', required: true, description: 'Suno 音乐 ID（custom_id）' }
    ],
    pricing: { official: 0.1, our: 0.01, unit: '¥/次' },
    responseExample: { code: 200, data: 199824, success: true, message: '请求成功' },
    guide: `
      ${ASYNC_FLOW}
      <p><strong>视频任务的 result 结构与生成音乐不同</strong>：没有 <code>fileInfo.mp3Url</code> 与 <code>custom_id</code>，
      核心数据在 <code>extend</code>（JSON 字符串）中。</p>
      <table>
        <thead><tr><th>字段路径</th><th>类型</th><th>说明</th></tr></thead>
        <tbody>
          <tr><td><code>proxy_url</code></td><td>string</td><td>平台临时签名代理地址，建议优先使用；过期后重新查询任务可获取新地址</td></tr>
          <tr><td><code>extend.status</code></td><td>string</td><td><code>complete</code> 表示生成成功</td></tr>
          <tr><td><code>extend.video_url</code></td><td>string</td><td>MP4 视频地址，有效期 1 小时</td></tr>
        </tbody>
      </table>
    `,
    notes: ['优先使用 proxy_url，原始 video_url 仅 1 小时有效']
  },
  {
    id: 'music-crop',
    name: '裁剪音乐',
    provider: 'SUNO',
    category: 'music',
    description: '按时间区间裁剪出音乐片段。',
    endpoint: '/api/v1/music/crop',
    method: 'POST',
    params: [
      { name: 'clip_id', type: 'string', required: true, description: 'Suno 音乐 ID（custom_id）' },
      { name: 'crop_start_s', type: 'number', required: true, description: '裁剪开始时间（秒），必须 ≥ 0', default: 0 },
      {
        name: 'crop_end_s',
        type: 'number',
        required: true,
        description: '裁剪结束时间（秒），必须大于开始时间',
        default: 60
      }
    ],
    pricing: { official: 0.1, our: 0.01, unit: '¥/次' },
    responseExample: { code: 200, data: 199824, success: true, message: '请求成功' },
    guide: `${ASYNC_FLOW}${AUDIO_RESULT_TABLE}`,
    notes: ['crop_end_s 必须大于 crop_start_s，否则参数校验失败']
  },
  {
    id: 'music-speed',
    name: '调整音乐速度',
    provider: 'SUNO',
    category: 'music',
    description: '对歌曲做变速处理，可选择是否保持原始音调。',
    endpoint: '/api/v1/music/speed',
    method: 'POST',
    params: [
      { name: 'clip_id', type: 'string', required: true, description: 'Suno 音乐 ID（custom_id）' },
      {
        name: 'speed_multiplier',
        type: 'number',
        required: true,
        description: '速度倍数',
        options: ['0.25', '0.5', '0.75', '1', '1.25', '1.5', '2'],
        default: 1
      },
      { name: 'keep_pitch', type: 'boolean', required: false, description: '变速同时保持原始音调', default: false },
      { name: 'title', type: 'string', required: false, description: '歌名' }
    ],
    pricing: { official: 0.1, our: 0.01, unit: '¥/次' },
    responseExample: { code: 200, data: 199824, success: true, message: '请求成功' },
    guide: `${ASYNC_FLOW}${AUDIO_RESULT_TABLE}`,
    notes: ['speed_multiplier 仅支持 0.25 / 0.5 / 0.75 / 1 / 1.25 / 1.5 / 2']
  }
]

export const apiCategories: ApiCategory[] = [
  { key: 'system', title: '系统通用接口', apis: apiList.filter((a) => a.category === 'system') },
  { key: 'music', title: 'SUNO 音乐生成', apis: apiList.filter((a) => a.category === 'music') }
]

export const findApi = (id: string) => apiList.find((a) => a.id === id)

<script setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { createMvPortrait, fetchMvPortrait, uploadMvRef } from '@/api'

/**
 * MV 形象设定：画风、人物、服装、场景、色调，以及人物参考图（上传照片或 AI 生成定妆照）。
 * 有参考图时每个镜头都以它为参考，保证全片是同一个人、同一套衣服。
 * v-model 为 { style, character, outfit, scene, palette, refs: [{ key, url }] }。
 */
const props = defineProps({
  modelValue: { type: Object, required: true },
  merchantId: { type: [Number, String], default: '' },
  styleNote: { type: String, default: '' },
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'charged'])

const MAX_REFS = 3
const MAX_SIZE = 10 * 1024 * 1024
const STYLES = [
  { key: 'realistic', title: '真实摄影', desc: '像真人实拍的 MV' },
  { key: 'cinematic', title: '电影剧照', desc: '电影级调色与光影' },
  { key: 'anime', title: '日系动画', desc: '赛璐璐动画风' },
  { key: 'guofeng', title: '国风', desc: '东方古典意境' },
  { key: '3d', title: '3D 动画', desc: '动画电影质感' },
  { key: 'illustration', title: '手绘插画', desc: '水彩绘本风' }
]
const FIELDS = [
  { key: 'character', label: '人物', placeholder: '如：二十出头的中国女生，鹅蛋脸，齐肩黑色短发，清瘦' },
  { key: 'outfit', label: '服装', placeholder: '如：白色棉麻连衣裙，米色针织开衫，帆布鞋' },
  { key: 'scene', label: '场景', placeholder: '如：夏日海边小镇，老街、防波堤与便利店' },
  { key: 'palette', label: '色调', placeholder: '如：黄昏暖色调，柔和逆光' }
]

const look = computed(() => props.modelValue)
const refs = computed(() => look.value.refs || [])

function update(patch) {
  emit('update:modelValue', { ...look.value, ...patch })
}

const styleTitle = computed(() => STYLES.find((s) => s.key === look.value.style)?.title || '真实摄影')

/* ---------------------------------- 上传参考图 ---------------------------------- */

const uploading = ref(false)

async function upload({ file }) {
  if (!props.merchantId) return ElMessage.warning('请先选择归属商户')
  if (refs.value.length >= MAX_REFS) return ElMessage.warning(`参考图最多 ${MAX_REFS} 张`)
  if (file.size > MAX_SIZE) return ElMessage.warning('参考图不能超过 10MB')
  const form = new FormData()
  form.append('merchant_id', props.merchantId)
  form.append('file', file, file.name)
  uploading.value = true
  try {
    const data = await uploadMvRef(form)
    update({ refs: [...refs.value, data] })
  } finally {
    uploading.value = false
  }
}

function removeRef(i) {
  update({ refs: refs.value.filter((_, idx) => idx !== i) })
}

/* ---------------------------------- 定妆照 ---------------------------------- */

const portrait = ref({ running: false, elapsed: 0 })
let portraitTimer = null

async function generatePortrait() {
  if (!props.merchantId) return ElMessage.warning('请先选择归属商户')
  if (refs.value.length >= MAX_REFS) return ElMessage.warning(`参考图已满 ${MAX_REFS} 张，请先删掉一张`)
  const data = await createMvPortrait({
    merchant_id: props.merchantId,
    look: toLookPayload(look.value),
    style_note: props.styleNote
  })
  emit('charged', data.cost)
  portrait.value = { running: true, elapsed: 0 }
  pollPortrait(data.task_id)
}

function pollPortrait(taskId) {
  portraitTimer = setTimeout(async () => {
    portrait.value.elapsed += 3
    try {
      const res = await fetchMvPortrait(taskId)
      if (res.status === 'completed') {
        portrait.value.running = false
        update({ refs: [...refs.value, { key: res.key, url: res.url }].slice(0, MAX_REFS) })
        ElMessage.success('定妆照已生成并设为参考图，不满意可删掉重新生成')
        return
      }
      if (res.status === 'failed') {
        portrait.value.running = false
        ElMessage.error(`定妆照生成失败：${res.reason || '未知原因'}`)
        return
      }
    } catch {
      // 网络抖动时继续轮询
    }
    if (portrait.value.elapsed > 300) {
      portrait.value.running = false
      ElMessage.warning('定妆照生成超时，可在任务列表查看结果')
      return
    }
    pollPortrait(taskId)
  }, 3000)
}

onBeforeUnmount(() => clearTimeout(portraitTimer))
</script>

<script>
/** 提交给后端的形象设定：参考图只传 COS 路径。 */
export function toLookPayload(look) {
  return {
    style: look.style || 'realistic',
    character: look.character || '',
    outfit: look.outfit || '',
    scene: look.scene || '',
    palette: look.palette || '',
    ref_keys: (look.refs || []).map((r) => r.key)
  }
}

/** 把后端返回的形象设定转成编辑器使用的结构。 */
export function fromLook(look = {}) {
  const keys = look.ref_keys || []
  const urls = look.ref_urls || []
  return {
    style: look.style || 'realistic',
    character: look.character || '',
    outfit: look.outfit || '',
    scene: look.scene || '',
    palette: look.palette || '',
    refs: keys.map((key, i) => ({ key, url: urls[i] || '' }))
  }
}

export function emptyLook() {
  return fromLook()
}
</script>

<template>
  <div class="look-editor">
    <!-- 只读：生成后展示用了什么设定 -->
    <template v-if="disabled">
      <div class="readonly">
        <el-tag size="small" effect="plain">{{ styleTitle }}</el-tag>
        <template v-for="f in FIELDS" :key="f.key">
          <span v-if="look[f.key]" class="text-muted">{{ f.label }}：{{ look[f.key] }}</span>
        </template>
      </div>
      <div v-if="refs.length" class="refs">
        <el-image
          v-for="r in refs"
          :key="r.key"
          :src="r.url"
          :preview-src-list="refs.map((x) => x.url)"
          preview-teleported
          fit="cover"
          class="ref"
        />
      </div>
    </template>

    <template v-else>
      <div class="styles">
        <div
          v-for="s in STYLES"
          :key="s.key"
          :class="['style', { active: (look.style || 'realistic') === s.key }]"
          @click="update({ style: s.key })"
        >
          <div class="style-title">{{ s.title }}</div>
          <div class="style-desc">{{ s.desc }}</div>
        </div>
      </div>

      <div v-for="f in FIELDS" :key="f.key" class="field">
        <span class="field-label">{{ f.label }}</span>
        <el-input
          :model-value="look[f.key]"
          :placeholder="f.placeholder"
          maxlength="200"
          size="small"
          clearable
          @update:model-value="(v) => update({ [f.key]: v })"
        />
      </div>

      <div class="field ref-field">
        <span class="field-label">参考图</span>
        <div class="refs">
          <div v-for="(r, i) in refs" :key="r.key" class="ref-wrap">
            <el-image
              :src="r.url"
              :preview-src-list="refs.map((x) => x.url)"
              :initial-index="i"
              preview-teleported
              fit="cover"
              class="ref"
            />
            <el-icon class="ref-remove" @click="removeRef(i)"><Close /></el-icon>
          </div>
          <div v-if="portrait.running" class="ref ref-loading" v-loading="true" element-loading-text="生成中" />
          <el-upload
            v-if="refs.length < MAX_REFS"
            :show-file-list="false"
            :http-request="upload"
            accept=".jpg,.jpeg,.png,.webp"
          >
            <div class="ref ref-add" v-loading="uploading">
              <el-icon><Plus /></el-icon>
              <span>上传照片</span>
            </div>
          </el-upload>
        </div>
      </div>
      <div class="ref-actions">
        <el-button
          size="small"
          :icon="'MagicStick'"
          :loading="portrait.running"
          :disabled="refs.length >= MAX_REFS"
          @click="generatePortrait"
        >
          按上面的设定生成定妆照
        </el-button>
        <span class="text-muted">
          上传人物 / 服装照片，或先生成一张定妆照，每个镜头都会以它为参考，全片人物长相与衣着保持一致
        </span>
      </div>
    </template>
  </div>
</template>

<style scoped lang="scss">
.look-editor {
  width: 100%;
}

.styles {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 6px;
  margin-bottom: 8px;
}

.style {
  border: 1px solid #dcdfe6;
  border-radius: 6px;
  padding: 4px 8px;
  cursor: pointer;
  line-height: 1.4;
  transition: all 0.15s;

  &.active {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }

  .style-title {
    font-weight: 600;
    font-size: 13px;
  }

  .style-desc {
    font-size: 12px;
    color: #909399;
  }
}

.field {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;

  .field-label {
    flex: none;
    width: 42px;
    font-size: 12px;
    color: #606266;
  }
}

.ref-field {
  align-items: flex-start;
}

.refs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ref {
  width: 72px;
  height: 96px;
  border-radius: 4px;
  display: block;
}

.ref-wrap {
  position: relative;

  .ref-remove {
    position: absolute;
    top: 2px;
    right: 2px;
    padding: 2px;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    cursor: pointer;
  }
}

.ref-add {
  border: 1px dashed #dcdfe6;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  font-size: 12px;
  color: #909399;
  cursor: pointer;

  &:hover {
    border-color: var(--el-color-primary);
    color: var(--el-color-primary);
  }
}

.ref-loading {
  border: 1px dashed #dcdfe6;
}

.ref-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  line-height: 1.4;
  margin-left: 50px;
}

.readonly {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 12px;
  font-size: 12px;
  margin-bottom: 6px;
}
</style>

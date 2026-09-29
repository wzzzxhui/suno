<script setup>
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const formRef = ref()
const form = reactive({ username: '', password: '' })

const rules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
}

async function submit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  await auth.signIn({ username: form.username, password: form.password })
  ElMessage.success('登录成功')
  router.replace(route.query.redirect || '/dashboard')
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="head">
        <span class="dot" />
        <div>
          <h1>安沐心平台</h1>
          <p>商户、密钥与任务的统一管理入口</p>
        </div>
      </div>

      <el-form ref="formRef" :model="form" :rules="rules" size="large" @keyup.enter="submit">
        <el-form-item prop="username">
          <el-input v-model="form.username" placeholder="用户名" :prefix-icon="'User'" autocomplete="username" />
        </el-form-item>
        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
            :prefix-icon="'Lock'"
            show-password
            autocomplete="current-password"
          />
        </el-form-item>
        <el-button type="primary" class="submit" :loading="auth.loading" @click="submit">登 录</el-button>
      </el-form>

      <p class="tip">
        首次使用请在后端执行：<code>go run ./cmd/server -create-admin 用户名 -admin-password 密码</code>
      </p>
    </div>
  </div>
</template>

<style scoped lang="scss">
.login-page {
  height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f2430 0%, #2b3245 60%, #3a3f52 100%);
}

.login-card {
  width: 380px;
  background: #fff;
  border-radius: 12px;
  padding: 32px 28px 24px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
}

.head {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 24px;

  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--admin-brand);
    margin-top: 8px;
    flex-shrink: 0;
  }

  h1 {
    margin: 0;
    font-size: 20px;
    font-weight: 700;
  }

  p {
    margin: 6px 0 0;
    font-size: 13px;
    color: #909399;
  }
}

.submit {
  width: 100%;
}

.tip {
  margin: 20px 0 0;
  font-size: 12px;
  color: #c0c4cc;
  line-height: 1.7;

  code {
    background: #f5f7fa;
    padding: 1px 4px;
    border-radius: 3px;
    color: #909399;
    word-break: break-all;
  }
}
</style>

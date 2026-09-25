<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { changePassword } from '@/api'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const collapsed = ref(false)

const menus = computed(() => {
  const layout = router.getRoutes().find((r) => r.path === '/')
  return (layout?.children || [])
    .filter((child) => child.meta?.title)
    .map((child) => ({
      path: `/${child.path}`,
      title: child.meta.title,
      icon: child.meta.icon
    }))
})

const currentTitle = computed(() => route.meta?.title || '')

const pwdVisible = ref(false)
const pwdSaving = ref(false)
const pwdForm = ref({ old_password: '', new_password: '', confirm: '' })

async function submitPassword() {
  if (pwdForm.value.new_password.length < 8) {
    ElMessage.warning('新密码至少 8 位')
    return
  }
  if (pwdForm.value.new_password !== pwdForm.value.confirm) {
    ElMessage.warning('两次输入的新密码不一致')
    return
  }

  pwdSaving.value = true
  try {
    await changePassword({
      old_password: pwdForm.value.old_password,
      new_password: pwdForm.value.new_password
    })
    ElMessage.success('密码已更新，请重新登录')
    pwdVisible.value = false
    auth.signOut()
    router.push({ name: 'login' })
  } finally {
    pwdSaving.value = false
  }
}

async function handleLogout() {
  await ElMessageBox.confirm('确定退出登录吗？', '提示', { type: 'warning' })
  auth.signOut()
  router.push({ name: 'login' })
}

onMounted(() => {
  auth.loadProfile().catch(() => {})
})
</script>

<template>
  <el-container style="height: 100vh">
    <el-aside :width="collapsed ? '64px' : '210px'" style="background: var(--admin-sider); transition: width 0.2s">
      <div class="brand">
        <span class="dot" />
        <span v-show="!collapsed" class="name">安沐心平台</span>
      </div>

      <el-menu
        :default-active="route.path"
        :collapse="collapsed"
        :collapse-transition="false"
        background-color="#1f2430"
        text-color="#c8cdd8"
        active-text-color="#ffb400"
        router
      >
        <el-menu-item v-for="item in menus" :key="item.path" :index="item.path">
          <el-icon><component :is="item.icon" /></el-icon>
          <template #title>{{ item.title }}</template>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <el-container>
      <el-header class="topbar">
        <div class="left">
          <el-icon class="toggle" @click="collapsed = !collapsed">
            <component :is="collapsed ? 'Expand' : 'Fold'" />
          </el-icon>
          <span class="title">{{ currentTitle }}</span>
        </div>

        <el-dropdown trigger="click">
          <span class="user">
            <el-avatar :size="28" style="background: var(--admin-brand)">
              {{ (auth.user?.nickname || auth.user?.username || 'A').slice(0, 1).toUpperCase() }}
            </el-avatar>
            <span class="nickname">{{ auth.user?.nickname || auth.user?.username || '管理员' }}</span>
            <el-icon><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item @click="pwdVisible = true">修改密码</el-dropdown-item>
              <el-dropdown-item divided @click="handleLogout">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </el-header>

      <el-main style="padding: 0; background: var(--admin-bg)">
        <router-view v-slot="{ Component }">
          <keep-alive :max="5">
            <component :is="Component" :key="route.path" />
          </keep-alive>
        </router-view>
      </el-main>
    </el-container>

    <el-dialog v-model="pwdVisible" title="修改密码" width="420px">
      <el-form :model="pwdForm" label-width="88px">
        <el-form-item label="原密码">
          <el-input v-model="pwdForm.old_password" type="password" show-password />
        </el-form-item>
        <el-form-item label="新密码">
          <el-input v-model="pwdForm.new_password" type="password" show-password placeholder="至少 8 位" />
        </el-form-item>
        <el-form-item label="确认密码">
          <el-input v-model="pwdForm.confirm" type="password" show-password />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="pwdVisible = false">取消</el-button>
        <el-button type="primary" :loading="pwdSaving" @click="submitPassword">保存</el-button>
      </template>
    </el-dialog>
  </el-container>
</template>

<style scoped lang="scss">
.brand {
  height: 56px;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 18px;
  color: #fff;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  overflow: hidden;
  white-space: nowrap;

  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--admin-brand);
    flex-shrink: 0;
  }

  .name {
    font-size: 15px;
    font-weight: 600;
  }
}

.el-menu {
  border-right: none;
}

.topbar {
  height: 56px;
  background: #fff;
  border-bottom: 1px solid #e4e7ed;
  display: flex;
  align-items: center;
  justify-content: space-between;

  .left {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .toggle {
    font-size: 18px;
    cursor: pointer;
    color: #606266;
  }

  .title {
    font-size: 15px;
    font-weight: 600;
  }

  .user {
    display: flex;
    align-items: center;
    gap: 8px;
    cursor: pointer;
    outline: none;

    .nickname {
      font-size: 14px;
    }
  }
}
</style>

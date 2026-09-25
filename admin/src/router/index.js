import { createRouter, createWebHashHistory } from 'vue-router'
import { getToken } from '@/api'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/Login.vue'),
    meta: { public: true, title: '登录' }
  },
  {
    path: '/',
    component: () => import('@/layouts/AdminLayout.vue'),
    redirect: '/dashboard',
    children: [
      {
        path: 'dashboard',
        name: 'dashboard',
        component: () => import('@/views/Dashboard.vue'),
        meta: { title: '数据概览', icon: 'DataLine' }
      },
      {
        path: 'studio',
        name: 'studio',
        component: () => import('@/views/MusicStudio.vue'),
        meta: { title: '音乐创作', icon: 'Microphone' }
      },
      {
        path: 'voice',
        name: 'voice',
        component: () => import('@/views/VoiceStudio.vue'),
        meta: { title: '音色翻唱', icon: 'Mic' }
      },
      {
        path: 'mv',
        name: 'mv',
        component: () => import('@/views/MvStudio.vue'),
        meta: { title: 'AI MV', icon: 'VideoCamera' }
      },
      {
        path: 'ai-studio',
        name: 'ai-studio',
        component: () => import('@/views/AiStudio.vue'),
        meta: { title: '图像视频', icon: 'Picture' }
      },
      {
        path: 'songs',
        name: 'songs',
        component: () => import('@/views/Songs.vue'),
        meta: { title: '作品库', icon: 'Film' }
      },
      {
        path: 'merchants',
        name: 'merchants',
        component: () => import('@/views/Merchants.vue'),
        meta: { title: '商户管理', icon: 'OfficeBuilding' }
      },
      {
        path: 'keys',
        name: 'keys',
        component: () => import('@/views/ApiKeys.vue'),
        meta: { title: '密钥管理', icon: 'Key' }
      },
      {
        path: 'tasks',
        name: 'tasks',
        component: () => import('@/views/Tasks.vue'),
        meta: { title: '任务管理', icon: 'Headset' }
      },
      {
        path: 'points',
        name: 'points',
        component: () => import('@/views/PointLogs.vue'),
        meta: { title: '积分流水', icon: 'Wallet' }
      },
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/Settings.vue'),
        meta: { title: '系统设置', icon: 'Setting' }
      }
    ]
  },
  { path: '/:pathMatch(.*)*', redirect: '/dashboard' }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

router.beforeEach((to) => {
  const logged = !!getToken()

  if (!to.meta.public && !logged) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && logged) {
    return { name: 'dashboard' }
  }
  document.title = to.meta.title ? `${to.meta.title} · 安沐心平台` : '安沐心平台'
  return true
})

export default router

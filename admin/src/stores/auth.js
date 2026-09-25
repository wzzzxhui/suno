import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as api from '@/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(null)
  const loading = ref(false)

  const isLogged = () => !!api.getToken()

  async function signIn(payload) {
    loading.value = true
    try {
      const data = await api.login(payload)
      api.setToken(data.token)
      user.value = data.user
      return data
    } finally {
      loading.value = false
    }
  }

  async function loadProfile() {
    if (!isLogged()) return null
    user.value = await api.fetchProfile()
    return user.value
  }

  function signOut() {
    api.clearToken()
    user.value = null
  }

  return { user, loading, isLogged, signIn, loadProfile, signOut }
})

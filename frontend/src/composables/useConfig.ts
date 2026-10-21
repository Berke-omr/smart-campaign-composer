// Which LLM answers (GET /api/config), for the "Demo mode" badge. Read once and
// shared by every component; the mode is fixed while the api runs.
import { readonly, ref } from 'vue'
import { fetchConfig } from '@/api/campaigns'
import type { AppConfig } from '@/api/types'

const config = ref<AppConfig | null>(null)
let loading: Promise<void> | null = null

export function useConfig() {
  if (!loading) {
    loading = fetchConfig()
      .then((c) => {
        config.value = c
      })
      .catch(() => {
        // Without the answer the badge stays hidden; the next caller tries again.
        loading = null
      })
  }
  return readonly(config)
}

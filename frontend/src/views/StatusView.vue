<script setup lang="ts">
// P1 placeholder page: proves the browser → nginx → api → Postgres path works.
// Replaced by the campaign composer in P6.
import { computed, onMounted, ref } from 'vue'
import { fetchUserStats, type UserStats } from '@/api/stats'

const stats = ref<UserStats | null>(null)
const error = ref<string | null>(null)
const loading = ref(false)

async function load() {
  loading.value = true
  error.value = null
  try {
    stats.value = await fetchUserStats()
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)

const number = new Intl.NumberFormat('en-US')
const percent = new Intl.NumberFormat('en-US', { style: 'percent' })

const tiles = computed(() => {
  const s = stats.value
  if (!s) return []
  const share = (n: number) => (s.total > 0 ? percent.format(n / s.total) : '—')
  return [
    { label: 'Users', value: number.format(s.total), hint: 'seeded fake profiles' },
    {
      label: 'SMS reachable',
      value: number.format(s.sms_opt_in),
      hint: `${share(s.sms_opt_in)} opted in`,
    },
    {
      label: 'Email reachable',
      value: number.format(s.email_opt_in),
      hint: `${share(s.email_opt_in)} opted in`,
    },
  ]
})
</script>

<template>
  <section class="status">
    <h2>System status</h2>

    <p v-if="loading && !stats" class="muted" role="status">Loading…</p>

    <div v-else-if="error" class="error" role="alert">
      <p>Could not reach the API: {{ error }}</p>
      <button type="button" @click="load">Retry</button>
    </div>

    <ul v-else class="tiles">
      <li v-for="tile in tiles" :key="tile.label" class="tile">
        <span class="tile-label">{{ tile.label }}</span>
        <strong class="tile-value" data-testid="tile-value">{{ tile.value }}</strong>
        <span class="tile-hint">{{ tile.hint }}</span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.status h2 {
  margin: 0 0 1rem;
  font-size: 1.1rem;
}

.tiles {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: 1rem;
  padding: 0;
  margin: 0;
  list-style: none;
}

.tile {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1rem 1.25rem;
  border: 1px solid var(--border);
  border-radius: 0.75rem;
  background: var(--surface);
}

.tile-label {
  color: var(--muted);
  font-size: 0.85rem;
}

.tile-value {
  font-size: 1.75rem;
  font-variant-numeric: tabular-nums;
}

.tile-hint {
  color: var(--muted);
  font-size: 0.8rem;
}

.error {
  padding: 1rem 1.25rem;
  border: 1px solid var(--danger);
  border-radius: 0.75rem;
  color: var(--danger);
}

.error button {
  margin-top: 0.5rem;
}

.muted {
  color: var(--muted);
}
</style>

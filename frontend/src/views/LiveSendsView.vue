<script setup lang="ts">
// "Live sends": opens the dashboard of the campaign that is sending now, or
// of the latest approved one; says so when nothing has been sent yet.
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { fetchCampaigns } from '@/api/campaigns'
import AppBanner from '@/components/ui/AppBanner.vue'
import AppButton from '@/components/ui/AppButton.vue'
import GlassCard from '@/components/ui/GlassCard.vue'

const router = useRouter()
const state = ref<'loading' | 'none' | 'failed'>('loading')

async function load() {
  state.value = 'loading'
  try {
    const list = await fetchCampaigns() // newest first
    const target =
      list.find((c) => c.status === 'sending') ??
      list.find((c) => c.status === 'completed' || c.status === 'failed')
    if (target) {
      await router.replace({ name: 'campaign-live', params: { id: target.id } })
    } else {
      state.value = 'none'
    }
  } catch {
    state.value = 'failed'
  }
}

onMounted(load)
</script>

<template>
  <section class="live-sends">
    <h1>Live sends</h1>
    <AppBanner v-if="state === 'failed'" tone="error" title="Can't reach the server">
      The campaigns could not be loaded.
      <template #action>
        <AppButton variant="secondary" @click="load">Try again</AppButton>
      </template>
    </AppBanner>
    <GlassCard v-else-if="state === 'none'">
      <p>Nothing has been sent yet. Approve a campaign to follow it here.</p>
      <div>
        <AppButton variant="primary" icon="plus" :to="{ name: 'composer' }">New campaign</AppButton>
      </div>
    </GlassCard>
    <p v-else class="secondary-text" role="status">Opening the latest send…</p>
  </section>
</template>

<style scoped>
.live-sends {
  display: flex;
  flex-direction: column;
  gap: var(--layout-gap);
}
</style>

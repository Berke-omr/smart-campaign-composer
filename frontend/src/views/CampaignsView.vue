<script setup lang="ts">
// All campaigns (GET /api/campaigns, the 50 most recent).
import { onMounted, ref } from 'vue'
import { fetchCampaigns } from '@/api/campaigns'
import type { CampaignSummary } from '@/api/types'
import CampaignGrid from '@/components/campaign/CampaignGrid.vue'
import AppBanner from '@/components/ui/AppBanner.vue'
import AppButton from '@/components/ui/AppButton.vue'
import GlassCard from '@/components/ui/GlassCard.vue'
import SkeletonLines from '@/components/ui/SkeletonLines.vue'

const campaigns = ref<CampaignSummary[] | null>(null)
const failed = ref(false)

async function load() {
  failed.value = false
  try {
    campaigns.value = await fetchCampaigns()
  } catch {
    failed.value = true
  }
}

onMounted(load)
</script>

<template>
  <section class="campaigns">
    <h1>Campaigns</h1>
    <AppBanner v-if="failed" tone="error" title="Can't reach the server">
      The campaigns could not be loaded.
      <template #action>
        <AppButton variant="secondary" @click="load">Try again</AppButton>
      </template>
    </AppBanner>
    <GlassCard v-else-if="!campaigns">
      <p class="secondary-text" role="status">Loading campaigns…</p>
      <SkeletonLines :lines="3" />
    </GlassCard>
    <GlassCard v-else-if="campaigns.length === 0">
      <p>No campaigns yet.</p>
      <div>
        <AppButton variant="primary" icon="plus" :to="{ name: 'composer' }">New campaign</AppButton>
      </div>
    </GlassCard>
    <CampaignGrid v-else :campaigns="campaigns" />
  </section>
</template>

<style scoped>
.campaigns {
  display: flex;
  flex-direction: column;
  gap: var(--layout-gap);
}
</style>

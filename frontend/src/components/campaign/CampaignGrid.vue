<script setup lang="ts">
// A grid of campaign cards (home page: the four most recent; /campaigns: all).
import type { CampaignSummary } from '@/api/types'
import CampaignCard from './CampaignCard.vue'

defineProps<{ campaigns: CampaignSummary[] }>()
</script>

<template>
  <ul class="grid">
    <li v-for="c in campaigns" :key="c.id"><CampaignCard :campaign="c" /></li>
  </ul>
</template>

<style scoped>
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 15rem), 1fr));
  gap: var(--layout-gap);
  margin: 0;
  padding: 0;
  list-style: none;
}

.grid > li {
  display: flex;
  min-width: 0;
}

.grid > li > * {
  flex: 1;
}
</style>

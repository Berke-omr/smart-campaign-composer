<script setup lang="ts">
// A campaign in a list: name, channels, people in the audience (not all of
// them consented to every channel), status, and progress where it is known.
// The list endpoint has no delivery counts, so a sending campaign shows an
// indeterminate bar rather than an invented percentage; a finished one is
// 100% done (sent and failed are both final).
import { computed } from 'vue'
import type { CampaignSummary } from '@/api/types'
import StatusPill from '@/components/ui/StatusPill.vue'
import { CHANNEL_LABELS } from '@/logic/labels'

const props = defineProps<{ campaign: CampaignSummary }>()

const to = computed(() => ({
  name:
    props.campaign.status === 'draft' || props.campaign.status === 'ready'
      ? 'campaign'
      : 'campaign-live',
  params: { id: props.campaign.id },
}))
const finished = computed(
  () => props.campaign.status === 'completed' || props.campaign.status === 'failed',
)
</script>

<template>
  <RouterLink :to="to" class="card glass" data-testid="campaign-card">
    <h3 class="name">{{ campaign.name }}</h3>
    <p class="channels caption">
      <span v-for="ch in campaign.channels" :key="ch" class="channel">
        <span class="dot" :style="{ background: `var(--color-${ch})` }" aria-hidden="true"></span>
        {{ CHANNEL_LABELS[ch] }}
      </span>
    </p>
    <p class="recipients numeric">
      <strong>{{ campaign.audience.total.toLocaleString('en-US') }}</strong>
      {{ campaign.audience.total === 1 ? 'person' : 'people' }}
    </p>
    <StatusPill kind="campaign" :status="campaign.status" />
    <div v-if="finished" class="progress">
      <span class="track" aria-hidden="true"><span class="fill"></span></span>
      <span class="caption secondary-text">100% done</span>
    </div>
    <div v-else-if="campaign.status === 'sending'" class="progress">
      <span class="track" aria-hidden="true"><span class="fill indeterminate"></span></span>
      <span class="caption secondary-text">In progress</span>
    </div>
    <p v-else class="caption secondary-text">Not sent yet</p>
  </RouterLink>
</template>

<style scoped>
.card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-6);
  text-decoration: none;
  min-width: 0;
  transition:
    background-color var(--duration-fast) var(--ease-ui),
    box-shadow var(--duration-fast) var(--ease-ui);
}

.card:hover {
  background: var(--color-glass-hover);
  box-shadow: var(--shadow-raised), var(--shadow-highlight);
}

.name {
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
  overflow-wrap: anywhere;
}

.channels {
  display: flex;
  gap: var(--space-4);
  color: var(--color-text-secondary);
}

.channel {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.dot {
  width: 0.625rem;
  height: 0.625rem;
  border-radius: var(--radius-pill);
}

.progress {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  width: 100%;
}

.track {
  position: relative;
  flex: 1;
  height: 0.5rem;
  border-radius: var(--radius-pill);
  background: var(--color-track);
  overflow: hidden;
}

.fill {
  position: absolute;
  inset: 0;
  border-radius: var(--radius-pill);
  background: var(--color-campaign-completed);
}

.fill.indeterminate {
  right: auto;
  width: 35%;
  background: var(--color-campaign-sending);
  animation: slide var(--duration-shimmer) ease-in-out infinite;
}

@keyframes slide {
  from {
    left: -35%;
  }
  to {
    left: 100%;
  }
}
</style>

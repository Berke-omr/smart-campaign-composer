<script setup lang="ts">
// Campaign status or message state as a pill: dot + text, never color alone.
// Campaign statuses and message states keep separate names (DESIGN.md section 5).
import { computed } from 'vue'
import type { CampaignStatus, MessageStatus } from '@/api/types'
import { CAMPAIGN_STATUS_LABELS, MESSAGE_STATUS_LABELS } from '@/logic/labels'

const props = defineProps<
  { kind: 'campaign'; status: CampaignStatus } | { kind: 'message'; status: MessageStatus }
>()

const label = computed(() =>
  props.kind === 'campaign'
    ? CAMPAIGN_STATUS_LABELS[props.status]
    : MESSAGE_STATUS_LABELS[props.status],
)
const family = computed(() => `${props.kind}-${props.status}`)
</script>

<template>
  <span
    class="pill"
    :style="{
      '--pill-color': `var(--color-${family})`,
      '--pill-tint': `var(--color-${family}-tint)`,
      '--pill-border': `var(--color-${family}-border)`,
      '--pill-ink': `var(--color-${family}-ink)`,
    }"
    data-testid="status-pill"
  >
    <span class="dot" aria-hidden="true"></span>{{ label }}
  </span>
</template>

<style scoped>
.pill {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-3);
  border-radius: var(--radius-pill);
  border: var(--border-thin) solid var(--pill-border);
  background: var(--pill-tint);
  color: var(--pill-ink);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
  font-weight: var(--font-weight-label);
  white-space: nowrap;
}

.dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: var(--radius-pill);
  background: var(--pill-color);
}
</style>

<script setup lang="ts">
// A personalization variable shown as a pill: "first_name", no braces,
// tinted with the channel's color (DESIGN.md section 5).
import type { Channel, Variable } from '@/api/types'

defineProps<{ variable: Variable; channel: Channel }>()
</script>

<template>
  <span
    class="token"
    :style="{
      '--token-tint': `var(--color-${channel}-tint)`,
      '--token-border': `var(--color-${channel}-border)`,
      '--token-ink': `var(--color-${channel}-ink)`,
    }"
    ><span class="sr-only">variable </span>{{ variable }}</span
  >
</template>

<style scoped>
.token {
  display: inline-block;
  padding: 0 var(--space-2);
  margin: 0 1px;
  border-radius: var(--radius-pill);
  border: var(--border-thin) solid var(--token-border);
  background: var(--token-tint);
  color: var(--token-ink);
  font-weight: var(--font-weight-label);
  line-height: 1.4;
  white-space: nowrap;
}
</style>

<script setup lang="ts">
// Sent → Sending → Queued → Retrying → Failed as proportional parts of one
// bar, with a legend that always lists all five counts (zeros included).
// The bar is decoration; the legend carries the numbers.
import type { Segment } from '@/logic/snapshot'
import { MESSAGE_STATUS_LABELS } from '@/logic/labels'

defineProps<{ segments: Segment[] }>()
</script>

<template>
  <div class="segmented">
    <div class="bar" aria-hidden="true">
      <span
        v-for="s in segments"
        v-show="s.count > 0"
        :key="s.status"
        class="part"
        :style="{ width: `${s.percent}%`, background: `var(--color-message-${s.status})` }"
      ></span>
    </div>
    <ul class="legend">
      <li v-for="s in segments" :key="s.status" class="legend-item" :data-status="s.status">
        <span class="label caption">
          <span
            class="dot"
            :style="{ background: `var(--color-message-${s.status})` }"
            aria-hidden="true"
          ></span>
          {{ MESSAGE_STATUS_LABELS[s.status] }}
        </span>
        {{ ' ' }}<strong class="count numeric">{{ s.count.toLocaleString('en-US') }}</strong>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.segmented {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.bar {
  display: flex;
  height: 1.25rem;
  border-radius: var(--radius-pill);
  background: var(--color-track);
  overflow: hidden;
}

.part {
  height: 100%; /* exact shares, no minimum width: the legend has every count */
  transition: width var(--duration-fast) var(--ease-ui);
}

.legend {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: var(--space-3) var(--space-4);
  margin: 0;
  padding: 0;
  list-style: none;
}

.legend-item {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.label {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--color-text-secondary);
}

.dot {
  width: 0.625rem;
  height: 0.625rem;
  border-radius: var(--radius-pill);
}

.count {
  font-size: var(--font-size-body);
  padding-left: calc(0.625rem + var(--space-2));
}
</style>

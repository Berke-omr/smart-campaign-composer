<script setup lang="ts">
// A single progress bar. The fill uses the exact share; only the shown
// percentage is rounded (by the caller).
import { computed } from 'vue'

const props = defineProps<{ value: number; max: number; label: string }>()

const share = computed(() => (props.max > 0 ? Math.min(1, props.value / props.max) : 0))
</script>

<template>
  <div
    class="track"
    role="progressbar"
    :aria-valuenow="value"
    aria-valuemin="0"
    :aria-valuemax="max"
    :aria-valuetext="label"
    :aria-label="label"
  >
    <div class="fill" :style="{ width: `${share * 100}%` }"></div>
  </div>
</template>

<style scoped>
.track {
  height: 0.875rem;
  border-radius: var(--radius-pill);
  background: var(--color-track);
  overflow: hidden;
}

.fill {
  height: 100%;
  border-radius: var(--radius-pill);
  background: var(--color-campaign-sending);
  transition: width var(--duration-fast) var(--ease-ui);
}
</style>

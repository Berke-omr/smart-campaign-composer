<script setup lang="ts">
// "121 / 160" in JetBrains Mono. The number is the worst-case length the
// backend checks (D15). Near the limit and over it the counter says so in
// words as well as color.
import { computed } from 'vue'
import { counterLevel } from '@/logic/content'

const props = defineProps<{ length: number; limit: number; id?: string }>()

const level = computed(() => counterLevel(props.length, props.limit))
</script>

<template>
  <span :id="id" class="counter" :class="level" data-testid="counter">
    <span v-if="level === 'near'" class="note">Near limit</span>
    <span v-else-if="level === 'over'" class="note">{{ length - limit }} over limit</span>
    <span class="numbers">{{ length }} / {{ limit }}</span>
  </span>
</template>

<style scoped>
.counter {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  color: var(--color-text-secondary);
}

.numbers {
  font-family: var(--font-counter);
  font-size: var(--font-size-counter);
  line-height: var(--line-height-counter);
  font-variant-numeric: tabular-nums;
}

.note {
  font-size: var(--font-size-caption);
  line-height: var(--line-height-caption);
  font-weight: 600;
}

.near {
  color: var(--color-message-retrying-ink);
}

.over {
  color: var(--color-campaign-failed-ink);
}
</style>

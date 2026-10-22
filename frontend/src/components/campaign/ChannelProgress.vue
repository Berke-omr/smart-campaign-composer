<script setup lang="ts">
// One channel on the dashboard: its sending rate against the 100/s limit,
// and its messages by state. Rate and completion are separate things.
import { computed } from 'vue'
import type { Channel, ChannelSnapshot } from '@/api/types'
import SegmentedProgress from '@/components/ui/SegmentedProgress.vue'
import { CHANNEL_LABELS } from '@/logic/labels'
import { gaugeFraction, segments } from '@/logic/snapshot'

const props = defineProps<{ channel: Channel; data: ChannelSnapshot }>()

const gauge = computed(() => gaugeFraction(props.data.rate, props.data.limit))
</script>

<template>
  <section class="channel glass" :aria-label="`${CHANNEL_LABELS[channel]} delivery`">
    <header class="head">
      <h2 class="title">
        <span
          class="dot"
          :style="{ background: `var(--color-${channel})` }"
          aria-hidden="true"
        ></span>
        {{ CHANNEL_LABELS[channel] }}
      </h2>
      <div class="rate">
        <p class="rate-value numeric" data-testid="rate">{{ data.rate }} msg/s</p>
        <div class="gauge" aria-hidden="true">
          <span
            class="gauge-fill"
            :style="{ width: `${gauge * 100}%`, background: `var(--color-${channel})` }"
          ></span>
        </div>
        <p class="caption secondary-text">limit {{ data.limit }}/s</p>
      </div>
    </header>
    <SegmentedProgress :segments="segments(data)" />
  </section>
</template>

<style scoped>
.channel {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
  padding: var(--space-6);
  min-width: 0;
}

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.title {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
}

.dot {
  width: 0.875rem;
  height: 0.875rem;
  border-radius: var(--radius-pill);
}

.rate {
  display: grid;
  grid-template-columns: auto 7rem;
  align-items: center;
  column-gap: var(--space-3);
}

.rate-value {
  font-size: var(--font-size-h3);
  font-weight: 700;
}

.gauge {
  height: 0.625rem;
  border-radius: var(--radius-pill);
  background: var(--color-track);
  overflow: hidden;
}

.gauge-fill {
  display: block;
  height: 100%;
  border-radius: var(--radius-pill);
  transition: width var(--duration-fast) var(--ease-ui);
}

.rate .caption {
  grid-column: 2;
  text-align: right;
}
</style>

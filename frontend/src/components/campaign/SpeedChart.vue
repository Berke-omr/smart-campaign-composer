<script setup lang="ts">
// Sends per second over the snapshot's 30-second window, one line per
// channel, under the dashed 100/s limit (D4, D25). Our own SVG: the series
// is replaced by each snapshot and drawn as is, so nothing is animated or
// invented, and the chart simply stays still while the dashboard reconnects.
import { computed } from 'vue'
import type { Channel } from '@/api/types'
import { CHANNEL_LABELS } from '@/logic/labels'
import { monotonePath, type Point } from '@/logic/snapshot'

const props = defineProps<{
  series: { channel: Channel; values: number[] }[]
  limit: number
  window: { from: string; to: string }
  finished: boolean
}>()

// Drawing area inside the viewBox.
const W = 640
const H = 260
const LEFT = 44
const RIGHT = 620
const TOP = 24
const BOTTOM = 220

const yMax = computed(() => Math.max(props.limit, ...props.series.flatMap((s) => s.values), 1))
const y = (v: number) => BOTTOM - (v / yMax.value) * (BOTTOM - TOP)
const ticks = computed(() => {
  const step = yMax.value <= 100 ? 20 : Math.ceil(yMax.value / 5 / 10) * 10
  const out: number[] = []
  for (let v = 0; v <= yMax.value; v += step) out.push(v)
  return out
})

function points(values: number[]): Point[] {
  const n = values.length
  const dx = n > 1 ? (RIGHT - LEFT) / (n - 1) : 0
  return values.map((v, i) => ({ x: LEFT + i * dx, y: y(v) }))
}

const lines = computed(() =>
  props.series.map((s) => {
    const pts = points(s.values)
    const line = monotonePath(pts)
    const last = pts[pts.length - 1]
    return {
      channel: s.channel,
      line,
      area: pts.length > 1 ? `${line}L${RIGHT},${BOTTOM}L${LEFT},${BOTTOM}Z` : '',
      last,
    }
  }),
)

const clock = (iso: string) =>
  new Date(iso).toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })

const xLabels = computed(() =>
  props.finished
    ? [
        { x: LEFT, text: clock(props.window.from), anchor: 'start' },
        { x: RIGHT, text: clock(props.window.to), anchor: 'end' },
      ]
    : [
        { x: LEFT, text: '-30s', anchor: 'start' },
        { x: LEFT + (RIGHT - LEFT) / 3, text: '-20s', anchor: 'middle' },
        { x: LEFT + (2 * (RIGHT - LEFT)) / 3, text: '-10s', anchor: 'middle' },
        { x: RIGHT, text: 'Now', anchor: 'end' },
      ],
)

/** The chart in words, for screen readers (the per-channel numbers are also in the cards). */
const summary = computed(() => {
  const parts = props.series.map((s) => {
    const now = s.values[s.values.length - 1] ?? 0
    const peak = Math.max(0, ...s.values)
    return `${CHANNEL_LABELS[s.channel]} ${now} per second at the end, highest ${peak}`
  })
  return `Sending speed over 30 seconds. ${parts.join('; ')}. Limit ${props.limit} per second.`
})
</script>

<template>
  <figure class="chart">
    <div class="legend" aria-hidden="true">
      <span v-for="s in series" :key="s.channel" class="legend-item">
        <span class="dot" :style="{ background: `var(--color-${s.channel})` }"></span>
        {{ CHANNEL_LABELS[s.channel] }}
      </span>
    </div>
    <svg :viewBox="`0 0 ${W} ${H}`" role="img" :aria-label="summary" class="svg">
      <text :x="LEFT - 8" :y="TOP - 10" class="axis" text-anchor="end">msg/s</text>
      <g v-for="t in ticks" :key="t">
        <line :x1="LEFT" :x2="RIGHT" :y1="y(t)" :y2="y(t)" class="grid" />
        <text :x="LEFT - 8" :y="y(t) + 4" class="axis" text-anchor="end">{{ t }}</text>
      </g>
      <line :x1="LEFT" :x2="RIGHT" :y1="y(limit)" :y2="y(limit)" class="limit" />
      <text :x="RIGHT" :y="y(limit) - 6" class="axis limit-label" text-anchor="end">
        Limit {{ limit }}/s
      </text>
      <g v-for="l in lines" :key="l.channel">
        <path
          v-if="l.area"
          :d="l.area"
          class="area"
          :style="{ fill: `var(--color-${l.channel}-tint)` }"
        />
        <path :d="l.line" class="line" :style="{ stroke: `var(--color-${l.channel})` }" />
        <circle
          v-if="l.last"
          :cx="l.last.x"
          :cy="l.last.y"
          r="4"
          :style="{ fill: `var(--color-${l.channel})` }"
        />
      </g>
      <text
        v-for="xl in xLabels"
        :key="xl.text"
        :x="xl.x"
        :y="BOTTOM + 24"
        class="axis"
        :text-anchor="xl.anchor"
      >
        {{ xl.text }}
      </text>
    </svg>
  </figure>
</template>

<style scoped>
.chart {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0;
}

.legend {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-4);
  font-size: var(--font-size-label);
}

.legend-item {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.dot {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: var(--radius-pill);
}

.svg {
  width: 100%;
  height: auto;
  overflow: visible;
}

.axis {
  fill: var(--color-text-secondary);
  font-family: var(--font-ui);
  font-size: 12px;
}

.grid {
  stroke: var(--color-track);
  stroke-width: 1;
}

.limit {
  stroke: var(--color-text-secondary);
  stroke-width: 1.5;
  stroke-dasharray: 6 5;
}

.area {
  stroke: none;
}

.line {
  fill: none;
  stroke-width: 2.5;
  stroke-linejoin: round;
  stroke-linecap: round;
}
</style>

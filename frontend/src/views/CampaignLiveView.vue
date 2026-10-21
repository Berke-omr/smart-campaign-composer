<script setup lang="ts">
// Live dashboard of an approved campaign (08 — Live): overall progress, each
// channel's states and rate, the speed chart and recent issues, from the
// snapshot stream. While the connection is lost the last snapshot stays on
// screen, labelled with its time; the first snapshot after the reconnect
// replaces it. A finished campaign shows its final state and keeps its chart.
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { fetchCampaign } from '@/api/campaigns'
import { ApiError } from '@/api/client'
import type { CampaignView, Channel } from '@/api/types'
import ChannelProgress from '@/components/campaign/ChannelProgress.vue'
import RecentIssues from '@/components/campaign/RecentIssues.vue'
import SpeedChart from '@/components/campaign/SpeedChart.vue'
import AppBanner from '@/components/ui/AppBanner.vue'
import AppButton from '@/components/ui/AppButton.vue'
import GlassCard from '@/components/ui/GlassCard.vue'
import ProgressBar from '@/components/ui/ProgressBar.vue'
import SkeletonLines from '@/components/ui/SkeletonLines.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import { useDeliveryStream } from '@/composables/useDeliveryStream'
import { CAMPAIGN_STATUS_LABELS } from '@/logic/labels'
import { progressPercent } from '@/logic/snapshot'

const props = defineProps<{ id: number }>()
const router = useRouter()

const campaign = ref<CampaignView | null>(null)
const loadError = ref<'not_found' | 'unreachable' | null>(null)

const live = useDeliveryStream(props.id, { check: checkAfterDisconnect })

async function load() {
  loadError.value = null
  try {
    const c = await fetchCampaign(props.id)
    if (c.status === 'draft' || c.status === 'ready') {
      await router.replace({ name: 'campaign', params: { id: c.id } })
      return
    }
    campaign.value = c
    live.start()
  } catch (e) {
    loadError.value = e instanceof ApiError && e.status === 404 ? 'not_found' : 'unreachable'
  }
}

/** The browser gave up on the stream (a 502 while the api restarts, or a 404/409). */
async function checkAfterDisconnect(): Promise<'retry' | 'stop'> {
  try {
    const c = await fetchCampaign(props.id)
    if (c.status === 'draft' || c.status === 'ready') {
      await router.replace({ name: 'campaign', params: { id: c.id } })
      return 'stop'
    }
    return 'retry'
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      loadError.value = 'not_found'
      return 'stop'
    }
    return 'retry' // still unreachable: keep the last snapshot and try again later
  }
}

onMounted(load)

const s = computed(() => live.snapshot.value)
const status = computed(() => s.value?.status ?? campaign.value?.status ?? 'sending')
const finished = computed(() => status.value === 'completed' || status.value === 'failed')
const channels = computed<Channel[]>(() =>
  (campaign.value?.channels ?? []).filter((ch) => s.value?.channels[ch] !== undefined),
)

const n = (v: number) => v.toLocaleString('en-US')
const clock = (iso: string | null | undefined) =>
  iso
    ? new Date(iso).toLocaleTimeString('en-GB', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      })
    : ''

const percent = computed(() => (s.value ? progressPercent(s.value.done, s.value.total) : 0))
const doneLabel = computed(() => (s.value ? `${n(s.value.done)} of ${n(s.value.total)} done` : ''))
const reconnecting = computed(
  () =>
    live.connection.value === 'reconnecting' ||
    (live.connection.value === 'connecting' && s.value !== null),
)

/** Short, meaningful announcements only: status and connection changes, never every second. */
const announcement = ref('')
watch(status, (now, before) => {
  if (now !== before && s.value) {
    const sent = channels.value.reduce((sum, ch) => sum + (s.value!.channels[ch]?.sent ?? 0), 0)
    const failed = channels.value.reduce((sum, ch) => sum + (s.value!.channels[ch]?.failed ?? 0), 0)
    announcement.value = finished.value
      ? `Campaign ${CAMPAIGN_STATUS_LABELS[now].toLowerCase()}: ${n(sent)} sent, ${n(failed)} failed.`
      : `Campaign is ${CAMPAIGN_STATUS_LABELS[now].toLowerCase()}.`
  }
})
watch(reconnecting, (now, before) => {
  if (now) announcement.value = 'Connection lost. Reconnecting…'
  else if (before) announcement.value = 'Connection is back. The numbers are live again.'
})
</script>

<template>
  <section class="live">
    <template v-if="loadError">
      <AppBanner v-if="loadError === 'not_found'" tone="error" title="This campaign does not exist">
        It may have been removed when the demo data was reset.
        <template #action>
          <AppButton variant="secondary" :to="{ name: 'composer' }">New campaign</AppButton>
        </template>
      </AppBanner>
      <AppBanner v-else tone="error" title="Can't reach the server">
        The campaign could not be loaded. Check that the app is running, then try again.
        <template #action>
          <AppButton variant="secondary" @click="load">Try again</AppButton>
        </template>
      </AppBanner>
    </template>

    <template v-else-if="campaign">
      <header class="head">
        <div class="title-block">
          <div class="title-row">
            <h1>{{ campaign.name }}</h1>
            <StatusPill kind="campaign" :status="status" />
          </div>
          <p class="secondary-text numeric">
            Started {{ clock(s?.approved_at ?? campaign.approved_at) }}
            <template v-if="s"> · {{ n(s.total) }} messages</template>
            <template v-if="finished && s?.finished_at">
              · finished {{ clock(s.finished_at) }}</template
            >
          </p>
        </div>
        <p class="connection" :class="{ on: !reconnecting && !finished && s, off: reconnecting }">
          <span class="conn-dot" aria-hidden="true"></span>
          <template v-if="reconnecting">Reconnecting…</template>
          <template v-else-if="finished">Final</template>
          <template v-else-if="s">Live</template>
          <template v-else>Connecting…</template>
        </p>
      </header>

      <AppBanner v-if="reconnecting" tone="info" title="Reconnecting…">
        Showing the last update from {{ clock(s?.at) }}. The numbers carry on from there as soon as
        the connection is back.
        <template #action>
          <AppButton variant="secondary" @click="live.retryNow()">Try now</AppButton>
        </template>
      </AppBanner>
      <AppBanner v-else-if="live.stale.value" tone="info" title="Waiting for fresh data">
        The last update is from {{ clock(s?.at) }}.
      </AppBanner>

      <p class="sr-only" role="status">{{ announcement }}</p>

      <template v-if="s">
        <section class="overall glass" aria-label="Overall progress">
          <div class="overall-numbers">
            <p class="percent numeric" data-testid="percent">{{ percent }}%</p>
            <p class="done numeric" data-testid="done">{{ doneLabel }}</p>
          </div>
          <ProgressBar :value="s.done" :max="s.total" :label="doneLabel" />
        </section>

        <div class="channels">
          <ChannelProgress v-for="ch in channels" :key="ch" :channel="ch" :data="s.channels[ch]!" />
        </div>

        <div class="lower">
          <GlassCard
            :title="
              finished
                ? 'Sending speed · last 30 seconds of sending'
                : 'Sending speed · last 30 seconds'
            "
            :heading-level="2"
            class="chart-card"
          >
            <SpeedChart
              :series="channels.map((ch) => ({ channel: ch, values: s!.channels[ch]!.per_second }))"
              :limit="s.channels[channels[0]!]?.limit ?? 100"
              :window="s.window"
              :finished="finished"
            />
          </GlassCard>
          <RecentIssues :issues="s.recent_issues" :at="s.at" :show-channel="channels.length > 1" />
        </div>
      </template>
      <GlassCard v-else>
        <p class="secondary-text" role="status">Loading live data…</p>
        <SkeletonLines :lines="3" />
      </GlassCard>
    </template>

    <GlassCard v-else>
      <p class="secondary-text" role="status">Loading campaign…</p>
      <SkeletonLines :lines="3" />
    </GlassCard>
  </section>
</template>

<style scoped>
.live {
  display: flex;
  flex-direction: column;
  gap: var(--layout-gap);
}

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.title-block {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.title-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.connection {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-weight: var(--font-weight-label);
  color: var(--color-text-secondary);
}

.conn-dot {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: var(--radius-pill);
  background: var(--color-text-secondary);
}

.connection.on {
  color: var(--color-campaign-completed-ink);
}

.connection.on .conn-dot {
  background: var(--color-campaign-completed);
  animation: pulse var(--duration-pulse) ease-in-out infinite;
}

.connection.off .conn-dot {
  background: transparent;
  border: 2px solid var(--color-text-secondary);
}

@keyframes pulse {
  50% {
    opacity: 0.4;
  }
}

.overall {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6);
}

.overall-numbers {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.percent {
  font-family: var(--font-ui);
  font-size: var(--font-size-h1);
  line-height: var(--line-height-h1);
  font-weight: 700;
}

.done {
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
}

.channels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 24rem), 1fr));
  gap: var(--layout-gap);
}

.lower {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
  gap: var(--layout-gap);
  align-items: start;
}

@media (width < 1200px) {
  .lower {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>

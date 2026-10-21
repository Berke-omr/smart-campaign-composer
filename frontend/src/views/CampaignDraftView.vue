<script setup lang="ts">
// Draft and review of one campaign (02 — Draft): the brief, the audience,
// and the messages streaming in. Approved campaigns open their dashboard.
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { fetchCampaign } from '@/api/campaigns'
import { ApiError, isUnreachable } from '@/api/client'
import type { CampaignView } from '@/api/types'
import AudienceCard from '@/components/campaign/AudienceCard.vue'
import MessageCard from '@/components/campaign/MessageCard.vue'
import AppBanner from '@/components/ui/AppBanner.vue'
import AppButton from '@/components/ui/AppButton.vue'
import GlassCard from '@/components/ui/GlassCard.vue'
import SkeletonLines from '@/components/ui/SkeletonLines.vue'
import StatusPill from '@/components/ui/StatusPill.vue'
import StepIndicator, { type Step } from '@/components/ui/StepIndicator.vue'
import { useDraftStream } from '@/composables/useDraftStream'
import { isFinal, needsDrafting } from '@/logic/draft'

const props = defineProps<{ id: number }>()
const router = useRouter()

const campaign = ref<CampaignView | null>(null)
const loadError = ref<'not_found' | 'unreachable' | 'other' | null>(null)

const drafts = useDraftStream(props.id, {
  onDone: () => void refresh(),
  check: checkAfterDisconnect,
})

function isLocked(c: CampaignView) {
  return c.status !== 'draft' && c.status !== 'ready'
}

async function load() {
  loadError.value = null
  try {
    const c = await fetchCampaign(props.id)
    if (isLocked(c)) {
      await router.replace({ name: 'campaign-live', params: { id: c.id } })
      return
    }
    campaign.value = c
    drafts.start(c)
  } catch (e) {
    loadError.value =
      e instanceof ApiError && e.status === 404
        ? 'not_found'
        : isUnreachable(e)
          ? 'unreachable'
          : 'other'
  }
}

/** After draft.done: the stored messages, their reports and the new status. */
async function refresh() {
  try {
    const c = await fetchCampaign(props.id)
    campaign.value = c
    if (!isLocked(c)) drafts.start(c)
  } catch {
    // The cards already hold the final results; the next load picks up the status.
  }
}

/** The browser gave up on the stream: decide from the campaign whether to reopen it. */
async function checkAfterDisconnect(): Promise<'retry' | 'stop'> {
  try {
    const c = await fetchCampaign(props.id)
    campaign.value = c
    if (isLocked(c)) {
      await router.replace({ name: 'campaign-live', params: { id: c.id } })
      return 'stop'
    }
    if (!needsDrafting(c)) {
      drafts.start(c) // everything is stored: show it, nothing left to stream
      return 'stop'
    }
    return 'retry'
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      loadError.value = 'not_found'
      return 'stop'
    }
    return 'retry'
  }
}

onMounted(load)

const channels = computed(() => campaign.value?.channels ?? [])
const allFinal = computed(() =>
  channels.value.every((ch) => {
    const d = drafts.progress.value.channels[ch]
    return d !== undefined && isFinal(d.state)
  }),
)

const steps = computed<Step[]>(() => [
  { label: 'Audience', status: 'done' },
  { label: 'Messages', status: allFinal.value ? 'done' : 'current' },
  { label: 'Review', status: allFinal.value ? 'current' : 'upcoming' },
  { label: 'Send', status: 'upcoming' },
])

const canReview = computed(
  () =>
    campaign.value !== null &&
    campaign.value.status === 'ready' &&
    allFinal.value &&
    campaign.value.audience.channel_messages > 0,
)
</script>

<template>
  <section class="draft">
    <AppButton variant="ghost" icon="arrow-left" :to="{ name: 'composer' }">New campaign</AppButton>

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

    <template v-else-if="!campaign">
      <p class="secondary-text" role="status">Loading campaign…</p>
      <GlassCard><SkeletonLines :lines="4" /></GlassCard>
    </template>

    <template v-else>
      <header class="title-row">
        <h1>{{ campaign.name }}</h1>
        <StatusPill kind="campaign" :status="campaign.status" />
      </header>
      <StepIndicator :steps="steps" />

      <AppBanner
        v-if="drafts.connection.value === 'reconnecting'"
        tone="info"
        title="Reconnecting…"
      >
        The messages continue as soon as the connection is back.
        <template #action>
          <AppButton variant="secondary" @click="drafts.retryNow()">Try now</AppButton>
        </template>
      </AppBanner>
      <AppBanner
        v-for="e in drafts.progress.value.errors"
        :key="e"
        tone="error"
        title="Something went wrong while saving a message"
      >
        {{ e }}
      </AppBanner>

      <div class="columns">
        <div class="left">
          <GlassCard title="Brief">
            <p class="brief">{{ campaign.brief }}</p>
            <div>
              <AppButton
                variant="secondary"
                :to="{ name: 'composer', query: { from: String(campaign.id) } }"
              >
                Edit brief
              </AppButton>
            </div>
          </GlassCard>
          <AudienceCard :campaign="campaign" />
        </div>

        <div class="right">
          <template v-for="ch in channels" :key="ch">
            <MessageCard
              v-if="drafts.progress.value.channels[ch]"
              :channel="ch"
              :draft="drafts.progress.value.channels[ch]!"
              :longest="campaign.audience.longest_values[ch] ?? {}"
            />
          </template>
          <div class="actions">
            <AppButton variant="primary" :disabled="!canReview">Review &amp; approve</AppButton>
          </div>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.draft {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: var(--space-5);
}

.draft > .btn {
  align-self: flex-start;
}

.title-row {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.columns {
  display: grid;
  grid-template-columns: minmax(0, 2fr) minmax(0, 3fr);
  gap: var(--layout-gap);
  align-items: start;
}

.left,
.right {
  display: flex;
  flex-direction: column;
  gap: var(--layout-gap);
  min-width: 0;
}

.brief {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.actions {
  display: flex;
  justify-content: flex-end;
}

@media (width < 1200px) {
  .columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>

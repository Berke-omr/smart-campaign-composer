<script setup lang="ts">
// Draft and review of one campaign (02 — Draft): the brief, the audience,
// and the messages streaming in; then editing, and the confirmation that
// starts sending. Approved campaigns open their dashboard.
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { approveCampaign, fetchCampaign } from '@/api/campaigns'
import { ApiError, isUnreachable } from '@/api/client'
import type { CampaignView, Channel, Report, SaveMessageResponse } from '@/api/types'
import AudienceCard from '@/components/campaign/AudienceCard.vue'
import ConfirmSendModal from '@/components/campaign/ConfirmSendModal.vue'
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

// ---- Editing ----

/** Channels with an edit in progress; their last validation no longer describes the text. */
const editing = reactive(new Set<Channel>())

function onEditing(channel: Channel, on: boolean) {
  if (on) editing.add(channel)
  else editing.delete(channel)
}

function onSaved(channel: Channel, res: SaveMessageResponse) {
  if (!campaign.value) return
  campaign.value = {
    ...campaign.value,
    status: res.status,
    messages: { ...campaign.value.messages, [channel]: res.message },
  }
  drafts.setMessage(channel, res.message)
}

// ---- Review & approve ----

const confirmOpen = ref(false)
const sending = ref(false)
const approveError = ref<{ title: string; details: string[] } | null>(null)

const steps = computed<Step[]>(() => [
  { label: 'Audience', status: 'done' },
  { label: 'Messages', status: allFinal.value ? 'done' : 'current' },
  {
    label: 'Review',
    status: confirmOpen.value ? 'done' : allFinal.value ? 'current' : 'upcoming',
  },
  { label: 'Send', status: confirmOpen.value ? 'current' : 'upcoming' },
])

const canReview = computed(
  () =>
    campaign.value !== null &&
    campaign.value.status === 'ready' &&
    allFinal.value &&
    editing.size === 0 &&
    campaign.value.audience.channel_messages > 0,
)

/** Why "Review & approve" is not available yet, in words. */
const reviewHint = computed(() => {
  const c = campaign.value
  if (!c || canReview.value) return ''
  if (!allFinal.value) return 'Available once every message is written and checked.'
  if (editing.size > 0) return 'Save or cancel your edit first.'
  if (c.audience.channel_messages === 0) return 'Nobody in this audience can be reached.'
  return 'Every selected channel needs a valid message.'
})

function openConfirm() {
  approveError.value = null
  confirmOpen.value = true
}

async function approve() {
  if (sending.value || !campaign.value) return
  sending.value = true
  approveError.value = null
  try {
    const res = await approveCampaign(campaign.value.id)
    confirmOpen.value = false
    await router.push({ name: 'campaign-live', params: { id: res.campaign_id } })
  } catch (e) {
    approveError.value = approvalFailure(e)
  } finally {
    sending.value = false
  }
}

function approvalFailure(e: unknown) {
  if (isUnreachable(e)) {
    return {
      title: "Can't reach the server",
      details: [
        'Nothing changes by trying again: a campaign that was already approved is never sent twice.',
      ],
    }
  }
  if (e instanceof ApiError) {
    const report = (e.body as { report?: Report } | null)?.report
    return {
      title: e.message.charAt(0).toUpperCase() + e.message.slice(1),
      details: report ? report.errors.map((x) => x.message) : [],
    }
  }
  return { title: 'Something went wrong', details: [] }
}
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
              :campaign-id="campaign.id"
              :editable="!isLocked(campaign) && !sending"
              @editing="onEditing(ch, $event)"
              @saved="onSaved(ch, $event)"
            />
          </template>
          <div class="actions">
            <p v-if="reviewHint" id="review-hint" class="caption secondary-text">
              {{ reviewHint }}
            </p>
            <AppButton
              variant="primary"
              :disabled="!canReview"
              :aria-describedby="reviewHint ? 'review-hint' : undefined"
              @click="openConfirm"
            >
              Review &amp; approve
            </AppButton>
          </div>
        </div>
      </div>

      <ConfirmSendModal
        :open="confirmOpen"
        :campaign="campaign"
        :sending="sending"
        :error="approveError"
        @close="confirmOpen = false"
        @confirm="approve"
      />
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
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-4);
  flex-wrap: wrap;
}

@media (width < 1200px) {
  .columns {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>

<script setup lang="ts">
// Home: the marketer describes a campaign (01 — Composer). Generate turns the
// brief into a draft campaign (POST /api/campaigns: audience rule and name)
// and opens it; the messages are written on the draft screen.
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createCampaign, fetchCampaign, fetchCampaigns, fetchExamples } from '@/api/campaigns'
import { ApiError, isUnreachable } from '@/api/client'
import {
  CHANNELS,
  TONES,
  type BriefExample,
  type CampaignSummary,
  type Channel,
  type Tone,
} from '@/api/types'
import CampaignGrid from '@/components/campaign/CampaignGrid.vue'
import AppBanner from '@/components/ui/AppBanner.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppChip from '@/components/ui/AppChip.vue'
import AppDialog from '@/components/ui/AppDialog.vue'
import AppIcon from '@/components/ui/AppIcon.vue'
import { charCount } from '@/logic/content'
import { CHANNEL_LABELS, TONE_LABELS } from '@/logic/labels'

const BRIEF_LIMIT = 2000

const route = useRoute()
const router = useRouter()

const brief = ref('')
const channels = ref<Channel[]>(['sms', 'email'])
const tone = ref<Tone>('friendly')
const briefField = ref<HTMLTextAreaElement | null>(null)

const generating = ref(false)
const failure = ref<{ title: string; details: string[]; unreachable: boolean } | null>(null)

const hour = new Date().getHours()
const greeting = hour < 12 ? 'Good morning' : hour < 18 ? 'Good afternoon' : 'Good evening'

const briefLength = computed(() => charCount(brief.value))
const briefTooLong = computed(() => briefLength.value > BRIEF_LIMIT)
const canGenerate = computed(
  () => brief.value.trim() !== '' && !briefTooLong.value && channels.value.length > 0,
)

function toggle(channel: Channel) {
  channels.value = channels.value.includes(channel)
    ? channels.value.filter((c) => c !== channel)
    : CHANNELS.filter((c) => c === channel || channels.value.includes(c))
}

async function generate() {
  if (!canGenerate.value || generating.value) return
  generating.value = true
  failure.value = null
  try {
    const campaign = await createCampaign({
      brief: brief.value,
      channels: channels.value,
      tone: tone.value,
    })
    await router.push({ name: 'campaign', params: { id: campaign.id } })
  } catch (e) {
    failure.value = describe(e)
  } finally {
    generating.value = false
  }
}

function describe(e: unknown) {
  if (isUnreachable(e)) {
    return {
      title: "Can't reach the server",
      details: ['Your brief is still here. Check that the app is running, then try again.'],
      unreachable: true,
    }
  }
  if (e instanceof ApiError) {
    const details = (e.body as { details?: unknown } | null)?.details
    return {
      title: e.message.charAt(0).toUpperCase() + e.message.slice(1),
      details: Array.isArray(details) ? details.map(String) : [],
      unreachable: false,
    }
  }
  return { title: 'Something went wrong', details: [], unreachable: false }
}

// ---- Brief library (GET /api/briefs/examples) ----

const libraryOpen = ref(false)
const examples = ref<BriefExample[] | null>(null)
const examplesError = ref(false)

async function openLibrary() {
  libraryOpen.value = true
  if (examples.value) return
  examplesError.value = false
  try {
    examples.value = await fetchExamples()
  } catch {
    examplesError.value = true
  }
}

function channelList(list: Channel[]): string {
  return list.map((c) => CHANNEL_LABELS[c]).join(' + ')
}

function useExample(example: BriefExample) {
  brief.value = example.brief
  channels.value = CHANNELS.filter((c) => example.channels.includes(c))
  tone.value = example.tone
  failure.value = null
  libraryOpen.value = false
  briefField.value?.focus()
}

// ---- Recent campaigns (GET /api/campaigns, newest first) ----

const recent = ref<CampaignSummary[] | null>(null)
const recentFailed = ref(false)
const RECENT = 4

onMounted(async () => {
  try {
    recent.value = await fetchCampaigns()
  } catch {
    recentFailed.value = true
  }
})

// "Edit brief" on the draft screen comes back here with ?from=<campaign id>.
onMounted(async () => {
  const from = Number(route.query.from)
  if (!Number.isInteger(from) || from <= 0) return
  try {
    const c = await fetchCampaign(from)
    brief.value = c.brief
    channels.value = c.channels
    tone.value = c.tone
  } catch {
    // The old brief is a convenience; without it the composer starts empty.
  }
})
</script>

<template>
  <section class="composer">
    <header class="hero">
      <h1 class="hero-title">{{ greeting }}</h1>
      <p class="hero-lead secondary-text">
        Describe your campaign. We'll find the right audience and draft your SMS &amp; email —
        nothing is sent until you approve.
      </p>
    </header>

    <form class="compose-card glass" @submit.prevent="generate">
      <label class="brief-label label-text" for="brief">Campaign brief</label>
      <textarea
        id="brief"
        ref="briefField"
        v-model="brief"
        class="brief-input"
        rows="3"
        placeholder="Who should get it, what should it say, and how should it sound?"
        :aria-invalid="briefTooLong || undefined"
        :aria-describedby="briefTooLong ? 'brief-error' : undefined"
      ></textarea>
      <p v-if="briefTooLong" id="brief-error" class="field-error caption">
        The brief is {{ briefLength.toLocaleString('en-US') }} characters; the limit is
        {{ BRIEF_LIMIT.toLocaleString('en-US') }}.
      </p>

      <div class="controls">
        <div class="options" role="group" aria-label="Campaign options">
          <AppChip
            v-for="c in CHANNELS"
            :key="c"
            :channel="c"
            :pressed="channels.includes(c)"
            @click="toggle(c)"
          >
            {{ CHANNEL_LABELS[c] }}
          </AppChip>
          <label class="chip-select">
            <AppIcon name="sliders" :size="18" />
            <span>Tone:</span>
            <select v-model="tone">
              <option v-for="t in TONES" :key="t" :value="t">{{ TONE_LABELS[t] }}</option>
            </select>
          </label>
          <AppChip action icon="folder" @click="openLibrary">Brief library</AppChip>
        </div>
        <AppButton
          variant="primary"
          type="submit"
          icon="sparkle"
          :disabled="!canGenerate"
          :loading="generating"
        >
          Generate
        </AppButton>
      </div>
      <p v-if="channels.length === 0" class="caption secondary-text">
        Choose at least one channel.
      </p>
      <p class="sr-only" role="status">{{ generating ? 'Finding the audience…' : '' }}</p>
    </form>

    <AppBanner v-if="failure" tone="error" :title="failure.title">
      <ul v-if="failure.details.length" class="details">
        <li v-for="d in failure.details" :key="d">{{ d }}</li>
      </ul>
      <template v-if="failure.unreachable" #action>
        <AppButton variant="secondary" :loading="generating" @click="generate">Try again</AppButton>
      </template>
    </AppBanner>

    <section class="recent" aria-labelledby="recent-title">
      <div class="recent-head">
        <h2 id="recent-title">Recent campaigns</h2>
        <AppButton
          v-if="recent && recent.length > RECENT"
          variant="ghost"
          :to="{ name: 'campaigns' }"
        >
          All campaigns
        </AppButton>
      </div>
      <p v-if="recentFailed" class="secondary-text">Recent campaigns could not be loaded.</p>
      <p v-else-if="!recent" class="secondary-text" role="status">Loading campaigns…</p>
      <p v-else-if="recent.length === 0" class="secondary-text">
        No campaigns yet. Your first one will appear here.
      </p>
      <CampaignGrid v-else :campaigns="recent.slice(0, RECENT)" />
    </section>

    <AppDialog :open="libraryOpen" title="Brief library" @close="libraryOpen = false">
      <p class="secondary-text">
        Start from a ready-made brief. You can change it before generating.
      </p>
      <p v-if="examplesError" class="field-error">Can't reach the server. Close and try again.</p>
      <p v-else-if="!examples" class="secondary-text" role="status">Loading briefs…</p>
      <ul v-else class="examples">
        <li v-for="ex in examples" :key="ex.id">
          <button type="button" class="example" @click="useExample(ex)">
            <span class="example-title">{{ ex.title }}</span>
            <span class="example-brief">{{ ex.brief }}</span>
            <span class="example-meta caption secondary-text">
              {{ channelList(ex.channels) }} · Tone:
              {{ TONE_LABELS[ex.tone] }}
            </span>
          </button>
        </li>
      </ul>
    </AppDialog>
  </section>
</template>

<style scoped>
.composer {
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
  max-width: 72rem;
  margin: 0 auto;
}

.hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  text-align: center;
  padding-top: var(--space-8);
}

.hero-title {
  font-size: var(--font-size-hero);
  line-height: var(--line-height-hero);
  font-weight: var(--font-weight-hero);
}

.hero-lead {
  max-width: 48rem;
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
}

.compose-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-6);
}

.brief-label {
  color: var(--color-text-secondary);
}

.brief-input {
  width: 100%;
  min-height: 9rem;
  padding: var(--space-5) var(--space-6);
  border: var(--border-thin) solid var(--color-border-control);
  border-radius: var(--radius-field);
  background: var(--color-field);
  font-size: var(--font-size-compose);
  line-height: var(--line-height-compose);
  resize: vertical;
}

.brief-input[aria-invalid='true'] {
  border-color: var(--color-campaign-failed);
}

.field-error {
  color: var(--color-campaign-failed-ink);
}

.controls {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
  margin-top: var(--space-2);
}

.options {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.chip-select {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--control-height);
  padding: 0 var(--space-4) 0 var(--space-5);
  border-radius: var(--radius-pill);
  border: var(--border-thin) solid var(--color-border-control);
  background: var(--color-glass);
  font-size: var(--font-size-label);
  font-weight: var(--font-weight-label);
}

.chip-select:focus-within {
  outline: var(--focus-width) solid var(--color-focus);
  outline-offset: var(--focus-offset);
}

.chip-select select {
  border: 0;
  background: transparent;
  font-weight: var(--font-weight-label);
  cursor: pointer;
}

.chip-select select:focus-visible {
  outline: none; /* the whole chip shows the ring */
}

.recent {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.recent-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
}

.details {
  margin: var(--space-1) 0 0;
  padding-left: var(--space-5);
}

.examples {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin: 0;
  padding: 0;
  list-style: none;
}

.example {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  width: 100%;
  padding: var(--space-4) var(--space-5);
  border: var(--border-thin) solid var(--color-border-control);
  border-radius: var(--radius-field);
  background: var(--color-field);
  text-align: left;
  cursor: pointer;
}

.example:hover {
  background: var(--color-glass-hover);
}

.example-title {
  font-weight: 600;
}

.example-brief {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

@media (width < 1200px) {
  .hero-title {
    font-size: var(--font-size-h1);
    line-height: var(--line-height-h1);
  }
}
</style>

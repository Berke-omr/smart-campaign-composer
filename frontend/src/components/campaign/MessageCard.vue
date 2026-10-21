<script setup lang="ts">
// One channel's message while it is written and checked (02 — Draft):
// Writing… (text streams in), Validating…, Fixing… · attempt n of 3, Valid,
// Needs manual edit. The counter shows the worst-case length (D15).
import { computed, ref, useId } from 'vue'
import type { Channel, Field, SaveMessageResponse, Values } from '@/api/types'
import AppButton from '@/components/ui/AppButton.vue'
import AppIcon from '@/components/ui/AppIcon.vue'
import CharacterCounter from '@/components/ui/CharacterCounter.vue'
import SkeletonLines from '@/components/ui/SkeletonLines.vue'
import { fieldLimit, fieldsOf, worstCaseLength } from '@/logic/content'
import { isFinal, type ChannelDraft } from '@/logic/draft'
import { CHANNEL_LABELS, draftStateLabel } from '@/logic/labels'
import MessageEditor from './MessageEditor.vue'
import MessageText from './MessageText.vue'

const props = withDefaults(
  defineProps<{
    channel: Channel
    draft: ChannelDraft
    longest: Values
    campaignId: number
    /** The campaign is still a draft or ready, so a finished message may be edited. */
    editable?: boolean
  }>(),
  { editable: false },
)
const emit = defineEmits<{ editing: [boolean]; saved: [SaveMessageResponse] }>()

const editing = ref(false)

function setEditing(on: boolean) {
  editing.value = on
  emit('editing', on)
}

function onSaved(res: SaveMessageResponse) {
  setEditing(false)
  emit('saved', res)
}

const hasText = computed(() => Object.values(props.draft.fields).some((v) => v))

const titleId = useId()
const FIELD_LABELS: Record<Field, string> = { text: 'Text', subject: 'Subject', body: 'Body' }

const fields = computed(() => fieldsOf(props.channel))
const writing = computed(() => props.draft.state === 'writing')

const stateLabel = computed(() =>
  editing.value
    ? 'Editing · not saved yet'
    : props.draft.state === 'pending'
      ? 'Waiting to start…'
      : draftStateLabel(props.draft.state, props.draft.attempt, props.draft.maxAttempts),
)

/** The field the text is streaming into: the email subject is complete before the body starts. */
const streamingField = computed<Field | null>(() => {
  if (!writing.value) return null
  if (props.channel === 'sms') return 'text'
  return props.draft.fields.body !== undefined ? 'body' : 'subject'
})

function text(field: Field): string {
  return props.draft.fields[field] ?? ''
}

function errorsOf(field: Field): string[] {
  return (props.draft.report?.errors ?? []).filter((e) => e.field === field).map((e) => e.message)
}
</script>

<template>
  <section
    class="message-card glass"
    :aria-labelledby="titleId"
    :data-state="draft.state"
    data-testid="message-card"
  >
    <header class="card-header">
      <h2 :id="titleId" class="channel-title">
        <span
          class="dot"
          :style="{ background: `var(--color-${channel})` }"
          aria-hidden="true"
        ></span>
        {{ CHANNEL_LABELS[channel] }}
      </h2>
      <span class="state" :class="`state-${draft.state}`" aria-live="polite" data-testid="state">
        <span v-if="draft.state === 'writing'" class="pulse" aria-hidden="true"></span>
        <AppIcon
          v-else-if="draft.state === 'validating' || draft.state === 'fixing'"
          name="spinner"
          :size="18"
        />
        <AppIcon v-else-if="draft.state === 'valid'" name="check" :size="18" />
        <AppIcon v-else-if="draft.state === 'needs_manual_edit'" name="warning" :size="18" />
        {{ stateLabel }}
      </span>
    </header>

    <MessageEditor
      v-if="editing"
      :campaign-id="campaignId"
      :channel="channel"
      :initial="draft.fields"
      :longest="longest"
      @saved="onSaved"
      @cancel="setEditing(false)"
    />

    <template v-else>
      <p v-if="draft.state === 'fixing' && draft.problems.length" class="caption secondary-text">
        Fixing: {{ draft.problems.join('; ') }}
      </p>

      <div v-for="field in fields" :key="field" class="field">
        <span v-if="channel === 'email'" class="field-label label-text">{{
          FIELD_LABELS[field]
        }}</span>
        <div class="field-box">
          <MessageText
            v-if="text(field)"
            :text="text(field)"
            :channel="channel"
            :writing="streamingField === field"
          />
          <p v-else-if="isFinal(draft.state)" class="secondary-text">No text yet.</p>
          <SkeletonLines
            v-if="streamingField === field || (draft.state === 'pending' && !text(field))"
          />
          <div v-if="fieldLimit(field) > 0 && text(field)" class="field-footer">
            <CharacterCounter
              :length="worstCaseLength(text(field), longest)"
              :limit="fieldLimit(field)"
            />
          </div>
        </div>
        <ul v-if="errorsOf(field).length" class="field-errors">
          <li v-for="e in errorsOf(field)" :key="e">{{ e }}</li>
        </ul>
      </div>

      <div v-if="draft.state === 'needs_manual_edit'" class="manual" role="note">
        <p class="manual-title">This message needs a manual edit</p>
        <ul v-if="draft.problems.length">
          <li v-for="p in draft.problems" :key="p">{{ p }}</li>
        </ul>
      </div>

      <div v-if="editable && isFinal(draft.state)" class="card-actions">
        <AppButton variant="secondary" @click="setEditing(true)">
          {{ !hasText ? 'Write message' : draft.state === 'valid' ? 'Edit' : 'Edit message' }}
        </AppButton>
      </div>
    </template>
  </section>
</template>

<style scoped>
.message-card {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6);
  min-width: 0;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.channel-title {
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

.state {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--font-size-label);
  font-weight: var(--font-weight-label);
  color: var(--color-text-secondary);
}

.state-writing {
  color: var(--color-campaign-sending-ink);
}

.state-validating {
  color: var(--color-campaign-sending-ink);
}

.state-fixing {
  color: var(--color-message-retrying-ink);
}

.state-valid {
  color: var(--color-campaign-completed-ink);
}

.state-needs_manual_edit {
  color: var(--color-campaign-failed-ink);
}

.pulse {
  width: 0.625rem;
  height: 0.625rem;
  border-radius: var(--radius-pill);
  background: var(--color-campaign-sending);
  animation: pulse var(--duration-pulse) ease-in-out infinite;
}

@keyframes pulse {
  50% {
    opacity: 0.35;
  }
}

.field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.field-label {
  color: var(--color-text-secondary);
}

.field-box {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-field);
  border: var(--border-thin) solid var(--color-glass-border);
  background: var(--color-field);
  min-height: 3.5rem;
}

.field-footer {
  display: flex;
  justify-content: flex-end;
}

.field-errors {
  margin: 0;
  padding-left: var(--space-5);
  color: var(--color-campaign-failed-ink);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
}

.manual {
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-field);
  border: var(--border-thin) solid var(--color-campaign-failed-border);
  background: var(--color-campaign-failed-tint);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
}

.manual-title {
  font-weight: 600;
  color: var(--color-campaign-failed-ink);
}

.card-actions {
  display: flex;
  justify-content: flex-end;
}

.manual ul {
  margin: var(--space-1) 0 0;
  padding-left: var(--space-5);
}
</style>

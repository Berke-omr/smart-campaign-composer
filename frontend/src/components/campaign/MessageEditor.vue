<script setup lang="ts">
// Editing one channel's message (Review). The template is plain text with
// {{variables}}; chips insert a variable at the caret. The counter shows the
// worst case as the backend computes it (D15), the preview renders the
// fictional sample user and the longest case. Save sends the edit through the
// backend's validator (PUT …/messages/{channel}); a rejected edit leaves the
// stored message unchanged and its errors appear next to the fields.
import { computed, nextTick, reactive, ref, useId } from 'vue'
import { saveMessage } from '@/api/campaigns'
import { ApiError, isUnreachable } from '@/api/client'
import {
  VARIABLES,
  type Channel,
  type Field,
  type FieldError,
  type MessageContent,
  type Report,
  type SaveMessageResponse,
  type Values,
  type Variable,
} from '@/api/types'
import AppButton from '@/components/ui/AppButton.vue'
import AppChip from '@/components/ui/AppChip.vue'
import CharacterCounter from '@/components/ui/CharacterCounter.vue'
import {
  fieldLimit,
  fieldsOf,
  insertAt,
  placeholder,
  renderMessage,
  sampleUser,
  worstCaseLength,
  worstCaseValues,
} from '@/logic/content'

const props = defineProps<{
  campaignId: number
  channel: Channel
  initial: Partial<Record<Field, string>>
  longest: Values
}>()
const emit = defineEmits<{ saved: [SaveMessageResponse]; cancel: [] }>()

const uid = useId()
const LABELS: Record<Field, string> = { text: 'SMS text', subject: 'Subject', body: 'Body' }

const fields = fieldsOf(props.channel)
const values = reactive<Record<Field, string>>({
  text: props.initial.text ?? '',
  subject: props.initial.subject ?? '',
  body: props.initial.body ?? '',
})
const inputs = ref<Partial<Record<Field, HTMLInputElement | HTMLTextAreaElement>>>({})

const saving = ref(false)
const fieldErrors = ref<FieldError[]>([])
const failure = ref<string | null>(null)

// The variable chips insert into the field that was focused last.
const target = ref<Field>(fields[0]!)
const selection = reactive({ start: 0, end: 0 })

function remember(field: Field, e: Event) {
  const el = e.target as HTMLInputElement | HTMLTextAreaElement
  target.value = field
  selection.start = el.selectionStart ?? values[field].length
  selection.end = el.selectionEnd ?? selection.start
}

async function insert(v: Variable) {
  const field = target.value
  const { value, caret } = insertAt(values[field], selection.start, selection.end, placeholder(v))
  values[field] = value
  selection.start = selection.end = caret
  await nextTick()
  const el = inputs.value[field]
  el?.focus()
  el?.setSelectionRange(caret, caret)
}

function setInput(field: Field, el: unknown) {
  if (el) inputs.value[field] = el as HTMLInputElement | HTMLTextAreaElement
}

function content(): MessageContent {
  return props.channel === 'sms'
    ? { text: values.text }
    : { subject: values.subject, body: values.body }
}

const preview = computed(() => ({
  sample: renderMessage(content(), sampleUser(props.longest)),
  worst: renderMessage(content(), worstCaseValues(props.longest)),
  sampleName: `${sampleUser(props.longest).first_name} ${sampleUser(props.longest).last_name}`,
}))

function errorsOf(field: Field) {
  return fieldErrors.value.filter((e) => e.field === field)
}

function describedBy(field: Field) {
  const ids = []
  if (fieldLimit(field) > 0) ids.push(`${uid}-${field}-counter`)
  if (errorsOf(field).length) ids.push(`${uid}-${field}-errors`)
  return ids.join(' ') || undefined
}

async function save() {
  if (saving.value) return
  saving.value = true
  failure.value = null
  try {
    const res = await saveMessage(props.campaignId, props.channel, content())
    fieldErrors.value = []
    emit('saved', res)
  } catch (e) {
    const report = e instanceof ApiError ? (e.body as { report?: Report } | null)?.report : null
    if (e instanceof ApiError && e.status === 422 && report) {
      fieldErrors.value = report.errors
      failure.value = 'Not saved: fix the highlighted fields. The previous version is kept.'
    } else if (isUnreachable(e)) {
      failure.value = "Can't reach the server. Your edit is still here; try saving again."
    } else if (e instanceof ApiError) {
      failure.value = e.message.charAt(0).toUpperCase() + e.message.slice(1)
    } else {
      failure.value = 'Something went wrong'
    }
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <form class="editor" @submit.prevent="save">
    <div class="chips" role="group" aria-label="Insert a personalization variable">
      <span class="caption secondary-text">Insert:</span>
      <AppChip
        v-for="v in VARIABLES"
        :key="v"
        action
        icon="plus"
        :aria-label="`Insert ${v}`"
        @mousedown.prevent
        @click="insert(v)"
      >
        <span class="var-chip" :class="`var-${channel}`">{{ v }}</span>
      </AppChip>
    </div>

    <div v-for="field in fields" :key="field" class="field">
      <label :for="`${uid}-${field}`" class="label-text">{{ LABELS[field] }}</label>
      <input
        v-if="field === 'subject'"
        :id="`${uid}-${field}`"
        :ref="(el) => setInput(field, el)"
        v-model="values[field]"
        type="text"
        class="input"
        :aria-invalid="errorsOf(field).length > 0 || undefined"
        :aria-describedby="describedBy(field)"
        @focus="remember(field, $event)"
        @select="remember(field, $event)"
        @keyup="remember(field, $event)"
        @click="remember(field, $event)"
        @input="remember(field, $event)"
      />
      <textarea
        v-else
        :id="`${uid}-${field}`"
        :ref="(el) => setInput(field, el)"
        v-model="values[field]"
        class="input"
        :rows="field === 'body' ? 8 : 4"
        :aria-invalid="errorsOf(field).length > 0 || undefined"
        :aria-describedby="describedBy(field)"
        @focus="remember(field, $event)"
        @select="remember(field, $event)"
        @keyup="remember(field, $event)"
        @click="remember(field, $event)"
        @input="remember(field, $event)"
      ></textarea>
      <div v-if="fieldLimit(field) > 0" class="field-footer">
        <span class="caption secondary-text">Counted with the longest names in this audience</span>
        <CharacterCounter
          :id="`${uid}-${field}-counter`"
          :length="worstCaseLength(values[field], longest)"
          :limit="fieldLimit(field)"
        />
      </div>
      <ul v-if="errorsOf(field).length" :id="`${uid}-${field}-errors`" class="field-errors">
        <li v-for="e in errorsOf(field)" :key="e.message">{{ e.message }}</li>
      </ul>
    </div>

    <section class="preview" :aria-labelledby="`${uid}-preview`">
      <h3 :id="`${uid}-preview`" class="preview-title label-text">Preview</h3>
      <div class="preview-case">
        <p class="caption secondary-text">Sample customer ({{ preview.sampleName }}, fictional)</p>
        <p v-for="field in fields" :key="field" class="preview-text" :class="`preview-${field}`">
          {{ preview.sample[field] }}
        </p>
      </div>
      <div class="preview-case">
        <p class="caption secondary-text">Longest names in this audience</p>
        <p v-for="field in fields" :key="field" class="preview-text" :class="`preview-${field}`">
          {{ preview.worst[field] }}
        </p>
      </div>
    </section>

    <p v-if="failure" class="failure" role="alert">{{ failure }}</p>

    <div class="actions">
      <AppButton variant="ghost" :disabled="saving" @click="emit('cancel')">Cancel</AppButton>
      <AppButton variant="primary" type="submit" :loading="saving">Save</AppButton>
    </div>
  </form>
</template>

<style scoped>
.editor {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.chips {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.var-chip {
  font-weight: var(--font-weight-label);
}

.var-sms {
  color: var(--color-sms-ink);
}

.var-email {
  color: var(--color-email-ink);
}

.field {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.input {
  width: 100%;
  padding: var(--space-3) var(--space-4);
  border: var(--border-thin) solid var(--color-border-control);
  border-radius: var(--radius-field);
  background: var(--color-field);
  font-size: var(--font-size-body);
  line-height: var(--line-height-body);
  resize: vertical;
}

input.input {
  min-height: var(--control-height);
}

.input[aria-invalid='true'] {
  border-color: var(--color-campaign-failed);
}

.field-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.field-errors {
  margin: 0;
  padding-left: var(--space-5);
  color: var(--color-campaign-failed-ink);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
}

.preview {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-field);
  background: var(--color-track);
}

.preview-case {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

.preview-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.preview-subject {
  font-weight: 600;
}

.failure {
  color: var(--color-campaign-failed-ink);
  font-weight: 600;
}

.actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
}
</style>

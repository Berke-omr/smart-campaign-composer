<script setup lang="ts">
// The last step before sending: the final numbers, the content, and "This
// can't be undone". Only "Send messages" approves (POST …/approve, which is
// idempotent on the backend; here the button also cannot be pressed twice).
// Errors stay in the dialog; Cancel leaves everything as it was.
import { computed } from 'vue'
import type { CampaignView, Channel } from '@/api/types'
import AppBanner from '@/components/ui/AppBanner.vue'
import AppButton from '@/components/ui/AppButton.vue'
import AppDialog from '@/components/ui/AppDialog.vue'
import AppIcon from '@/components/ui/AppIcon.vue'
import { CHANNEL_LABELS } from '@/logic/labels'
import MessageText from './MessageText.vue'

const props = defineProps<{
  open: boolean
  campaign: CampaignView
  sending: boolean
  error: { title: string; details: string[] } | null
}>()
const emit = defineEmits<{ close: []; confirm: [] }>()

const n = (v: number) => v.toLocaleString('en-US')
const a = computed(() => props.campaign.audience)

const rows = computed(() =>
  props.campaign.channels.map((ch: Channel) => ({
    channel: ch,
    reachable: ch === 'sms' ? a.value.sms_reachable : a.value.email_reachable,
    excluded: ch === 'sms' ? a.value.sms_excluded : a.value.email_excluded,
    message: props.campaign.messages[ch]?.message ?? null,
  })),
)
</script>

<template>
  <AppDialog :open="open" title="Send this campaign?" :busy="sending" @close="emit('close')">
    <p class="name">{{ campaign.name }}</p>

    <section class="block" aria-label="Audience">
      <ul class="conditions">
        <li v-for="line in a.description.lines" :key="line">{{ line }}</li>
      </ul>
      <p class="numeric">
        <strong>{{ n(a.total) }}</strong> {{ a.total === 1 ? 'person' : 'people' }}
      </p>
      <AppBanner
        v-if="campaign.unsupported_criteria.length"
        tone="warning"
        title="Not every part of the brief was applied"
      >
        Not applied: {{ campaign.unsupported_criteria.join(', ') }}.
      </AppBanner>
    </section>

    <section class="block" aria-label="Messages">
      <div v-for="r in rows" :key="r.channel" class="channel" data-testid="confirm-channel">
        <p class="channel-line">
          <span
            class="dot"
            :style="{ background: `var(--color-${r.channel})` }"
            aria-hidden="true"
          ></span>
          <strong>{{ CHANNEL_LABELS[r.channel] }}</strong>
          · {{ n(r.reachable) }} {{ r.reachable === 1 ? 'message' : 'messages' }}
          <span class="secondary-text">· {{ n(r.excluded) }} without consent left out</span>
        </p>
        <div v-if="r.message" class="content">
          <MessageText v-if="r.channel === 'sms'" :text="r.message.text ?? ''" channel="sms" />
          <template v-else>
            <MessageText :text="r.message.subject ?? ''" channel="email" class="subject" />
            <MessageText :text="r.message.body ?? ''" channel="email" class="body" />
          </template>
        </div>
      </div>
      <p class="total numeric" data-testid="confirm-total">
        {{ n(a.channel_messages) }} {{ a.channel_messages === 1 ? 'message' : 'messages' }} in total
      </p>
    </section>

    <p class="undo">
      <AppIcon name="warning" />
      This can't be undone. Messages start going out as soon as you confirm.
    </p>

    <AppBanner v-if="error" tone="error" :title="error.title">
      <ul v-if="error.details.length" class="details">
        <li v-for="d in error.details" :key="d">{{ d }}</li>
      </ul>
    </AppBanner>

    <template #footer>
      <AppButton variant="secondary" :disabled="sending" @click="emit('close')">Cancel</AppButton>
      <AppButton variant="primary" icon="send" :loading="sending" @click="emit('confirm')">
        Send messages
      </AppButton>
    </template>
  </AppDialog>
</template>

<style scoped>
.name {
  font-family: var(--font-heading);
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
  font-weight: 600;
}

.block {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-5);
  border-radius: var(--radius-field);
  background: var(--color-field);
  border: var(--border-thin) solid var(--color-glass-border);
}

.conditions {
  margin: 0;
  padding-left: var(--space-5);
}

.channel {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.channel-line {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.dot {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: var(--radius-pill);
}

.content {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  padding-left: var(--space-5);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
}

.content .subject {
  font-weight: 600;
}

.content .body {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.total {
  font-weight: 600;
}

.undo {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-weight: 700;
  color: var(--color-campaign-failed-ink);
}

.details {
  margin: var(--space-1) 0 0;
  padding-left: var(--space-5);
}
</style>

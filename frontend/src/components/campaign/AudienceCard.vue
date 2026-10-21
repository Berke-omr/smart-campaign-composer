<script setup lang="ts">
// Who the campaign reaches: the rule as readable conditions, the people
// counted, and per channel how many can be reached and how many are left out
// for lack of consent (R4: no consent is never shown as reachable).
import { computed } from 'vue'
import type { CampaignView, Channel } from '@/api/types'
import AppBanner from '@/components/ui/AppBanner.vue'
import { CHANNEL_LABELS } from '@/logic/labels'

const props = defineProps<{ campaign: CampaignView }>()

const n = (v: number) => v.toLocaleString('en-US')
const a = computed(() => props.campaign.audience)
const joiner = computed(() => (props.campaign.rule.match === 'any' ? 'or' : 'and'))

const reach = computed(() =>
  props.campaign.channels.map((ch: Channel) => ({
    channel: ch,
    reachable: ch === 'sms' ? a.value.sms_reachable : a.value.email_reachable,
    excluded: ch === 'sms' ? a.value.sms_excluded : a.value.email_excluded,
  })),
)
</script>

<template>
  <section class="audience glass" aria-labelledby="audience-title">
    <h2 id="audience-title">Audience</h2>

    <AppBanner
      v-if="campaign.unsupported_criteria.length"
      tone="warning"
      title="Some of the brief could not be targeted"
    >
      Not applied: {{ campaign.unsupported_criteria.join(', ') }}. The counts below leave
      {{ campaign.unsupported_criteria.length === 1 ? 'it' : 'them' }} out.
    </AppBanner>

    <ul class="conditions" aria-label="Conditions">
      <li v-for="(line, i) in a.description.lines" :key="line" class="condition-row">
        <span v-if="i > 0" class="joiner caption secondary-text">{{ joiner }}</span>
        <span class="condition">{{ line }}</span>
      </li>
    </ul>

    <p class="people numeric" data-testid="people">
      <strong>{{ n(a.total) }}</strong> {{ a.total === 1 ? 'person' : 'people' }}
    </p>

    <p v-if="a.total === 0" class="empty">No people match this audience.</p>

    <ul class="reach" aria-label="Reach per channel">
      <li v-for="r in reach" :key="r.channel" class="reach-row">
        <span
          class="dot"
          :style="{ background: `var(--color-${r.channel})` }"
          aria-hidden="true"
        ></span>
        <span>
          <strong>{{ CHANNEL_LABELS[r.channel] }}</strong> · {{ n(r.reachable) }} reachable
          <span class="secondary-text">· {{ n(r.excluded) }} without consent</span>
        </span>
      </li>
    </ul>

    <p class="messages numeric" data-testid="channel-messages">
      {{ n(a.channel_messages) }} {{ a.channel_messages === 1 ? 'message' : 'messages' }} in total
    </p>
    <p v-if="a.total > 0 && a.channel_messages === 0" class="empty">
      No one in this audience can be reached on the selected channels.
    </p>
    <p class="caption secondary-text">Only people who opted in to a channel will receive it.</p>
  </section>
</template>

<style scoped>
.audience {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6);
  min-width: 0;
}

.conditions {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.condition-row {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.condition {
  padding: var(--space-2) var(--space-5);
  border-radius: var(--radius-field);
  background: var(--color-track);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
  overflow-wrap: anywhere;
}

.people {
  font-family: var(--font-heading);
  font-size: var(--font-size-h2);
  line-height: var(--line-height-h2);
}

.people strong {
  font-size: var(--font-size-h1);
}

.empty {
  font-weight: 600;
  color: var(--color-campaign-failed-ink);
}

.reach {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin: 0;
  padding: 0;
  list-style: none;
}

.reach-row {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
}

.dot {
  flex: none;
  width: 0.75rem;
  height: 0.75rem;
  border-radius: var(--radius-pill);
}

.messages {
  font-weight: 600;
}
</style>

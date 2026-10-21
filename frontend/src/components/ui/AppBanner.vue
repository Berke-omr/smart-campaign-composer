<script setup lang="ts">
// Inline notice: warning (skipped conditions), info (reconnecting), error
// (server failure). Errors are announced at once; the others politely.
import { computed } from 'vue'
import AppIcon, { type IconName } from './AppIcon.vue'

const props = defineProps<{ tone: 'warning' | 'info' | 'error'; title: string }>()

const icon = computed<IconName>(() =>
  props.tone === 'warning' ? 'warning' : props.tone === 'error' ? 'error' : 'refresh',
)
const family = computed(
  () =>
    ({ warning: 'message-retrying', info: 'campaign-sending', error: 'campaign-failed' })[
      props.tone
    ],
)
</script>

<template>
  <div
    class="banner"
    :role="tone === 'error' ? 'alert' : 'status'"
    :style="{
      '--banner-tint': `var(--color-${family}-tint)`,
      '--banner-border': `var(--color-${family}-border)`,
      '--banner-ink': `var(--color-${family}-ink)`,
    }"
  >
    <AppIcon :name="icon" class="banner-icon" />
    <div class="banner-body">
      <p class="banner-title">{{ title }}</p>
      <div v-if="$slots.default" class="banner-text"><slot /></div>
    </div>
    <div v-if="$slots.action" class="banner-action"><slot name="action" /></div>
  </div>
</template>

<style scoped>
.banner {
  display: flex;
  align-items: flex-start;
  gap: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border-radius: var(--radius-field);
  border: var(--border-thin) solid var(--banner-border);
  background: var(--banner-tint);
}

.banner-icon {
  margin-top: 2px;
  color: var(--banner-ink);
}

.banner-body {
  flex: 1;
  min-width: 0;
}

.banner-title {
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
  font-weight: 600;
  color: var(--banner-ink);
}

.banner-text {
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
  color: var(--color-text);
}

.banner-action {
  flex: none;
  align-self: center;
}
</style>

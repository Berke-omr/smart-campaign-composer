<script setup lang="ts">
// A message template as text with its variables shown as pills. Invalid
// variable text stays visible as written, so the marketer sees what is wrong.
import { computed } from 'vue'
import type { Channel } from '@/api/types'
import VariableToken from '@/components/ui/VariableToken.vue'
import { parseTemplate } from '@/logic/content'

const props = defineProps<{ text: string; channel: Channel; writing?: boolean }>()

const tokens = computed(() => parseTemplate(props.text))
</script>

<template>
  <p class="message-text">
    <template v-for="(t, i) in tokens" :key="i">
      <VariableToken v-if="t.kind === 'variable'" :variable="t.variable" :channel="channel" />
      <template v-else>{{ t.text }}</template>
    </template>
    <span v-if="writing" class="cursor" aria-hidden="true"></span>
  </p>
</template>

<style scoped>
.message-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.cursor {
  display: inline-block;
  width: 2px;
  height: 1.1em;
  margin-left: 1px;
  vertical-align: text-bottom;
  background: var(--color-campaign-sending);
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}
</style>

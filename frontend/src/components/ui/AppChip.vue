<script setup lang="ts">
// A rounded chip. With `pressed` it is a toggle button (aria-pressed); with
// `action` a plain button; otherwise static text that is not focusable.
// A channel chip shows the channel's dot and, when pressed, its tint.
import { computed } from 'vue'
import type { Channel } from '@/api/types'
import AppIcon, { type IconName } from './AppIcon.vue'

const props = withDefaults(
  defineProps<{
    pressed?: boolean
    action?: boolean
    channel?: Channel
    icon?: IconName
    disabled?: boolean
  }>(),
  { pressed: undefined, action: false, channel: undefined, icon: undefined, disabled: false },
)
defineEmits<{ click: [MouseEvent] }>()

const interactive = computed(() => props.pressed !== undefined || props.action)
const tinted = computed(() => props.channel !== undefined && props.pressed === true)
</script>

<template>
  <button
    v-if="interactive"
    type="button"
    class="chip chip-interactive"
    :class="{ tinted }"
    :aria-pressed="pressed === undefined ? undefined : pressed"
    :disabled="disabled"
    :style="
      channel
        ? {
            '--chip-channel': `var(--color-${channel})`,
            '--chip-tint': `var(--color-${channel}-tint)`,
            '--chip-border': `var(--color-${channel}-border)`,
            '--chip-ink': `var(--color-${channel}-ink)`,
          }
        : undefined
    "
    @click="$emit('click', $event)"
  >
    <span v-if="channel" class="chip-dot" aria-hidden="true"></span>
    <AppIcon v-else-if="icon" :name="icon" :size="18" />
    <slot />
  </button>
  <span v-else class="chip">
    <span
      v-if="channel"
      class="chip-dot"
      aria-hidden="true"
      :style="{ '--chip-channel': `var(--color-${channel})` }"
    ></span>
    <AppIcon v-else-if="icon" :name="icon" :size="18" />
    <slot />
  </span>
</template>

<style scoped>
.chip {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--control-height);
  padding: 0 var(--space-5);
  border-radius: var(--radius-pill);
  border: var(--border-thin) solid var(--color-glass-border);
  background: var(--color-glass);
  color: var(--color-text);
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
  font-weight: var(--font-weight-label);
  white-space: nowrap;
}

.chip-interactive {
  border-color: var(--color-border-control);
  cursor: pointer;
  transition: background-color var(--duration-fast) var(--ease-ui);
}

.chip-interactive:hover:not(:disabled) {
  background: var(--color-glass-hover);
}

.chip-interactive:disabled {
  cursor: not-allowed;
  opacity: var(--opacity-disabled);
}

.chip-interactive.tinted {
  background: var(--chip-tint);
  border-color: var(--chip-ink);
  color: var(--chip-ink);
}

.chip-dot {
  width: 0.75rem;
  height: 0.75rem;
  border-radius: var(--radius-pill);
  background: var(--chip-channel);
}

/* An unpressed channel toggle shows a hollow dot, so the state is not color alone. */
.chip-interactive[aria-pressed='false'] .chip-dot {
  background: transparent;
  border: 2px solid var(--chip-channel);
}
</style>

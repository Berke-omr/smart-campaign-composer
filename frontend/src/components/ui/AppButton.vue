<script setup lang="ts">
// Primary (gradient), secondary (glass) and ghost buttons (DESIGN.md section 5).
// While loading the button keeps its label, shows a spinner and cannot be
// pressed again, so a slow request is never sent twice from the UI.
import { computed } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import AppIcon, { type IconName } from './AppIcon.vue'

const props = withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'ghost'
    type?: 'button' | 'submit'
    disabled?: boolean
    loading?: boolean
    icon?: IconName
    /** Renders a router link instead of a button. */
    to?: RouteLocationRaw
  }>(),
  {
    variant: 'secondary',
    type: 'button',
    disabled: false,
    loading: false,
    icon: undefined,
    to: undefined,
  },
)

const inactive = computed(() => props.disabled || props.loading)
</script>

<template>
  <RouterLink v-if="to !== undefined" :to="to" class="btn" :class="`btn-${variant}`">
    <AppIcon v-if="icon" :name="icon" />
    <span><slot /></span>
  </RouterLink>
  <button
    v-else
    class="btn"
    :class="`btn-${variant}`"
    :type="type"
    :disabled="inactive"
    :aria-busy="loading || undefined"
  >
    <AppIcon v-if="loading" name="spinner" />
    <AppIcon v-else-if="icon" :name="icon" />
    <span><slot /></span>
  </button>
</template>

<style scoped>
.btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  min-height: var(--control-height);
  padding: 0 var(--space-6);
  border-radius: var(--radius-pill);
  border: var(--border-thin) solid transparent;
  font-size: var(--font-size-body);
  font-weight: var(--font-weight-label);
  line-height: var(--line-height-body);
  text-decoration: none;
  white-space: nowrap;
  cursor: pointer;
  transition:
    box-shadow var(--duration-fast) var(--ease-ui),
    background-color var(--duration-fast) var(--ease-ui);
}

.btn:disabled {
  cursor: not-allowed;
  opacity: var(--opacity-disabled);
}

.btn[aria-busy='true'] {
  cursor: progress;
  opacity: 1;
}

/* Primary: the AA gradient keeps white text at 4.5:1; the gloss stays in the top
   quarter, above the text, so it never lightens the background behind the label. */
.btn-primary {
  color: var(--color-white);
  background: linear-gradient(90deg, var(--color-primary-aa-start), var(--color-primary-aa-end));
  box-shadow: var(--shadow-card);
}

.btn-primary::before {
  content: '';
  position: absolute;
  inset: 0 0 75% 0;
  border-radius: var(--radius-pill) var(--radius-pill) 0 0;
  background: linear-gradient(to bottom, rgb(255 255 255 / 22%), transparent);
  pointer-events: none;
}

.btn-primary:not(:disabled):hover {
  box-shadow: var(--shadow-raised);
}

.btn-secondary {
  color: var(--color-text);
  background: var(--color-glass);
  border-color: var(--color-border-control);
}

.btn-secondary:not(:disabled):hover {
  background: var(--color-glass-hover);
}

.btn-ghost {
  color: var(--color-text);
  background: transparent;
  padding: 0 var(--space-3);
}

.btn-ghost:not(:disabled):hover {
  background: var(--color-glass);
}
</style>

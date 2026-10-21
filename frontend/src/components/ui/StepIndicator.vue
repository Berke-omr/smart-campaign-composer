<script setup lang="ts">
// Audience → Messages → Review → Send. Informational, not navigation: done
// steps show a check, the current one is marked with aria-current="step".
import AppIcon from './AppIcon.vue'

export interface Step {
  label: string
  status: 'done' | 'current' | 'upcoming'
}

defineProps<{ steps: Step[] }>()
</script>

<template>
  <ol class="steps" aria-label="Progress">
    <li
      v-for="(step, i) in steps"
      :key="step.label"
      class="step"
      :class="step.status"
      :aria-current="step.status === 'current' ? 'step' : undefined"
    >
      <span class="marker" aria-hidden="true">
        <AppIcon v-if="step.status === 'done'" name="check" :size="16" />
      </span>
      <span class="label">{{ step.label }}</span>
      <span v-if="step.status === 'done'" class="sr-only">(done)</span>
      <span v-if="i < steps.length - 1" class="line" aria-hidden="true"></span>
    </li>
  </ol>
</template>

<style scoped>
.steps {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin: 0;
  padding: 0;
  list-style: none;
}

.step {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex: 1 1 auto;
  min-width: 0;
}

.marker {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  width: 2rem;
  height: 2rem;
  border-radius: var(--radius-pill);
  border: 2px solid var(--color-border-control);
  background: var(--color-glass);
}

.label {
  font-size: var(--font-size-body);
  color: var(--color-text-supporting);
  white-space: nowrap;
}

.line {
  flex: 1;
  min-width: var(--space-6);
  height: 2px;
  border-radius: var(--radius-pill);
  background: var(--color-track);
}

.done .marker {
  border-color: var(--color-sms);
  background: var(--color-sms);
  color: var(--color-white);
}

.done .label {
  color: var(--color-text);
}

.current .marker {
  border-color: var(--color-primary-aa-start);
  background: linear-gradient(135deg, var(--color-primary-start), var(--color-primary-end));
}

/* The coral marker is the accent; the label stays dark enough to read (3.6:1 in coral). */
.current .label {
  color: var(--color-text);
  font-weight: 600;
}
</style>

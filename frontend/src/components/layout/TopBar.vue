<script setup lang="ts">
// Top bar: only the "Demo mode" badge, shown when the scripted mock LLM answers
// (GET /api/config). Search, notifications, settings and profile are out of scope (D22).
import { computed } from 'vue'
import AppIcon from '@/components/ui/AppIcon.vue'
import { useConfig } from '@/composables/useConfig'

const config = useConfig()
const demo = computed(() => config.value?.llm.mode === 'mock')
</script>

<template>
  <div class="topbar">
    <span
      v-if="demo"
      class="demo-pill glass"
      title="Audiences and messages come from scripted examples"
      data-testid="demo-mode"
    >
      <AppIcon name="info" />
      Demo mode
    </span>
  </div>
</template>

<style scoped>
.topbar {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-3);
  min-height: var(--control-height);
}

.demo-pill {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--control-height);
  padding: 0 var(--space-5);
  border-radius: var(--radius-pill);
  font-size: var(--font-size-label);
  font-weight: var(--font-weight-label);
}
</style>

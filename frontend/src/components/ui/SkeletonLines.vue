<script setup lang="ts">
// Faint placeholder lines while content is loading or being written.
// Hidden from screen readers; the surrounding label says what is happening.
withDefaults(defineProps<{ lines?: number }>(), { lines: 2 })
</script>

<template>
  <div class="skeleton" aria-hidden="true">
    <span
      v-for="n in lines"
      :key="n"
      class="line"
      :style="{ width: n === lines ? '55%' : '92%' }"
    ></span>
  </div>
</template>

<style scoped>
.skeleton {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.line {
  display: block;
  height: 0.75rem;
  border-radius: var(--radius-pill);
  background: linear-gradient(
    90deg,
    var(--color-track) 0%,
    var(--color-glass-highlight) 50%,
    var(--color-track) 100%
  );
  background-size: 200% 100%;
  animation: shimmer var(--duration-shimmer) linear infinite;
}

@keyframes shimmer {
  from {
    background-position: 100% 0;
  }
  to {
    background-position: -100% 0;
  }
}
</style>

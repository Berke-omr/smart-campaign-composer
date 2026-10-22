<script setup lang="ts">
// A glass card: heading, optional actions on the right, content, optional footer.
withDefaults(defineProps<{ title?: string; headingLevel?: 2 | 3 }>(), {
  title: undefined,
  headingLevel: 2,
})
</script>

<template>
  <section class="card glass">
    <header v-if="title || $slots.heading || $slots.actions" class="card-header">
      <slot name="heading">
        <component :is="`h${headingLevel}`" class="card-title">{{ title }}</component>
      </slot>
      <div v-if="$slots.actions" class="card-actions"><slot name="actions" /></div>
    </header>
    <slot />
    <footer v-if="$slots.footer" class="card-footer"><slot name="footer" /></footer>
  </section>
</template>

<style scoped>
.card {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6);
  min-width: 0;
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.card-title {
  font-size: var(--font-size-h2);
  line-height: var(--line-height-h2);
}

.card-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}

.card-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-3);
}
</style>

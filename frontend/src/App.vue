<script setup lang="ts">
import { RouterView } from 'vue-router'
</script>

<template>
  <header class="app-header">
    <h1>Smart Campaign Composer</h1>
  </header>
  <main class="app-main">
    <RouterView />
  </main>
</template>

<style scoped>
.app-header {
  padding: 1rem 1.5rem;
  border-bottom: 1px solid var(--border);
  background: var(--surface);
}

.app-header h1 {
  margin: 0;
  font-size: 1.15rem;
}

.app-main {
  max-width: 64rem;
  margin: 0 auto;
  padding: 1.5rem 1rem;
}
</style>

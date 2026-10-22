<script setup lang="ts">
import { RouterView } from 'vue-router'
import AppSidebar from '@/components/layout/AppSidebar.vue'
import TopBar from '@/components/layout/TopBar.vue'
</script>

<template>
  <div class="backdrop" aria-hidden="true">
    <span class="blob blob-aqua"></span>
    <span class="blob blob-sky"></span>
    <span class="blob blob-peach"></span>
  </div>
  <div class="shell">
    <AppSidebar />
    <div class="main-column">
      <TopBar />
      <main class="main">
        <!-- Keyed by path: /campaigns/20 → /campaigns/21 must load the other campaign,
             not keep the first one's state in a reused component. -->
        <RouterView :key="$route.path" />
      </main>
    </div>
  </div>
</template>

<style scoped>
/* Atmospheric glows behind everything; never interactive. */
.backdrop {
  position: fixed;
  inset: 0;
  z-index: -1;
  overflow: hidden;
  pointer-events: none;
}

.blob {
  position: absolute;
  border-radius: 50%;
  filter: blur(var(--blur-blob));
  opacity: 0.7;
}

.blob-aqua {
  top: -20vh;
  left: -10vw;
  width: 55vw;
  height: 55vh;
  background: var(--color-blob-aqua);
}

.blob-sky {
  top: 20vh;
  right: -15vw;
  width: 50vw;
  height: 60vh;
  background: var(--color-blob-sky);
}

.blob-peach {
  bottom: -25vh;
  left: 25vw;
  width: 50vw;
  height: 45vh;
  background: var(--color-blob-peach);
}

.shell {
  display: grid;
  grid-template-columns: var(--sidebar-width) minmax(0, 1fr);
  gap: var(--layout-gap);
  padding: var(--shell-inset);
  min-height: 100vh;
}

.main-column {
  display: flex;
  flex-direction: column;
  gap: var(--space-6);
  min-width: 0;
}

.main {
  min-width: 0;
}

@media (width < 1200px) {
  .shell {
    grid-template-columns: var(--sidebar-compact-width) minmax(0, 1fr);
  }
}
</style>

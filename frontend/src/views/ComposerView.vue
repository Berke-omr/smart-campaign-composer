<script setup lang="ts">
// Home: the marketer describes a campaign (01 — Composer).
</script>

<template>
  <section class="composer">
    <header class="hero">
      <h1 class="hero-title">New campaign</h1>
      <p class="hero-lead secondary-text">
        Describe your campaign. We'll find the right audience and draft your SMS &amp; email —
        nothing is sent until you approve.
      </p>
    </header>
  </section>
</template>

<style scoped>
.composer {
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
  max-width: 72rem;
  margin: 0 auto;
}

.hero {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  text-align: center;
  padding-top: var(--space-8);
}

.hero-title {
  font-size: var(--font-size-hero);
  line-height: var(--line-height-hero);
  font-weight: var(--font-weight-hero);
}

.hero-lead {
  max-width: 48rem;
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
}

@media (width < 1200px) {
  .hero-title {
    font-size: var(--font-size-h1);
    line-height: var(--line-height-h1);
  }
}
</style>

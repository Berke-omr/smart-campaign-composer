<script setup lang="ts">
// Navigation. Only destinations that exist are listed; Audiences, Templates,
// search, notifications, settings and profile are out of scope (D22).
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppIcon from '@/components/ui/AppIcon.vue'

const route = useRoute()

// "New campaign" stays active while a campaign is being drafted or reviewed;
// "Live sends" while a dashboard is open.
const composing = computed(() => route.name === 'composer' || route.name === 'campaign')
const listing = computed(() => route.name === 'campaigns')
const watching = computed(() => route.name === 'campaign-live' || route.name === 'live-sends')
</script>

<template>
  <aside class="sidebar glass">
    <RouterLink to="/" class="brand" aria-label="Composer, new campaign">
      <svg class="brand-mark" viewBox="0 0 32 32" width="32" height="32" aria-hidden="true">
        <defs>
          <linearGradient id="brand-gradient" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stop-color="var(--color-primary-start)" />
            <stop offset="1" stop-color="var(--color-primary-end)" />
          </linearGradient>
        </defs>
        <path
          d="M16 2c1 7.5 6.5 13 14 14-7.5 1-13 6.5-14 14-1-7.5-6.5-13-14-14 7.5-1 13-6.5 14-14z"
          fill="url(#brand-gradient)"
        />
      </svg>
      <span class="brand-name">Composer</span>
    </RouterLink>

    <nav aria-label="Main">
      <p class="nav-heading caption">Create</p>
      <RouterLink
        to="/"
        class="nav-item"
        :class="{ active: composing }"
        :aria-current="composing ? 'page' : undefined"
      >
        <AppIcon name="plus" />
        New campaign
      </RouterLink>

      <p class="nav-heading caption">Main</p>
      <RouterLink
        to="/campaigns"
        class="nav-item"
        :class="{ active: listing }"
        :aria-current="listing ? 'page' : undefined"
      >
        <AppIcon name="chart" />
        Campaigns
      </RouterLink>
      <RouterLink
        to="/live"
        class="nav-item"
        :class="{ active: watching }"
        :aria-current="watching ? 'page' : undefined"
      >
        <AppIcon name="send" />
        Live sends
      </RouterLink>
    </nav>
  </aside>
</template>

<style scoped>
.sidebar {
  position: sticky;
  top: var(--shell-inset);
  display: flex;
  flex-direction: column;
  gap: var(--space-8);
  height: calc(100vh - 2 * var(--shell-inset));
  padding: var(--space-6) var(--space-4);
  overflow-y: auto;
}

.brand {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: 0 var(--space-2);
  text-decoration: none;
  border-radius: var(--radius-field);
}

.brand-name {
  font-family: var(--font-heading);
  font-size: var(--font-size-h2);
  line-height: var(--line-height-h2);
  font-weight: 700;
}

nav {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.nav-heading {
  padding: 0 var(--space-2);
  color: var(--color-text-secondary);
}

.nav-heading:not(:first-child) {
  margin-top: var(--space-6);
}

.nav-item {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: var(--control-height);
  padding: 0 var(--space-4);
  border-radius: var(--radius-field);
  text-decoration: none;
  color: var(--color-text);
  transition: background-color var(--duration-fast) var(--ease-ui);
}

.nav-item:hover {
  background: var(--color-glass-hover);
}

.nav-item.active {
  background: var(--color-navy);
  color: var(--color-white);
}
</style>

<script setup lang="ts">
// The latest provider calls that did not end in a sent message. The backend
// classifies them (kind, outcome); this list words them and dates them
// against the snapshot's own time (D25 K4). The code stays secondary.
import type { Issue } from '@/api/types'
import AppIcon from '@/components/ui/AppIcon.vue'
import { CHANNEL_LABELS } from '@/logic/labels'
import { issueAge, issueText } from '@/logic/snapshot'

defineProps<{ issues: Issue[]; at: string; showChannel: boolean }>()
</script>

<template>
  <section class="issues glass" aria-labelledby="issues-title">
    <h2 id="issues-title" class="title">Recent issues</h2>
    <p v-if="issues.length === 0" class="secondary-text">No issues so far.</p>
    <ul v-else class="list">
      <li
        v-for="(issue, i) in issues"
        :key="`${issue.at}-${i}`"
        class="row"
        :class="issue.outcome"
        :title="`Attempt ${issue.attempt}: ${issue.error}`"
        data-testid="issue"
      >
        <AppIcon :name="issue.outcome === 'failed' ? 'error' : 'warning'" class="icon" />
        <div class="body">
          <p class="text">{{ issueText(issue) }}</p>
          <p class="caption secondary-text">
            <template v-if="showChannel">{{ CHANNEL_LABELS[issue.channel] }} · </template>
            {{ issue.code ?? 'no response' }}
          </p>
        </div>
        <span class="age caption secondary-text">{{ issueAge(issue.at, at) }}</span>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.issues {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  padding: var(--space-6);
  min-width: 0;
}

.title {
  font-size: var(--font-size-h3);
  line-height: var(--line-height-h3);
}

.list {
  display: flex;
  flex-direction: column;
  margin: 0;
  padding: 0;
  list-style: none;
}

.row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: var(--space-3);
  padding: var(--space-3) 0;
  border-bottom: var(--border-thin) solid var(--color-track);
}

.row:last-child {
  border-bottom: 0;
}

.icon {
  margin-top: 2px;
}

.retry .icon {
  color: var(--color-message-retrying-ink);
}

.failed .icon {
  color: var(--color-campaign-failed-ink);
}

.text {
  font-size: var(--font-size-label);
  line-height: var(--line-height-label);
  font-weight: var(--font-weight-label);
}

.age {
  white-space: nowrap;
}
</style>

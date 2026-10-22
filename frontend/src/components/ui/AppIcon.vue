<script setup lang="ts">
// Outline icons drawn inline, so there is no icon package. Paths are from
// Lucide (https://lucide.dev), ISC License:
//   Copyright (c) for portions of Lucide are held by Cole Bemis 2013-2022 as part
//   of Feather (MIT). All other copyright (c) for Lucide are held by Lucide
//   Contributors 2022.
//   Permission to use, copy, modify, and/or distribute this software for any
//   purpose with or without fee is hereby granted, provided that the above
//   copyright notice and this permission notice appear in all copies.
//   THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES.
// Icons are decorative (aria-hidden); the control next to them carries the name.

export type IconName =
  | 'plus'
  | 'chart'
  | 'send'
  | 'info'
  | 'arrow-left'
  | 'sliders'
  | 'folder'
  | 'sparkle'
  | 'check'
  | 'warning'
  | 'error'
  | 'spinner'
  | 'close'
  | 'refresh'

defineProps<{ name: IconName; size?: number }>()

const PATHS: Record<IconName, string[]> = {
  plus: ['M5 12h14', 'M12 5v14'],
  chart: ['M3 3v16a2 2 0 0 0 2 2h16', 'M18 17V9', 'M13 17V5', 'M8 17v-3'],
  send: [
    'M14.536 21.686a.5.5 0 0 0 .937-.024l6.5-19a.496.496 0 0 0-.635-.635l-19 6.5a.5.5 0 0 0-.024.937l7.93 3.18a2 2 0 0 1 1.112 1.11z',
    'm21.854 2.147-10.94 10.939',
  ],
  info: ['M12 2a10 10 0 1 0 0 20a10 10 0 1 0 0-20', 'M12 16v-4', 'M12 8h.01'],
  'arrow-left': ['m12 19-7-7 7-7', 'M19 12H5'],
  sliders: [
    'M21 4h-7',
    'M10 4H3',
    'M21 12h-9',
    'M8 12H3',
    'M21 20h-5',
    'M12 20H3',
    'M14 2v4',
    'M8 10v4',
    'M16 18v4',
  ],
  folder: [
    'M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z',
  ],
  sparkle: [
    'M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z',
  ],
  check: ['M20 6 9 17l-5-5'],
  warning: [
    'm21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3',
    'M12 9v4',
    'M12 17h.01',
  ],
  error: ['M12 2a10 10 0 1 0 0 20a10 10 0 1 0 0-20', 'm15 9-6 6', 'm9 9 6 6'],
  spinner: ['M21 12a9 9 0 1 1-6.219-8.56'],
  close: ['M18 6 6 18', 'm6 6 12 12'],
  refresh: [
    'M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8',
    'M21 3v5h-5',
    'M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16',
    'M8 16H3v5',
  ],
}
</script>

<template>
  <svg
    class="icon"
    :class="{ 'icon-spin': name === 'spinner' }"
    :width="size ?? 20"
    :height="size ?? 20"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
  >
    <path v-for="d in PATHS[name]" :key="d" :d="d" />
  </svg>
</template>

<style scoped>
.icon {
  flex: none;
}

.icon-spin {
  animation: icon-spin 1s linear infinite;
}

@keyframes icon-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>

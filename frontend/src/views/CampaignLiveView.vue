<script setup lang="ts">
// Live dashboard of a sending or finished campaign (08 — Live). Filled in later.
defineProps<{ id: number }>()
</script>

<template>
  <section>
    <h1>Campaign {{ id }}</h1>
  </section>
</template>

<script setup lang="ts">
// Modal dialog on the native <dialog> element: showModal() makes the rest of
// the page inert (focus stays inside) and Escape fires "cancel". The parent
// owns `open`; Escape or the close button ask it to close. Focus goes back to
// the element that opened the dialog. While `busy`, it cannot be dismissed,
// so a request in flight is never abandoned halfway.
import { nextTick, onMounted, ref, useId, watch } from 'vue'
import AppIcon from './AppIcon.vue'

const props = withDefaults(defineProps<{ open: boolean; title: string; busy?: boolean }>(), {
  busy: false,
})
const emit = defineEmits<{ close: [] }>()

const el = ref<HTMLDialogElement | null>(null)
const titleId = useId()
let opener: HTMLElement | null = null

function sync(open: boolean) {
  const d = el.value
  if (!d) return
  if (open && !d.open) {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    d.showModal()
  } else if (!open && d.open) {
    d.close()
    void nextTick(() => opener?.focus())
  }
}

onMounted(() => sync(props.open))
watch(
  () => props.open,
  (open) => sync(open),
  { flush: 'post' },
)

function requestClose() {
  if (!props.busy) emit('close')
}

function onCancel(e: Event) {
  e.preventDefault() // the parent decides; it may be busy
  requestClose()
}
</script>

<template>
  <dialog ref="el" class="dialog glass" :aria-labelledby="titleId" @cancel="onCancel">
    <header class="dialog-header">
      <h2 :id="titleId" class="dialog-title">{{ title }}</h2>
      <button
        type="button"
        class="dialog-close"
        aria-label="Close"
        :disabled="busy"
        @click="requestClose"
      >
        <AppIcon name="close" />
      </button>
    </header>
    <div class="dialog-body"><slot /></div>
    <footer v-if="$slots.footer" class="dialog-footer"><slot name="footer" /></footer>
  </dialog>
</template>

<style scoped>
.dialog {
  width: min(40rem, calc(100vw - 2 * var(--space-6)));
  max-height: calc(100vh - 2 * var(--space-8));
  padding: var(--space-6);
  color: var(--color-text);
  background: var(--color-glass-hover);
  box-shadow: var(--shadow-raised), var(--shadow-highlight);
  overflow: auto;
}

.dialog::backdrop {
  background: var(--color-overlay);
}

.dialog[open] {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--space-4);
}

.dialog-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--icon-button-size);
  height: var(--icon-button-size);
  margin: calc(-1 * var(--space-2));
  border: 0;
  border-radius: var(--radius-pill);
  background: transparent;
  cursor: pointer;
}

.dialog-close:hover:not(:disabled) {
  background: var(--color-glass);
}

.dialog-close:disabled {
  opacity: var(--opacity-disabled);
  cursor: not-allowed;
}

.dialog-body {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.dialog-footer {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-3);
  flex-wrap: wrap;
}
</style>

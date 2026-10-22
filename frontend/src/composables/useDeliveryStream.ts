// The live dashboard's data: the snapshot stream of one campaign
// (GET /api/campaigns/{id}/events, docs/API.md). Each snapshot is the whole
// state, so it replaces the previous one; while the connection is lost the
// last snapshot stays on screen and the next one replaces it completely.
import { onBeforeUnmount, ref } from 'vue'
import { eventsURL } from '@/api/campaigns'
import type { DeliveryDoneEvent, Snapshot } from '@/api/types'
import { openStream, type ConnectionState, type StreamHandle } from '@/logic/stream'

/** Snapshots come every second; this much silence means the connection is gone. */
export const STALL_MS = 5000

export function useDeliveryStream(id: number, options: { check: () => Promise<'retry' | 'stop'> }) {
  const snapshot = ref<Snapshot | null>(null)
  const connection = ref<ConnectionState>('connecting')
  /** The api could not read this second's snapshot (delivery.error); the shown one is older. */
  const stale = ref(false)
  const done = ref<DeliveryDoneEvent | null>(null)
  let handle: StreamHandle | null = null

  function start() {
    handle?.close()
    handle = openStream({
      url: eventsURL(id),
      handlers: {
        'delivery.snapshot': (d) => {
          snapshot.value = d as Snapshot
          stale.value = false
        },
        'delivery.error': () => {
          stale.value = true
        },
        'delivery.done': (d) => {
          done.value = d as DeliveryDoneEvent
        },
      },
      doneEvent: 'delivery.done',
      onState: (s) => (connection.value = s),
      check: options.check,
      stallMs: STALL_MS,
    })
  }

  function stop() {
    handle?.close()
    handle = null
  }

  onBeforeUnmount(stop)

  return { snapshot, connection, stale, done, start, stop, retryNow: () => handle?.retryNow() }
}

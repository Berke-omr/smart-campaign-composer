// Runs the draft stream of one campaign (GET /api/campaigns/{id}/draft/stream)
// and keeps the message cards' state. Stored channels are shown at once; the
// stream is opened only while some selected channel has no message yet.
import { onBeforeUnmount, ref } from 'vue'
import { draftStreamURL } from '@/api/campaigns'
import type {
  CampaignView,
  DraftDeltaEvent,
  DraftDoneEvent,
  DraftErrorEvent,
  DraftStateEvent,
} from '@/api/types'
import {
  applyDraftEvent,
  initialProgress,
  needsDrafting,
  type DraftEvent,
  type DraftProgress,
} from '@/logic/draft'
import { openStream, type ConnectionState, type StreamHandle } from '@/logic/stream'

export interface DraftStreamOptions {
  /** Called once every channel is final (draft.done). */
  onDone: (done: DraftDoneEvent) => void
  /** Called when the browser gave up on the stream (e.g. a 502); see openStream. */
  check: () => Promise<'retry' | 'stop'>
}

export function useDraftStream(id: number, options: DraftStreamOptions) {
  const progress = ref<DraftProgress>({ channels: {}, errors: [], done: null })
  const connection = ref<ConnectionState>('closed')
  let handle: StreamHandle | null = null

  function apply(ev: DraftEvent) {
    progress.value = applyDraftEvent(progress.value, ev)
  }

  /** Shows the campaign's stored messages and, if some are missing, starts writing them. */
  function start(view: CampaignView) {
    handle?.close()
    handle = null
    progress.value = { ...initialProgress(view), errors: progress.value.errors }
    if (!needsDrafting(view) || (view.status !== 'draft' && view.status !== 'ready')) {
      connection.value = 'closed'
      return
    }
    handle = openStream({
      url: draftStreamURL(id),
      handlers: {
        'draft.state': (d) => apply({ type: 'draft.state', data: d as DraftStateEvent }),
        'draft.delta': (d) => apply({ type: 'draft.delta', data: d as DraftDeltaEvent }),
        'draft.error': (d) => apply({ type: 'draft.error', data: d as DraftErrorEvent }),
        'draft.done': (d) => {
          apply({ type: 'draft.done', data: d as DraftDoneEvent })
          options.onDone(d as DraftDoneEvent)
        },
      },
      doneEvent: 'draft.done',
      onState: (s) => (connection.value = s),
      check: options.check,
    })
  }

  function stop() {
    handle?.close()
    handle = null
  }

  onBeforeUnmount(stop)

  return {
    progress,
    connection,
    start,
    stop,
    retryNow: () => handle?.retryNow(),
  }
}

# API contract

> REST and SSE contract of the api service, as built in P3, P4 and P5. It is the input for the frontend (P6).
> Rationale: [`DECISIONS.md`](../DECISIONS.md). Examples below are real responses from the mock LLM.

## Conventions

- Base URL: `http://localhost:8080/api` through the frontend's nginx (the api itself listens on `:8081`).
  nginx does not buffer `/api/`, so server-sent events arrive as they are written.
- Request and response bodies are JSON (UTF-8). Request bodies are decoded strictly: an unknown key is a `422`.
- Lengths are Unicode characters (runes), never bytes.
- Errors:

  | Status | Body | When |
  |---|---|---|
  | `404` | `{"error": "campaign not found"}` | unknown id or channel |
  | `409` | `{"error": "..."}` | content is locked, drafts are already being written, or the campaign is not ready to approve |
  | `413` | `{"error": "request body is too large"}` | body over 64 KB |
  | `422` | `{"error": "...", "details": ["...", ...]}` | invalid input; `details` lists every problem |
  | `422` | `{"error": "invalid content", "report": {...}}` | an edited message failed validation (see [Report](#report)) |
  | `422` | `{"error": "...", "channel": "sms", "report": {...}}` | at approval, a message no longer fits the audience |
  | `500` | `{"error": "internal error"}` | details are logged, never returned |

## LLM mode

### `GET /api/config`

Which LLM answers, for the UI's "Demo mode" badge (D12).

```json
{"llm": {"mode": "mock", "model": null}}
{"llm": {"mode": "anthropic", "model": "claude-sonnet-5-5"}}
```

Chosen at startup by `LLM_PROVIDER` (`auto` = Claude when `ANTHROPIC_API_KEY` is set, mock otherwise).

## Brief library

### `GET /api/briefs/examples`

Ready-made briefs. With the mock LLM each one runs a scripted, deterministic scenario; with Claude they are ordinary briefs.

```json
{
  "examples": [
    {
      "id": "cart-reminder-istanbul",
      "title": "Cart reminder — İstanbul",
      "brief": "Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.",
      "channels": ["sms", "email"],
      "tone": "friendly"
    }
  ]
}
```

Five examples: cart reminder (İstanbul), win-back 90 days, new season (SMS only, with an unsupported criterion), weekend sale (email only, all subscribers), VIP thanks in İzmir (the SMS is repaired on attempt 2).

## Audience

### `GET /api/segments/fields`

The targetable fields, their operators and value rules, and the DSL limits (for a rule editor). Unchanged since P2.

### `POST /api/segments/preview`

Body: a segment rule (`{"match": "all"|"any", "conditions": [...]}`, D9). Response:

```json
{
  "audience": {
    "total": 177, "sms_reachable": 99, "email_reachable": 133,
    "sms_excluded": 78, "email_excluded": 44, "channel_messages": 232
  },
  "description": {
    "summary": "Users matching all of the following",
    "lines": ["City is İstanbul", "Added to cart in the last 30 days", "No purchase in the last 30 days"]
  },
  "as_of": "2026-10-19T12:00:00Z"
}
```

- `*_excluded`: people in the audience without consent (or contact data) for that channel. They are never counted as reachable (R4).
- `channel_messages`: messages over both channels; one person reachable on both gets two. On a campaign it counts only the selected channels.
- `422` with `details` (structured codes, see P2) for an invalid rule.

## Campaigns

### Statuses

| Code | UI label | Meaning |
|---|---|---|
| `draft` | Draft | being drafted, or a selected channel has no valid message |
| `ready` | Ready | every selected channel has a valid message; can be approved (P4) |
| `sending` | Sending | approved; messages are going out |
| `completed` | Completed | every message is final and at least one was sent; some may have failed |
| `failed` | Failed | every message is final and none could be delivered |

`draft` and `ready` move automatically as messages are stored. `POST …/approve` moves `ready` to `sending`; the
worker moves `sending` to `completed` or `failed` once no message is queued, sending or retrying. One failed message
never makes a campaign Failed. From `sending` on, content is locked (`409`).

### `POST /api/campaigns`

Turns a brief into a draft: the model proposes the audience rule and a short campaign name, the backend validates the rule (up to 3 attempts, D10) and stores the draft. Messages are written by the stream below.

Request:

```json
{"brief": "Son 30 günde ...", "channels": ["sms", "email"], "tone": "friendly"}
```

- `brief`: 1–2,000 characters.
- `channels`: non-empty subset of `sms`, `email`; duplicates are dropped and the order is normalized to `sms`, `email`.
- `tone`: `friendly` (default), `professional`, `playful`, `urgent`.

`201` with the [campaign view](#campaign-view). `422` when the input is invalid, or when no valid rule came out of 3 attempts:

```json
{"error": "could not build an audience from this brief (3 attempts)", "details": ["condition 1: unknown field \"gender\"; allowed fields: city, language, ..."]}
```

### `GET /api/campaigns`

The 50 most recent campaigns:

```json
{"campaigns": [{"id": 3, "name": "Cart reminder — İstanbul", "status": "ready", "channels": ["sms", "email"],
  "audience": {"total": 175, "sms_reachable": 98, "email_reachable": 132, "sms_excluded": 77, "email_excluded": 43, "channel_messages": 230},
  "created_at": "2026-10-19T17:32:23.272213Z"}]}
```

### `GET /api/campaigns/{id}`

The [campaign view](#campaign-view).

### Campaign view

```json
{
  "id": 3,
  "name": "Cart reminder — İstanbul",
  "status": "draft",
  "brief": "Son 30 günde ...",
  "tone": "friendly",
  "channels": ["sms", "email"],
  "audience": {
    "total": 175, "sms_reachable": 98, "email_reachable": 132,
    "sms_excluded": 77, "email_excluded": 43, "channel_messages": 230,
    "description": {"summary": "Users matching all of the following",
                    "lines": ["City is İstanbul", "Added to cart in the last 30 days", "No purchase in the last 30 days"]}
  },
  "rule": {"match": "all", "conditions": [{"field": "city", "op": "eq", "value": "İstanbul"}, "..."]},
  "unsupported_criteria": [],
  "as_of": "2026-10-19T17:32:22.85917Z",
  "llm": {"mode": "mock", "model": null},
  "messages": {"sms": null, "email": null},
  "approved_at": null,
  "finished_at": null,
  "created_at": "...", "updated_at": "..."
}
```

- `unsupported_criteria`: parts of the brief no field can express (e.g. `["kadın kullanıcılar"]`). Show them as a warning; they were **not** applied.
- `as_of`: anchor of "in the last N days". Counts and longest values stay the same for the life of the draft.
- `messages.<channel>`: `null` until drafted, then a [message view](#message-view).
- `approved_at`: when sending started (the dashboard's "Started 14:32:05"); `null` before approval. `finished_at`: when the
  campaign became `completed` or `failed`.
- The confirm-send modal needs nothing else: title, condition lines, people, reachable and excluded per channel,
  `channel_messages` (selected channels only) and each channel's message with its preview are all in this view.

### Message view

```json
{
  "state": "valid",
  "source": "generated",
  "attempts": 1,
  "message": {"text": "Merhaba {{first_name}}, sepetindeki ürünler seni bekliyor! Bugüne özel %30 indirimle alışverişini tamamla."},
  "report": { "...": "see Report" },
  "problems": [],
  "updated_at": "..."
}
```

- `state`: `valid` or `needs_manual_edit`. `source`: `generated` or `edited`.
- `message`: `{text}` for SMS, `{subject, body}` for email; templates with `{{variables}}`. `null` if the model never produced one (e.g. it declined twice); then `report` is `null` too.
- `problems`: why it needs a manual edit (empty when valid).

### Report

Validation of one message, recomputed whenever it is read (D15):

```json
{
  "valid": true,
  "errors": [],
  "lengths": [{"field": "text", "template_length": 106, "worst_case_length": 105, "limit": 160}],
  "longest_values": {"first_name": "Muhammed Emin", "last_name": "Montgomery-Fitzgerald", "city": "İstanbul"},
  "preview": {
    "sample_user": {"first_name": "Ayşe", "last_name": "Yılmaz", "city": "İstanbul"},
    "sample": {"text": "Merhaba Ayşe, sepetindeki ürünler seni bekliyor! ..."},
    "worst_case": {"text": "Merhaba Muhammed Emin, sepetindeki ürünler seni bekliyor! ..."}
  }
}
```

- The limit is checked against `worst_case_length`: every variable replaced by the longest value among the people **this channel** reaches (`longest_values`). A template that fits them fits every recipient. Show this number in the counter so the UI and the validator agree.
- `limit` is `0` for the email body (no limit).
- `sample_user` is fictional; `worst_case` is the longest message anyone can receive.
- `errors[]`: `{field, code, message}` with `code` one of `empty`, `too_long`, `unknown_variable`, `malformed_variable`, `multiline` (a subject must be one line), `emoji` (no emoji or other pictographic symbols in any field, e.g. `text contains emoji ("😊"); write it without emoji or other pictographic symbols`). Messages are actionable, e.g. `text is 163 characters with the longest values in this audience filled in ({{first_name}} = "Muhammed Emin", 13 characters); the limit is 160. Shorten it by at least 3 characters.`

Content rules: SMS ≤ 160, email subject ≤ 80, body unlimited; variables `{{first_name}}`, `{{last_name}}`, `{{city}}` only, written exactly like that; no emoji in any field.

### `GET /api/campaigns/{id}/draft/stream` (SSE)

Writes the message of every selected channel that has none yet, and streams the progress. Use `EventSource`.

- Channels already stored are replayed first as one final `draft.state` event each, **without calling the model**. Reopening a finished draft costs nothing.
- Channels are written in parallel. Each finished channel is stored **before** its final event is sent.
- If the client disconnects, writing stops and nothing unfinished is stored; opening the stream again starts the missing channels over.
- The same happens when the api shuts down: the stream ends without `draft.done`, and the browser reconnects (P5).
- One stream per campaign at a time: a second one gets `409` (as JSON, before any event). `404` for an unknown campaign; `409` once content is locked.

Response headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`. Each event:

```
event: draft.state
data: {"channel":"sms","attempt":1,"state":"writing","max_attempts":3}

```

| Event | Data | Meaning |
|---|---|---|
| `draft.state` | `{channel, attempt, state, max_attempts, problems?, result?}` | a channel's state changed |
| `draft.delta` | `{channel, attempt, field, value}` | the current text of one field (`text`, `subject` or `body`) |
| `draft.error` | `{channel?, message}` | something failed outside the model (e.g. saving); the stream may continue |
| `draft.done` | `{campaign_id, status}` | every channel is final; `status` is the campaign's new status |

States per channel (D22), in order:

| `state` | Card label | Notes |
|---|---|---|
| `writing` | Writing… | attempt 1; `draft.delta` events follow |
| `validating` | Validating… | the answer is complete and being checked |
| `fixing` | Fixing… · attempt *n* of 3 | a repair attempt; `problems` says what is being fixed; new deltas replace the old text |
| `valid` | Valid | final; `result` is the [channel result](#channel-result) |
| `needs_manual_edit` | Needs manual edit | final; `result.problems` explains why |

Real sequences (from the tests):

```
sms    writing1 validating1 valid1                                   (cart reminder)
sms    writing1 validating1 fixing2 validating2 valid2               (broken JSON, then repaired)
sms    writing1 validating1 fixing2 validating2 fixing3 validating3 needs_manual_edit3   (200-character SMS)
sms    writing1 fixing2 needs_manual_edit2                           (declined twice: a refusal is retried once)
email  writing1 fixing2 fixing3 needs_manual_edit3                   (every attempt timed out)
```

`draft.delta.value` is the field's whole text so far, not an increment: replace, don't append. Within an attempt it only grows. The email subject is complete before the body starts.

#### Channel result

```json
{"channel": "sms", "state": "valid", "attempts": 1,
 "message": {"text": "Merhaba {{first_name}}, ..."},
 "report": { "...": "see Report" },
 "problems": [],
 "usage": {"input_tokens": 256, "output_tokens": 74, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 626}}
```

`usage` is zero with the mock.

### `PUT /api/campaigns/{id}/messages/{channel}`

Replaces a channel's message with the marketer's version. It goes through the same validator as the model's drafts (D10).

```json
{"text": "Merhaba {{first_name}}, sepetin seni bekliyor!"}
{"subject": "{{first_name}}, sepetin seni bekliyor", "body": "Merhaba {{first_name}},\n\n..."}
```

- `200`: `{"message": <message view>, "status": "ready"}` (source `edited`).
- `422` `{"error": "invalid content", "report": {...}}`: the edit is rejected and the stored message is left unchanged.
- `422` when SMS fields are sent for email or the other way round; `409` once the campaign has left `draft`/`ready`.

### `POST /api/campaigns/{id}/approve`

Approves a `ready` campaign and starts sending. No request body. In one transaction the backend
(DECISIONS.md D24):

1. locks the campaign, so approvals and edits of one campaign run one after the other;
2. reads everyone each selected channel reaches (consent **and** contact data) at the campaign's `as_of`, the same anchor
   as the counts the marketer confirmed;
3. validates each message again against the longest names of exactly these people;
4. renders the message for each person (variables filled in) and stores one delivery per person and channel;
5. moves the campaign to `sending`.

`200`:

```json
{"campaign_id": 3, "status": "sending", "approved_at": "2026-10-19T20:48:20.140645Z",
 "deliveries": {"sms": 98, "email": 131, "total": 229}}
```

- **Idempotent:** approving again (a double click, a retried request, ten at once) returns the same body and writes
  nothing. Later it reports the current status (`completed` / `failed`) with the same counts.
- `409` `{"error": "campaign is not ready to send: every selected channel needs a valid message"}`: the campaign is a draft.
- `422` `{"error": "no one in this audience can be reached on the selected channels"}`: nothing would be sent. The campaign
  stays `ready`.
- `422` `{"error": "the sms message no longer fits this audience; edit it and approve again", "channel": "sms",
  "report": {...}}`: someone whose name makes the message too long joined the audience after it was validated. Nothing
  was written; the campaign stays `ready`. The [report](#report) has the new longest values.
- `404` for an unknown campaign. Afterwards the content is locked: edits and draft streams answer `409`.

### Message states

Each person–channel pair is one delivery. The worker sends at most 100 per second per channel and retries per
DECISIONS.md D6.

| Code | UI label | Meaning |
|---|---|---|
| `queued` | Queued | waiting for its first attempt |
| `sending` | Sending | claimed by the worker: being sent, or about to be (within a fraction of a second) |
| `sent` | Sent | the provider accepted it |
| `retrying` | Retrying | the last attempt failed in a way that may pass (5xx, timeout, network, 429); a new one is scheduled |
| `failed` | Failed | the provider rejected it (other 4xx), or its attempts ran out (5 errors, or 10 answers of 429) |

Every provider call is logged in `delivery_attempts` (attempt number, `started_at`, `finished_at`, HTTP status or none
for a timeout, outcome `sent` / `retry` / `failed`, error, the provider's `Retry-After`, and when the retry is due). The
[live dashboard](#live-dashboard)'s "Recent issues" and sends-per-second chart read it.

## Live dashboard

### `GET /api/campaigns/{id}/events` (SSE)

The live state of a sending campaign (DECISIONS.md D7, D25). Use `EventSource`.

- The first event is a full [snapshot](#snapshot), sent at once; then one every second. Every snapshot carries the
  whole state, never a change, so a client that reconnects needs nothing but the next one (no `Last-Event-ID`).
- When the campaign is `completed` or `failed`: its final snapshot, then `delivery.done`, then the stream closes.
  Opening the stream of a finished campaign gives those two events at once. **Call `es.close()` on `delivery.done`**:
  `EventSource` reopens any stream that closes.
- `404` for an unknown campaign and `409` for a campaign that is not approved yet (`draft` or `ready`), as JSON before
  any event. `EventSource` does not reconnect after an answer that is not `200`.
- The stream begins with `retry: 3000` (the browser waits 3 s before reconnecting) and has the same headers as the
  draft stream (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`).
- The server stops reading as soon as the client leaves. When the api shuts down, open streams end without
  `delivery.done`.

| Event | Data | Meaning |
|---|---|---|
| `delivery.snapshot` | a [snapshot](#snapshot) | the campaign's whole live state |
| `delivery.error` | `{message}` | this second's snapshot could not be read (e.g. the database is restarting); the stream stays open and tries again next second. Keep showing the last snapshot |
| `delivery.done` | `{campaign_id, status}` | the campaign is finished; the stream closes |

**Reconnecting (for the "Reconnecting…" state, DESIGN.md).** While the api restarts, nginx answers `502`, and
`EventSource` then gives up for good (`readyState` becomes `CLOSED`) instead of retrying. The client has to reopen the
stream itself, with a backoff, and keep the last snapshot on screen meanwhile; the first snapshot after the reconnect
replaces it completely. Only `delivery.done` means the stream is over.

### Snapshot

A real one, from the mock provider's defaults (an SMS-only campaign about 10 s into sending; `recent_issues` cut to three):

```json
{
  "campaign_id": 13, "status": "sending",
  "approved_at": "2026-10-21T10:16:15.197516Z", "finished_at": null,
  "at": "2026-10-21T10:16:25.255557959Z",
  "total": 1226, "done": 860,
  "window": {"from": "2026-10-21T10:15:54Z", "to": "2026-10-21T10:16:24Z"},
  "channels": {
    "sms": {
      "total": 1226, "done": 860,
      "queued": 266, "sending": 51, "retrying": 49, "sent": 844, "failed": 16,
      "per_second": [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 65, 93, 93, 92, 90, 92, 92, 94, 92],
      "rate": 92, "limit": 100
    }
  },
  "recent_issues": [
    {"at": "2026-10-21T10:16:25.042721Z", "channel": "sms", "kind": "rejected", "code": 400, "outcome": "failed",
     "retry_in_ms": null, "attempt": 1, "error": "400 invalid recipient"},
    {"at": "2026-10-21T10:16:24.981641Z", "channel": "sms", "kind": "no_answer", "code": null, "outcome": "retry",
     "retry_in_ms": 579, "attempt": 1, "error": "no answer within 3s"},
    {"at": "2026-10-21T10:16:24.713263Z", "channel": "sms", "kind": "throttled", "code": 429, "outcome": "retry",
     "retry_in_ms": 1284, "attempt": 1, "error": "429 provider busy"}
  ]
}
```

| Field | Meaning |
|---|---|
| `status`, `approved_at`, `finished_at` | as in the [campaign view](#campaign-view): "Started 14:32:05" is `approved_at` |
| `at` | when the snapshot was taken (the api's clock). Use it for "last updated" and for the age of an issue ("now", "3s") |
| `total`, `done` | messages over the selected channels; `done` = sent + failed ("143 of 232 done"). Progress = `done / total` |
| `channels` | the selected channels only. The five states add up to the channel's `total` in every snapshot |
| `per_second` | 30 values, oldest first: provider calls **started** in each second of `window` (retries and 429s included, since the 100/s limit is on calls). Replace the series with each snapshot; don't append |
| `window` | the seconds of `per_second`, whole seconds, `from` inclusive, `to` exclusive |
| `rate` | the newest value of `per_second`: the gauge's "84 msg/s". `0` once the campaign is finished |
| `limit` | the per-channel limit, `100` (the chart's dashed line) |
| `recent_issues` | up to 10 calls that did not end in a sent message, newest first |

How the window moves:

- **While sending**, the window ends one second before the current one: a call is logged when its answer arrives, so
  the second that has just ended is still incomplete. The chart therefore lags by one to two seconds, and a point is
  final when it appears. A call that times out is logged up to 3 s after it started, so such a point can still grow by
  one or two in the next snapshots.
- **Once the campaign is finished**, the window ends with its last call and stays there, so the chart keeps showing how
  the campaign went out ("Keep chart and issue history").

No value of `per_second` can exceed 100: the worker spaces each channel's calls at least 10 ms apart, measured on the
very timestamps this series counts (D4).

#### Recent issues

| Field | Meaning |
|---|---|
| `at` | when the answer came, or when the worker stopped waiting for one |
| `kind` | `throttled` (429), `provider_error` (5xx), `rejected` (other 4xx), `no_answer` (timeout or network error) |
| `code` | the HTTP status; `null` when there was no answer |
| `outcome` | `retry` or `failed` (the message will not be sent) |
| `retry_in_ms` | when retrying: how long after this call the retry was scheduled; `null` when failed |
| `attempt`, `error` | the attempt number and the provider's or worker's message, for a tooltip |

The UI writes the text, e.g. `throttled` + `retry` → "Provider busy · retrying in 1s", `provider_error` + `retry` →
"Provider error · retrying", `rejected` → "Rejected · won't retry", `no_answer` + `retry` → "No answer · retrying",
any `failed` → "won't retry".

## Health and metrics

These are on the api itself (`localhost:8081`), not under `/api`, so nginx does not expose them on port 8080.

| Endpoint | Answer |
|---|---|
| `GET /healthz` | `200 {"status":"ok"}` while the process serves HTTP. No dependencies (Docker's health check) |
| `GET /readyz` | `200 {"status":"ok"}` when the database answers a ping within 2 s; otherwise `503 {"status":"unavailable","error":"database unreachable"}` (the cause is logged) |
| `GET /metrics` | Prometheus text format (below) |

### `GET /metrics`

Each process serves what happens in it (DECISIONS.md D25). The api is at `localhost:8081/metrics`; the worker at
`127.0.0.1:18091/metrics` (published for debugging only, like the mock provider). Both also have the Go runtime and
process metrics (`go_goroutines`, `process_resident_memory_bytes`, …).

| Metric | Where | Type | Labels | Meaning |
|---|---|---|---|---|
| `scc_provider_calls_total` | worker | counter | `channel`, `result` (`sent`, `retry`, `failed`), `code` (HTTP status, or `none` for a timeout or network error) | provider calls. Retries: `result="retry"`; 429s: `code="429"` |
| `scc_provider_call_duration_seconds` | worker | histogram | `channel` | from the rate limiter letting a call go to its answer or timeout |
| `scc_deliveries` | api | gauge | `channel`, `status` | deliveries in each state, read from the database when scraped (every combination, zeros included) |
| `scc_llm_attempts_total` | api | counter | `task` (`segment`, `sms`, `email`), `result` (`valid`, `invalid`, `refused`, `truncated`, `timeout`, `error`) | model attempts. Validation failures: `result="invalid"` |
| `scc_llm_repairs_total` | api | counter | `task` | repair attempts (attempt 2 or 3) |
| `scc_llm_attempt_duration_seconds` | api | histogram | `task` | one attempt: the model call, streaming included, and its validation |

- An attempt cut short because the client left is not counted: it says nothing about the model.
- The worker's counters start at zero when it restarts (Prometheus handles counter resets); `scc_deliveries` does not,
  because it is read from the table.
- If the database is down, the api's `/metrics` still answers `200` without `scc_deliveries`, and logs why.

## Mock SMS/email provider

The worker sends to a fake provider (`mock-provider`, published on `127.0.0.1:18090` for debugging only). It behaves
like a real one: slow at times, failing, throttling, rejecting some recipients.

| Request | Body |
|---|---|
| `POST /v1/sms` | `{"to": "+905…", "text": "…"}` |
| `POST /v1/email` | `{"to": "…", "subject": "…", "text": "…", "html": "…"}` (the worker wraps the plain text in HTML) |
| `GET /stats` | counters per channel (below) |
| `GET /healthz` | `{"status": "ok"}` |

| Answer to a send | The worker's reaction (D6) |
|---|---|
| `202 {"id": "msg_000123", "status": "accepted"}` | `sent` |
| `429 {"error": "provider busy"}` or `{"error": "rate limit exceeded"}`, `Retry-After: <seconds>` | retry after `Retry-After` (+ up to 0.5 s) |
| `503 {"error": "service unavailable"}`, a timeout, a network error | retry with full-jitter backoff (up to 1, 2, 4, 8 s) |
| `400 {"error": "invalid recipient"}` (also a missing key or a bad body) | `failed`, never retried |

- **`Idempotency-Key` is required.** An accepted message or a rejection is stored under the key: a repeated key gets
  the original answer with `Idempotent-Replayed: true` and never makes a second message. A `503` or `429` is not
  stored; the provider did not take the message, so a retry is processed again. The worker sends the delivery's UUID.
- `GET /stats` returns per channel: `received`, `accepted`, `replayed`, `rejected`, `errors`, `throttled` (random 429),
  `limited` (429 from its rate limit), `slow`, and `peak_per_second` (most requests in any 1 s window: an independent
  check of the worker's limit). Everything is in memory; a restart forgets it.

Settings (environment of `mock-provider`; empty means the default):

| Variable | Default | Meaning |
|---|---|---|
| `MOCK_LATENCY_MIN` / `MOCK_LATENCY_MAX` | `20ms` / `120ms` | uniform latency per request |
| `MOCK_ERROR_RATE` | `0.03` | share of requests answered `503` |
| `MOCK_THROTTLE_RATE` | `0.03` | share answered `429` with `Retry-After` 1–2 s |
| `MOCK_SLOW_RATE` / `MOCK_SLOW_DELAY` | `0.005` / `10s` | accepted at once but answered after the delay, past the worker's timeout |
| `MOCK_REJECT_RATE` | `0.01` | share of recipients rejected, decided by a hash of the address (always the same ones) |
| `MOCK_RATE_LIMIT` | `100` | requests per channel in any 1 s window before `429`; `0` turns it off |

The worker's own settings: `PROVIDER_URL` (`http://mock-provider:8080`), `PROVIDER_TIMEOUT` (`3s`, one call),
`WORKER_CONCURRENCY` (`32` calls in flight per channel), `WORKER_LEASE` (`30s`, at least twice the timeout).

## Try it

```bash
curl -s localhost:8080/api/briefs/examples
curl -s -X POST localhost:8080/api/campaigns -H 'Content-Type: application/json' \
  -d '{"brief":"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'\''daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.","channels":["sms","email"],"tone":"friendly"}'
curl -N localhost:8080/api/campaigns/1/draft/stream
curl -s -X POST localhost:8080/api/campaigns/1/approve
curl -N localhost:8080/api/campaigns/1/events
curl -s localhost:18090/stats
curl -s localhost:8081/readyz
curl -s localhost:8081/metrics | grep '^scc_'
curl -s 127.0.0.1:18091/metrics | grep '^scc_provider_calls_total'
```

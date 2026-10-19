# API contract

> REST and SSE contract of the api service, as built in P3. It is the input for the frontend (P6).
> Delivery endpoints (approve, send, live dashboard) arrive in P4/P5 and will be added here.
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
  | `409` | `{"error": "..."}` | content is locked, or drafts are already being written |
  | `413` | `{"error": "request body is too large"}` | body over 64 KB |
  | `422` | `{"error": "...", "details": ["...", ...]}` | invalid input; `details` lists every problem |
  | `422` | `{"error": "invalid content", "report": {...}}` | an edited message failed validation (see [Report](#report)) |
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
| `sending` | Sending | approved, messages are going out (P4) |
| `completed` | Completed | every message is final (P4) |
| `failed` | Failed | sending could not run (P4) |

`draft` and `ready` move automatically as messages are stored. From `sending` on, content is locked (`409`).

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
  "created_at": "...", "updated_at": "..."
}
```

- `unsupported_criteria`: parts of the brief no field can express (e.g. `["kadın kullanıcılar"]`). Show them as a warning; they were **not** applied.
- `as_of`: anchor of "in the last N days". Counts and longest values stay the same for the life of the draft.
- `messages.<channel>`: `null` until drafted, then a [message view](#message-view).

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
- `errors[]`: `{field, code, message}` with `code` one of `empty`, `too_long`, `unknown_variable`, `malformed_variable`, `multiline` (a subject must be one line). Messages are actionable, e.g. `text is 163 characters with the longest values in this audience filled in ({{first_name}} = "Muhammed Emin", 13 characters); the limit is 160. Shorten it by at least 3 characters.`

Content rules: SMS ≤ 160, email subject ≤ 80, body unlimited; variables `{{first_name}}`, `{{last_name}}`, `{{city}}` only, written exactly like that.

### `GET /api/campaigns/{id}/draft/stream` (SSE)

Writes the message of every selected channel that has none yet, and streams the progress. Use `EventSource`.

- Channels already stored are replayed first as one final `draft.state` event each, **without calling the model**. Reopening a finished draft costs nothing.
- Channels are written in parallel. Each finished channel is stored **before** its final event is sent.
- If the client disconnects, writing stops and nothing unfinished is stored; opening the stream again starts the missing channels over.
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

## Try it

```bash
curl -s localhost:8080/api/briefs/examples
curl -s -X POST localhost:8080/api/campaigns -H 'Content-Type: application/json' \
  -d '{"brief":"Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'\''daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.","channels":["sms","email"],"tone":"friendly"}'
curl -N localhost:8080/api/campaigns/1/draft/stream
```

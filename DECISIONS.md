# Decisions

> **Status: draft (2026-10-19).** Decisions were made during planning (P0) and will be updated as
> implementation confirms or changes them. Each entry records the options considered, the choice,
> and the trade-off accepted. Requirement IDs (R1…, U1…) refer to [`docs/PLAN.md`](docs/PLAN.md).
>
> Status legend: **Accepted** · **To verify** (accepted, but depends on something checked during
> implementation) · **Superseded**.

## Index

| # | Decision | Status |
|---|---|---|
| D1 | Backend language: Go | Accepted |
| D2 | Database: PostgreSQL | Accepted |
| D3 | Queue: Postgres `deliveries` table with `SKIP LOCKED` | Accepted |
| D4 | Rate limiting: per-channel token bucket, burst 1 | Accepted |
| D5 | Exactly-once effect: unique constraint + provider idempotency key | Accepted |
| D6 | Retry policy | Accepted |
| D7 | Live updates: SSE with periodic snapshots | Accepted |
| D8 | Claude structured output: `output_config.format` | To verify |
| D9 | Segment rule: flat, whitelisted JSON DSL | Accepted |
| D10 | Two-layer validation + repair loop | Accepted |
| D11 | Streaming: segment non-streamed, content streamed | Accepted |
| D12 | LLM abstraction and mock LLM | Accepted |
| D13 | Model selection | Accepted |
| D14 | User data model: profile attributes | Accepted |
| D15 | Content rules: length, variables, email format | Accepted |
| D16 | Testing strategy | Accepted |
| D17 | Frontend stack | Accepted (UI library chosen in P6) |
| D18 | Observability endpoints | Accepted |

---

## D1 — Backend language: Go

- **Options:** Go · PHP (Laravel).
- **Decision:** Go.
- **Why:** The system is dominated by concurrent, long-running work: a queue worker with
  per-channel rate limits, and long-lived SSE connections. Go covers these with the standard library
  (goroutines, `net/http`) plus `golang.org/x/time/rate`. There is an official Anthropic Go SDK.
  A single static binary keeps images small. Go code is explicit, which matters because every part
  must be explainable in the review call.
- **Trade-off:** Laravel would give queues, migrations and validation out of the box and faster
  scaffolding. But PHP-FPM ties up a worker per SSE connection, and framework "magic" is harder to
  defend for someone new to the stack.

## D2 — Database: PostgreSQL

- **Options:** PostgreSQL · MySQL 8 · SQLite.
- **Decision:** PostgreSQL.
- **Why:** `FOR UPDATE SKIP LOCKED` (queue, D3), unique constraints (dedup, D5), JSONB for storing
  LLM outputs and segment rules, partial indexes for the queue's hot path.
- **Trade-off:** MySQL 8 also supports `SKIP LOCKED`, but its JSON and indexing ergonomics are weaker.
  SQLite handles concurrent writers poorly.

## D3 — Queue: Postgres `deliveries` table

- **Options:**
  - **A. Postgres-backed queue:** the `deliveries` table is both queue and ledger.
  - **B. Redis + asynq.**
  - **C. RabbitMQ / NATS JetStream.**
- **Decision:** A. One row per (campaign, user, channel) with `status`, `attempts`,
  `next_attempt_at`, `locked_until`, `last_error`. Workers claim batches with
  `SELECT … FOR UPDATE SKIP LOCKED`. A claim sets a lease (`locked_until`), so a crashed worker's
  rows become claimable again.
- **Why:**
  - **Transactional:** approving a campaign and enqueuing its messages happen in one transaction.
    There is no "approved in DB but never enqueued" window. With B, that window exists and would
    need an outbox table anyway.
  - **One source of truth:** the dashboard aggregates from the same table.
  - **Cheap scheduled sending:** the B1 bonus is just `next_attempt_at` in the future.
  - **Enough capacity:** peak load is 2 channels × 100 msg/s, well within Postgres' range.
- **Trade-off:** We write the lease/retry logic ourselves (small and fully testable). At real
  Insider scale (millions per minute), a dedicated broker (Kafka/SQS) would replace this. The
  `delivery` package hides the claim logic behind an interface to keep that migration contained.
  River (a Postgres-native Go job library) was considered. We rejected it because a job per message
  would duplicate state already held in `deliveries`.

## D4 — Rate limiting: per-channel token bucket, burst 1

- **Decision:** One limiter per channel in the worker process: 100 tokens/s, **burst 1**. A
  per-channel worker pool keeps enough requests in flight to reach the rate despite provider latency.
- **Why burst 1:** With burst *b*, a token bucket allows up to *b* + 100 sends in a 1-second window.
  With burst 100 that is ~200, which violates "at most 100 per second". Burst 1 spaces sends
  ≥ 10 ms apart, so any half-open 1 s window holds at most 100. A test measures this.
- **Trade-off:** The limit is enforced inside a single worker process. Running several worker
  replicas would need a shared limiter (Redis or a Postgres token table). That is documented, not
  built. Network jitter can make the provider observe small bursts; the provider's 429 plus our
  retry handling cover that, as in real life.

## D5 — "Same message never twice": unique constraint + idempotency key

- **Decision:**
  - **Layer 1 (our side):** `UNIQUE (campaign_id, user_id, channel)` on `deliveries`. Approval is a
    conditional update (`… WHERE status = 'ready'`), so double clicks or concurrent requests create
    one set of deliveries.
  - **Layer 2 (provider side):** every send carries `Idempotency-Key: <delivery id>`. The mock
    provider stores seen keys and returns the original result for repeats.
- **Why:** The queue is at-least-once by nature. A worker can crash, or a request can time out
  after the provider accepted it. At-least-once delivery plus an idempotent receiver gives an
  exactly-once *effect*. Layer 1 alone would not cover the "accepted but response lost" case.
- **Scope assumption:** "same message" = same campaign + same user + same channel (see A4).

## D6 — Retry policy

| Outcome | Action |
|---|---|
| 2xx | `sent` |
| 429 | Wait `Retry-After` (+ jitter), retry; does not trip permanent failure |
| 5xx, timeout, connection error | Exponential backoff with full jitter (base 1 s, cap 60 s), retry |
| Other 4xx (e.g. invalid recipient) | `failed` immediately; retrying cannot help |
| Max attempts reached (5) | `failed`, with `last_error` kept for the dashboard |

- **Why full jitter:** It avoids retry storms where every failed message retries at the same instant
  and causes another wave of 429s.

## D7 — Live updates: SSE with periodic snapshots

- **Options:** SSE · WebSocket · polling.
- **Decision:** SSE for both LLM streaming and the dashboard. For the dashboard, the API sends an
  **aggregate snapshot** (counts by channel and status, throughput) about once per second.
- **Why:** Traffic is one-way, server → client. SSE runs over plain HTTP and the browser reconnects
  automatically. Snapshots are idempotent: after a reconnect the next snapshot carries the full
  state, so no "missed event" handling is needed. API and worker never talk directly; both only use
  Postgres.
- **Trade-off:** One aggregate query per open dashboard per second. That is fine at this scale and
  backed by an index on `(campaign_id, status)`. The frontend proxy must not buffer SSE
  (`proxy_buffering off`).

## D8 — Claude structured output: `output_config.format`

- **Options:**
  - **A.** Ask for JSON in the prompt and parse it (fragile, no guarantee).
  - **B.** Strict tool use (`strict: true`).
  - **C.** Native Structured Outputs (`output_config.format` with a JSON schema).
- **Decision:** C, with B as fallback.
- **Why:** With C, the API constrains the response to the schema and it works with streaming. For
  B: on current Opus/Sonnet models a forced `tool_choice` (`any`/`tool`) returns 400, so B needs
  `auto` plus prompting plus a "did it actually call the tool?" check. That is more moving parts.
- **To verify (P3):** the Go SDK binding for `output_config.format` is not in the docs we checked.
  We will confirm it against the SDK source. If it is missing, we fall back to B, which is
  documented for Go.
- **Schema limits that shape the design:**
  - No recursive schemas → the DSL is flat (D9).
  - No `minLength`/`maxLength` or `minimum`/`maximum` → character limits and numeric ranges
    **cannot** be enforced by the schema and are enforced by the backend (D10).

## D9 — Segment rule: flat, whitelisted JSON DSL

- **Decision:** Claude emits a rule, never SQL. Example:

  ```json
  {
    "match": "all",
    "conditions": [
      { "field": "city",             "op": "eq",                   "value": "İstanbul" },
      { "field": "last_cart_add_at", "op": "within_last_days",     "value": 30 },
      { "field": "last_purchase_at", "op": "not_within_last_days", "value": 30 }
    ],
    "unsupported_criteria": [],
    "explanation": "Users in İstanbul who added to cart in the last 30 days but did not purchase."
  }
  ```

- **Field registry:** each field maps to a column, a type, allowed operators and value rules.
- **Compiler:** builds a parameterized query. Column names come only from the registry; values are
  only bind parameters (`$1, $2 …`).
- **Why flat** (`all`/`any` + list, plus an `in` operator): Structured Outputs rejects recursive
  schemas (D8). The flat form still expresses the sample brief and common variants
  ("İstanbul or Ankara" → `city in [...]`).
- **Enumerated values:** fields like `city` are enums in the schema, drawn from the seed. Claude
  cannot invent a city, and we avoid Turkish-İ casing bugs (`strings.ToLower("İ")` is not `"i"`).
- **NULL semantics are explicit:** `not_within_last_days` on `last_purchase_at` includes users who
  never purchased (`IS NULL OR < now() - interval`). Tested in P2.
- **Unsupported criteria:** If the brief asks for something the registry cannot express (e.g.
  gender), Claude must list it in `unsupported_criteria` instead of guessing. The UI shows a warning.
- **Consent is not part of the DSL:** the backend always appends the channel consent filter (R4).

## D10 — Two-layer validation + repair loop

- **Layer 1, shape:** JSON decoded strictly into Go structs (unknown fields rejected). This is
  checked even though the API guarantees it, because the mock LLM, fixtures and future model or SDK
  changes must pass the same gate.
- **Layer 2, business rules:**
  - Segment rule: field whitelist, operator/type compatibility, value ranges.
  - Content: SMS ≤ 160 and subject ≤ 80 (rune-based, worst-case rendered), variable whitelist,
    non-empty fields.
- **Repair loop:** If layer 2 fails, re-ask Claude with the concrete error list ("SMS is 187
  characters; limit 160"), at most 2 retries. If it still fails, return the draft *and* the errors
  to the UI so the marketer can fix it by hand. **Never silently truncate.**
- **Same validator for humans:** marketer edits go through the same layer-2 validator before
  approval.
- **Model refusals and truncation:** `stop_reason` of `refusal` or `max_tokens` is handled before
  parsing. Server-side model fallback on refusal will be evaluated in P3.

## D11 — Streaming strategy

- **Segment:** not streamed. The output is short and nothing useful can be shown until the whole
  rule is validated and counted.
- **Content:** streamed. The backend accumulates the partial JSON and extracts the current value of
  known fields (`sms.text`, `email.subject`, `email.body`). It emits typed SSE events:
  `draft.delta`, `draft.repairing`, `draft.validated`, `draft.invalid`. The UI marks streamed text as
  "validating" until `draft.validated` arrives.
- **Why parse in the backend:** One source of truth, a clean typed event contract for the frontend,
  and the mock LLM can stream through the same path.

## D12 — LLM abstraction and mock LLM

- **Decision:** `llm.Client` interface with two implementations: `anthropic` and `mock`. The
  provider is chosen by `LLM_PROVIDER=auto|anthropic|mock`; `auto` uses Anthropic when
  `ANTHROPIC_API_KEY` is set and the mock otherwise. The UI shows a "Mock LLM" badge.
- **Mock behaviour — scenario catalog:**
  - Each scenario is a fixture file (`backend/testdata/llm-scenarios/`) describing a brief and the
    scripted model responses for it: segment JSON, content stream chunks, and optional faults per
    attempt (so the repair loop can be exercised: attempt 1 invalid → attempt 2 valid).
  - Happy-path scenarios: the case's sample brief plus variants (other cities, SMS-only, "any of"
    rules, a brief with an unsupported criterion).
  - Fault scenarios: broken JSON, unknown field, over-limit SMS, unknown variable, refusal, timeout.
  - Selection: tests pick a scenario by id; key-less runs match the typed brief to a scenario, and an
    unmatched brief falls back to a generic keyword-based response so a reviewer can still try their
    own brief without a key.
  - Streams with artificial delay so the UI behaves like the real thing.
- **Recorded fixtures:** real Claude responses captured once with a key and replayed in tests, so
  tests also cover the real response shape.

## D13 — Model selection

- **Decision:** default `claude-opus-5-5`, configurable via `ANTHROPIC_MODEL`. `effort` is set
  explicitly per call (not left to the model default), tuned for latency in P3.
- **Note:** on this model thinking cannot be disabled; `effort` is the speed/depth control.

## D14 — User data model: profile attributes

- **Decision:** Behaviour is stored as derived attributes on the user profile, like a CDP profile,
  not as a raw event table. Draft fields:
  - Identity and contact: `id`, `first_name`, `last_name`, `email`, `phone`, `city`, `language`.
  - Consent: `sms_opt_in`, `email_opt_in`.
  - Behaviour: `last_cart_add_at`, `last_purchase_at`, `order_count`, `total_spent`.
  - `created_at`.
- **Why:** The DSL maps 1:1 to columns, which keeps the compiler simple and safe. The seed computes
  the attributes deterministically.
- **Trade-off:** Field-to-field comparisons ("cart add after last purchase") and arbitrary event
  history are not expressible. This is documented as a known limitation.

## D15 — Content rules

- **Length:** counted in Unicode characters (`utf8.RuneCountInString`; Go's `len()` counts bytes).
  GSM-7/UCS-2 segment accounting is out of scope (A1).
- **Worst case:** templates are validated with each variable replaced by the longest value present
  in the target audience. The audience is frozen at approval, so no rendered message can exceed the
  limit.
- **Variables:** whitelist `{{first_name}}`, `{{last_name}}`, `{{city}}`, each with a fallback value
  for empty data. Unknown variables are validation errors.
- **Email:** subject (≤ 80) + plain-text body. The backend wraps it in a simple HTML template.
  Claude never produces HTML.

## D16 — Testing strategy

| Layer | What | How |
|---|---|---|
| Unit | DSL validator/compiler, content validator, renderer, retry policy, partial-JSON extractor | Table-driven Go tests |
| Integration | Queue claim/lease, idempotent approval, dedup, rate limit, consent filtering | Real Postgres in `docker-compose.test.yml` |
| LLM | Repair loop, fault handling, streaming events | Mock LLM + recorded fixtures; no API key needed (R17) |
| Frontend | Character counter, draft rendering | Vitest |
| End-to-end | Brief → approve → all delivered | Smoke script in P7 |

- `make test` runs everything inside containers, so the reviewer only needs Docker and `make`.

## D17 — Frontend stack

- **Decision:** Vue 3 (Composition API) + Vite + TypeScript, served as a static build by nginx,
  which also proxies `/api` (SSE unbuffered).
- **Component library:** Naive UI or PrimeVue, decided at the start of P6.

## D18 — Observability endpoints

- `GET /healthz`: liveness, no dependencies.
- `GET /readyz`: readiness, checks the DB connection.
- `GET /metrics`: Prometheus format.
  - Counters: deliveries by channel/status, retries, 429s.
  - Histograms: LLM latency, provider latency.
  - Count of LLM validation failures and repairs.

---

## Assumptions

| # | Topic | Assumption |
|---|---|---|
| A1 | "160 characters" | Unicode characters, not bytes; GSM-7/UCS-2 segmenting out of scope (Turkish ş/ğ/ı force UCS-2 → 70 chars/segment in reality) |
| A2 | Limit applies to | Both template (worst-case rendered) and final message; see D15 |
| A3 | Variables | `first_name`, `last_name`, `city`, with fallbacks |
| A4 | "Same message" | Same campaign + user + channel |
| A5 | "Abandoned cart" semantics | Cart add within N days AND no purchase within N days (never-purchased included) |
| A6 | Consent | Per-channel opt-in flags; missing phone/email also means unreachable |
| A7 | 100 msg/s | Per channel, across the whole system |
| A8 | Delivery status | Provider's synchronous response (`sent`/`failed`); delivery-receipt webhooks out of scope |
| A9 | Channels | Both by default; marketer can disable one; the brief may restrict channels |
| A10 | Users of the tool | Single marketer, no authentication or multi-tenancy |
| A11 | Content language | Same as the brief (per-user language is bonus B3) |
| A12 | After approval | Content is locked; no editing (pause/cancel is nice-to-have) |

## Open questions (deferred — HR not contacted yet)

Resolved for now with working assumptions; revisit before submission.

1. **Real key or mock during evaluation?** → Assume **both**. The real-key path is treated as the
   primary demo (prompts, latency, error handling polished as if evaluators use a key); the mock LLM
   is a scenario catalog (D12) that tests and key-less runs rely on.
2. **Repository visibility?** → Private during development; visibility decided before submission.

## Out of scope (will be listed in README)

Authentication and multi-tenancy · delivery-receipt webhooks · quiet hours and frequency capping ·
GSM-7/UCS-2 segment billing · horizontal scaling of the worker (shared rate limiter) ·
field-to-field segment conditions.

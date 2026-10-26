# Decisions

> **Status: final (2026-10-26).** Decisions were made during planning (P0) and updated as each
> phase confirmed or changed them; D23–D28 record what was built. Each entry records the options
> considered, the choice, and the trade-off accepted. Requirement IDs (R1…, U1…) refer to
> [`docs/PLAN.md`](docs/PLAN.md).
>
> Status legend: **Accepted** · **To verify** (accepted, but depends on something checked during
> implementation) · **Superseded**.

## Index

| # | Decision | Status |
|---|---|---|
| D1 | Backend language: Go | Accepted (limiter library dropped in P4) |
| D2 | Database: PostgreSQL | Accepted |
| D3 | Queue: Postgres `deliveries` table with `SKIP LOCKED` | Accepted (built in P4) |
| D4 | Rate limiting: per channel, at most one send per 10 ms | Accepted (revised in P4: own spacer, not a token bucket) |
| D5 | Exactly-once effect: unique constraint + provider idempotency key | Accepted (key is a UUID, P4) |
| D6 | Retry policy | Accepted (429 budget added in P4) |
| D7 | Live updates: SSE with periodic snapshots | Accepted (built in P5, D25) |
| D8 | Claude structured output: `output_config.format` | Accepted (verified in P3) |
| D9 | Segment rule: flat, whitelisted JSON DSL | Accepted |
| D10 | Two-layer validation + repair loop | Accepted (refusal retry added in P3) |
| D11 | Streaming: segment non-streamed, content streamed | Accepted (events revised in P3) |
| D12 | LLM abstraction and mock LLM | Accepted (catalog location revised in P3) |
| D13 | Model selection: Claude Sonnet 5.5 | Accepted (effort chosen in P3) |
| D14 | User data model: profile attributes | Accepted |
| D15 | Content rules: length, variables, email format | Accepted (no emoji, after P4) |
| D16 | Testing strategy | Accepted |
| D17 | Frontend stack | Accepted (P6: own components, no UI library, D26) |
| D18 | Observability endpoints | Accepted (revised in P5: a metrics endpoint per process, D25) |
| D19 | Migrations and seed run as one-shot compose services | Accepted (P1) |
| D20 | Integration tests: one fresh database per test | Accepted (P1) |
| D21 | How the segment engine is verified | Accepted (P2) |
| D22 | Design reference: visual only; adopted additions | Accepted (before P3) |
| D23 | LLM layer as built (P3) | Accepted (P3) |
| D24 | Approval and delivery as built (P4) | Accepted (P4) |
| D25 | Tracking as built (P5) | Accepted (P5) |
| D26 | Frontend as built (P6) | Accepted (P6) |
| D27 | Hardening and submission (P7) | Accepted (P7) |
| D28 | Security quick wins after the audit (P9) | Accepted (P9) |

---

## D1 — Backend language: Go

- **Options:** Go · PHP (Laravel).
- **Decision:** Go.
- **Why:** The system is dominated by concurrent, long-running work: a queue worker with
  per-channel rate limits, and long-lived SSE connections. Go covers these with the standard library
  (goroutines, `net/http`); the rate limiter is a few lines of our own (P4, D24 K6). There is an
  official Anthropic Go SDK.
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
- **As built (P4):** besides those columns, each row holds the recipient's address and the message
  rendered for them at approval (D24 K2), a UUID idempotency key (D5), `throttled` (429 answers, D6)
  and the provider's message id. `attempts` is incremented when a worker claims the row, so a call
  cut short by a crash still counts, and it fences a claim whose lease ran out (D24). One row per
  provider call goes to `delivery_attempts`.
- **Trade-off:** We write the lease/retry logic ourselves (small and fully testable). At real
  Insider scale (millions per minute), a dedicated broker (Kafka/SQS) would replace this. The
  `delivery` package hides the claim logic behind an interface to keep that migration contained.
  River (a Postgres-native Go job library) was considered. We rejected it because a job per message
  would duplicate state already held in `deliveries`.

## D4 — Rate limiting: per channel, at most one send per 10 ms

- **Decision:** One limiter per channel in the worker process, letting one send through at a time and
  never sooner than **10 ms after the previous one** (100/s with no burst). A per-channel worker pool
  keeps enough requests in flight to reach the rate despite provider latency.
- **Why no burst:** With burst *b*, a token bucket allows up to *b* + 100 sends in a 1-second window.
  With burst 100 that is ~200, which violates "at most 100 per second". Spacing sends ≥ 10 ms apart
  means 101 sends span at least a second, so any half-open 1 s window holds at most 100.
- **Revised in P4 (D24 K6):** the plan was `golang.org/x/time/rate` with burst 1. Its guarantee is on
  *reservations*: when it is saturated they are exactly 10 ms apart, and a goroutine that wakes late
  can land next to one that wakes on time, so the measured send times can put 101 in a window. We use a
  small spacer of our own instead: it takes the timestamp **after** waiting and spaces the next send
  from that. The same timestamp is stored as the call's `started_at`, so the bound holds for exactly
  the times the dashboard charts, and the test asserts it on them. Cost: the real rate is a little
  under 100/s (timer overshoot; 90–97/s measured), and no new dependency.
- **Trade-off:** The limit is enforced inside a single worker process. Running several worker
  replicas would need a shared limiter (Redis or a Postgres token table). That is documented, not
  built. Network jitter can make the provider observe small bursts; the provider's 429 plus our
  retry handling cover that, as in real life.

## D5 — "Same message never twice": unique constraint + idempotency key

- **Decision:**
  - **Layer 1 (our side):** `UNIQUE (campaign_id, user_id, channel)` on `deliveries`. Approval is a
    conditional update (`… WHERE status = 'ready'`), so double clicks or concurrent requests create
    one set of deliveries.
  - **Layer 2 (provider side):** every send carries `Idempotency-Key: <the delivery's UUID>`. The
    mock provider stores seen keys and returns the original result for repeats. *(Revised in P4: the
    plan said the delivery id. Ids restart after a reseed or a clean install while a provider still
    remembers the keys it saw, so a new delivery could get an old message's answer and be silently
    dropped. A UUID per row is unique for good.)*
- **Why:** The queue is at-least-once by nature. A worker can crash, or a request can time out
  after the provider accepted it. At-least-once delivery plus an idempotent receiver gives an
  exactly-once *effect*. Layer 1 alone would not cover the "accepted but response lost" case.
- **Scope assumption:** "same message" = same campaign + same user + same channel (see A4).

## D6 — Retry policy

| Outcome | Action |
|---|---|
| 2xx | `sent` |
| 429 | Wait `Retry-After` (+ up to 0.5 s jitter), retry; does not use up the attempts for errors |
| 5xx, timeout, connection error | Exponential backoff with full jitter (base 1 s, cap 60 s), retry |
| Other 4xx (e.g. invalid recipient) | `failed` immediately; retrying cannot help |
| 5 attempts that ended in an error | `failed`, with `last_error` kept for the dashboard |
| 10 answers of 429 | `failed` (P4: so that every delivery ends, D24 K7) |

- **Why full jitter:** It avoids retry storms where every failed message retries at the same instant
  and causes another wave of 429s.
- **429 has its own budget (P4, K7):** a 429 is the provider asking us to slow down, not a fault of the
  message. Counting it among the 5 attempts would fail healthy messages under throttling; not counting
  it at all could leave a campaign sending forever. A crash during a call counts as an error attempt.
- **Note:** with 5 attempts the backoff windows are 1, 2, 4 and 8 s, so the 60 s cap only matters if
  the attempt count is raised. Retry-After is capped at 60 s too.

## D7 — Live updates: SSE with periodic snapshots

- **Options:** SSE · WebSocket · polling.
- **Decision:** SSE for both LLM streaming and the dashboard. For the dashboard, the API sends an
  **aggregate snapshot** (counts by channel and status, throughput) about once per second.
- **Why:** Traffic is one-way, server → client. SSE runs over plain HTTP and the browser reconnects
  automatically. Snapshots are idempotent: after a reconnect the next snapshot carries the full
  state, so no "missed event" handling is needed. API and worker never talk directly; both only use
  Postgres.
- **Trade-off:** One aggregate query per open dashboard per second. That is fine at this scale and
  backed by an index on `(campaign_id, channel, status)`. The frontend proxy must not buffer SSE
  (`proxy_buffering off`).
- **As built (P5, D25):** each second a snapshot is read in one transaction (counts, status, the
  30-second sends-per-second series, recent issues) and sent whole; a finished campaign gets its
  final snapshot and `delivery.done`, then the stream closes.

## D8 — Claude structured output: `output_config.format`

- **Options:**
  - **A.** Ask for JSON in the prompt and parse it (fragile, no guarantee).
  - **B.** Strict tool use (`strict: true`).
  - **C.** Native Structured Outputs (`output_config.format` with a JSON schema).
- **Decision:** C, with B as fallback.
- **Why:** With C, the API constrains the response to the schema and it works with streaming. For
  B: on current Opus/Sonnet models a forced `tool_choice` (`any`/`tool`) returns 400, so B needs
  `auto` plus prompting plus a "did it actually call the tool?" check. That is more moving parts.
- **Verified (P3):** the Go SDK (v1.82.0) has a typed binding (`OutputConfigParam.Format`,
  `JSONOutputFormatParam`), checked in the SDK source, so C is used as planned and B was not needed.
  *(Before P3 this was "to verify": the binding was not in the docs we had checked.)*
- **How it is verified (P3):** a one-time smoke test on the real Anthropic API with Claude Sonnet
  5.5 (D13), using the developer's own `ANTHROPIC_API_KEY`. The number of calls is agreed in
  advance. Responses are recorded as fixtures and replayed in tests, so CI and reviewers without a
  key still exercise the real response shape. Sonnet 5.5 rejects forced `tool_choice` (400), which
  is one more reason to prefer C over B.
- **Segment schema (built in P2):** `segment.RuleSchema` generates the rule's schema from the
  registry. It has 12 `anyOf` variants, one per (field, operator group), so incompatible
  combinations and unknown cities are excluded at generation time. The real API accepts it (P3
  smoke test), so the fallback to a generic condition schema was not needed. The validator remains
  the real guarantee either way.
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

**As implemented in P2:**

- **Fields:**
  - Targetable: `city`, `language`, `last_cart_add_at`, `last_purchase_at`, `created_at`,
    `order_count`, `total_spent`.
  - Not targetable: names, email, phone (personal data) and the consent flags.
- **Operators:**
  - Enums: `eq`, `neq`, `in`, `not_in`.
  - Relative time: `within_last_days`, `not_within_last_days`, `is_null`, `is_not_null`.
  - Numbers: `eq`…`lte` and `between`.
- **City list comes from the data:** read at api startup, most common first (P2 decision K2a).
- **Empty conditions target all users** (K3a), needed for briefs like "all email subscribers".
- **"Parse, don't validate":** `Validate` returns a `Validated` value with unexported fields, and
  `Compile` accepts only that type. The compiler cannot be handed an unchecked rule; this is
  enforced by the type system, not by convention.
- **Relative-time cutoffs:** computed in Go from an `asOf` time (UTC), not with SQL `now()`.
  Results are reproducible, and P4 can count at approval with the same anchor as the preview.
- **No indexes yet:** the sample brief runs as a 1.3 ms sequential scan over 5,000 rows. At real
  scale the first candidates are btree indexes on `city`, `last_cart_add_at` and
  `last_purchase_at`, or a precomputed segment table.

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
  parsing. A cut-off answer is a failed attempt. A refusal is retried once within the three attempts,
  and a second refusal is final (`needs_manual_edit` with the category). Server-side fallback is
  not used (D23).

## D11 — Streaming strategy

- **Segment:** not streamed. The output is short and nothing useful can be shown until the whole
  rule is validated and counted.
- **Content:** streamed. The backend accumulates the partial JSON and extracts the current value of
  known fields (`sms.text`, `email.subject`, `email.body`). It emits typed SSE events:
  `draft.state` (the design's states: writing, validating, fixing n of 3, valid, needs manual edit),
  `draft.delta` (a field's text so far), `draft.error` and `draft.done`. *(Revised in P3: the
  first draft had `draft.repairing` / `draft.validated` / `draft.invalid`; one state event maps 1:1
  onto the design's card states.)* Contract: [`docs/API.md`](docs/API.md).
- **Why parse in the backend:** One source of truth, a clean typed event contract for the frontend,
  and the mock LLM can stream through the same path.

## D12 — LLM abstraction and mock LLM

- **Decision:** `llm.Client` interface with two implementations: `anthropic` (the official
  Anthropic Go SDK) and `mock`.
  - The provider is chosen by `LLM_PROVIDER=auto|anthropic|mock`: `auto` uses Anthropic when
    `ANTHROPIC_API_KEY` is set and the mock otherwise.
  - `LLM_MODEL` overrides the default model (D13).
  - The API reports which mode is active so the UI can show a demo/mock badge.
- **Client construction is explicit:** key, retries and timeouts are passed as SDK options
  (`option.WithAPIKey`, `WithMaxRetries`, `WithRequestTimeout`) from our config. We do not rely on
  the SDK silently picking up environment variables, so what runs is what the config says.
- **Why only the direct API:** the case requires the key to come from `ANTHROPIC_API_KEY`, and the
  developer uses the same key path as reviewers. An OpenRouter path was considered and dropped
  before P3.
- **Mock behaviour — scenario catalog:**
  - Each scenario is a JSON file (`backend/internal/llm/mock/scenarios/`, embedded in the binary)
    describing a brief and the scripted model answers for it per task and attempt, including faults
    (so the repair loop can be exercised: attempt 1 invalid → attempt 2 valid). Each file declares
    its expected outcome, and a test recomputes it with the real validators. *(Revised in P3: the
    plan said `backend/testdata/`, but Go never builds packages under `testdata`, and the key-less
    demo needs the catalog at runtime.)*
  - Happy-path scenarios: the case's sample brief plus variants (other cities, SMS-only, "any of"
    rules, a brief with an unsupported criterion).
  - Fault scenarios: broken JSON, unknown field, over-limit SMS, unknown variable, refusal, timeout.
  - Selection: tests pick a scenario by id; key-less runs match the typed brief to a scenario, and an
    unmatched brief falls back to a generic keyword-based response so a reviewer can still try their
    own brief without a key.
  - *Limits of the keyword fallback (found while writing `docs/TEST-SCENARIOS.md`, kept as they
    are):* it reads only cities, "sepet"/"cart", a few "did not buy" phrases and the first "N gün";
    it never fills `unsupported_criteria`, so a brief with only unsupported criteria (gender, age,
    email opens) targets **all users without a warning**; a negation it does not know is read as the
    positive ("sepetine ürün eklememiş" → added to cart in the last 30 days); a city written without
    Turkish letters ("Eskisehir") is missed, and a brief with no Turkish letters gets English copy;
    the tone is ignored. Each test-plan brief therefore has its own catalog scenario
    (`10-…` to `21-…`, not in the brief library). With Claude these briefs go through the prompt's
    `unsupported_criteria` rule (D9).
  - Streams with artificial delay so the UI behaves like the real thing.
  - **Example-brief library** (`GET /api/briefs/examples`, D22): the happy-path scenarios' briefs
    are exposed as ready-made examples, so a reviewer without a key can run a deterministic
    end-to-end demo with one click.
- **Recorded fixtures:** real Claude responses captured once with a key
  (`internal/llm/anthropic/testdata/recorded/`, by `go test -tags live`) and replayed through the
  real SDK and the generator, so tests also cover the real response shape, including a real refusal.

## D13 — Model selection: Claude Sonnet 5.5

- **Decision:** Claude Sonnet 5.5 (`claude-sonnet-5-5`), configurable via `LLM_MODEL`.
  *(Revised before P3; the first draft defaulted to Opus 5.5.)*
- **Why:** both tasks (rule extraction and short copy) are interactive. The marketer waits for
  them, so latency and cost per draft matter more than maximum reasoning depth.
- **Model behaviour we design around:**
  - `effort` is set explicitly per call, not left to the model default (levels are recalibrated on
    this model). P3 chose `low` for every task (D23).
  - `thinking: {type: "disabled"}` returns 400. If latency requires it, we use
    `{type: "between_tools"}` or a lower effort.
  - Forced `tool_choice` returns 400, consistent with structured outputs as the primary method (D8).
  - `stop_reason` of `refusal` or `max_tokens` is handled before any parsing (D10).
- **Evaluated in P3:** the server-side refusal fallback (`fallbacks: "default"` with the
  `server-side-fallback-2026-07-01` beta). Not adopted, see D23.

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
  limit. *(P4:)* the approval validates each message again against the longest values of exactly the
  people being frozen, and renders it for each of them; the `deliveries` table then refuses any
  rendered SMS over 160 or subject over 80 characters (a CHECK), so the limit is a database invariant. *(P3:)* the longest values are taken per channel, among the people that channel reaches;
  Postgres `char_length` and Go's rune count agree, which a test checks on the whole seed.
- **Variables:** whitelist `{{first_name}}`, `{{last_name}}`, `{{city}}`, each with a fallback value
  for empty data (`Müşterimiz` for a first name; the users table already forbids empty names and
  cities, and the worst case counts the fallback if it is longer). Unknown, misspelled or
  space-padded variables are validation errors that name the correct spelling. The prompt also
  forbids Turkish suffixes on variables (`{{city}}'daki` is wrong for İzmir).
- **No emoji (after P4):** no field may contain an emoji or another pictographic symbol. Claude ended
  the real-API test's SMS with "😊" without being asked. Most emoji take two UTF-16 units in an SMS
  while our counter counts one, phones and mail clients render them unevenly, and the brand voice is
  the marketer's call. The prompt forbids them and the validator rejects them (code `emoji`), so the
  repair loop removes them and a marketer's edit with one is refused; nothing is stripped silently.
  The rule is an explicit list: runes outside the Basic Multilingual Plane, U+2600–27BF (☀ ✨ ✔),
  U+2B00–2BFF (⭐) and the joiners U+FE0F, U+200D, U+20E3; Turkish letters, %, ₺, © and ™ pass.
  *Possible extension:* an `allow_emoji` choice next to the tone, with the prompt allowing one emoji
  and SMS length counted in UTF-16 units so the counter stays honest. It needs a UI switch (P6/P8).
- **Email:** subject (≤ 80, a single line: it becomes a mail header) + plain-text body with no
  limit. The backend wraps it in a simple HTML template. Claude never produces HTML.
- **Design conflict resolved in favour of this rule (D22):** the design counts the raw template
  and uses a single variable. We keep three variables and the worst-case check.
  - The API returns the longest value of each variable in the audience and the computed
    worst-case lengths.
  - The UI counter shows the same number the backend enforces, so the screen never disagrees with
    validation.

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
- **Component library:** none *(decided in P6, D26 K1)*. The plan left Naive UI or PrimeVue open;
  the glass style (D22) is a set of tokens plus about fifteen small components, and the two parts that
  are hard to get right (a modal dialog and a choice of tone) are native elements (`<dialog>`,
  `<select>`). No state library and no chart library either (D26 K2, K3).

## D18 — Observability endpoints

- `GET /healthz`: liveness, no dependencies.
- `GET /readyz`: readiness, checks the DB connection.
- `GET /metrics`: Prometheus format.
  - Counters: deliveries by channel/status, retries, 429s.
  - Histograms: LLM latency, provider latency.
  - Count of LLM validation failures and repairs.
- **Revised in P5 (D25):** deliveries by channel and status are a *gauge* read from the database
  (it is state, not a count of events); retries and 429s are labels of the worker's call counter.
  The api and the worker each serve their own `/metrics`.

## D19 — Migrations and seed run as one-shot compose services

- **Options:** (a) separate `migrate` and `seed` containers that run once and exit; (b) the api
  migrates and seeds on startup.
- **Decision:** (a). Startup order is explicit in compose (`service_completed_successfully`):
  `postgres → migrate → seed → api`, and `migrate → worker`.
- **Why:** Running several api replicas can't race on migrations. "Process is up" and "data is
  ready" stay separate signals. A failed migration stops the stack visibly instead of
  crash-looping the api.
- **Safety nets anyway:**
  - Migrations hold a Postgres advisory lock (goose session locker), so concurrent migrators wait.
  - The seeder takes `LOCK TABLE users IN EXCLUSIVE MODE` and skips when rows exist, so it is
    idempotent and safe to re-run (`make reseed` uses `-reset`).
  - Both are tested with three concurrent runners.

## D20 — Integration tests: one fresh database per test

- **Decision:** `testdb.New(t)` creates a uniquely named database, migrates it, and drops it
  (`WITH (FORCE)`) when the test ends.
- **Why:** `go test ./...` runs packages in parallel against the same server. Sharing one database
  with truncation between tests would make them race. Creating a database costs ~50 ms, which is
  acceptable.
- **No false greens:** without `TEST_DATABASE_URL` the tests skip, so a laptop `go test` works.
  `make test` sets `REQUIRE_DB_TESTS=1`, which turns the skip into a failure.

## D21 — How the segment engine is verified

The segment engine decides who receives messages, so wrong counts are a business bug even when
nothing crashes. It is tested from four independent angles:

| Angle | What it catches |
|---|---|
| Differential test against a Go oracle (300 random rules) | Semantic bugs in the compiler: NULL handling, flipped comparisons, a missing consent filter |
| Three-way sample brief (compiler / hand SQL / oracle) | The case's own example drifting |
| SQL-shape test (2,000 random rules) + fuzzing | Any input text leaking into the SQL string |
| Structured error codes per rule violation | The repair loop in P3 receiving vague feedback |

Each angle was confirmed with a deliberate mutation that the tests then caught (PLAN.md →
Verification log → P2).

## D22 — Design reference: visual only; adopted additions

- **Input:** `DESIGN.md` and `design/screens/` (composer, draft generating, live sending), added
  before P3.
- **Decision:** the design is a **visual** reference: layout, components, colour and typography.
  Product rules come from CASE.md and this file. Where the design disagrees, our decisions win:
  - **Variables:** `{{first_name}}`, `{{last_name}}`, `{{city}}` stay (A3/D15). The design uses
    only `first_name`.
  - **Length:** the worst-case rendered length is checked (D15). The design counts the raw template.
  - **Copy rules** in DESIGN.md (e.g. never mentioning AI in the UI) are not binding; A13 applies.
- **Adopted from the design (API additions, P3):**
  - The generate request takes the brief, the selected channels (SMS/email) and a tone. The model
    also returns a short campaign name.
  - Per-channel draft states: `writing → validating → fixing (attempt n of 3) → valid /
    needs_manual_edit`. Three attempts in total = the D10 repair loop. SMS can be validated while
    email is still streaming.
  - Campaign statuses: Draft, Ready, Sending, Completed, Failed. Message states (P4): Queued,
    Sending, Sent, Retrying, Failed.
  - `Describe` uses the design's wording, e.g. "Added to cart in the last 30 days" and
    "No purchase in the last 30 days".
  - The API reports the LLM mode (mock or anthropic) for a demo badge.
- **Suggested by us and accepted:**

  | Feature | Phase | Why |
  |---|---|---|
  | Consent-exclusion counts per channel and total channel messages in the preview (e.g. 99 + 133 = 232) | P3 | Makes R4 visible: people without consent are shown as excluded, never as reachable |
  | Example-brief library tied to mock scenarios (`GET /api/briefs/examples`) | P3 | Gives key-less reviewers a deterministic demo |
  | Worst-case preview with the audience's longest name, next to a fictional sample user | P3 | Makes D15 visible instead of a hidden rule |
  | "Recent issues" feed from a `delivery_attempts` log, and per-second sends per channel for a "last 30 s" chart | P4/P5 | Shows retries, 429s and the 100/s limit live (R10, R12) |

- **Out of scope** (present in the design, no backend): search, notifications, settings, profile,
  and the Audiences and Templates pages.

## D23 — LLM layer as built (P3)

Decisions taken at the start of P3 (K1–K7) and after the first smoke test. Evidence: PLAN.md →
Verification log → P3.

| # | Topic | Decision | Why |
|---|---|---|---|
| K1 | Structured output | Typed `output_config.format` in the Go SDK; strict tool use only if the API had rejected the schema | The binding exists (D8) |
| K2 | Drafting calls | One streamed call per channel, in parallel; each repair is a fresh single-turn request quoting the rejected answer and the validator's problems | Each channel has its own state and three attempts; SMS finishes first and is validated while the email streams. Sonnet 5.5 binds thinking blocks to a conversation, so single-turn repairs avoid editing history; the system prompt stays the cached prefix |
| K3 | Thinking and effort | Adaptive thinking at effort `low` for every task | In the smoke test `medium` gave the same rule and equally valid drafts, and its segment request thought for 3,532 tokens and was then declined |
| K4 | Refusals | Handled before parsing; no server-side fallback; retried **once** within the attempt budget (added after the smoke test) | On Sonnet 5.5 the fallback only re-serves `cyber` and `frontier_llm` declines. The decline we saw on a harmless brief was `reasoning_extraction`, so a plain retry is the useful remedy; a second refusal is final |
| K5 | Mock catalog location | `internal/llm/mock/scenarios/*.json`, embedded | Go does not build packages in `testdata`, and the key-less demo needs the catalog |
| K6 | Stream contract | `POST /api/campaigns` (audience, synchronous) + `GET /api/campaigns/{id}/draft/stream` (EventSource-compatible SSE); finished channels are stored before their final event, and reopening replays them without model calls | Works with `EventSource`; a reload never pays for a second generation |
| K7 | Draft → Ready, edits | `ready` exactly when every selected channel has a valid message, recomputed in the same transaction as each save; `PUT …/messages/{channel}` in P3 with the same validator; an invalid edit is rejected and nothing is stored | `needs_manual_edit` needs a way out in P3; the stored content is always the last valid or generated version |

- **Client construction:** `option.WithoutEnvironmentDefaults()` plus explicit key, retries and
  timeout, so the SDK reads nothing from the environment (D12). `LLM_PROVIDER=anthropic` without a
  key stops the api at startup.
- **Prompts:** the segment prompt lists the registry's fields; the content prompts give the character
  cost of each variable in this audience; the brief is wrapped in tags as data. Golden files under
  `internal/llm/testdata/prompts/` make every prompt change visible in review.
- **Campaign name:** produced by the audience call (English, two to five words), because the draft
  screen shows it before any message has streamed.
- **Observed, then closed:** in smoke test A the email's first token took ~9 s at both effort levels
  (SMS ~1.5 s). In four later samples it took 1.2–1.9 s. A phase measurement shows that the response
  headers and the first text arrive together, with no thinking, so the wait is on the API side before
  the response starts. No change; the per-attempt timeout covers a slow start (PLAN.md, P3 follow-up).

## D24 — Approval and delivery as built (P4)

Decisions taken at the start of P4 (K1–K9). Evidence: PLAN.md → Verification log → P4.

| # | Topic | Decision | Why |
|---|---|---|---|
| K1 | Approval anchor | Count and freeze the audience at the campaign's `audience_as_of`, the anchor of the preview; inside the approval transaction, validate every message again against the longest values of exactly the frozen people | What the marketer confirmed is what is sent (D9 already planned this). Approving at "now" changes the numbers just because time passed (the live 177 → 175). The users table changes only on a reseed, and the re-validation catches that |
| K2 | Render time | At approval, in the same transaction: each delivery stores the recipient's address and the rendered text; the email's HTML is built at send time from the stored text | D15's limit becomes a CHECK on every row; the worker needs no join or template logic; the ledger shows exactly what was sent. Cost: data and consent are frozen at approval (no re-check before each send) |
| K3 | Worker topology | One process; per channel one claimer (10-row buffer), 32 senders and one limiter | A slow or failing channel never holds up the other; "Sending" stays close to what is really in flight. One replica, because the limit is in-process (D4) |
| K4 | Completed / Failed | When every delivery is final: `completed` if at least one was sent, `failed` if none was. Approving with nobody reachable is refused (`422`) | The design: one failed message never fails a campaign. A threshold would be an arbitrary number; "failed only on a system error" would never happen, since approval is atomic |
| K5 | Mock provider defaults | 20–120 ms latency, 3% 503, 3% 429 (Retry-After 1–2 s), 0.5% slow accepts (10 s), 1% rejected recipients (by address hash), 100 requests per channel per second | The case asks for errors, latency and 429s now and then. On the sample campaign that is about 7 + 7 errors, one timeout and two rejections: retries are visible, the campaign still completes |
| K6 | Rate limiter | Own spacer, no `x/time/rate` | See D4 |
| K7 | 429 budget | 429s do not use up the 5 attempts for errors; their own budget is 10 | See D6 |
| K8 | Idempotency key | A UUID per delivery, not its id | See D5 |
| K9 | Reseed | `make reseed` empties users **and** campaigns (`TRUNCATE … CASCADE`) | Deliveries reference users: truncating users alone fails, and reused ids would point old deliveries at new people |

- **Approval:** one transaction locks the campaign row (the same lock message saves take), reads
  the reachable people per channel, re-validates, renders, inserts with `COPY` and moves the campaign
  to `sending` with a conditional update. A second approval waits for the lock, finds `sending` and
  returns the same answer; `UNIQUE (campaign_id, user_id, channel)` is the backstop.
- **Claim and lease:** a claim takes due rows and rows whose 30 s lease ran out, with
  `FOR UPDATE SKIP LOCKED`, and increments `attempts`. Finishing a call writes the attempt row and the
  new state in one transaction, fenced by that attempt number, so a worker whose lease ran out cannot
  overwrite a newer claim; its duplicate call is absorbed by the provider's key check (D5).
- **Clocks:** `started_at` is the limiter's moment and `next_attempt_at` is computed from the same Go
  clock. The database's clock was 1.8 ms behind the host under Docker on a Mac; computing the retry
  time there made the log's "retrying in 2s" inconsistent (found by a flaky test, see PLAN.md).
- **Stopping:** on SIGTERM the worker stops claiming, finishes what it holds (up to 8 s) and exits;
  the compose file gives it a 10 s grace period, because the development machine's default was 1 s
  and Docker killed it mid-drain. After a SIGKILL the lease brings the rows back.
- **Completion:** a periodic sweep finishes campaigns with no queued, sending or retrying rows, so a
  crash between a delivery's last call and the check only delays it.

## D25 — Tracking as built (P5)

Decisions taken at the start of P5 (K1–K6). Evidence: PLAN.md → Verification log → P5. Contract:
[`docs/API.md`](docs/API.md) → Live dashboard, Health and metrics.

| # | Topic | Decision | Why |
|---|---|---|---|
| K1 | Metrics topology | Each process serves its own `/metrics`. The worker counts its provider calls (by channel, result and HTTP status) and times them; the api counts and times its LLM attempts and reads `scc_deliveries` (by channel and status) from the database when scraped | Counting where the event happens is cheap and the latencies are real; state that lives in the database is right after any restart when read from it. The api never calls the worker, so D7's "only Postgres between them" holds. Rejected: everything derived from the database in the api (a full aggregate per scrape, histograms in SQL); everything in the worker (deliveries by status is state, not a counter) |
| K2 | Library | `prometheus/client_golang` v1.24.1 | Correct text format, histograms and lint for free, plus Go runtime metrics (open streams show up in `go_goroutines`). v1.25.0 was 13 days old; v1.24.1 has had three months and a patch release. A hand-written exposition saves a dependency but risks escaping and bucket mistakes |
| K3 | Snapshot and stream | A full snapshot at once, then every second; final snapshot + `delivery.done`, then close; `404` / `409` as JSON before the stream for an unknown or unapproved campaign; `retry: 3000` | Idempotent snapshots make reconnects trivial (D7). `EventSource` reopens any closed stream, so the end needs an event of its own; it does not reopen after a non-200 answer, so errors before the stream are final. A finished campaign answers with its state and `done` at once, so the dashboard of a finished campaign uses the same stream |
| K4 | Recent issues | Structured fields (`kind`, `code`, `outcome`, `retry_in_ms`, `at`, raw `error`); the UI writes the sentence | The classification is the retry policy's (D6), so it belongs to the backend; the sentence and the age ("now", "3s") are presentation and change every second anyway. `kind` comes from the HTTP status alone (`no_answer` when there is none), so no error string is parsed and the schema did not change |
| K5 | Clock of the window | The api's Go clock, whole Unix seconds, `[from, to)`, ending one second before the current one; a finished campaign's window ends with its last call | `started_at` comes from the worker's Go clock and the database's `now()` runs ~1.8 ms behind (D24), so the two are never mixed. A call is logged when it ends, so the newest second is incomplete; one settling second makes each point final when shown, except a timeout logged up to 3 s later. Anchoring a finished campaign keeps its chart ("keep chart and issue history") |
| K6 | Streams at shutdown | When the api begins to shut down it cancels every open stream (dashboard and drafts) through `http.Server.RegisterOnShutdown`; other requests get `HTTP_SHUTDOWN_TIMEOUT=8s` inside a 10 s compose grace period | `Shutdown` waits for open requests and never cancels them, so one open dashboard held the shutdown for its whole timeout, and Docker killed the api after 1 s on the development machine. A draft stream ends exactly as if its client had left (nothing unfinished is stored) |

- **One transaction per snapshot:** a repeatable-read, read-only transaction, so the status, the counts and
  the series belong to the same moment (a campaign never shows `completed` next to a message still retrying).
- **The loop stops** when the request's context ends (client gone, or shutdown) or when a write fails; each
  is tested on its own. A failed snapshot is sent as `delivery.error` and retried on the next tick.
- **Calls, not messages, per second:** the series and the gauge count provider calls started, retries and
  429s included, because the 100/s limit is on calls (D4). Calls cut short by a crash or a stop are not
  logged and therefore not counted (rare; see P4).
- **Hooks instead of imports:** `llm.Generator` and `delivery.Worker` report through an `Observe` function,
  so neither package depends on Prometheus. An LLM attempt cut short because the client left is not
  reported.
- **Reconnecting is the client's job after a `502`:** while the api restarts nginx answers `502`, and
  `EventSource` gives up for good after a non-200 answer. P6 reopens the stream itself (the design's
  "Reconnecting…").
- **Not built:** a Prometheus server or Grafana in the compose file (the endpoints are there to scrape),
  a per-stream metric, and an index for "recent issues" (it reads at most a campaign's attempts through
  the existing `(campaign_id, started_at)` index).


## D26 — Frontend as built (P6)

Decisions taken at the start of P6 (K1–K11, approved together) and while building it. Evidence: PLAN.md →
Verification log → P6. The design stays a visual reference (D22); where it and these decisions differ, the
difference is named below.

| # | Topic | Decision | Why |
|---|---|---|---|
| K1 | UI library | Own small components (`AppButton`, `GlassCard`, `StatusPill`, `AppDialog`, …) on native elements; no UI library (closes D17) | The glass look is tokens and a few shapes; a themed library would have to be overridden piece by piece. Native `<dialog>` gives an inert background, Escape and a top layer. Every line can be explained in the review |
| K2 | Chart | Own SVG: the 30-value series per channel, fixed axis up to the limit, dashed 100/s line, an accessible text summary. The curve is a monotone cubic (Steffen tangents) | 2 × 30 points need no library. An ordinary spline bulges above a run of 95–100/s and would draw the line over the limit the chart is there to prove; with Steffen tangents every control point stays between its two samples (tested) |
| K3 | State | Composables (`useDraftStream`, `useDeliveryStream`, `useConfig`) over plain TypeScript in `src/logic/` (reducer, counter, snapshot wording, stream); no Pinia | Screens share only `/api/config`; each screen reloads its campaign by id, so a page reload works the same. The logic is tested without Vue |
| K4 | Server-sent events | `EventSource` plus our own reconnect: the browser's retry when an open stream ends; after a non-200 answer (the 502 while the api restarts) the browser gives up, so we wait 1, 2, 4, 8, 10 s (±20%), ask the campaign (`GET`) whether to reopen, stop or change screen, and open a new stream. The dashboard also treats 5 s without a snapshot as a lost connection. `es.close()` on `draft.done` / `delivery.done` | `EventSource` hides the status code of a failed open; the follow-up `GET` tells a restart (retry) from a 404 or an unapproved campaign (stop). A `fetch` stream reader would see the code but needs our own SSE parser, and the contract was written for `EventSource` (`retry: 3000`) |
| K5 | Counter | The backend's parser, renderer and worst-case length ported to `src/logic/content.ts` (code points, invalid variables counted as written); the backend stays the judge on save. **API addition:** the campaign view's `audience.longest_values` per channel (`content.WorstCaseValues`) | The counter must show the number the validator enforces (D15) while a message streams and for a channel the model never wrote (declined twice, timed out), when no report exists yet. A test compares the port with 720 answers recorded from the live api (`scripts/record-worst-case.mjs`) |
| K6 | Fonts | `@fontsource-variable/inter`, `/urbanist`, `/jetbrains-mono` 5.3.0 (OFL), imported in `main.ts` | Self-hosted, no request to Google Fonts; each subset has a `unicode-range`, so Turkish pages load latin-ext and nothing else (checked with `document.fonts`) |
| K7 | Tests | Vitest + `@vue/test-utils` + jsdom: logic tests, component tests with a fake `EventSource`, the recorded counter fixture, mutation checks; the end-to-end flow checked in the browser against the live stack. No Playwright | A browser test runner would add a large image and a full stack to `make test`; the API flow is covered by the backend tests and the P7 smoke script |
| K8 | Message editor | A plain textarea (an input for the one-line subject) with chips that insert a variable at the caret; variables are pills when the message is read and in the confirm dialog; previews for the fictional sample customer and the longest names | The design draws pills inside the editable text. `contenteditable` with atomic pills brings caret, Turkish keyboard, paste and undo problems; the stored template stays one plain string |
| K9 | Scope of the chrome | The top bar has only "Demo mode" (from `/api/config`); the sidebar has New campaign, Campaigns and Live sends. Search, notifications, settings, profile, Audiences and Templates are not shown (D22). The greeting follows the time of day without a name (A10: no users) | No dead controls |
| K10 | P1 status page | Removed with its tests; the backend's `/api/stats/users` stays | Replaced by the composer |
| K11 | `make test` | The frontend test stage runs `vue-tsc --build` before Vitest | A type error in a test file failed only the image build |

- **Contrast (DESIGN.md §8):** measured in the browser by compositing every text color over its glass layers
  and over the strongest glow. Three pairs were below 4.5:1 and were changed: secondary text directly on the
  page (3.99:1 → a derived `--color-text-supporting`, 5.48:1), the current step's coral label (3.61:1 → text
  color next to the coral marker, as the design asks) and the amber banner ink on a bare tint (4.20:1 → tint
  over glass, 4.92:1). The primary button's gloss ends where its label starts.
- **"Edit brief"** opens the composer filled from the campaign (`?from=<id>`); generating creates a new
  campaign, because a campaign's brief cannot be edited through the API. The old draft stays in the list.
- **Lists:** the list endpoint carries no delivery counts, so a sending campaign's card shows an indeterminate
  bar, never a percentage; finished ones are 100% done. Cards call the audience size "people": not all of them
  consented to every channel, so "recipients" would overstate it.
- **Not built:** dark theme (the design is light only), mobile layouts below 1024 px, a way to pause or cancel
  a send (A12), browser end-to-end tests in `make test`.

## D27 — Hardening and submission (P7)

Decisions taken at the start of P7 (approved together). Evidence: PLAN.md → Verification log → P7.

| # | Topic | Decision | Why |
|---|---|---|---|
| H1 | Smoke test | A Go binary, `backend/cmd/smoke` (standard library only), built into the backend image and run as a compose service behind the `smoke` profile. `make smoke` starts the stack with `LLM_PROVIDER=mock` and runs S05, S11, S18, S17 and S03 of TEST-SCENARIOS.md through the HTTP API, following each approved campaign's live stream to `delivery.done`. It exits before any request if `/api/config` does not report the mock | A reviewer needs only Docker, as for `make test`; bash + `jq` would need `jq` on the host. The mock check and the forced `LLM_PROVIDER=mock` keep it from making a paid call when `.env` holds a key. Only numbers that do not move with the clock are pinned (city and order count: S05's 79 / 45 / 49; S11's 0); time-based audiences are checked against each other (deliveries = reachable per channel, sent + failed = total) |
| H2 | Secret scan | gitleaks v8.21.2 from its Docker image, run once over every commit (`--log-opts=--all`, `--redact`), plus a pattern scan of the tracked files for personal data (home path, user name, e-mail addresses, the developer's and HR contacts' names, local IPs, key prefixes). Not a `make` target | No installation. The history matters because the repository becomes public: a key removed in a later commit would still be readable. The one finding, a delivery's random UUID idempotency key in a test log of session 03, is a false positive and is documented rather than ignored in a config file |
| H3 | api stop grace period | No change: the api already has `stop_grace_period: 10s` with `HTTP_SHUTDOWN_TIMEOUT: 8s` (added in P5) | Checked on the running stack: `StopTimeout` is 10 for the api and the worker |
| H4 | Repository visibility | Public at submission, after the history scan is clean and the P7 pull request is merged. The developer changes the setting | Reviewers can open it without being invited by GitHub user name, which the HR email does not give. Nothing in the history is secret (H2) |
| H5 | CI (bonus B4) | GitHub Actions running `make test`, in its own pull request after P7, limited to one hour; dropped and listed in the README if it is not green by then | The cheapest bonus and the last one in the cut order; kept out of the P7 pull request so submission does not wait for it |

---

## D28 — Security quick wins after the audit (P9)

A security audit on submission day (`docs/security-threat-model.md`, `docs/security-risk-register.md`,
`docs/security-release-checklist.md`) found that, with no authentication (A10), three paths let someone other than
the marketer drive the app on a reviewer's machine: the network (R-01), another page in the same browser (R-02) and
framing (R-08). P9 closes those three; the other findings stay in the risk register. Evidence: PLAN.md → Verification
log → P9.

| # | Topic | Decision | Why |
|---|---|---|---|
| S1 | Published ports | The UI and api ports are published on `127.0.0.1`, like Postgres, the worker's metrics and the mock provider. The api port stays published (health, metrics and the curl examples use it) | Both were bound to every interface: any host on the same network could create and approve campaigns. Not publishing `:8081` would have broken the documented URLs for no gain once it is loopback-only |
| S2 | CSRF defence | Browser request metadata, not a token: `Sec-Fetch-Site` must be `same-origin` or `none` when sent; on unsafe methods `Origin` must equal `Host` when sent, and a body must be `application/json` (415). Bodiless requests (`POST …/approve`) need no `Content-Type` | Keeps the API contract, the frontend, the smoke test and the curl examples unchanged on submission day. The alternative, `application/json` required on every unsafe request, would change the approve contract; it comes naturally with a content-version body for approval (risk R-11). A browser that sends neither header does not exist among current browsers |
| S3 | DNS rebinding | `Host` allowlist from `HTTP_ALLOWED_HOSTS`, default `localhost`, `127.0.0.1`, `::1`, `api` (the smoke test on the compose network); an empty setting uses the default, never "allow all". nginx forwards `$http_host`, so `Origin` and `Host` compare with their ports | A rebinding page reaches 127.0.0.1 but still sends its own domain as `Host`. `$host` would drop the port and every same-origin POST through nginx would fail the `Origin` check |
| S4 | Guard scope | `/api` only; `/healthz`, `/readyz` and `/metrics` stay outside. Rejections are logged with the reason (no bodies) | Probes and scrapers call with their own `Host` and change nothing |
| S5 | Browser headers | One nginx include with CSP (`frame-ancestors 'none'`, no `'unsafe-inline'`), `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, COOP; repeated in every location with its own `add_header`; `server_tokens off` | The build has no inline script or style (checked), so the strict CSP held through the whole flow with no violation. nginx drops server-level `add_header` in a location that sets its own, which would have left `/assets/` without them |
| S6 | `index.html` caching | `Cache-Control: no-cache` on `location /` | Found while verifying S5: the browser reused a heuristically cached `index.html` with its old headers, so framing was blocked only with a cache-busting URL |

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
| A13 | UI language | Interface text in English (like Insider One's product UI); briefs and generated messages stay in the brief's language (Turkish in the examples) |

## Open questions (closed)

Both were closed without contacting HR.

1. **Real key or mock during evaluation?** → **Both** (closed in P7: `docker compose up` without a key
   runs the mock; with a key it runs Claude, and three test scenarios were checked on the real model).
   The real-key path is treated as the primary demo (prompts, latency, error handling polished as if
   evaluators use a key); the mock LLM is a scenario catalog (D12) that tests and key-less runs rely on. *Development uses the
   developer's own `ANTHROPIC_API_KEY` with Sonnet 5.5 (D13), so the reviewer path is the tested path.*
2. **Repository visibility?** → Private during development, **public at submission** (D27 H4).

## Out of scope (listed in the README)

Authentication and multi-tenancy · delivery-receipt webhooks · quiet hours and frequency capping ·
GSM-7/UCS-2 segment billing · horizontal scaling of the worker (shared rate limiter) ·
a Prometheus server or dashboards (the `/metrics` endpoints are ready to scrape) ·
field-to-field segment conditions · re-checking consent between approval and send (the audience is
frozen at approval) · pausing or cancelling a campaign that is sending · a mock provider that
remembers keys across restarts · a smarter keyword fallback in the mock LLM (briefs outside the
catalog get a rough reading without unsupported-criteria warnings, D12).

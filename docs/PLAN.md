# Implementation Plan — Smart Campaign Composer

> Source of requirements: [`docs/CASE.md`](CASE.md). Rationale for every choice: [`DECISIONS.md`](../DECISIONS.md).
> Deadline: **2026-10-26 (Mon)**. Plan approved on 2026-10-19.

## Working agreement

1. **Plan → approve → implement → verify.** No phase starts without explicit approval.
2. Every phase ends with **verification evidence** (test output, curl transcript, clean-clone run)
   recorded in the phase's *Verification log* below — not just "it works".
3. Core flow before bonus. Anything not delivered is listed in the README.
4. Chat in Turkish; code, commits and docs in English.

## Status tracker

| Phase | Title | Status | Date |
|---|---|---|---|
| P0 | Plan & docs | ✅ done | 2026-10-19 |
| P1 | Skeleton & infrastructure | ✅ done (PR, branch `feat/p1-skeleton`) | 2026-10-19 |
| P2 | Segment DSL & audience | ✅ done (PR, branch `feat/p2-segment-dsl`) | 2026-10-19 |
| P3 | LLM layer | ✅ done (PR, branch `feat/p3-llm`) | 2026-10-19 |
| P4 | Approval & delivery pipeline | ✅ done (PR, branch `feat/p4-delivery`) | 2026-10-20 |
| P5 | Tracking (SSE, metrics, health) | ✅ done (PR, branch `feat/p5-tracking`) | 2026-10-21 |
| P6 | Frontend | ✅ done (PR, branch `feat/p6-frontend`) | 2026-10-21 |
| P7 | Hardening & submission | ✅ done (PR, branch `feat/p7-hardening`) | 2026-10-26 |
| P8 | Bonus (optional) | ✅ B4 CI only (PR, branch `feat/ci`); B1–B3 not built | 2026-10-26 |
| P9 | Security quick wins (audit R-01, R-08, R-02) | ✅ done (PR, branch `fix/security-quick-wins`) | 2026-10-26 |

---

## Requirement IDs and traceability

IDs are referenced from tests and commits so reviewers can see which requirement each test proves.

| ID | Requirement (from CASE.md) | Phase |
|---|---|---|
| R1 | Claude turns the brief into a segment rule | P3 |
| R2 | Segment rule validated in backend; LLM output never executed as SQL | P2 |
| R3 | UI shows estimated recipient count | P2 (API), P6 (UI) |
| R4 | No message on a channel the user has not consented to | P2, P4 |
| R5 | SMS + email content generated and streamed to the screen | P3, P6 |
| R6 | SMS ≤ 160 chars, email subject ≤ 80 chars | P3 |
| R7 | Personalization variables such as `{{first_name}}` | P3, P4 (render) |
| R8 | Marketer previews, edits and approves | P4 (API), P6 (UI) |
| R9 | Asynchronous queue | P4 |
| R10 | ≤ 100 messages/second per channel | P4 |
| R11 | Same message never reaches the same person twice | P4 |
| R12 | Retry strategy (own design) | P4 |
| R13 | Live delivery status (dashboard) | P5, P6 |
| R14 | Metrics endpoint | P5 |
| R15 | Health check endpoint | P1, P5 |
| R16 | Claude output is structured JSON and validated in backend | P3 |
| R17 | Tests run without a real API key, using a mock LLM | P3 |
| R18 | Key read from `ANTHROPIC_API_KEY` in `.env`; `.env.example` in repo | P1, P3 |
| R19 | Seed of several thousand fake users | P1 |
| R20 | Mock SMS/email provider producing errors, latency and 429s | P4 |
| R21 | GitHub/GitLab repository | P7 |
| R22 | One-command setup: `docker compose up` (seed + mock provider included) | P1, P7 |
| R23 | `make test` runs the tests | P1, P7 |
| R24 | README: setup + short architecture summary + what is not done | P7 |
| R25 | DECISIONS.md | P0, P7 |
| R26 | `ai-sessions/` with exports, secrets and personal data removed | P7 |
| R27 | Vue.js frontend, Go or PHP backend | all |
| U1 | *(own)* App runs with the mock LLM when no API key is set | P3 |
| U2 | *(own)* Segment rule is a whitelisted DSL compiled to a parameterized query | P2 |
| B1–B4 | Bonus: scheduled sending, A/B variants, per-user language, CI | P8 |

---

## Phases

### P0 — Plan & docs ✅
- **Goal:** lock scope and decisions before writing code.
- **Deliverables:** `docs/CASE.md`, `docs/PLAN.md`, `DECISIONS.md` (draft), `git init`.
- **Done when:** plan approved by the user.
- **Estimate:** 1–2 h.

### P1 — Skeleton & infrastructure
- **Goal:** an empty but end-to-end bootable system.
- **Deliverables:**
  - `docker-compose.yml`: `postgres`, `api`, `worker`, `mock-provider`, `frontend`, one-shot `seed`.
  - Single Go module with `cmd/{api,worker,mockprovider,seed}`, one multi-stage Dockerfile.
  - Migrations (goose), `users` table, deterministic seed of ~5,000 users.
  - `GET /healthz`; Vue 3 + Vite + TS app that calls the API and shows the result.
  - `Makefile` (`up`, `down`, `test`, `logs`, `seed`), `docker-compose.test.yml`, `.env.example`.
- **Done when:**
  - From a clean clone, `docker compose up` brings everything up with no manual step.
  - `curl localhost:<api>/healthz` → 200; `SELECT count(*) FROM users` → 5000; re-running seed does not duplicate.
  - `make test` runs inside containers and is green (at least one real test per Go package created).
- **Estimate:** 5 h. **Covers:** R15, R18, R19, R22, R23.

### P2 — Segment DSL & audience
- **Goal:** a safe segment engine, independent of the LLM.
- **Deliverables:**
  - `internal/segment`: DSL types, field registry (field → column, type, allowed operators, value rules), validator, compiler to parameterized SQL.
  - Consent filter always appended by backend, per channel.
  - `POST /api/segments/preview` → `{ total, sms_reachable, email_reachable }`.
- **Done when:**
  - Table-driven tests: valid rules, invalid rules (unknown field, wrong operator for type, out-of-range values, too many conditions), SQL-injection attempts in field names and values.
  - The sample brief's rule returns exactly the same count as a hand-written SQL query over the seed.
  - NULL semantics tested (e.g. "no purchase in last 30 days" includes users who never purchased).
- **Estimate:** 5 h. **Covers:** R2, R3 (API), R4, U2.

### P3 — LLM layer
- **Goal:** brief → validated segment rule + validated SMS/email drafts, with or without an API key.
- **Deliverables:**
  - `internal/llm`: `Client` interface; `anthropic` implementation (structured outputs); `mock` implementation driven by a scenario catalog (`internal/llm/mock/scenarios/`, embedded: happy-path and fault scenarios, streaming).
  - Prompts + JSON schemas for segment and content; two-layer validation (schema shape, then business rules).
  - Repair loop: on validation failure, re-ask with the concrete errors (max 2 retries), then surface errors.
  - `internal/content`: variable whitelist, length rules (rune-based), worst-case length check, renderer.
  - Campaign draft endpoints + SSE stream for content generation (`draft.state` with the design's states, `draft.delta`, `draft.error`, `draft.done`; DECISIONS.md D23).
  - Provider selection: `LLM_PROVIDER=auto|anthropic|mock`, model via `LLM_MODEL` (default Claude Sonnet 5.5, `claude-sonnet-5-5`, D13). The SDK client is built from explicit options.
  - **Design-driven additions (DECISIONS.md D22):**
    - Generate request: brief + selected channels + tone. The model also returns a short campaign name.
    - Per-channel draft states `writing → validating → fixing (attempt n of 3) → valid / needs_manual_edit`. SMS is validated as soon as its JSON is complete, while email may still stream.
    - Preview response adds per-channel consent exclusions and total channel messages (e.g. 99 + 133 = 232).
    - Preview rendering for a fictional sample user, plus a worst-case rendering with the audience's longest values. The API returns those longest values and the computed lengths (D15).
    - `GET /api/briefs/examples`: briefs of the happy-path mock scenarios.
    - The API reports the LLM mode (mock / anthropic).
    - Campaign statuses Draft, Ready, Sending, Completed, Failed. `segment.Describe` uses the design's wording.
  - `docs/API.md`: REST and SSE contract (endpoints, event types, states). It is the input for P6.
- **Done when:**
  - With the mock: sample brief → valid DSL → count → streamed, validated SMS + email.
  - Every fault scenario (broken JSON, unknown field, 200-char SMS, unknown variable, refusal, timeout) produces the expected repair-then-error behaviour — one test per scenario.
  - The real-key path is treated as the primary demo: prompts and error handling polished as if evaluators run with a key.
  - Smoke test on the real Anthropic API with Sonnet 5.5 (the number of calls agreed in advance). The responses are saved as fixtures under `testdata/` and replayed in tests.
  - Go SDK binding for `output_config.format` verified against the SDK source, and the 12-variant schema accepted by the real API. Fallbacks: strict tool use / a generic condition schema.
- **Estimate:** 9 h (+1 h for the design-driven additions). **Covers:** R1, R5 (API), R6, R7, R16, R17, U1.

### P4 — Approval & delivery pipeline
- **Goal:** reliable, rate-limited, exactly-once-effect sending.
- **Deliverables:**
  - Campaign state machine; approval as a conditional update (idempotent).
  - Approval freezes the audience: `deliveries` rows inserted in the same transaction, `UNIQUE(campaign_id, user_id, channel)`.
  - Worker: claim with `FOR UPDATE SKIP LOCKED` + lease (`locked_until`), per-channel rate limiter (100/s, burst 1), per-channel worker pool.
  - Retry policy: exponential backoff with full jitter for 5xx/timeouts/network errors; honour `Retry-After` on 429; permanent failure on other 4xx; max attempts → `failed`.
  - `Idempotency-Key: <delivery id>` on every provider call.
  - Mock provider: configurable latency, 5xx rate, 429 rate and its own rate limit via env; idempotency-key dedup.
  - `delivery_attempts` log: one row per provider call (HTTP status, error, `retry_after`, timestamp). It feeds the dashboard's "Recent issues" (D22).
  - Message states Queued, Sending, Sent, Retrying, Failed.
- **Done when (integration tests against real Postgres):**
  - Approving twice creates one set of deliveries.
  - Provider accepts but the response times out → retry → provider records exactly one message.
  - In any half-open 1 s window, sends per channel ≤ 100.
  - Worker killed mid-campaign and restarted → every delivery ends exactly once, none lost.
  - Users without consent for a channel receive 0 messages on it.
- **Estimate:** 8 h. **Covers:** R4, R7 (render), R8 (API), R9, R10, R11, R12, R20.

### P5 — Tracking
- **Goal:** observable sending.
- **Deliverables:**
  - `GET /api/campaigns/{id}/events` (SSE, periodic aggregate snapshots).
  - Snapshots also carry per-channel sends per second for the last 30 s (for the speed chart against the 100/s line) and the latest issues from `delivery_attempts` (D22).
  - `/metrics` (Prometheus), `/healthz` (liveness) + `/readyz` (DB ping).
- **Done when:** `curl -N` shows snapshots changing during a send, and no per-second value exceeds 100 per channel. Counters on `/metrics` increase. `/readyz` fails when Postgres is stopped.
- **Estimate:** 3 h. **Covers:** R13 (API), R14, R15.

### P6 — Frontend
- **Goal:** the whole flow usable in the browser.
- **Deliverables:**
  - Composer: brief input → segment card (rule as readable chips, explanation, counts per channel, unsupported-criteria warnings) → streamed drafts → live character counters, variable highlighting, preview rendered for a sample user → edit → approve dialog with final counts.
  - Campaign dashboard: per-channel progress, status counts, throughput, recent errors.
  - Campaign list. "Mock LLM" badge when running without a key.
  - **Visual reference:** `DESIGN.md` + `design/screens/`, visual only (D22). Glass theme tokens in one theme file. Fonts self-hosted via npm, so there is no runtime dependency on Google Fonts. Revisit D17: own small components may fit the glass style better than a themed UI library.
- **Done when:** full flow works in the browser with the mock LLM; edited content is re-validated by the backend (an over-limit edit is rejected); a few Vitest component tests pass.
- **Estimate:** 10–12 h (design-heavy). **Covers:** R3, R5, R8, R13 (UI).

### P7 — Hardening & submission
- **Deliverables:** README (setup, architecture diagram, known gaps), final DECISIONS.md, e2e smoke script, secret scan, `ai-sessions/` exports + index. Manual end-to-end scenarios (20 briefs, expected audiences and outcomes) are in [`docs/TEST-SCENARIOS.md`](TEST-SCENARIOS.md); the smoke script can reuse them.
- **Done when:** fresh clone into an empty directory → `docker compose up` and `make test` pass; secret scan is clean; no personal data in exports.
- **Estimate:** 4 h. **Covers:** R21–R26.

### P8 — Bonus (only after P7 is green), in this order
| Item | Estimate | Note |
|---|---|---|
| B4 CI (GitHub Actions running `make test`) | 1 h | cheapest bonus |
| B1 Scheduled sending | 2 h | `next_attempt_at` already exists in the queue design |
| LLM eval (`make eval`, needs a key) | 2 h | not a bonus in the case, but strengthens the LLM score |
| B3 Per-user language content | 3 h | seed already has a `language` field |
| B2 A/B variants | 4 h | |

### P9 — Security quick wins (after the security audit)
The audit of 2026-10-26 ([`security-threat-model.md`](security-threat-model.md),
[`security-risk-register.md`](security-risk-register.md), [`security-release-checklist.md`](security-release-checklist.md))
found three attack paths that work against a reviewer's machine today. P9 closes them; authentication, approval
version binding (R-11) and UUID campaign ids stay out of scope.
- **Decisions (approved):** CSRF defence from browser request metadata (`Sec-Fetch-Site`, `Origin`, `Host`), with
  `Content-Type: application/json` required only on requests that have a body, so the API contract does not change;
  the api port stays published, on `127.0.0.1`; the branch starts from `feat/ci` (PR #11 not merged yet).
- **Step 1 — R-01:** publish the UI and api ports on `127.0.0.1` only; README "Security notes".
- **Step 2 — R-08:** nginx security headers (CSP with `frame-ancestors 'none'`, `X-Frame-Options`, `nosniff`,
  `Referrer-Policy`, `Permissions-Policy`, COOP) from one include file, repeated in `location /assets/`;
  `server_tokens off`; smoke checks the headers.
- **Step 3 — R-02:** `/api` guard middleware: `Host` allowlist (`HTTP_ALLOWED_HOSTS`, default
  `localhost,127.0.0.1,::1,api`, fail closed); `Sec-Fetch-Site` must be `same-origin` or `none` when present; on
  unsafe methods `Origin` must equal `Host` and a body must be JSON (415). nginx forwards `$http_host`. Table tests,
  smoke negatives, and an attack page on another origin.
- **Step 4 — docs:** API.md "Request guards", DECISIONS.md, README, audit documents (R-01/R-02/R-08 fixed, checklist
  A-11…A-16), verification log.
- **Done when:** `make test` and `make smoke` pass; checklist A-11…A-16 give the expected output; the full flow works
  in a browser with no CSP violation; the attack page creates no campaign; CI is green.

---

## Calendar (assumes ~6–7 focused hours/day)

| Day | Date | Plan |
|---|---|---|
| 1 | Mon 19 Oct | P0 ✅ + start P1 |
| 2 | Tue 20 Oct | P1 + P2 |
| 3 | Wed 21 Oct | P3 |
| 4 | Thu 22 Oct | P4 |
| 5 | Fri 23 Oct | P5 + start P6 |
| 6 | Sat 24 Oct | P6 |
| 7 | Sun 25 Oct | P7 (+ CI bonus if time) |
| — | Mon 26 Oct | Buffer: final clean-clone check, export sessions, submit |

Core estimate ≈ 44 h, revised to ≈ 47 h after the design review (P3 +1 h, P6 +2 h; D22). P3, P4 and P6 are the most likely to overrun; the buffer is the bonus time and the morning of 26 Oct.

## Cut order (if time runs short; first item is cut first)

1. A/B variants, per-user language, scheduled sending (CI is cut last among bonuses)
2. LLM eval suite
3. Campaign list screen (keep only the current campaign)
4. Frontend unit tests (backend tests stay)
5. Prometheus format → simple JSON metrics
6. Automatic repair loop → single attempt + clear error in UI
7. Dashboard polish (recent errors, throughput) → counts + progress bars only

**Never cut:** DSL validation + parameterized compile, consent filter, schema + limit validation,
mock LLM, mock provider with real faults, queue + rate limit + retry + dedup, SSE live status,
`docker compose up`, `make test`, README, DECISIONS.md, `ai-sessions/`.

## Risks

| Risk | Mitigation |
|---|---|
| P3/P4 overrun | Bonus time + 26 Oct buffer; cut order above |
| Go SDK binding for `output_config.format` unclear | Verify in P3 against SDK source; fallback to strict tool use |
| First time with Go and Vue | Explicit code over clever code; explain idioms in PR-sized steps |
| SSE buffered by the frontend proxy | nginx `proxy_buffering off` for SSE routes; verified with `curl -N` |
| Reviewer machine differences (arm64/amd64, no Go/Node installed) | Multi-arch base images; everything runs in containers, including tests |
| Secrets/PII leaking into `ai-sessions/` | `.env` git-ignored; secret scan before submit |
| API spend during development | Smoke tests are few and agreed before each run; afterwards the recorded fixtures are replayed |
| Design-heavy frontend (glass UI) overruns P6 | Visual reference only (D22); cut order item 7 (dashboard polish) applies first |

## Target repository layout

```
smart-campaign-composer/
├── README.md · DECISIONS.md · Makefile · .env.example · .gitignore
├── docker-compose.yml          # postgres, api, worker, mock-provider, frontend, seed (one-shot)
├── docker-compose.test.yml     # make test → tests run in containers against a real postgres
├── docs/          CASE.md · PLAN.md · API.md
├── ai-sessions/   README.md (session index) + exports
├── backend/                    # single Go module, one Dockerfile, several binaries
│   ├── cmd/
│   │   ├── api/                # REST + SSE
│   │   ├── worker/             # dispatcher: claim → rate limit → send → retry
│   │   ├── mockprovider/       # fake SMS/email API: latency, 5xx, 429, idempotency
│   │   └── seed/               # deterministic fake users (idempotent)
│   ├── internal/
│   │   ├── config/
│   │   ├── db/                 # pgx pool, migrations runner
│   │   ├── segment/            # DSL types, field registry, validator, SQL compiler
│   │   ├── content/            # variables, length rules, validator, renderer
│   │   ├── llm/                # Client interface, prompts, schemas, parsing, generator (repair loop)
│   │   │   ├── anthropic/      # Claude client; testdata/recorded = real API responses
│   │   │   └── mock/           # mock LLM; scenarios/*.json embedded (not testdata: Go skips it)
│   │   ├── campaign/           # state machine, service, repository
│   │   ├── delivery/           # queue claim, rate limiter, retry policy, provider client
│   │   ├── httpapi/            # handlers, SSE, middleware
│   │   ├── tracking/           # live dashboard snapshots (P5)
│   │   └── metrics/            # Prometheus metrics of the api and the worker (P5)
│   └── migrations/
├── frontend/                   # Vue 3 + Vite + TS, served by nginx
│   └── src/ api/ · views/ (Composer, CampaignDashboard, Campaigns) · components/
└── .github/workflows/ci.yml    # bonus
```

---

## Verification log

Evidence recorded at the end of each phase.

### P0
- `docs/CASE.md` checked against the source artifact by script: 39 source sentences, all present
  (5 differ only by an added "—" separator between step title and description).

### P1

**Clean-clone acceptance run** (fresh `git clone` of the branch into an empty directory, no `.env`,
dev stack and volumes removed first, images built with `--no-cache`):

```
docker compose build --no-cache                  -> exit 0
docker compose up -d --wait                      -> exit 0 (18 s); api, frontend, mock-provider,
                                                    postgres healthy; migrate + seed exited 0
curl :8081/healthz                               -> 200
curl :8080/api/stats/users (through nginx)       -> {"total":5000,"sms_opt_in":3156,"email_opt_in":3714}
curl :8080/                                      -> <title>Smart Campaign Composer</title>
SELECT count(*) FROM users                       -> 5000
docker compose run --rm seed (second run)        -> "users already present, skipping"; count still 5000
make test                                        -> exit 0 (7 Go packages ok with -race; Vitest 3 passed)
```

**Checks made while building P1:**
- Tests can fail: a deliberately failing Go test and a failing Vitest spec each make
  `make test-backend`, `make test-frontend` and `make test` exit non-zero (status 2).
- `REQUIRE_DB_TESTS=1` without `TEST_DATABASE_URL` fails the integration tests instead of skipping.
- Mutation check on the seed: allowing SMS consent without a phone is caught twice: by the Go
  invariant test (per-user message) and by the DB constraint `users_sms_opt_in_needs_phone` on COPY.
- Seed is identical across platforms: macOS (native Go) and the Linux container produce the same
  counts (3156 SMS / 3714 email opt-ins) for seed 42.
- Concurrency: three simultaneous migrators and three simultaneous seeders → schema applied once,
  users inserted exactly once (advisory lock / `LOCK TABLE`).
- Graceful shutdown: `docker compose stop api` logs "shutting down http server"; runtime runs as
  uid 10001; backend image 80 MB.
- Bad config reports every invalid variable at once (`LOG_LEVEL` and `HTTP_SHUTDOWN_TIMEOUT` together).
- Browser check of http://localhost:8080: tiles render 5,000 / 3,156 (63%) / 3,714 (74%), no console errors.
- Sample brief audience measured on the seed (P2 will reproduce it through the DSL):
  İstanbul + cart in last 30 days + no purchase in last 30 days = **177 users**
  (99 SMS-reachable, 133 email-reachable). The plan's estimate of 250–300 assumed ~25% recent
  carts; the generator yields ~18%.

**Deviations from the P1 plan:**
- Mock provider host port is `127.0.0.1:18090`, not 8090: 8090 (and 8082) were taken on the dev
  machine, which is exactly the clash reviewers could hit. It is debug-only, so it moved to an
  uncommon port bound to localhost.
- `npm audit`: 4 high findings, all in the dev-only lint toolchain (`braces` via
  `@vue/eslint-config-typescript`); production dependencies have 0. Fix requires a breaking
  upgrade, so it is deferred; nothing from that chain ships in the nginx image.

### P2

**Correctness of the segment engine:**
- *Three-way check of the sample brief* (5,000 users, seed 42): compiled rule, hand-written SQL and an
  independent Go oracle all return **177 users, 99 SMS-reachable, 133 email-reachable**. This is
  the same number P1 measured by hand, now reproduced through the DSL.
- *Differential test:* 300 random valid rules on 3,000 users; SQL counts equal the oracle's for
  every rule, with at least 150 of the 300 rules hitting an audience that is neither empty nor
  everyone (guards against a test that passes vacuously).
- *NULL semantics* pinned on hand-made rows (never / 10 days ago / 60 days ago).
- *Mutation check:* removing the `IS NULL` branch of `not_within_last_days` fails the differential,
  three-way and NULL tests. The sample brief silently drops from **177 to 74 users** without it.

**Safety of the SQL:**
- *SQL-shape test:* compiled SQL for 2,000 random rules contains only registry columns, fixed
  keywords, operators and `$n` placeholders, with exactly one placeholder per argument.
- *Mutation check:* inlining a value into the SQL (`city = 'Bursa'`) is caught by the shape test
  and by the per-operator test.
- *Injection attempts* in field, op, value and match are all rejected by validation, before any SQL.
- *Fuzzing:* `go test -fuzz=FuzzParseValidateCompile -fuzztime 30s` ran 607,967 executions and
  found 270 interesting inputs, with no failures. The seed corpus runs in every `make test`.

**Schema for P3:**
- 12 `anyOf` variants, 4.8 KB with 15 cities.
- A test asserts it stays inside the Structured Outputs subset (closed objects, every property
  required, no `$ref` or bounds keywords). Mutation check: adding `minimum` fails it.
- The schema serializes byte-identically across calls, so the prompt cache stays valid.
- A JSON Schema validator confirms it accepts 1,000 random valid rules and rejects incompatible
  combinations (operator not allowed for the field, unknown city, value of the wrong type, extra key).

**Live stack** (`make up`, through nginx):
```
POST /api/segments/preview  (sample brief)          -> 200 {"total":177,"sms_reachable":99,"email_reachable":133}
                                                       + description lines, as_of
POST /api/segments/preview  ("istanbul", "phone")   -> 422 invalid_value (lists allowed cities) + unknown_field
POST /api/segments/preview  (no conditions)         -> 200 {"total":5000,"sms_reachable":3156,"email_reachable":3714}, "All users"
GET  /api/segments/fields                           -> 7 fields, 15 cities from the data (most common first), limits
api log                                             -> "segment registry ready", cities: 15
```

**Performance:** `EXPLAIN ANALYZE` of the sample brief: sequential scan over 5,000 rows in
**1.3 ms**. No indexes added (see DECISIONS.md D9).

**Test suite:** `make test` exit 0. The segment package has 109 tests and subtests, including
integration tests against Postgres in the container.

### P3

**Real API (Claude Sonnet 5.5, Go SDK v1.82.0), 11 paid calls in total (≤ $0.07):**
- *SDK binding (D8):* `OutputConfigParam{Effort, Format: JSONOutputFormatParam{Schema}}` exists in the SDK
  source, so Structured Outputs is used directly. `option.WithoutEnvironmentDefaults()` stops the SDK from
  reading base URLs, tokens or profiles from the environment (D12).
- *Smoke test A* (`go test -tags live`, 8 calls, responses recorded under
  `internal/llm/anthropic/testdata/recorded/`). The 12-variant segment schema is accepted by the real API.

  | Call | Effort | Total | First text | Input / cache read / cache write / output | Result |
  |---|---|---|---|---|---|
  | segment, sample brief | low | 3.76 s | – | 102 / 0 / 4,413 / 133 | 3 conditions, "Cart reminder — İstanbul" |
  | segment, sample brief | medium | – | – | 102 / 4,413 / 0 / 3,636 (3,532 thinking) | **declined** (`reasoning_extraction`) after part of the JSON |
  | segment, İstanbul or Ankara, 90 days | low | 2.04 s | – | 90 / 4,413 / 0 / 107 | `city in [...]` + no purchase in 90 days |
  | segment, "kadın kullanıcılar" | low | 1.75 s | – | 71 / 4,413 / 0 / 95 | cart in 60 days; unsupported: "kadın kullanıcılar (cinsiyet)" |
  | SMS | low | 2.45 s | 1.84 s | 256 / 0 / 540 / 69 | worst case 116 / 160 |
  | email | low | 11.43 s | 9.88 s | 254 / 0 / 622 / 182 | subject 49 / 80 |
  | SMS | medium | 1.69 s | 1.28 s | 256 / 540 / 0 / 70 | worst case 105 / 160 |
  | email | medium | 10.00 s | 8.83 s | 254 / 622 / 0 / 174 | subject 56 / 80 |

  Outcome: effort `low` for every task (medium was no better and produced the only refusal); a refusal is
  retried once (D23); a prompt rule forbids suffixes on variables after the email wrote `{{city}}'daki`
  (wrong for İzmir). The prompt cache works for both the segment prefix (4,413 tokens) and the content prefixes.
- *Smoke test B* (the app itself in anthropic mode, 3 calls): `POST /api/campaigns` 2.81 s with the design's
  three audience lines; SMS first text 2.48 s, valid at 2.73 s (worst case 124 / 160); email first text
  1.60 s, valid at 3.17 s (subject 56 / 80). The email used "`{{city}} için`", so the suffix rule worked. The
  ~9 s email delay from smoke test A did not recur. Reopening the stream replayed the stored drafts with no
  model call.

**Validation and repair (mock LLM, all in `make test`):**
- *One test per fault scenario*, each pinned to the exact state sequence the UI shows (`TestFaultScenarios`):
  broken JSON → `writing1 validating1 fixing2 validating2 valid2`; 200-character SMS → three attempts then
  `needs_manual_edit3`; unknown variable → repaired on attempt 2; refusal → declined twice is final
  (`writing1 fixing2 needs_manual_edit2`), declined once recovers; timeout → `fixing2 fixing3
  needs_manual_edit3`; unknown field (segment) → repaired on attempt 2, with the validator's message quoted
  in the repair prompt.
- *Scenario catalog consistency:* every scenario declares its outcome (`valid@N` / `needs_manual_edit`), and
  a test recomputes it from the scripted answers with the real validators and the attempt budget.
- *Recorded Claude answers* are replayed through the real SDK and the whole generator (segment valid on
  attempt 1; SMS `writing → validating → valid`; a real refusal is a typed error even after partial JSON).
- *Content rules:* 160 `ş` (320 bytes) valid / 161 rejected; worst case uses the longest values of the
  channel's recipients ("Muhammed Emin", "Montgomery-Fitzgerald"); a property test renders every accepted
  template for random recipients and never exceeds 160; Postgres `char_length` equals Go's rune count on
  the whole seed; `FuzzValidate` (361,258 execs) and `FuzzPartialStrings` (2,396,842 execs) found nothing.
- *Streaming:* for every prefix of 300 random answers the decoded field values are prefixes of the final
  values; SMS reaches `valid` while the email is still streaming; a disconnect stops the work and stores
  nothing.

**Mutation checks.** Each change was made and had to compile, the named tests failed, and the change
was reverted with `git status` clean afterwards. A mutant that does not compile proves nothing; its build
error must not be mistaken for a test result.

| Mutation | Caught by |
|---|---|
| `auto` ignores the key | `TestLoadLLMProviderSelection` |
| `maxLength` in the SMS schema | schema subset test |
| strict decoding removed | 3 parse tests |
| refusal treated as `end_turn` | 2 stop-reason tests + the recorded refusal |
| rune count → `len()` | 3 content tests |
| worst case → raw template length | 3 content tests (see the note below) |
| channel filter dropped from the longest-value query | oracle comparison (13 vs 11 characters) |
| example SMS lengthened past 160 | catalog consistency test |
| `MaxAttempts` 3 → 2 | 4 generator tests |
| over-long text silently cut to 160 | `TestNeverSilentlyTruncates` + fault test |
| first refusal final | 2 generator tests |
| campaign status always `ready` | 2 campaign tests |

*Note on the worst-case mutation:* the first attempt replaced `RuneCountInString(rendered)` with
`RuneCountInString(f.value)`, which left `rendered` unused, so the mutant did not compile
(`declared and not used: rendered`). The test output had been filtered to `--- FAIL` lines, which hid the
`[build failed]` line, and the following `ok (cached)` belonged to the run after the revert. At the time
this was misreported as "the change did not apply". Reproduced on 2026-10-19 with unfiltered output; the
mutation that compiles (`RuneCountInString(f.value) + 0*len(rendered)`) is caught by the 3 tests above.

**Live stack** (through nginx, mock mode unless noted):
```
GET  /api/config                    -> {"llm":{"mode":"mock","model":null}}  (anthropic mode: model claude-sonnet-5-5)
LLM_PROVIDER=anthropic, no key      -> api exits: "LLM_PROVIDER=anthropic needs ANTHROPIC_API_KEY"
POST /api/segments/preview          -> 175 people, SMS 98 (77 excluded), email 132 (43 excluded), 230 messages
GET  /api/briefs/examples           -> 5 examples
POST /api/campaigns (sample brief)  -> 201 "Cart reminder — İstanbul", draft, in 0.44 s
GET  .../draft/stream (curl -N)     -> 66 events over 1.75 s; SMS valid at 0.99 s while the email streamed; draft.done ready
PUT  .../messages/sms (161 chars)   -> 422 with the report
```
Live counts are 175, not 177: the seed's timestamps are fixed at seed time while "the last 30 days" moves
with the clock. Tests pin `asOf` and still get 177 / 99 / 133.

**Test suite:** `make test` exit 0 (13 Go packages with `-race` against Postgres, Vitest 3 passed).

**Follow-up after review (2026-10-19):**
- *Email first-token delay* (~9 s in smoke test A): measured by phase with `go test -tags live -run
  '^TestEmailLatency$'` (4 calls, ≤ $0.016). Response headers, `message_start`, the text block and the first
  text delta arrive together, with no thinking block (0 thinking tokens):

  | Call | Headers = first text | End | Note |
  |---|---|---|---|
  | email #1 | 1.93 s | 3.64 s | prompt cache written |
  | email #2 | 1.16 s | 3.05 s | cache read |
  | email #3 | 1.15 s | 2.85 s | cache read |
  | SMS (control) | 3.15 s | 3.62 s | prompt cache written |

  The 9 s delay did not recur in four later samples (smoke test B and #1–#3). The whole wait is before the API
  sends its response headers, so it is on the API side, not in our streaming code. Closed as transient; the
  per-attempt timeout (60 s) already covers a slow start, and the card shows "Writing…" meanwhile.
- *Untested guard:* removing the "do not save after the client left" check passed every test (a save
  with the dead request fails anyway). `TestClientLeavingStoresNothing` now also requires no `draft.error`
  and no logged save failure; removing the check fails it 3 of 3 times, and the intact code passes 10 of
  10 runs with `-race`.

**Deviations from the P3 plan:**
- SSE event names follow the design's states (`draft.state` + `draft.delta` + `draft.error` + `draft.done`)
  instead of `draft.validated` / `draft.invalid` / `draft.repairing` (decision K6, D23).
- The mock scenario catalog lives in `internal/llm/mock/scenarios/` (embedded), not `testdata/` (K5).
- A refusal is retried once within the 3 attempts, instead of never (decided after smoke test A, D23).

### P4

All integration tests run against real Postgres (`-race`). Timing-dependent tests were repeated: the
`delivery` package five times in a row, 100 of 100 test runs passed.

**Done criteria:**

| Criterion | Test | Result |
|---|---|---|
| Approving twice creates one set of deliveries | `TestConcurrentApprovalsCreateOneSet` (10 concurrent approvals, then one more) | one set of 232, every caller gets the same answer |
| Provider accepts, the answer times out, retry, exactly one message | `TestAnswerLostAfterAccept` | provider holds 1 message, 1 slow answer + 1 replay; log `retry  no answer within 200ms \| sent Accepted` |
| ≤ 100 sends per channel in any half-open 1 s window | `TestRateLimitPerChannel` (400 + 400, instant provider), `TestSpacerHoldsTheLimitInEveryWindow` | busiest window 93–97 per channel on `started_at` on the Mac, 92–93 in the Linux test container, the same at the provider; 300 sends from 32 goroutines take ≥ 2.99 s and are ≥ 10 ms apart |
| Worker killed mid-campaign and restarted: every delivery ends exactly once, none lost | `TestWorkerKilledMidCampaign` (worker in a child process, SIGKILL at ~200 sent with ~60 rows claimed, second process) | 1,000 of 1,000 final, one final attempt row each, one provider message per key; 0–6 replays per run |
| No message on a channel without consent | `TestApprovalFreezesTheAudience`, `TestRecipientsMatchOracle` (100 random rules), `TestSampleCampaignEndToEnd` | deliveries = preview (99 + 133); every provider message traces back to a consenting user |

**Other evidence:**
- *End to end:* the sample brief drafted with the mock LLM, approved and sent through a provider with
  5% 503, 5% 429, 2% slow accepts, 2% rejections and its own limit: 228 sent, 4 failed (the 4
  rejected recipients), campaign `completed`, every call logged.
- *Retry matrix* (`TestRetryMatrix`, scripted per recipient): 503 then OK → sent after 2; four 503s then
  OK → sent after 5; always 503 → failed after 5; 400 → failed after 1; one 429 → sent, Retry-After and
  retry time logged; always 429 → failed after 10; six 429s, four 503s, then OK → sent on call 11 (K7).
- *Approval re-validation:* a user with an 80-character name added after drafting → `422` with the SMS
  report, nothing written, campaign stays `ready`.
- *Rendering:* every one of the 232 deliveries equals the template rendered with its recipient's data;
  `content.EmailHTML` escapes `<script>`.

**Mutation checks.** Each mutant compiled, the named tests failed, and the file was restored and
compared with its backup (`cmp`) afterwards. A scratch script refused to continue if the replacement
did not apply exactly once or the mutant did not build.

| Mutation | Caught by |
|---|---|
| SMS length CHECK dropped from `deliveries` | `TestDeliveryConstraints/SMS_of_161_characters` |
| Approval reads the campaign without the row lock | `TestConcurrentApprovalsCreateOneSet` (unique key violation) |
| SMS recipients without the consent check | `TestRecipientsMatchOracle` (2,747 vs 1,847), 4 approval tests (159 SMS instead of 99) |
| Approval skips the re-validation | `TestApprovalRevalidatesAgainstTheFrozenAudience` (the DB CHECK rejects the row instead: 500, not 422) |
| Provider does not store accepted answers | 3 provider tests |
| Provider stores a 503 as a final answer | `TestFailuresAreNotStored` |
| Limiter interval 9 ms | `TestSpacerHoldsTheLimitInEveryWindow` (102 in one window) |
| A 400 is retried | `TestRetryPolicy/400_fails_at_once` |
| A 429 uses up an error attempt | `TestRetryPolicy/4_errors_after_6_429s_still_retry` |
| A new Idempotency-Key on every call | 7 tests, including the end-to-end and kill tests |
| Claim without `FOR UPDATE SKIP LOCKED` | `TestClaimTakesDisjointRows` (a row claimed 4 times) |
| Claim ignores expired leases | 3 tests; the kill test ends with `sending=60 sent=940` |
| Finish without the attempt fence | `TestStaleClaimCannotFinish` |
| A campaign with nothing sent ends `completed` | `TestCampaignOutcome` |
| Senders stop at once on SIGTERM (no drain) | `TestGracefulStopLeavesNothingClaimed`, every run |
| Claim cancelled by the stop signal | `TestGracefulStopLeavesNothingClaimed`, 3 of 5 runs (timing-dependent race) |

*Invalid first attempt:* the first run of the row-lock mutation had a wrong backup path; `cp` failed,
the `&&` chain skipped the edit, and `go test` printed `ok` for the intact code. It was not counted. The
scratch script above was written after it.

**Problems found while verifying, and fixed:**
- *A test hung for 5 minutes:* Go's HTTP server notices a client that left only after the request body
  has been read. A test handler that never read the body was never cancelled. The mock provider had the
  same blind spot (a slow answer kept waiting after the worker gave up); both now read to the end.
- *A flaky retry-time check (2 of 5 runs):* `next_attempt_at` came from Postgres's `now()` while
  `finished_at` came from Go. Measured: the database clock is 1.77 ms behind the host under Docker on a
  Mac. The retry time is now computed from the Go clock; 20 of 20 runs pass.
- *A row left in `sending` after a graceful stop (live):* two causes. A stop arriving during a claim
  query cancelled it although its update could already have leased rows; the claim now completes
  (commit `fix(worker)`). And the worker never stopped gracefully on this machine: it exited with
  `137` after about a second because every container had `StopTimeout=1`. The compose file now gives
  the worker `stop_grace_period: 10s`. Afterwards: SIGTERM mid-campaign → exit 0, `worker stopped`,
  no row left in `sending`.

**Live stack** (`LLM_PROVIDER=mock`; `/api/config` checked before each run, so no paid call):
```
POST /approve on a draft                     -> 409 not ready
POST /approve twice (sample brief)           -> identical 200 bodies {"sms":98,"email":131,"total":229}; preview 98/131/229
PUT  /messages/sms after approval            -> 409
worker started later                         -> campaign 4: 228 sent, 1 failed (400 invalid recipient), completed in ~5 s
Weekend sale (3,714 emails), SIGTERM after 8 s, restart, SIGKILL after 8 s, restart:
  deliveries                                 -> 3,683 sent, 31 failed; one final attempt row each
  provider (delta, not restarted)            -> accepted 3,683 = sent, rejected 31 = failed, replayed 22 = slow answers 22
  busiest half-open second                   -> 94 calls (started_at) and 94 at the provider; steady 90-92/s
  calls started vs logged                    -> 4,049 vs 3,997: the 52 rows claimed at the SIGKILL
  recent issues (attempt log)                -> "429 provider busy, retry in 2.1s", "no answer within 3s, retry in 1.5s"
```
The first live run's provider numbers were discarded: `docker compose up -d worker` after the SIGKILL
recreated the mock provider too and wiped its memory mid-campaign. Later runs restart only the worker
(`docker compose start worker`) and print the provider's start time before and after.

**Real Claude, end to end** (Claude Sonnet 5.5, 3 paid calls, agreed beforehand; `/api/config` checked before the
first call and set back to mock afterwards):
```
POST /api/campaigns (sample brief)   -> 201 in 8.4 s, attempt 1 (102 input / 133 output tokens): the same three
                                        conditions as the mock scenario; 166 people, 91 SMS + 125 email = 216
GET  .../draft/stream                 -> 3.2 s, sms and email both "writing1 validating1 valid1"; status ready
                                        SMS worst case 124 / 160 (it ends with an emoji), subject 61 / 80;
                                        the email uses {{first_name}} and {{city}}
POST .../approve (twice)              -> identical 200 bodies {"sms":91,"email":125,"total":216}
worker                                -> completed 4.8 s after approval: 215 sent, 1 failed (400 invalid recipient);
                                        230 calls, all logged (10 x 503, 3 x 429, 1 timeout, then sent)
checks                                -> all 216 deliveries equal the template with the recipient's first name and
                                        city substituted in SQL (independent of the Go renderer); none still holds
                                        "{{"; none without consent; longest rendered SMS 124 = the predicted worst
                                        case; provider accepted 215 = sent, rejected 1, replayed 1 (the timeout)
```
The emoji counts as one character in our counter (Unicode characters, A1); on a real SMS network it would force UCS-2
and take two UTF-16 units. Segment billing stays out of scope.

**Test suite:** `make test` exit 0 (14 Go packages with `-race` against Postgres in containers, Vitest 3 passed).
The `delivery` package takes about 30 s, most of it the rate, kill and graceful-stop tests.

**Follow-up after P4: no emoji** (branch `feat/no-emoji`, DECISIONS.md D15). Prompted by the emoji in the real-API SMS.
- The content prompts forbid emoji; the validator rejects them in every field with code `emoji`.
- `TestEmojiAreRejected`: 9 kinds rejected in SMS text, subject and body (face, sparkles, star, check mark, heart with
  variation selector, flag, keycap, zero-width-joiner family, a mathematical digit outside the BMP); ordinary Turkish
  text with %, ₺, ©, ™, quotes, dash and arrow passes; the message names each emoji once and invisible ones by code point.
- New fault scenario `fault-emoji`: `writing1 validating1 fixing2 validating2 valid2`; the repair prompt quotes
  `text contains emoji ("😊")` (`TestEmojiRepair`).
- Mutation checks: disabling the check fails 9 subtests, the fault scenario, the repair test and the catalog consistency
  test; dropping the BMP symbol blocks fails the sparkles, star and check-mark cases (the heart is still caught by its
  U+FE0F).
- Live (mock LLM): an SMS edit ending in 😊 → `422` with code `emoji`; an email subject starting with ✨ → `422`; the
  stored message and the `ready` status are unchanged.
- *Flaky P4 test found on the way:* `make test` failed once because `TestGracefulStopLeavesNothingClaimed` required a
  drain under 1 s and measured 1.48 s in the container (no row was left claimed). It now sets a 30 s drain timeout and
  requires the drain to end within 10 s; senders that stop at once and a drain that waits for the timeout both still
  fail it. `make test` exit 0 afterwards.

**Deviations from the P4 plan:**
- Rate limiter: our own spacer instead of `golang.org/x/time/rate` (D4, D24 K6); no new dependency.
- Idempotency key: a UUID per delivery instead of the delivery id (D5, K8).
- 429s have their own budget of 10 instead of counting among the 5 attempts (D6, K7).
- `make reseed` now also empties campaigns (K9).
- Estimate: about 10 h instead of 8 h.

### P5

All integration tests run against real Postgres (`-race`). The timing-dependent tests (the `httpapi` stream and
shutdown tests, `metrics`, `llm`) were each run five times in a row; every run passed.

**Done criteria:**

| Criterion | Evidence | Result |
|---|---|---|
| `curl -N …/events` shows changing snapshots during a send | live, through nginx: weekend sale (3,714 emails) and new season (1,226 SMS) approved together | 48 and 20 snapshots; done 0 → 3,714 and 0 → 1,226, never going back; then `delivery.done` and curl exits 0 |
| No per-second value above 100 per channel, compared with a SQL window query on `delivery_attempts` | live; `TestLiveDashboardDuringASend` | highest value in any snapshot 93 (email) / 94 (SMS); busiest half-open 1 s window in the log 94 / 94; the provider's own `peak_per_second` 94 / 94. In the test: 96–99 over 5 runs |
| Snapshot counts equal the database (integration test, real Postgres) | `TestCountsPerChannelAndStatus`, `TestLiveDashboardDuringASend` | exact per channel and status; the five states add up to the total in every snapshot; the final snapshot equals the table |
| A reconnect gets the whole state | `TestLiveDashboardDuringASend` (a second client mid-send), `TestEventsOfAFinishedCampaign`; live after an api restart | the first event is a complete snapshot |
| The loop stops when the client leaves | `TestEventsStopWhenTheClientLeaves` (writes never fail, so only the context can stop it), `TestEventsStopWhenWritingFails` | the handler returns and reads no further snapshot; live, the api's `go_goroutines` went 10 → 15 with two streams open → 10 after they closed |
| Counters on `/metrics` increase during a send | live | worker calls 0 → 898 → 2,672 → 5,287 (before, at 5 s, at 15 s, at the end); the api's `scc_deliveries` queued email 3,214 → 2,284 between 5 s and 15 s |
| `/readyz` fails when Postgres is stopped and recovers | live; `TestReadyzWithAStoppedDatabase` | `503` after the 2 s ping timeout (2.03 s), `/healthz` `200` meanwhile; `200` on the first request after Postgres was healthy again |

**The series against the attempt log (live):** 945 + 172 points of the captured snapshots compared with an independent
`date_trunc('second', started_at)` count. None was above the log. 46 were below, by 1–3, each in a second that had at
least that many timeouts and seen less than 5 s later: a timeout is logged 3 s after its call started, after the
settling second (D25 K5). The final snapshots equal the log in all 30 seconds.

**Metrics against the ledger:** live, the worker's counters after the run (5,287 calls: 4,890 sent; 347 retried, of them
162 × 429, 153 × 503 and 32 without an answer; 50 rejected) equal `delivery_attempts` grouped by channel, outcome and
status. `TestWorkerCountersMatchTheAttemptLog` asserts the same equality, and the deliveries gauge against the table,
with 8% 503, 8% 429, 3% slow answers and 3% rejections.

**Shutdown with open streams (live):** `docker compose stop api` with one dashboard open on the api and one through
nginx took 0.43 s, exit code 0, and both streams ended without `delivery.done`. While the api was down nginx answered
`502` (after which `EventSource` stops retrying; P6 reconnects itself, docs/API.md). After `docker compose start api`
the reopened stream gave the campaign's completed state and `delivery.done` at once; the worker had finished it
meanwhile.

**Database down (live):** the api's `/metrics` still answered `200` without `scc_deliveries`, and logged
`metrics could not be gathered … scc_deliveries …` with the failed connection.

**Mutation checks.** A scratch script refused to go on unless the tree was clean, the replacement applied exactly once
and the mutant passed `go build ./...` and `go vet`; it ran the package tests unfiltered (`-race -count=1`), then
restored the file, compared it with its backup (`cmp`) and required a clean `git status`. All 19 mutants compiled.

| Mutation | Caught by |
|---|---|
| `/readyz` ignores the ping | `TestReadyz`, `TestReadyzWithAStoppedDatabase` |
| window's lower bound exclusive | `TestPerSecondSeries`, `TestFinishedCampaignKeepsItsLastWindow` |
| no settling second | `TestPerSecondSeries` |
| done counts retrying messages | `TestCountsPerChannelAndStatus` |
| issues include sent calls | `TestRecentIssues` |
| a finished campaign's window follows the clock | `TestFinishedCampaignKeepsItsLastWindow` |
| the stream ignores the client leaving | `TestEventsStopWhenTheClientLeaves` |
| the stream ignores failed writes | `TestEventsStopWhenWritingFails` |
| a finished campaign keeps streaming | 4 tests, through the test client's 60 s timeout |
| an unapproved campaign streams | `TestEventsAnswersBeforeTheStream/draft` and `/ready` |
| streams ignore the shutdown | `TestShutdownEndsDashboardStreams` (`context deadline exceeded`), `TestShutdownEndsDraftStreams` |
| the draft stream ignores the shutdown | `TestShutdownEndsDraftStreams` (both channels stored, `draft.done` sent) |
| 429 counted as 503 | `TestWorkerCountersMatchTheAttemptLog` |
| repairs not counted | `TestLLMMetrics` |
| the gauge drops `retrying` | `TestWorkerCountersMatchTheAttemptLog` (after the fix below) |
| the worker never calls its hook | `TestWorkerCountersMatchTheAttemptLog` |
| an LLM timeout reported as `error` | `TestObserveReportsEveryAttempt` |
| LLM attempts the caller cut short are reported | `TestObserveReportsEveryAttempt` |
| the metrics error logged as a value | `TestMetricsWithoutTheDatabase` |

*A surviving mutant:* dropping `retrying` from the gauge first passed every test, because the test looped over the
code's own list of statuses. The test now lists them itself (commit `test(metrics)`), and the mutant fails it.
*On reading the results:* one batch of mutation runs was shown through an extra terminal filter that also hid the
`--- FAIL` lines; the failing test names above were read from the full logs the script keeps.

**Problems found while verifying, and fixed:**
- *The metrics error log said nothing:* promhttp passes the error inside a list, which the JSON log handler printed as
  `{}`. It is now logged as text (commit `fix(metrics)`), and `TestMetricsWithoutTheDatabase` requires the cause in
  the log.
- *The gauge test above.*

**Test suite:** `make test` exit 0 (16 Go packages with `-race` against Postgres in containers, Vitest 3 passed).

**Deviations from the P5 plan** (approved before implementation, D25):
- Two `/metrics` endpoints (api and worker) instead of one, and deliveries by status as a gauge instead of a counter
  (D18 revised).
- Draft streams also end when the api shuts down (P3 code), and `llm.Generator` and `delivery.Worker` got `Observe`
  hooks.
- New dependency `github.com/prometheus/client_golang` v1.24.1 (7 indirect modules, and one more used only by its test
  helpers).
- `MessagesPerSecond` moved from `cmd/worker` to the `delivery` package, so the snapshot reports the limit.
- Estimate: 3 h in the plan, revised to about 8 h when P5 started (two endpoints, shutdown behaviour, depth of
  verification).

### P6

Everything below ran against the live stack with the mock LLM (`LLM_PROVIDER=mock`, `/api/config` checked before
creating campaigns), first through Vite's dev server and at the end through the production build behind nginx
(`:8080`). No paid call was made.

**Done criteria:**

| Criterion | Evidence | Result |
|---|---|---|
| The whole flow works in the browser with the mock LLM | production build: Brief library → "Cart reminder — İstanbul" → Generate → draft → Review & approve → Send messages → dashboard | SMS valid after ~1.0 s while the email was still being written, email valid at ~1.8 s, status Ready; approval opened the dashboard, which ended Completed with 213 of 213 done (SMS 89 sent, 1 failed; email 123 sent) and its chart and issues kept |
| An over-limit edit is refused by the backend and shown in the UI | `[test] Every SMS answer is 200 characters long…` → SMS "Needs manual edit" → Edit → Save as it was | `422`; next to the field: "text is 201 characters with the longest values in this audience filled in ({{first_name}} = "Muhammed Emin", 13 characters); the limit is 160. Shorten it by at least 41 characters."; the counter showed "41 over limit 201 / 160" before saving. A shortened text with `{{first_name}}` inserted at the caret saved as Valid (79 / 160) and the campaign became Ready |
| The dashboard follows the stream, shows Reconnecting… during `docker compose stop api` / `start api`, and carries on without resetting | weekend sale, 3,714 emails; the page's numbers sampled every 200 ms | live 150 → 826 done; Reconnecting… within about a second of `docker compose stop api`, banner "Showing the last update from 15:03:28", numbers kept at 826; after `start api` four `502`s (backoff), then live again at 2,458; never lower than before; Completed at 3,714 (3,683 sent, 31 failed). No request after `delivery.done` |
| The counter equals the backend's `worst_case_length` (test) | `content.spec.ts`: the port against 720 answers recorded from the live api by `scripts/record-worst-case.mjs` | equal for all 720 (broken variables, Turkish letters, combining marks, characters outside the BMP, SMS text and email subject, two audiences with three sets of longest values). Live: SMS 109 / 160 and subject 46 / 80 on screen = the report's 109 and 46; `audience.longest_values` = each report's `longest_values` |
| Vitest tests for the draft stream and snapshot logic, plus component tests; `make test` green | `make test` | exit 0: 16 Go packages (`-race`, Postgres); frontend `vue-tsc --build`, then 12 files / 108 tests |
| 1440 and 1024 px layouts, keyboard and focus, reduced motion | in-app browser | see below; reduced motion could not be emulated there |

**The draft states on screen** (VIP thanks — İzmir, the SMS repaired on attempt 2, recorded with a `MutationObserver`):
SMS Writing… with the counter growing, "Near limit" at 146, "18 over limit" at 178, Validating…, "Fixing… · attempt 2
of 3" with the old text kept, the new attempt's text replacing it from 3 / 160, Validating…, Valid at 109 / 160; the email
became Valid while the SMS was being fixed. `[test] The SMS is declined twice…`: "Needs manual edit" with the refusal's
category, "No text yet.", Review & approve disabled with the reason in words.

**Layout and accessibility (in-app browser):**
- 1440 and 1024 px: composer, draft, confirm dialog, dashboard, campaign list; at 1024 the draft columns, the channel
  cards and chart/issues stack (one 736 px column), the list has two columns and the dialog fits (640 px); no
  horizontal overflow (`scrollWidth = clientWidth`) on any of them.
- 200% text (root font size doubled) at 1024 px: no overflow; the sidebar's items were cut off ("Compo", "Campai"),
  so its widths are now in rem; afterwards no element in it overflows.
- Keyboard: Tab order brand → New campaign → Campaigns → Live sends → brief → SMS → Email → Tone → Brief library →
  campaign cards, each with the 2 px focus ring (the tone chip draws it with `:focus-within`); in the dialog focus
  stays inside, Escape closes it and focus returns to the button that opened it.
- Contrast, every text color composited over its glass layers and over the strongest glow (sky at 70%), AA 4.5:1:
  three pairs failed and were fixed (D26): secondary text on the page 3.99 → 5.48, the current step label 3.61 →
  13.14, the amber banner 4.20 → 4.92. Others, worst case: secondary on glass 4.74, white on the AA gradient 4.90 /
  5.81, status pills 4.92–7.75, counter near/over 5.59 / 9.70, variable pills 6.07 / 7.30.
- Fonts: a Turkish page loads Inter and Urbanist latin-ext; `document.fonts.check` is true for "İzmir" in Urbanist
  and "ğşıİüöçâ" in Inter; counters use JetBrains Mono.
- Reduced motion: the `prefers-reduced-motion: reduce` rule (no animations or transitions) is in the loaded CSS, but
  the in-app browser cannot emulate the setting (`matchMedia` stays false). Checked in P7 with macOS "Reduce motion"
  turned on: see P7.

**Mutation checks.** Same procedure as P5, with a scratch script: clean tree, the replacement applied exactly once,
the mutant passes `vue-tsc --build` and `vite build` (`go build` and `go vet` for the backend), the whole suite runs
unfiltered with its full log kept, then the file is restored, compared with its backup (`cmp`) and `git status` must
be clean.

| Mutation | Caught by |
|---|---|
| a draft delta is appended instead of replacing | `replaces a field with each delta instead of appending` |
| fixing clears the text at once | `keeps the old text while fixing, then the new attempt replaces all of it` |
| a late delta of a replaced attempt is applied | `ignores a late delta of an attempt that was already replaced` |
| the counter uses `str.length` (UTF-16 units) | `counts code points, not UTF-16 units` + the 720 recorded answers |
| the counter uses the template, not the worst case | the recorded answers + 2 `worstCaseLength` tests |
| `{{ first_name }}` is filled in like a variable | `keeps text that is not a valid variable exactly as written` + the recorded answers |
| "Near limit" only above 90% | `144 of 160 is near`, `72 of 80 is near` |
| the done event does not close the stream | `closes the stream on the done event and never reopens it` |
| CLOSED is left to the browser (no own reconnect) | `reopens the stream itself after the browser gave up…`, `stops when the check says…` |
| the backoff is not reset by a received event | `reopens the stream itself after the browser gave up, with growing waits` |
| an issue's age is measured with the browser clock | `is measured against the snapshot's own time` |
| a failed 429/503/timeout reads "retrying" | 3 `issueText` cases |
| progress may read 100% before the end | `231 of 232 reads 99 percent` |
| ordinary spline tangents | `never draws above the highest sample` |
| (api) raw longest values instead of the worst case | `TestViewHasLongestValuesPerChannel` (fallback when every name is shorter) |
| (api) every channel instead of the selected ones | `TestViewHasLongestValuesPerChannel` (SMS-only campaign) |
| (api) only channels that already have a message | `TestViewHasLongestValuesPerChannel` (before drafting) |
| a loading button stays pressable | `confirms with the final numbers and approves once…` |
| `approve()` without its guard | **survived** at first (the disabled button hid it); now `approves once even if the dialog confirms twice…` |
| the dashboard never replaces its first snapshot | `keeps the last numbers while reconnecting, then replaces them` |
| "Waiting for fresh data" is never cleared | `marks the numbers as old when a snapshot could not be read` |
| a ready campaign is not sent back from the dashboard | `sends a campaign that is not approved yet back to its draft` |
| a 422 report is not shown at the fields | `shows a rejected edit next to its field and keeps the text` |
| approval allowed while a message is being edited | `blocks approval while a message is being edited` |
| the draft stream opens although every message is stored | 4 `CampaignDraftView` tests |

*Invalid attempts, not counted:* one replacement matched two places in `CampaignLiveView.vue` (the script refused it);
its next form (`'draft' && 'ready'`) did not compile (`TS2367`, no overlap). The compiling form (`'draft'` only) is in
the table.

**Repeated runs:** the timing-based tests use fake timers; the whole Vitest suite was run five times in a row after
S2 and after S6 and three times after S4 and S7, every run green.

**Problems found while verifying, and fixed:**
- *A reused screen:* going from `/campaigns/20` to `/campaigns/21` kept campaign 20 on screen, because the router
  reused the component. The router view is now keyed by path.
- *Contrast and 200% text:* see above.
- *The nav item went unreadable for 160 ms* after approving (the background changed slowly, the text color at once).
- *The confirm dialog let the cards behind it show through* its 72% white where the browser did not apply the
  backdrop blur; the dialog is now opaque.
- *Legend numbers read as "Sent844"* to a screen reader (no space between the label and the count).
- *Extra `/events` requests* in one network log turned out to be Vite reloading the open dashboard while its file was
  being written; a fresh load opens one stream for a finished campaign and nothing more.

**Deviations from the P6 plan** (approved before implementation, D26):
- Backend: the campaign view gained `audience.longest_values`, `content.worstValues` became `WorstCaseValues`, a Go
  test was added, and docs/API.md documents the field.
- `make test` type-checks the frontend first (`frontend/Dockerfile`).
- New dependencies: the three `@fontsource-variable` packages, 5.3.0 (published 2026-07-19).
- New files beyond the plan: `scripts/record-worst-case.mjs` and its fixture, a Vitest setup file (jsdom's `<dialog>`
  has no `showModal`).
- Estimate: 18.5 h planned for S1–S9; P6 was finished on 2026-10-21.

### P7

Run on 2026-10-26. Every campaign was created with the mock LLM (`/api/config` read first); no paid call was made.

**Done criteria:**

| Criterion | Evidence | Result |
|---|---|---|
| Fresh clone into an empty directory → `docker compose up` without `.env` | `gh repo clone … --branch feat/p7-hardening` into an empty scratch directory (no `.env`), the dev stack stopped first; `docker compose -p scc-clean up -d --build --wait` with `ANTHROPIC_API_KEY` and `LLM_PROVIDER` unset (own project name, so a new empty volume) | every service healthy, migrate and seed exited 0 (19 s with a warm Docker cache); `/api/config` → `{"llm":{"mode":"mock","model":null}}`; seed log `inserted 5000, random_seed 42`; `/api/stats/users` → 5,000 / 3,156 / 3,714; `/healthz`, `/readyz` ok; UI `200` |
| `make test` passes in the clone | `make test` in the clone | exit 0: 16 Go packages `ok` (`-race`, Postgres), frontend 12 files / 108 tests |
| e2e smoke passes | `make smoke` in the clone (`COMPOSE_PROJECT_NAME=scc-clean`) and on the dev stack | 40 checks passed, 0 failed, 31 s, both times. On the fresh seed S18 reached 175 / 225 and S03 1,276, the "right after a reseed" numbers of TEST-SCENARIOS.md |
| Secret scan is clean | gitleaks v8.21.2 (Docker), `git` mode over every commit (`--log-opts=--all`, 84 commits), `--redact` | one finding, a false positive (below); the new session 05 export: no leaks |
| No personal data in exports | pattern scan of every tracked file (home path, user name, e-mail addresses, the developer's and HR contacts' names, local IPs, `sk-ant-` strings longer than the prefix) | one name left in session 03, masked (below); other matches are the patterns themselves inside earlier grep commands, fake seed names, a test IP (`10.0.0.5`) and the GitHub owner name, which stays |

**Smoke test** (`make smoke`, D27 H1). Before the first run against the stack, the binary was pointed at a static
server answering `/api/config` with `"mode":"anthropic"`: it printed why and exited 1 without another request. What
it checks:

| Scenario | Checks |
|---|---|
| S05 | audience 79 / 45 / 49 (pinned: city and order count do not depend on the clock); SMS `writing1 validating1 fixing2 validating2 valid2`, email `writing1 validating1 valid1`; approve `200` with 45 + 49 = 94; approving again returns the same campaign and counts |
| S11 | audience 0; the email is still valid; approve `422` "no one in this audience can be reached on the selected channels"; status stays `ready` |
| S18 | SMS `… fixing3 validating3 needs_manual_edit3`; approve `409`; edit `200` (source `edited`, status `ready`); deliveries = reachable per channel |
| S17 | rejected edits `too_long`, `emoji`, `unknown_variable` (`422` with that code); a valid edit `200` |
| S03 | unsupported `kadın kullanıcılar`; SMS only; deliveries `{sms: reachable, email: 0}` |
| S05, S18, S03 | the live stream followed to `delivery.done`: status `completed`, `done` = total, sent + failed = total per channel |

**Secret scan findings:**
- gitleaks `generic-api-key` in `ai-sessions/03-2026-10-19-delivery-p4.md` line 4365 (commit `2d1b086`): a test log
  from a P4 mutation check printing a claimed delivery, whose `Key` is the delivery's idempotency key, a UUID from
  `idempotency_key UUID … DEFAULT gen_random_uuid()` (`backend/migrations/0003_create_deliveries.sql`) in a throwaway
  test database. Not a secret; documented here instead of a `.gitleaksignore` (approved).
- The pattern scan found the developer's first name, lower-case inside a regex, in session 03's printed copy of the
  converter's source (the same line is masked in session 04). Masked in the current file; the earlier commit still
  has it.

**Hardening candidates:**
- api stop grace period: already `stop_grace_period: 10s` with `HTTP_SHUTDOWN_TIMEOUT: 8s` since P5 (`2632b5e`).
  `docker inspect -f '{{.Config.StopTimeout}}'` on the running stack: api 10, worker 10 (the frontend keeps the
  machine's 1 s; nginx has no long work to finish). No change (D27 H3).
- Reduced motion, with macOS "Reduce motion" turned on, in the in-app browser against the production build:
  `matchMedia('(prefers-reduced-motion: reduce)').matches` is true, and on the composer, a draft (`/campaigns/10`) and
  a live dashboard (`/campaigns/11/live`) no element has an animation or transition longer than 10 ms (0 of 155,
  165 and 233 elements). Not checked while a draft was streaming.

**Not verified:** a clean clone on a machine without cached Docker images (the base images and build layers were
already present here), and an amd64 host.

### P8

Only B4 (CI) was built, in its own pull request after P7 (D27 H5). `.github/workflows/ci.yml` runs `make test` on
`ubuntu-24.04` for every push to `main` and every pull request, with read-only permissions and no secrets.

| Criterion | Evidence | Result |
|---|---|---|
| `make test` passes on GitHub Actions | run `40244495916` on the CI pull request (commit `68bffe4`) | `success` in 3 min 45 s: 16 Go packages `ok`, frontend 12 files / 108 tests, no `FAIL` in the log |

B1 (scheduled sending), B2 (A/B variants), B3 (per-user language) and the LLM eval suite were cut (cut order, items 1 and 2).

---

### P9

Run on 2026-10-26 on the local stack in mock mode (`/api/config` read before every browser or API check); no paid
call was made. The full before/after table is in [`security-risk-register.md`](security-risk-register.md) §5.

**Done criteria:**

| Criterion | Evidence | Result |
|---|---|---|
| `make test` passes | `make test` after commit `3cfbb5e` | exit 0: 16 Go packages `ok` (`-race`, Postgres), frontend 12 test files |
| `make smoke` passes | `make smoke` | 52 checks passed, 0 failed (31–32 s): the five scenarios, 6 UI-header checks, 6 request-guard checks (2 through nginx) |
| Checklist A-11…A-16 | the commands of [`security-release-checklist.md`](security-release-checklist.md), run verbatim | no `0.0.0.0`/`[::]` binding; `415`; `403`; `403`; 4 headers on `/` and on an asset; `Server: nginx` |
| Not reachable from the network | `curl` to the machine's LAN address, `:8080` and `:8081` | before: `200` / `200`; after: connection refused |
| Full flow in a browser, no CSP violation | built-in browser: brief library → generate → draft stream → edit SMS (PUT) → approve → live dashboard, with a `securitypolicyviolation` listener | every request `200`; 0 violations; no console error; campaign completed (2,118 messages) |
| Framing blocked | page on `http://127.0.0.1:8765` with `<iframe src="http://localhost:8080/?nocache=…">` | blank; `frame-ancestors 'none'` violation; `ERR_BLOCKED_BY_RESPONSE` |
| Attack page creates nothing | page on `http://127.0.0.1:8765`: `text/plain` create, bodiless approve of campaigns 1 and 26, `EventSource` + `<img>` on a draft stream, `text/plain` form POST | all `403`; campaigns 27 → 27, deliveries 7,299 → 7,299, campaigns 1 and 26 still `ready`; rejections logged with reasons |
| Guard tests catch regressions | four hand-made mutations of `guard.go` (accept cross-site, skip Host, skip JSON body, skip Origin) | each one fails `go test -run Guard` |
| CI green | — | pending: needs the pull request |

**Found while verifying:** the first framing test still showed the app. The browser had reused a heuristically cached
`index.html` (served without `Cache-Control`) with its old headers; a cache-busting URL was blocked. `location /` now
sends `Cache-Control: no-cache` (D28 S6).

**Not done here (on purpose):** PR #11 (CI) could not be merged from this session (the merge needs the developer), so
this branch starts from `feat/ci`; its pull request shows the CI commits until #11 is merged.

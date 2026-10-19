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
| P3 | LLM layer | ⏳ next | — |
| P4 | Approval & delivery pipeline | ⬜ | — |
| P5 | Tracking (SSE, metrics, health) | ⬜ | — |
| P6 | Frontend | ⬜ | — |
| P7 | Hardening & submission | ⬜ | — |
| P8 | Bonus (optional) | ⬜ | — |

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
  - `internal/llm`: `Client` interface; `anthropic` implementation (structured outputs); `mock` implementation driven by a scenario catalog (`testdata/llm-scenarios/`: happy-path and fault scenarios, streaming).
  - Prompts + JSON schemas for segment and content; two-layer validation (schema shape, then business rules).
  - Repair loop: on validation failure, re-ask with the concrete errors (max 2 retries), then surface errors.
  - `internal/content`: variable whitelist, length rules (rune-based), worst-case length check, renderer.
  - Campaign draft endpoints + SSE stream for content generation (`draft.delta`, `draft.validated`, `draft.invalid`, `draft.repairing`).
  - Provider selection: `LLM_PROVIDER=auto|anthropic|mock`.
- **Done when:**
  - With the mock: sample brief → valid DSL → count → streamed, validated SMS + email.
  - Every fault scenario (broken JSON, unknown field, 200-char SMS, unknown variable, refusal, timeout) produces the expected repair-then-error behaviour — one test per scenario.
  - The real-key path is treated as the primary demo: prompts and error handling polished as if evaluators run with a key.
  - Manual smoke test with a real key; the real responses are saved as fixtures under `testdata/` and replayed in tests.
  - Go SDK binding for `output_config.format` verified against the SDK source (fallback: strict tool use).
- **Estimate:** 8 h. **Covers:** R1, R5 (API), R6, R7, R16, R17, U1.

### P4 — Approval & delivery pipeline
- **Goal:** reliable, rate-limited, exactly-once-effect sending.
- **Deliverables:**
  - Campaign state machine; approval as a conditional update (idempotent).
  - Approval freezes the audience: `deliveries` rows inserted in the same transaction, `UNIQUE(campaign_id, user_id, channel)`.
  - Worker: claim with `FOR UPDATE SKIP LOCKED` + lease (`locked_until`), per-channel rate limiter (100/s, burst 1), per-channel worker pool.
  - Retry policy: exponential backoff with full jitter for 5xx/timeouts/network errors; honour `Retry-After` on 429; permanent failure on other 4xx; max attempts → `failed`.
  - `Idempotency-Key: <delivery id>` on every provider call.
  - Mock provider: configurable latency, 5xx rate, 429 rate and its own rate limit via env; idempotency-key dedup.
- **Done when (integration tests against real Postgres):**
  - Approving twice creates one set of deliveries.
  - Provider accepts but the response times out → retry → provider records exactly one message.
  - In any half-open 1 s window, sends per channel ≤ 100.
  - Worker killed mid-campaign and restarted → every delivery ends exactly once, none lost.
  - Users without consent for a channel receive 0 messages on it.
- **Estimate:** 8 h. **Covers:** R4, R7 (render), R8 (API), R9, R10, R11, R12, R20.

### P5 — Tracking
- **Goal:** observable sending.
- **Deliverables:** `GET /api/campaigns/{id}/events` (SSE, periodic aggregate snapshots), `/metrics` (Prometheus), `/healthz` (liveness) + `/readyz` (DB ping).
- **Done when:** `curl -N` shows snapshots changing during a send; counters on `/metrics` increase; `/readyz` fails when Postgres is stopped.
- **Estimate:** 3 h. **Covers:** R13 (API), R14, R15.

### P6 — Frontend
- **Goal:** the whole flow usable in the browser.
- **Deliverables:**
  - Composer: brief input → segment card (rule as readable chips, explanation, counts per channel, unsupported-criteria warnings) → streamed drafts → live character counters, variable highlighting, preview rendered for a sample user → edit → approve dialog with final counts.
  - Campaign dashboard: per-channel progress, status counts, throughput, recent errors.
  - Campaign list. "Mock LLM" badge when running without a key.
- **Done when:** full flow works in the browser with the mock LLM; edited content is re-validated by the backend (an over-limit edit is rejected); a few Vitest component tests pass.
- **Estimate:** 10 h. **Covers:** R3, R5, R8, R13 (UI).

### P7 — Hardening & submission
- **Deliverables:** README (setup, architecture diagram, known gaps), final DECISIONS.md, e2e smoke script, secret scan, `ai-sessions/` exports + index.
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

Core estimate ≈ 44 h. P3 and P4 are the most likely to overrun; the buffer is the bonus time and the morning of 26 Oct.

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

## Target repository layout

```
smart-campaign-composer/
├── README.md · DECISIONS.md · Makefile · .env.example · .gitignore
├── docker-compose.yml          # postgres, api, worker, mock-provider, frontend, seed (one-shot)
├── docker-compose.test.yml     # make test → tests run in containers against a real postgres
├── docs/          CASE.md · PLAN.md
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
│   │   ├── llm/                # Client interface, anthropic/, mock/, prompts, schemas, repair loop
│   │   ├── campaign/           # state machine, service, repository
│   │   ├── delivery/           # queue claim, rate limiter, retry policy, provider client
│   │   ├── httpapi/            # handlers, SSE, middleware
│   │   └── metrics/
│   ├── migrations/
│   └── testdata/               # LLM fixtures (recorded real responses + faulty cases)
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
_pending_

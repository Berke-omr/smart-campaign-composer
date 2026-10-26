# Security Risk Register — Smart Campaign Composer

| | |
|---|---|
| **Audit date** | 2026-10-26 |
| **Code under review** | branch `feat/ci`, commit `68bffe4` |
| **Scope, method, architecture** | [`security-threat-model.md`](security-threat-model.md) |
| **Release gate** | [`security-release-checklist.md`](security-release-checklist.md) |
| **Status** | R-02 and R-08 **fixed**, R-01 **partly fixed** (network exposure closed, authentication open) in P9, branch `fix/security-quick-wins` ([§5](#5-remediation-log)); every other finding **Open** |
| **Line references** | `file:line` evidence points to the audited commit `68bffe4` (`git show 68bffe4:<path>`); P9 changed some of those files |

## 1. Rating method

- **Likelihood (L)** and **Impact (I)** on 1–5, rated for a **production-like deployment with real
  customer data**, because that is what a release decision has to protect. **Score = L × I**:
  15–25 **High**, 8–14 **Medium**, 1–7 **Low**.
- **Demo context** states the effect today, where reviewers run the stack locally on fake seed data
  with a mock SMS/email provider and possibly their own Anthropic key.
- **Priority** follows the reference catalogue: **P0** must be closed before any shared or production
  use; **P1** in the first production release; **P2** maturity.
- **Targets** are milestones rather than dates:
  **M1** before the stack is reachable by anyone but its operator (or the repository is made public),
  **M2** before real customer data or a real provider is connected,
  **M3** production hardening.
- **Risk owner** for every item is the repository owner (developer), as the only role in the project.
- **Evidence** types: `file:line` (code/config), *test* (existing automated test), *probe* (harmless
  request against the local stack on 2026-10-26, LLM mode `mock`), *scan* (pattern count, no values
  printed).

## 2. Ranked summary

| Rank | ID | Finding | Priority | L | I | Score | Demo context | Target | Status |
|---|---|---|---|---|---|---|---|---|---|
| 1 | [R-01](#r-01-no-authentication-and-the-ui-and-api-listen-on-every-interface) | No authentication; UI and API listen on every interface | P0 | 5 | 5 | **25 High** | Anyone on the same network can use the app and trigger paid LLM calls | M1 | Partly fixed (P9) |
| 2 | [R-02](#r-02-cross-site-request-forgery-and-dns-rebinding-can-drive-the-whole-flow-up-to-approval) | CSRF and DNS rebinding can drive the flow up to approval | P0 | 3 | 5 | **15 High** | A web page open in the reviewer's browser can create and approve campaigns | M1 | Fixed (P9) |
| 3 | [R-03](#r-03-no-ceiling-on-llm-spend-or-request-volume) | No ceiling on LLM spend or request volume | P0 | 3 | 4 | **12 Medium** | Reviewer's own key is exposed to cost burn via R-01/R-02 | M1 | Open |
| 4 | [R-04](#r-04-customer-names-are-sent-to-the-llm-provider-and-returned-by-the-api) | Customer names go to the LLM provider and back out of the API | P0 | 4 | 3 | **12 Medium** | Seed data is fake: no real exposure today | M2 | Open |
| 5 | [R-05](#r-05-no-way-to-stop-a-campaign-that-is-sending) | No way to stop a campaign that is sending | P0 | 3 | 4 | **12 Medium** | Mock provider: no real messages | M2 | Open |
| 6 | [R-06](#r-06-retention-erasure-and-legal-basis-for-personal-data-are-undefined) | Retention, erasure and legal basis undefined | P0 (conditional: real data) | 3 | 4 | **12 Medium** | Not applicable to fake data | M2 | Open |
| 7 | [R-07](#r-07-every-service-uses-one-postgres-superuser-role) | Every service uses one Postgres superuser role | P0 | 2 | 5 | **10 Medium** | Local only; port bound to 127.0.0.1 | M2 | Open |
| 8 | [R-08](#r-08-no-browser-security-headers-the-approval-dialog-can-be-framed) | No browser security headers; the approval dialog can be framed | P0 | 2 | 4 | **8 Medium** | Clickjacking possible from any page | M1 | Fixed (P9) |
| 9 | [R-09](#r-09-prompt-injection-and-content-integrity-are-bounded-but-untested) | Prompt injection and content integrity are bounded but untested | P1 | 3 | 3 | **9 Medium** | Marketer reviews every message | M2 | Open |
| 10 | [R-10](#r-10-supply-chain-and-ci-gates-are-thin) | Supply-chain and CI gates are thin | P1 | 2 | 4 | **8 Medium** | Matters once the repo is public | M1 (branch protection) / M3 | Open |
| 11 | [R-11](#r-11-approval-is-not-bound-to-the-reviewed-content-and-there-is-no-second-approver) | Approval not bound to the reviewed content; no second approver | P1 | 2 | 3 | **6 Low** | Single user, one tab | M2 | Open |
| 12 | [R-12](#r-12-no-audit-trail-of-who-did-what-no-alerting) | No audit trail of who did what; no alerting | P1 | 3 | 2 | **6 Low** | Logs are enough for a demo | M3 | Open |
| 13 | [R-13](#r-13-secret-lifecycle-and-container-hardening) | Secret lifecycle and container hardening | P2 | 2 | 3 | **6 Low** | Rotate the key after the demo | M1 (rotation) / M3 | Open |
| 14 | [R-14](#r-14-minor-exposure-and-robustness-items) | Minor exposure and robustness items | P2 | 2 | 1 | **2 Low** | — | M3 | Open |

Fix order for the least effort per risk removed: R-01 port binding and R-08 headers are configuration
changes of a few lines; R-02's Content-Type/Origin/Host middleware is one small Go file with tests; these
three together close the attack chains that work **today** against a reviewer's machine. *(Done in P9, see [§5](#5-remediation-log).)*

## 3. Findings

### R-01 No authentication, and the UI and API listen on every interface

| Field | |
|---|---|
| **Asset / flow** | All `/api/*` routes: campaigns, audience counts, drafts, edit, **approve** (→ mass messaging), dashboard |
| **Threat scenario** | Any host that can reach the machine opens `http://<host>:8080` (or `:8081`), reads campaigns and the customer names in message reports, burns LLM credit, edits drafts and approves sends (threat model T2) |
| **Current control** | None in code. Out of scope by assumption A10 ("single marketer, no authentication", `DECISIONS.md:644`), listed in `README.md:139-141`. Internal services (Postgres, worker metrics, mock provider) are bound to `127.0.0.1` |
| **Evidence** | `backend/internal/httpapi/router.go:81-84` (middleware: request id, logger, recoverer only), `:109-142` (routes); `docker-compose.yml:79` (`"${API_HOST_PORT:-8081}:8080"`) and `:154` (`"${UI_HOST_PORT:-8080}:80"`) have no host IP; *probe* `docker compose ps`: `0.0.0.0:8081->8080`, `0.0.0.0:8080->80` (also `[::]`) |
| **Gap** | No identity, no authorisation per route, publicly bound ports, no audit of the actor |
| **Likelihood** | 5 in production (an unauthenticated internet-facing admin tool is found quickly); 2 in the demo (needs a shared network) |
| **Impact** | Confidentiality (customer data), integrity (campaign content), financial (LLM spend, SMS cost), legal/reputational (unwanted commercial messages): 5 |
| **Autonomy / spread** | One request approves a campaign to the whole audience; sending then proceeds autonomously at 100 msg/s per channel |
| **Inherent risk** | 25 — High |
| **Recommended fix** | **M1:** bind published ports to loopback: `"127.0.0.1:${UI_HOST_PORT:-8080}:80"`; do not publish the api port at all (the UI proxies `/api`) or bind it to `127.0.0.1`. **M2:** authenticate every `/api` route (OIDC/SSO at a reverse proxy such as oauth2-proxy, or a middleware validating a session/JWT); authorise per action with at least two roles, `marketer` (create/edit) and `approver` (approve); keep `/healthz` and `/readyz` unauthenticated on an internal port |
| **Compensating control** | Until fixed: run the stack only on a trusted network; stop it when not in use; use `LLM_PROVIDER=mock` when not demonstrating Claude |
| **Residual risk** | After M1: Low for the demo (local browser only, see R-02). After M2: Low |
| **Verification test** | `docker compose ps` shows only `127.0.0.1:` bindings; from another host `curl http://<ip>:8080` fails. After M2: an httptest table over every `/api` route asserts 401 without credentials and 403 for `marketer` on approve |
| **Status** | **Partly fixed (P9):** ports published on `127.0.0.1` (`docker-compose.yml`, commit `8e2fcb9`); from the LAN address both ports now refuse the connection (before: 200). Authentication (M2) remains open and accepted for the take-home by A10; it must not be carried into any shared deployment |

### R-02 Cross-site request forgery and DNS rebinding can drive the whole flow up to approval

| Field | |
|---|---|
| **Asset / flow** | `POST /api/campaigns`, `GET /api/campaigns/{id}/draft/stream`, `PUT …/messages/{channel}`, `POST …/approve` |
| **Threat scenario** | Threat model T1/T3. A page the marketer has open sends a `text/plain` POST (a CORS "simple request", so no preflight) to create a campaign, opens the draft stream for the next sequential ids (an `EventSource` or `<img>` suffices; the server generates and stores drafts although the response is unreadable cross-origin), then POSTs approve. With DNS rebinding the page can also **read** every response |
| **Current control** | No CORS headers are sent, so a plain cross-origin read is blocked. Approval requires a `ready` campaign and re-validates content (`backend/internal/campaign/service.go:307-335`) |
| **Evidence** | `backend/internal/httpapi/campaigns.go:35-53` (`readJSON` never checks `Content-Type`); `:135-146` (approve reads no body); `:55-62` + `backend/migrations/0002_create_campaigns.sql:6` (sequential `BIGINT` ids); `:151-173` (generation and DB writes on `GET`); `router.go:81-84` (no Origin/Host check). *Probes:* `POST /api/segments/preview` with `Content-Type: text/plain` and `Origin: http://attacker.example` → **200** on both `:8081` and `:8080`; `GET /api/config` with `Host: attacker.example` → **200** |
| **Gap** | No Content-Type enforcement, no Origin / `Sec-Fetch-Site` check, no Host allowlist, state change on GET, guessable ids. The full chain was **not executed** (it would create and send campaigns); see threat model §11 |
| **Likelihood** | 3 (needs the marketer to visit a hostile page while the app runs; a known, scriptable technique against localhost tools) |
| **Impact** | 5: bypasses the human approval gate, the system's main safety control |
| **Autonomy / spread** | Whole audience, irreversible |
| **Inherent risk** | 15 — High |
| **Recommended fix** | Add one middleware for unsafe methods (POST/PUT/PATCH/DELETE): (1) reject any body whose `Content-Type` is not `application/json` with **415** — this forces a CORS preflight that the api never approves; (2) reject when `Sec-Fetch-Site` is `cross-site`, or when `Origin` is present and not in an allowlist (`http://localhost:8080`, `http://127.0.0.1:8080`, configured public origin), with **403**. Add a **Host allowlist** for all requests (421/403) against DNS rebinding. Make approve require a JSON body (it carries the content version of R-11, so it cannot be a bodiless POST). Move draft *generation* behind `POST …/draft` (keep `GET …/draft/stream` replay-only), or at least reject the GET stream when `Sec-Fetch-Site` is `cross-site`. Once R-01 adds cookie sessions: `SameSite=Strict` plus the same Origin check |
| **Compensating control** | Close the stack when not in use; use a separate browser profile for the demo |
| **Residual risk** | Low |
| **Verification test** | httptest cases: `text/plain` POST → 415; `Origin: http://evil.example` → 403; `Sec-Fetch-Site: cross-site` → 403; `Host: evil.example` → 403/421; same-origin JSON → unchanged behaviour. Manual: a test page on another port tries the chain and fails with no campaign created |
| **Status** | **Fixed (P9, commit `3cfbb5e`):** `/api` request guard (`backend/internal/httpapi/guard.go`) with Host allowlist, `Sec-Fetch-Site`, `Origin` = `Host` and JSON-body rules; nginx forwards `$http_host`. Approve still takes no body, so the content-version binding of R-11 remains open. Evidence in [§5](#5-remediation-log) |

### R-03 No ceiling on LLM spend or request volume

| Field | |
|---|---|
| **Asset / flow** | Anthropic API key and budget; api availability |
| **Threat scenario** | Threat model T5. Scripted `POST /api/campaigns` plus draft streams (via R-01/R-02, or a careless loop) burn credit. Worst case per campaign: 3 tasks (segment, SMS, email) × 3 attempts × up to 3 HTTP tries (1 + `LLM_MAX_RETRIES=2`) = **27 model calls**, each up to 8,000 output tokens |
| **Current control** | Per-attempt timeout 60 s and SDK retries 2 (`backend/internal/config/config.go:86-87`); 3-attempt budget, second refusal final (`backend/internal/llm/generate.go:15-17,85-101`); `max_tokens` per task (`backend/internal/llm/anthropic/client.go:55-59`); one draft stream per campaign per process (`service.go:258-275`); brief ≤ 2,000 runes (`service.go:21`); finished drafts are replayed without model calls (`service.go:187-191`); usage logged per request (`service.go:107-108,247-248`) |
| **Evidence** | No rate limiter in `router.go:81-142`; metrics count attempts only, not tokens (`backend/internal/metrics/metrics.go:104-113`; *probe* metric names: `scc_llm_attempt_duration_seconds`, `scc_llm_attempts_total`, `scc_llm_repairs_total`) |
| **Gap** | No per-client or global rate limit, no cap on concurrent model calls, no token/cost budget with a hard stop, no token metric or alert. Whether the Anthropic workspace has a spend limit is unverified |
| **Likelihood** | 3 |
| **Impact** | 4 (financial; api saturation blocks legitimate users) |
| **Autonomy / spread** | Each request fans out into up to 27 paid calls without a human in the loop |
| **Inherent risk** | 12 — Medium |
| **Recommended fix** | Rate-limit `POST /api/campaigns` and draft generation per client (for example `go-chi/httprate`, a few per minute) and add a global semaphore on concurrent model calls; record `Usage` into a counter `scc_llm_tokens_total{task,kind}`; enforce a daily token budget that returns 503 with a clear message when exhausted (fail closed); alert at 50/80/100 %; set a spend limit on the Anthropic workspace; consider `LLM_MAX_RETRIES=1` for interactive requests |
| **Compensating control** | Keep `LLM_PROVIDER=mock` except during deliberate Claude demos; a reviewer should use a key with a low spend limit |
| **Residual risk** | Low |
| **Verification test** | Load test: the N+1-th create within the window → 429; with the budget set to a small number, the next create → 503 and no outbound call (mock client counts calls) |
| **Status** | Open |

### R-04 Customer names are sent to the LLM provider and returned by the API

| Field | |
|---|---|
| **Asset / flow** | `users.first_name`, `last_name`, `city` → content prompt → Anthropic; and → `GET /api/campaigns/{id}` |
| **Threat scenario** | Threat model T6/T7. For every draft and every repair, the single longest first name, last name and city in the audience are quoted into the prompt. With real data this is personal data transferred to a third-party processor abroad. The same values are returned in `audience.longest_values` and in every message report's preview |
| **Current control** | Only three fields leave; never email, phone, purchase data or ids (`DECISIONS.md` D9: names, email and phone are not targetable) |
| **Evidence** | `backend/internal/llm/prompts.go:154-158` (`fmt.Fprintf(&b, "- %s: %d (%q)\n", …, val)`); golden file `backend/internal/llm/testdata/prompts/email_user.txt` shows real-looking names in the prompt; repair texts quote the values (`backend/internal/content/validate.go:136-151`, used by the repair loop); API exposure `backend/internal/campaign/service.go:408-417` and `backend/internal/content/validate.go:64-72` (`LongestValues`, `Preview.WorstCase`) |
| **Gap** | Data minimisation: the model only needs the **length** of each value, not the value |
| **Likelihood** | 4 (happens on every draft) |
| **Impact** | 3 (a few names per campaign; legal exposure under KVKK if no basis for transfer) |
| **Autonomy / spread** | Automatic, every campaign |
| **Inherent risk** | 12 — Medium |
| **Recommended fix** | Send only the character count of each variable, or a synthetic placeholder of the same length (e.g. `"XXXXXXXXXXXXX"`); keep validating against the real values on the server (no change to `content.Validate`). Split `tooLongMessage` into a UI message and a model message without values. In the API, return lengths (or same-length placeholders) instead of the real longest values; the UI counter only needs lengths. This also removes the indirect-injection path of R-09 |
| **Compensating control** | Use only fake data (true today); zero-data-retention terms with the provider |
| **Residual risk** | Low (only the brief, written by the marketer, leaves the system) |
| **Verification test** | Golden prompt files contain no seed names (`grep` in `TestPromptGolden`); a test asserts that a prompt built for an audience never contains any `users.first_name` of that audience; API test asserts `longest_values` carries no real value |
| **Status** | Open (no real data today) |

### R-05 No way to stop a campaign that is sending

| Field | |
|---|---|
| **Asset / flow** | Sending campaigns; provider calls |
| **Threat scenario** | Threat model T9. A wrong audience, wrong content or an approval obtained through R-02/R-08 cannot be stopped; the only option is stopping the worker container, which stops **all** campaigns and leaves no record |
| **Current control** | Content locked after approval (A12); confirm dialog states "This can't be undone" (`ConfirmSendModal.vue:81-84`) |
| **Evidence** | Statuses are `draft, ready, sending, completed, failed` only (`backend/migrations/0002_create_campaigns.sql:12-13`); known gap in `README.md:143-148` and `DECISIONS.md:614,646` |
| **Gap** | No per-campaign pause/cancel, no global send switch, no runbook |
| **Likelihood** | 3 (human error in mass messaging is common) |
| **Impact** | 4 (irreversible messages to customers; legal/reputational) |
| **Autonomy / spread** | 100 msg/s per channel: 5,000 people in under a minute |
| **Inherent risk** | 12 — Medium |
| **Recommended fix** | Add `paused`/`cancelled` campaign statuses; make the worker's claim query only take deliveries whose campaign is `sending`; add an authorised `POST /api/campaigns/{id}/cancel` that marks remaining deliveries `cancelled` in one transaction; add a global `SENDING_ENABLED` flag (DB row or env) checked before each claim batch; document the runbook (who presses it, how to verify) |
| **Compensating control** | `docker compose stop worker` (stops everything; leases bring rows back on restart) |
| **Residual risk** | Low (messages already accepted by the provider remain sent) |
| **Verification test** | Integration test: start a campaign, cancel after N sends → no provider call after the cancel commits; remaining rows `cancelled`; dashboard shows the final state |
| **Status** | Open (known gap) |

### R-06 Retention, erasure and legal basis for personal data are undefined

| Field | |
|---|---|
| **Asset / flow** | `deliveries` (recipient address + rendered text), `campaigns.brief`, `users`; transfer to the LLM provider |
| **Threat scenario** | Personal data accumulates indefinitely; a data-subject erasure request cannot be fulfilled; messages are sent without a checked legal basis |
| **Current control** | Consent flags per channel, applied by the backend (`backend/internal/segment/audience.go:52-60`); no PII in logs (`backend/internal/delivery/worker.go:148-168`) |
| **Evidence** | `backend/migrations/0003_create_deliveries.sql:19` (`user_id … REFERENCES users (id)` without `ON DELETE`: deleting a user who has deliveries fails), `:21-24` (address and rendered content kept), no purge job anywhere in `backend/cmd/*` |
| **Gap** | No retention period, no erasure path, no processing record; legal review of KVKK (controller/processor roles, cross-border transfer to the LLM provider) and of the commercial electronic messages rules (consent registry, İYS, for Turkish recipients) has not been done — **not verifiable from code** |
| **Likelihood** | 3 |
| **Impact** | 4 (legal, regulatory) |
| **Autonomy / spread** | All customers |
| **Inherent risk** | 12 — Medium (applies only with real data) |
| **Recommended fix** | Define retention (e.g. null out rendered `text/subject/body` and `recipient` after N days, keep status counts); add an erasure procedure that anonymises `deliveries` and deletes the user; decide `ON DELETE` behaviour explicitly; record the processing purpose and legal basis; obtain legal sign-off; check consent against the authoritative registry at approval and, for long sends, at send time (README already lists "consent is not re-checked between approval and send") |
| **Compensating control** | Fake data only (true today) |
| **Residual risk** | Low after legal sign-off and retention job |
| **Verification test** | Integration test: erasing a user with deliveries succeeds and leaves no address or name; a purge job test with a fake clock |
| **Status** | Open (conditional on real data; legal part unverified) |

### R-07 Every service uses one Postgres superuser role

| Field | |
|---|---|
| **Asset / flow** | Database; host running Postgres |
| **Threat scenario** | Threat model T11. Any SQL injection or code execution in the api, worker or seed job inherits a superuser connection: read/modify every table, `COPY … TO PROGRAM` (OS command execution as the postgres user), drop the schema. Long queries are not bounded |
| **Current control** | No SQL injection found: the compiler emits only registry identifiers and bind parameters (`backend/internal/segment/compile.go:23-24`), tests `segment/validate_test.go:114`, `compile_test.go:150`; port bound to `127.0.0.1` (`docker-compose.yml:35`) |
| **Evidence** | One `DATABASE_URL` for all services with static `scc`/`scc` and `sslmode=disable` (`docker-compose.yml:15-17,30-31`); *probe*: `rolsuper = t` for `scc`, `statement_timeout = 0` |
| **Gap** | No least privilege, no statement timeout, static password, no TLS (acceptable on the compose network, not across hosts) |
| **Likelihood** | 2 (needs a second vulnerability) |
| **Impact** | 5 (full data and host compromise) |
| **Autonomy / spread** | — |
| **Inherent risk** | 10 — Medium |
| **Recommended fix** | Separate roles: `scc_migrator` (owner, DDL), `scc_api` (SELECT on `users`; SELECT/INSERT/UPDATE on campaigns, messages; INSERT on deliveries), `scc_worker` (SELECT/UPDATE on deliveries, INSERT on delivery_attempts, UPDATE campaigns status), `scc_seed` (users only, dev only); none superuser. Set `statement_timeout` (e.g. 5 s for the api role) and `idle_in_transaction_session_timeout`; passwords from secrets; `sslmode=verify-full` when the database is remote |
| **Compensating control** | Postgres reachable only from the compose network and localhost |
| **Residual risk** | Low |
| **Verification test** | `SELECT rolsuper FROM pg_roles WHERE rolname IN (…)` all false; as `scc_api`: `DROP TABLE users` and `COPY users TO PROGRAM 'id'` fail; a query exceeding the timeout is cancelled |
| **Status** | Open |

### R-08 No browser security headers; the approval dialog can be framed

| Field | |
|---|---|
| **Asset / flow** | Web UI served by nginx, in particular the "Send messages" confirmation |
| **Threat scenario** | Threat model T4. A hostile page frames the UI transparently and lures the marketer into clicking the approval. Missing CSP removes defence-in-depth against any future XSS; `Server: nginx/1.30.6` discloses the version |
| **Current control** | Vue escapes text (no `v-html` in `frontend/src`, grep); email HTML built with `html/template` (`backend/internal/content/email.go:8-39`, test `content_test.go:261`) |
| **Evidence** | `frontend/nginx.conf:1-31` sets no `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`, `server_tokens off`; *probe* `curl -I http://127.0.0.1:8080/` returns only `Server, Date, Content-Type, Content-Length, Last-Modified, Connection, ETag, Accept-Ranges` |
| **Gap** | No anti-framing, no CSP, no nosniff, version disclosure |
| **Likelihood** | 2 |
| **Impact** | 4 (bypasses the human gate) |
| **Autonomy / spread** | Whole audience |
| **Inherent risk** | 8 — Medium |
| **Recommended fix** | In the `server` block: `server_tokens off;` and, with `always`: `Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"`, `X-Frame-Options "DENY"`, `X-Content-Type-Options "nosniff"`, `Referrer-Policy "no-referrer"`, `Permissions-Policy "camera=(), microphone=(), geolocation=()"`. Note: nginx drops server-level `add_header` in a location that has its own `add_header`, so repeat them in `location /assets/` (`nginx.conf:22-25`) or use an `include` snippet. Check the browser console for CSP violations after the change |
| **Compensating control** | — |
| **Residual risk** | Low |
| **Verification test** | `curl -sI http://127.0.0.1:8080/` and `…/assets/<file>.js` both show every header; an `<iframe src="http://localhost:8080">` on another origin stays blank; the UI works with no CSP errors in the console |
| **Status** | **Fixed (P9, commit `a2f12c4`):** `frontend/security-headers.inc` included in the server block and in each location with its own `add_header`; `server_tokens off`; `index.html` served with `Cache-Control: no-cache`. Evidence in [§5](#5-remediation-log) |

### R-09 Prompt injection and content integrity are bounded but untested

| Field | |
|---|---|
| **Asset / flow** | Brief → prompts; customer names → prompts; model output → SMS/email content |
| **Threat scenario** | Threat model T7/T8. (a) The brief contains `</brief>` and instructions, or asks for a phishing link; (b) a customer's crafted long first name is quoted into the prompt; (c) the model invents a link, code or price. A plausible-looking message reaches thousands of people if the reviewer misses it |
| **Current control** | Strong and deterministic: closed JSON schemas (`backend/internal/llm/schemas.go`), strict decoding (`parse.go:134-146`), audience only through the whitelisted DSL (no SQL, consent enforced by the backend), variable whitelist, emoji, length and single-line checks (`backend/internal/content/validate.go:92-134`), prompt rules against invented codes/prices/links (`prompts.go:108`), human approval of the exact content. Tests for DSL injection (`segment/validate_test.go:114`), fault scenarios (`generate_test.go:105`), golden prompts (`llm_test.go:290`) |
| **Evidence** | `prompts.go:95-97,146` (brief inserted between tags without escaping), `:167-169` (previous answer inserted between tags), `:157` (customer values); `content.Validate` has no link/URL rule (`content/validate.go:92-134`); no adversarial LLM test in `backend/internal/llm/*_test.go` (only DSL-level injection tests) |
| **Gap** | No delimiter neutralisation, no link policy, no adversarial evaluation set, no repeated real-model runs (only three scenarios were checked on Claude, `README.md:160-161`) |
| **Likelihood** | 3 |
| **Impact** | 3 (phishing or misleading message under the brand; bounded by review) |
| **Autonomy / spread** | Whole audience once approved |
| **Inherent risk** | 9 — Medium |
| **Recommended fix** | (1) Implement R-04 (removes customer text from prompts). (2) Neutralise the delimiters in untrusted text (reject or escape `<brief>`, `</brief>`, `<previous_answer>` in the brief and in the quoted answer). (3) Add a link rule to `content.Validate`: any URL in a message must appear verbatim in the brief, else error `unexpected_link` (the repair loop then fixes it); optionally the same for phone numbers and discount codes. (4) Build an adversarial set (Turkish and English; direct, split, encoded and customer-name injections; requests for links, codes and other audiences) that runs on the mock in CI and periodically against Claude with several repetitions, and measure harm (was an invalid rule accepted, did an unexpected link reach a `valid` draft), not only refusal rate |
| **Compensating control** | Human review of the exact content in the confirm dialog |
| **Residual risk** | Low |
| **Verification test** | Unit: brief with `</brief>` is rejected or escaped in the prompt; a draft containing a URL absent from the brief is `invalid` with `unexpected_link`. Eval: N runs per adversarial brief, 0 harmful `valid` drafts |
| **Status** | Open |

### R-10 Supply-chain and CI gates are thin

| Field | |
|---|---|
| **Asset / flow** | Go modules, npm packages, base images, GitHub Actions, `main` branch |
| **Threat scenario** | Threat model T12. A compromised or vulnerable dependency, a moved image tag or action tag, or an unreviewed push to `main` (once public) |
| **Current control** | `go.sum` and `package-lock.json` with `npm ci`; images and the action pinned to exact versions; CI token `permissions: contents: read` (`.github/workflows/ci.yml:10-11`); gitleaks run once over every commit before submission (`DECISIONS.md:624`, `docs/PLAN.md:804`); backend built with `-trimpath` |
| **Evidence** | `.github/workflows/ci.yml:22` (`actions/checkout@v4.2.2`, tag, not SHA); images by tag, not digest (`backend/Dockerfile:6,23`, `frontend/Dockerfile:5,20`, `docker-compose.yml:28`); `npm ci --no-audit` (`frontend/Dockerfile:8`); CI runs only `make test` (`ci.yml:23-24`); *probe* `gh api …/branches/main/protection` → 404 "Branch not protected" |
| **Gap** | No SCA (`govulncheck`, `npm audit`), no secret scan in CI, no dependency update automation, no SBOM, no branch protection, tag pinning |
| **Likelihood** | 2 |
| **Impact** | 4 (code execution in build or runtime) |
| **Autonomy / spread** | Every build |
| **Inherent risk** | 8 — Medium |
| **Recommended fix** | **M1:** branch protection on `main` (required CI check, pull request review, no force push) before the repository becomes public. **M3:** CI jobs `govulncheck ./...`, `npm audit --omit=dev --audit-level=high`, gitleaks on every pull request; Dependabot or Renovate for gomod, npm, docker and github-actions; pin actions by commit SHA and images by digest; generate an SBOM (e.g. syft) for the release image |
| **Compensating control** | Small dependency set; lockfiles |
| **Residual risk** | Low |
| **Verification test** | Branch protection API returns the rule; a pull request with a known-vulnerable module version fails CI |
| **Status** | Open |

### R-11 Approval is not bound to the reviewed content, and there is no second approver

| Field | |
|---|---|
| **Asset / flow** | `POST /api/campaigns/{id}/approve` |
| **Threat scenario** | Threat model T10. The marketer opens the confirm dialog; meanwhile the message is edited (second tab, another user, or a CSRF `PUT`); the confirm approves whatever is stored at that moment, not what was shown |
| **Current control** | Row lock and re-validation against the frozen audience at approval (`backend/internal/campaign/repository.go:292-348`, `service.go:307-335`); audience anchored to `as_of` so the count matches the preview; idempotent approval (tests `approve_test.go:107,146`) |
| **Evidence** | Approve takes no body (`backend/internal/httpapi/campaigns.go:135-146`); `freeze` uses the current stored messages (`service.go:313-332`) |
| **Gap** | No version/hash check; requester and approver are the same person |
| **Likelihood** | 2 |
| **Impact** | 3 |
| **Autonomy / spread** | Whole audience |
| **Inherent risk** | 6 — Low |
| **Recommended fix** | The UI sends `{"content_version": "<sha256 of rule + as_of + messages>"}` (or the latest `updated_at`); the server compares it under the row lock and returns 409 when it differs. In production, require a different user with the `approver` role above a threshold (e.g. > 1,000 messages) |
| **Compensating control** | Single user; the dialog shows the content |
| **Residual risk** | Low |
| **Verification test** | Integration: edit after fetching the version → approve with the old version returns 409 and inserts no deliveries |
| **Status** | Open |

### R-12 No audit trail of who did what; no alerting

| Field | |
|---|---|
| **Asset / flow** | Create, edit, approve; LLM calls; sending |
| **Threat scenario** | After an incident (unwanted send, cost spike) nobody can tell who approved, from where, which prompt version produced the draft, or notice it while it happens |
| **Current control** | JSON logs with `request_id`, method, path, status, duration (`backend/internal/httpapi/router.go:156-175`); campaign approval logged (`service.go:297-298`); token usage logged; model id stored per campaign (`0002_create_campaigns.sql:20-21`); prompt changes visible through golden files; Prometheus metrics for LLM attempts and provider calls |
| **Evidence** | No actor or client IP in logs; no prompt hash per draft; no alert rules (`README.md:159`: no Prometheus server or dashboards); no risk owner/RACI document |
| **Gap** | Accountability and detection |
| **Likelihood** | 3 |
| **Impact** | 2 (slower detection and forensics) |
| **Autonomy / spread** | — |
| **Inherent risk** | 6 — Low |
| **Recommended fix** | An append-only `campaign_events` table (campaign, actor, action, time, client IP, content hash, model, prompt hash); store the system-prompt hash with each draft; alerts on LLM error/refusal rate, token spend, provider failure ratio, approvals of large audiences, 4xx/5xx spikes; ship logs to central storage with a retention period; write down owners for the system, the data and security |
| **Compensating control** | Container logs |
| **Residual risk** | Low |
| **Verification test** | Approve a campaign → one `approved` event with actor and hash; synthetic error burst fires the alert |
| **Status** | Open |

### R-13 Secret lifecycle and container hardening

| Field | |
|---|---|
| **Asset / flow** | `ANTHROPIC_API_KEY`; containers |
| **Threat scenario** | A long-lived key leaks (screen share, `docker inspect`, a session export) and stays valid; a container compromise is easier to escalate |
| **Current control** | Key only in git-ignored `.env` (`.gitignore:1-4`; *scan*: 0 key-pattern matches in HEAD and history, `.env` never committed, `.env.example` key empty, `.env` mode 600); SDK reads nothing implicit (`llm/anthropic/client.go:74`); backend runs as uid 10001 (`backend/Dockerfile:26-28`) |
| **Evidence** | Key passed as a plain environment variable (`docker-compose.yml:75`, visible to `docker inspect`); no rotation procedure; nginx image runs its master as root (`frontend/Dockerfile:20`); compose sets no `read_only`, `cap_drop`, `no-new-privileges`, memory or pid limits (`docker-compose.yml:26-163`) |
| **Gap** | Rotation and container hardening |
| **Likelihood** | 2 |
| **Impact** | 3 |
| **Autonomy / spread** | — |
| **Inherent risk** | 6 — Low |
| **Recommended fix** | **M1:** rotate the development key after the submission and after any screen-shared demo; give reviewers' runs their own key with a spend limit. **M3:** one key per environment with spend limits; Docker/Kubernetes secrets or a vault instead of env vars; `nginxinc/nginx-unprivileged`; `read_only: true` with `tmpfs` where needed, `cap_drop: [ALL]`, `security_opt: ["no-new-privileges:true"]`, `mem_limit`, `pids_limit` |
| **Compensating control** | — |
| **Residual risk** | Low |
| **Verification test** | Old key rejected by the API after rotation; `docker inspect` shows the hardening options; the stack still passes `make smoke` |
| **Status** | Open |

### R-14 Minor exposure and robustness items

| Field | |
|---|---|
| **Asset / flow** | `/metrics`, `/api/segments/preview`, HTTP server, email body |
| **Threat scenario** | (a) Operational counters readable on the public api port; (b) with more users, the audience count is an oracle for facts about small groups (e.g. one person with a given city and spend); (c) slow-body clients hold connections (no `ReadTimeout`/`IdleTimeout`); (d) the email body has no length limit apart from the 64 KiB request cap and `max_tokens` |
| **Current control** | Metrics carry no personal data (*probe* metric names); body limits 64 KiB; `ReadHeaderTimeout` 5 s |
| **Evidence** | `router.go:91-93` (metrics on the public router), `segments.go:51-85`, `backend/cmd/api/main.go:129-135` (no `WriteTimeout` on purpose for SSE; no `ReadTimeout`/`IdleTimeout`), `backend/internal/content/validate.go:82` (`body` limit 0) |
| **Gap** | Minor |
| **Likelihood** | 2 |
| **Impact** | 1 |
| **Inherent risk** | 2 — Low |
| **Recommended fix** | Serve `/metrics` on a separate internal listener; if the tool gets multiple users, show small counts as "< 10"; set `ReadTimeout` (e.g. 30 s) and `IdleTimeout` (e.g. 120 s), which do not affect SSE writes; cap the email body (e.g. 5,000 characters) in the validator and a DB `CHECK` |
| **Residual risk** | Negligible |
| **Verification test** | `curl :8081/metrics` from outside fails; slowloris-style body test is cut off; 5,001-character body → `too_long` |
| **Status** | Open |

## 4. Checked and found sound

| Area | What was checked | Evidence |
|---|---|---|
| SQL injection through the LLM or the API | Every identifier from the registry, every value a bind parameter; `Validated` cannot be forged | `compile.go:16-39`, `segment/validate.go:50-56`; tests `segment/validate_test.go:114`, `rule_test.go:53`, `compile_test.go:150`, differential oracle `audience_test.go:44-64` |
| XSS / HTML injection | No `v-html`, `innerHTML`, `eval` or `new Function` in `frontend/src` (grep); email HTML escaped | `MessageText.vue:14-22`; `email.go:8-39`, `content_test.go:261` |
| Template injection in rendering | Values are inserted once and never re-parsed | `backend/internal/content/template.go:113-128` |
| SSRF | No user- or model-supplied URL is fetched; provider and Anthropic URLs come from configuration | `config.go:155`, `llm/anthropic/client.go:73-82` |
| Unsafe deserialisation / mass assignment | Strict JSON with `DisallowUnknownFields` on every input | `campaigns.go:46-48`, `parse.go:136-145`, `rule.go:34` |
| Error leakage | Generic 500 body, detail only in logs | `router.go:147-152`; tests `router_test.go:133`, `segments_test.go:115` |
| Secrets in the repository | Key patterns (Anthropic, AWS, GitHub, Slack) in HEAD and full history: 0; `.env` never tracked; `.env.example` key empty | *scan* 2026-10-26; gitleaks 84 commits (`docs/PLAN.md:804`) |
| Personal data in session exports | Only `@anthropic.com`/`@example.com` addresses and Vue directives match the e-mail pattern; `192.168.` appears only in scrub instructions | *scan* 2026-10-26 |
| Personal data in logs | Worker logs ids only; request log has no query string or body | `worker.go:148-168`, `router.go:166-173` |
| CORS | No `Access-Control-Allow-*` headers: cross-origin reads are blocked (writes are R-02) | *probe* |
| Dev tooling in production | `vite-plugin-vue-devtools` absent from the built assets | *probe*: 0 asset files mention devtools |
| Panic handling | `middleware.Recoverer` | `router.go:84` |
| Fail-safe drafting | Invalid output becomes `needs_manual_edit`, never auto-approved; refusal handled before parsing | `generate.go:85-101`, `llm/anthropic/client.go:170-182`; test `generate_test.go:267` |
| Duplicate sends | DB uniqueness + provider idempotency key + lease fencing | `0003:17,41`, `delivery/client.go:65`; tests `queue_test.go:206`, `worker_test.go` |
| Paid calls from tests and smoke | Tests use the mock and recorded fixtures; smoke forces the mock and refuses Claude | `Makefile:44-46`, `DECISIONS.md:623` |

## 5. Remediation log

### P9 — R-01 (network part), R-08, R-02 (2026-10-26)

Branch `fix/security-quick-wins` (from `feat/ci`), commits `8e2fcb9` (R-01), `a2f12c4` (R-08), `3cfbb5e` (R-02).
Decisions: `DECISIONS.md` D28. Every check ran on the local stack in mock mode (`/api/config` read first); no paid
call was made.

| Check | Before (audit) | After (P9) |
|---|---|---|
| `docker compose ps`: published bindings of `api` / `frontend` | `0.0.0.0` and `[::]` | `127.0.0.1` only |
| `curl` to the machine's LAN address on `:8080` / `:8081` | `200` / `200` | connection refused / refused |
| `curl` to `localhost:8080`, `127.0.0.1:8080`, `localhost:8081/healthz` | `200` | `200` |
| Security headers on `/`, `/index.html`, an SPA route, `/assets/*.js`, `/api/config`, a 404 | none | all six on every path; `Server: nginx` without version |
| `<iframe src="http://localhost:8080/?nocache=…">` on `http://127.0.0.1:8765` | — | blank; console: violates `frame-ancestors 'none'`; network: `ERR_BLOCKED_BY_RESPONSE` |
| Full flow in a browser (brief library → generate → draft stream → edit SMS → approve → live dashboard) | — | every request `200`; 0 CSP violations (`securitypolicyviolation` listener), no console error |
| `POST /api/segments/preview`, `text/plain` + `Origin: http://attacker.example` (`:8081` and `:8080`) | `200` | `403` |
| Same with `text/plain` and no `Origin` | `200` | `415` |
| `GET /api/config` with `Host: attacker.example` | `200` | `403` |
| `POST /api/segments/preview` JSON without browser headers (curl, scripts) | `200` | `200` |
| Attack page on `http://127.0.0.1:8765` (threat model T1): `text/plain` create, bodiless approve of ready campaigns 1 and 26, `EventSource` and `<img>` on a draft stream, a `text/plain` form POST to approve | not run (it would send) | every request `403` (Chrome: `ERR_ABORTED` / `ERR_BLOCKED_BY_RESPONSE`); campaigns 27 → 27, deliveries 7,299 → 7,299, campaigns 1 and 26 still `ready`, no draft generated; api logged each rejection with its reason |
| Guard unit tests (`backend/internal/httpapi/guard_test.go`) | — | 32 cases pass; four mutations (accepting cross-site, skipping the Host check, the JSON-body check, the Origin check) each make them fail |
| `make test` | pass (CI) | pass: 16 Go packages `ok` (race detector, Postgres), frontend 12 test files |
| `make smoke` | 40 checks | 52 checks pass: the five scenarios plus six UI-header and six request-guard checks, two of them through nginx |

Found while verifying: the browser kept a heuristically cached `index.html` (served without `Cache-Control`) with its
old headers, so the first framing test still rendered the app. `location /` now sends `Cache-Control: no-cache`
(D28 S6).

Still open from these findings: authentication and per-route authorisation (R-01, M2); approval bound to the reviewed
content (R-11).

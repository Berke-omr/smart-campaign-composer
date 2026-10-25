# Security Release Checklist — Smart Campaign Composer

Go/no-go gate derived from the reference catalogue's §9 and from the findings in
[`security-risk-register.md`](security-risk-register.md). Architecture and scope are in
[`security-threat-model.md`](security-threat-model.md).

**Status column** records the audit of 2026-10-26 (branch `feat/ci`, commit `68bffe4`):
✅ pass · ❌ fail (open finding) · ⚠️ not re-checked in this audit, re-run before release · N/A.

There are two gates:

- **Gate A — take-home submission / local demo.** Reviewers run the stack on their own machine with
  fake seed data, the mock provider, and with or without their own Anthropic key.
- **Gate B — any shared or production deployment** (other users, a server, real customer data or a
  real SMS/email provider). Every Gate A item still applies.

If a **Required** item fails, the release does not go out, or the affected capability stays off.

## Gate A — submission / local demo

### A.1 Secrets and personal data

| # | Check | Level | How to verify (prints counts or verdicts, never values) | Expected | Status |
|---|---|---|---|---|---|
| A-01 | `.env.example` holds no key | Required | `grep -cE '^ANTHROPIC_API_KEY=.+' .env.example` | `0` | ✅ |
| A-02 | `.env` has never been tracked | Required | `git log --all --format=%h -- .env \| wc -l` | `0` | ✅ |
| A-03 | No secret in any commit | Required | `docker run --rm -v "$PWD:/repo:ro" zricethezav/gitleaks:v8.21.2 git /repo --redact --no-banner --log-opts="--all"` | Only the documented false positive (`docs/PLAN.md`, P7 "Secret scan findings") | ✅ (P7 run; pattern scan 2026-10-26: 0 key matches in HEAD and history) |
| A-04 | Session exports carry no personal data (e-mails other than example.com/anthropic.com, local IPs, names) | Required | `git grep -h -oE '@[A-Za-z0-9.-]+\.[a-z]{2,}' -- ai-sessions \| sort \| uniq -c` and `git grep -c '192\.168\.' -- ai-sessions` | Only `@anthropic.com`, `@example.com` and Vue directives; `192.168.` only inside scrub instructions | ✅ |
| A-05 | Logs carry no message content or recipients | Required | Review `backend/internal/delivery/worker.go:148-168`, `backend/internal/httpapi/router.go:156-175` | ids, status and timing only | ✅ |

### A.2 Safe defaults and tests

| # | Check | Level | How to verify | Expected | Status |
|---|---|---|---|---|---|
| A-06 | Without a key the api uses the mock LLM | Required | Start without `.env`, then `curl -s http://127.0.0.1:8080/api/config` | `{"llm":{"mode":"mock","model":null}}` | ✅ (P7 clean-clone run, `docs/PLAN.md`) |
| A-07 | Unit, integration and component tests pass | Required | `make test` | exit 0 | ✅ CI on `feat/ci`, 2026-10-25 23:22 UTC; ⚠️ not re-run locally for this audit |
| A-08 | End-to-end smoke passes on the mock only | Required | `make smoke` | `40 checks passed, 0 failed`; refuses to run if `/api/config` is not mock | ⚠️ last run in P7 |
| A-09 | DSL injection tests are part of the suite | Required | `go test ./internal/segment -run 'Injection\|SQLShape'` inside `make test` | pass | ✅ (`segment/validate_test.go:114`, `compile_test.go:150`) |
| A-10 | README states the security limits (no authentication, run on a trusted machine only, every brief is a paid call with a key) | Required | Read `README.md` → Known gaps | Present | ⚠️ partial: authentication is listed as out of scope (`README.md:118-120`) and paid calls are mentioned (`:32-34`); there is no warning that the UI/API are reachable from the network or that other browser tabs can drive the API |

### A.3 Exposure of the running stack (low-effort fixes, high value)

| # | Check | Level | How to verify | Expected | Status |
|---|---|---|---|---|---|
| A-11 | Published ports are loopback only (R-01) | Recommended | `docker compose ps --format '{{.Service}} {{.Ports}}' \| grep -E '0\.0\.0\.0\|\[::\]'` | no output | ❌ `api` and `frontend` bound to `0.0.0.0`/`[::]` |
| A-12 | Cross-site writes are rejected (R-02) | Recommended | `curl -s -o /dev/null -w '%{http_code}\n' -X POST -H 'Content-Type: text/plain' --data '{"match":"all","conditions":[]}' http://127.0.0.1:8080/api/segments/preview` | `415` | ❌ `200` |
| A-13 | Foreign `Origin` on unsafe methods is rejected (R-02) | Recommended | same request with `-H 'Content-Type: application/json' -H 'Origin: http://attacker.example'` | `403` | ❌ `200` |
| A-14 | Foreign `Host` is rejected — DNS rebinding (R-02) | Recommended | `curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: attacker.example' http://127.0.0.1:8080/api/config` | `403` or `421` | ❌ `200` |
| A-15 | Security headers present on `/` and `/assets/*` (R-08) | Recommended | `curl -sI http://127.0.0.1:8080/ \| grep -ciE '^(content-security-policy\|x-frame-options\|x-content-type-options\|referrer-policy):'` (repeat for one file under `/assets/`) | `4` for both | ❌ `0` |
| A-16 | nginx version not disclosed (R-08) | Recommended | `curl -sI http://127.0.0.1:8080/ \| grep -i '^server:'` | `Server: nginx` | ❌ `nginx/1.30.6` |

### A.4 Repository and key hygiene at submission

| # | Check | Level | How to verify | Expected | Status |
|---|---|---|---|---|---|
| A-17 | `main` is protected before the repository is made public (R-10) | Required if public | `gh api repos/Berke-omr/smart-campaign-composer/branches/main/protection --jq '.required_status_checks.contexts'` | contains the CI job | ❌ "Branch not protected" (repository is private today: ✅ for now) |
| A-18 | CI token is read-only | Required | `.github/workflows/ci.yml:10-11` | `contents: read` | ✅ |
| A-19 | Development key rotated after the submission and after any screen-shared demo (R-13) | Required (after submission) | Anthropic console: old key revoked; a request with it returns 401 | revoked | ⏳ pending, after submission |
| A-20 | Stack stopped and volumes removed on shared machines after the review | Recommended | `make clean` | no `smart-campaign-composer` containers or volumes | ⏳ after the review |

**Gate A verdict on 2026-10-26:** all **Required** items that can be checked today pass (A-01…A-09,
A-18), A-10 is partial, and A-17/A-19 are due at the moment the repository goes public and after the
submission. The **Recommended** exposure items (A-11…A-16) fail; they are configuration-level fixes
(R-01 port binding, R-08 headers) plus one small middleware (R-02). Without them the demo is safe
only on a trusted network with no untrusted pages open in the same browser.

## Gate B — shared or production deployment

Every item is **Required** (reference §9: if one is "no", the capability stays off in production).

| # | Reference §9 item | Check for this application | Findings | Status |
|---|---|---|---|---|
| B-01 | Purpose, owner, data scope, tools and forbidden actions are written | `README.md`, `DECISIONS.md` cover purpose, data, autonomy; add owners (system, data, security), a RACI and a change process for prompts/models | R-12 | ⚠️ partial |
| B-02 | User, agent and tenant identities are separable end to end | Every `/api` route authenticated; roles `marketer` / `approver`; the actor recorded on every create, edit and approve | R-01, R-12 | ❌ |
| B-03 | Every tool call is authorised outside the model and its arguments validated | Model output → strict schema → DSL validator → parameterised SQL; content validator; approval re-validates (✅). Add per-route authorisation | R-01 | ⚠️ validation ✅, authorisation ❌ |
| B-04 | No unnecessary tool, write, delete or network access | The model has no tools (✅); the database role is superuser (❌); internal services on loopback (✅) | R-07 | ❌ |
| B-05 | Secrets separate from prompts, memory, code and logs; short-lived and revocable | Separation ✅ (scan, `llm/anthropic/client.go:74`, `db.go:26-27`); per-environment keys with spend limits, secret store, rotation procedure ❌ | R-13 | ⚠️ |
| B-06 | External content is marked untrusted | Brief and customer text in delimited blocks; delimiters neutralised; customer values no longer sent (R-04) | R-04, R-09 | ❌ |
| B-07 | High-impact actions need exact-action human approval | Confirm dialog shows audience, counts, exclusions and full content (✅); approval bound to the shown content version, protected from CSRF and framing, second approver above a threshold | R-02, R-08, R-11 | ❌ |
| B-08 | Code/tool execution isolated from production and host resources | No execution of model output (N/A); containers non-root, read-only, capabilities dropped | R-13 | ⚠️ |
| B-09 | Data egress limited by destination and class; DLP | Only lengths (not names) sent to the LLM; provider DPA / zero-retention confirmed; legal basis for transfer | R-04, R-06 | ❌ |
| B-10 | Step, time, token, tool, data and cost limits | Attempt budget, timeouts and `max_tokens` (✅); rate limit, concurrency cap and token budget with hard stop (❌) | R-03 | ❌ |
| B-11 | Calls, authorisation, approvals, data class and results logged for forensics | `campaign_events` audit table, prompt hash per draft, central logs with retention, alerts | R-12 | ❌ |
| B-12 | Kill switch, credential revocation, quarantine and rollback tested | Per-campaign cancel and global send switch, key revocation drill, rollback of prompt/model version | R-05, R-13 | ❌ |
| B-13 | Adversarial and regression tests meet acceptance thresholds | DSL injection and fault tests (✅); adversarial LLM set on mock in CI and repeated runs on Claude with a harm threshold (❌) | R-09 | ⚠️ |
| B-14 | Personal-data and sector/regulatory scope assessed | KVKK roles and transfer basis, retention and erasure, commercial-message consent registry; legal sign-off | R-06 | ❌ (not verifiable from code) |
| B-15 | Classic web security baseline | CSRF/Origin/Host checks, security headers, `ReadTimeout`/`IdleTimeout`, metrics on an internal port, TLS at the edge | R-02, R-08, R-14 | ❌ |
| B-16 | Supply chain in CI | `govulncheck`, `npm audit`, gitleaks per pull request, dependency updates, SHA/digest pinning, SBOM, branch protection | R-10 | ❌ |
| B-17 | Database least privilege and limits | Separate roles, no superuser, `statement_timeout`, TLS to a remote database | R-07 | ❌ |

**Gate B verdict on 2026-10-26: no-go.** The application is not ready for a shared or production
deployment until at least R-01 … R-08 (all P0) are closed and their verification tests pass.

## Pre-release run sheet

Run in the repository root, with the stack started in mock mode
(`LLM_PROVIDER=mock docker compose up -d --build --wait`), so no check can make a paid call.

```bash
curl -s http://127.0.0.1:8080/api/config
```

```bash
grep -cE '^ANTHROPIC_API_KEY=.+' .env.example
```

```bash
docker run --rm -v "$PWD:/repo:ro" zricethezav/gitleaks:v8.21.2 git /repo --redact --no-banner --log-opts="--all"
```

```bash
make test
```

```bash
make smoke
```

```bash
docker compose ps --format '{{.Service}} {{.Ports}}'
```

```bash
curl -sI http://127.0.0.1:8080/
```

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST -H 'Content-Type: text/plain' --data '{"match":"all","conditions":[]}' http://127.0.0.1:8080/api/segments/preview
```

```bash
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: attacker.example' http://127.0.0.1:8080/api/config
```

```bash
docker compose exec -T postgres psql -U scc -d scc -Atc "select rolname, rolsuper from pg_roles where rolname like 'scc%'"
```

Record the outputs (without secret values) in `docs/PLAN.md` under the release's verification log.

## Sign-off

| Role | Name | Gate | Date | Decision |
|---|---|---|---|---|
| Developer / system owner | | A | | |
| Reviewer (security) | | A / B | | |
| Data owner (Gate B only) | | B | | |
| Legal (Gate B only) | | B | | |

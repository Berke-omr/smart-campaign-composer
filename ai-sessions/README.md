# AI sessions

Exports of the Claude Code sessions used to build this project, in chronological order.
They show how the problem was approached and how AI output was verified at each step.

| # | Date | File | Topic |
|---|---|---|---|
| 01 | 2026-10-19 | [01-2026-10-19-planning-p0-p2.md](01-2026-10-19-planning-p0-p2.md) | Case analysis, plan and decisions (P0); skeleton and infrastructure (P1); segment DSL and audience counting (P2); design review and P3 kickoff prompt |
| 02 | 2026-10-19 | [02-2026-10-19-llm-layer-p3.md](02-2026-10-19-llm-layer-p3.md) | LLM layer (P3): structured outputs verified on the real API, mock LLM scenario catalog, content validation, streamed drafts and campaign drafts; post-review fixes (mutation-check record, disconnect test, email latency); P4 kickoff prompt |
| 03 | 2026-10-19 – 20 | [03-2026-10-19-delivery-p4.md](03-2026-10-19-delivery-p4.md) | Approval and delivery (P4): audience frozen at approval, mock provider with faults and idempotency, rate-limited worker with leases and retries, verified with integration tests, mutation checks and live runs (problems found while verifying: a hung test, clock skew, a 1 s stop timeout); a real-API end-to-end run; the no-emoji rule; P5 kickoff prompt |
| 04 | 2026-10-21 | [04-2026-10-21-tracking-p5.md](04-2026-10-21-tracking-p5.md) | Live tracking (P5): plan with open decisions approved first, readiness check, dashboard snapshots read in one transaction, the SSE stream and how it ends, streams closed on api shutdown, Prometheus metrics in the api and the worker; verified with integration tests, 19 mutation checks (one surviving mutant exposed a test gap) and live runs (per-second series against the attempt log, the api stopped with open streams, Postgres stopped); P6 kickoff prompt |
| 05 | 2026-10-21 – 22 | [05-2026-10-21-frontend-p6.md](05-2026-10-21-frontend-p6.md) | Frontend (P6): plan with eleven decisions approved first (own components instead of a UI library, own SVG chart, composables, our own reconnect around `EventSource`); the backend's worst-case counter ported and checked against 720 answers recorded from the live api; streamed draft cards, edits refused with the backend's report, the confirm dialog and the live dashboard; verified with Vitest, 25 mutation checks (one surviving mutant exposed a test gap) and browser runs (1440 / 1024 px, 200% text, keyboard, contrast, the api stopped during a send); PR and the test-scenarios kickoff prompt |
| 06 | 2026-10-22 – 26 | [06-2026-10-22-test-scenarios.md](06-2026-10-22-test-scenarios.md) | Test scenarios: a research subagent (campaign types marketers run, sources) and a design subagent (20 scenarios); each scenario checked against the segment registry, the live seed (read-only SQL, cross-checked with the preview endpoint) and the mock LLM's keyword fallback (simulated, which showed it inverts a negated cart and never reports unsupported criteria); 12 mock catalog entries with a mutation check; audience numbers measured at the seed's own time to equal a fresh reseed; manual runs in mock mode and three briefs on the real model; P7 kickoff prompt |
| 07 | 2026-10-26 | [07-2026-10-26-hardening-p7.md](07-2026-10-26-hardening-p7.md) | Hardening and submission (P7): plan with five decisions approved first (one hardening candidate turned out to be done already in P5); an end-to-end smoke test through the API (`make smoke`, mock LLM only, checked to refuse a non-mock api); gitleaks over the whole history (one false positive) and a personal-data scan (a name left in session 03, masked); reduced motion checked with the macOS setting; README, DECISIONS and PLAN finalized; a fresh clone without `.env` running `docker compose up`, `make smoke` and `make test`; the session 05 export; the P7 PR merged and CI (`make test` on GitHub Actions) opened as its own PR |
| 08 | 2026-10-26 | [08-2026-10-26-security-audit-p9.md](08-2026-10-26-security-audit-p9.md) | Security audit and quick wins (P9): the application audited against an AI-agent security control catalogue (architecture, trust boundaries, LLM profile, which controls apply and which do not, with `file:line`, test and live-probe evidence; values never printed); threat model, risk register (R-01…R-14) and release checklist; then three fixes approved as a plan with three decisions: loopback-only ports, nginx security headers with a strict CSP, and an `/api` request guard against CSRF and DNS rebinding; verified with 32 guard tests and four mutations, `make test`, `make smoke` (52 checks), the audit's probes before and after, and browser runs (full flow with no CSP violation, a framing page, a cross-site attack page that created nothing); a stale cached `index.html` found while verifying; a first merge of PR #11 refused by the session's permission check; at the end a submission check against the release checklist, this export, and both PRs merged on the developer's request (the merges come after the export) |

## How the exports are produced

1. The session is exported with the Claude Code desktop app (session menu → Export). This produces
   the raw JSONL transcript. Sessions 05, 07 and 08 were converted from the same JSONL transcript as
   Claude Code keeps it on disk.
2. The transcript is converted to Markdown:
   - user prompts, assistant messages and every tool call are kept in order;
   - tool calls are collapsed in `<details>` blocks and long outputs are truncated;
   - thinking blocks, system reminders and bookkeeping records are left out, and loaded skill
     instructions are reduced to one line.
3. Personal data is redacted, and the result is checked with `grep` before committing:
   - home path and local username;
   - the developer's first name (replaced with "User", also in `DESIGN.md` and the design screens);
   - email addresses other than `example.com`;
   - the local network IP;
   - names of HR contacts.

   The check confirms that no API keys or tokens remain. Raw exports are not committed.

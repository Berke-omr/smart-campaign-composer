# AI sessions

Exports of the Claude Code sessions used to build this project, in chronological order.
They show how the problem was approached and how AI output was verified at each step.

| # | Date | File | Topic |
|---|---|---|---|
| 01 | 2026-10-19 | [01-2026-10-19-planning-p0-p2.md](01-2026-10-19-planning-p0-p2.md) | Case analysis, plan and decisions (P0); skeleton and infrastructure (P1); segment DSL and audience counting (P2); design review and P3 kickoff prompt |
| 02 | 2026-10-19 | [02-2026-10-19-llm-layer-p3.md](02-2026-10-19-llm-layer-p3.md) | LLM layer (P3): structured outputs verified on the real API, mock LLM scenario catalog, content validation, streamed drafts and campaign drafts; post-review fixes (mutation-check record, disconnect test, email latency); P4 kickoff prompt |
| 03 | 2026-10-19 – 20 | [03-2026-10-19-delivery-p4.md](03-2026-10-19-delivery-p4.md) | Approval and delivery (P4): audience frozen at approval, mock provider with faults and idempotency, rate-limited worker with leases and retries, verified with integration tests, mutation checks and live runs (problems found while verifying: a hung test, clock skew, a 1 s stop timeout); a real-API end-to-end run; the no-emoji rule; P5 kickoff prompt |
| 04 | 2026-10-21 | [04-2026-10-21-tracking-p5.md](04-2026-10-21-tracking-p5.md) | Live tracking (P5): plan with open decisions approved first, readiness check, dashboard snapshots read in one transaction, the SSE stream and how it ends, streams closed on api shutdown, Prometheus metrics in the api and the worker; verified with integration tests, 19 mutation checks (one surviving mutant exposed a test gap) and live runs (per-second series against the attempt log, the api stopped with open streams, Postgres stopped); P6 kickoff prompt |

## How the exports are produced

1. The session is exported with the Claude Code desktop app (session menu → Export). This produces
   the raw JSONL transcript.
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

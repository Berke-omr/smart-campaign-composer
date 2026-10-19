# AI sessions

Exports of the Claude Code sessions used to build this project, in chronological order.
They show how the problem was approached and how AI output was verified at each step.

| # | Date | File | Topic |
|---|---|---|---|
| 01 | 2026-10-19 | [01-2026-10-19-planning-p0-p2.md](01-2026-10-19-planning-p0-p2.md) | Case analysis, plan and decisions (P0); skeleton and infrastructure (P1); segment DSL and audience counting (P2); design review and P3 kickoff prompt |
| 02 | 2026-10-19 | [02-2026-10-19-llm-layer-p3.md](02-2026-10-19-llm-layer-p3.md) | LLM layer (P3): structured outputs verified on the real API, mock LLM scenario catalog, content validation, streamed drafts and campaign drafts; post-review fixes (mutation-check record, disconnect test, email latency); P4 kickoff prompt |

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

# AI sessions

Exports of the Claude Code sessions used to build this project, in chronological order.
They show how the problem was approached and how AI output was verified at each step.

| # | Date | File | Topic |
|---|---|---|---|
| 01 | 2026-10-19 | [01-2026-10-19-planning-p0-p2.md](01-2026-10-19-planning-p0-p2.md) | Case analysis, plan and decisions (P0); skeleton and infrastructure (P1); segment DSL and audience counting (P2); design review and P3 kickoff prompt |
| 02 | 2026-10-19 | _(export pending)_ | LLM layer (P3): structured outputs, mock LLM, content validation, streamed drafts |

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

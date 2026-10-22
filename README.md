# Smart Campaign Composer

A marketer describes a campaign in plain language; Claude turns it into a target audience and
SMS/email drafts, a human reviews and approves, and the messages are sent through a queue to a
mock provider while a dashboard shows delivery live.

> **Status: work in progress.** Built in phases — see [`docs/PLAN.md`](docs/PLAN.md) for progress
> and [`DECISIONS.md`](DECISIONS.md) for the reasoning behind each choice. The full architecture
> summary and the list of unfinished items will be added here at the end (phase P7).

## Quick start

Requirements: Docker (with Compose) and `make`. No Go or Node installation is needed.

```bash
docker compose up
```

| What | URL |
|---|---|
| Web UI | http://localhost:8080 |
| API | http://localhost:8081 (e.g. `/healthz`, `/api/stats/users`) |
| Mock provider (debug) | http://127.0.0.1:18090/healthz |
| PostgreSQL | `127.0.0.1:55432` (user/password/db: `scc`) |

No `.env` file is required. To change ports, the seed size or (later) add a Claude API key,
copy `.env.example` to `.env` and edit it.

## Tests

```bash
make test
```

Runs the backend and frontend test suites in containers, against a throwaway PostgreSQL.

## Useful commands

| Command | What it does |
|---|---|
| `make up` / `make down` | Start / stop the stack |
| `make clean` | Stop and delete all data (volumes) |
| `make logs` | Follow logs of all services |
| `make reseed` | Delete all users and seed again |
| `make psql` | Open a SQL shell |

## Repository layout

```
backend/    Go module: cmd/{api,worker,mockprovider,migrate,seed}, internal/…, migrations/
frontend/   Vue 3 + Vite + TypeScript, served by nginx
docs/       CASE.md (the brief), PLAN.md (phases and progress), API.md (contract),
            TEST-SCENARIOS.md (manual end-to-end scenarios)
ai-sessions/  Exported Claude Code sessions
```

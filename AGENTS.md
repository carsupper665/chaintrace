## Development rules

Global engineering rules for every process in this repo (Go API, Next.js BFF, Python
Agent). See `docs/development-rules.md`. Read it before adding a new component.

### Implementation approach

- Write the minimum amount of code needed to solve the current problem.
- Prefer the simplest direct implementation over abstraction or configuration for
  possible future needs.
- Keep modules independent. Coordinate across boundaries through small, explicit
  contracts rather than coupling implementations together.
- Give each module one focused responsibility; compose the focused pieces only at
  the integration boundary.

## Running locally

Backend `.env` and frontend `frontend/.env` both have to be set up before
anything works; login and analysis fail closed without SMTP and TronGrid
credentials. See `docs/local-development.md`.

## Agent skills

### Issue tracker

Issues are tracked as local Markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Triage uses the canonical `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, and `wontfix` role strings. See `docs/agents/triage-labels.md`.

### Domain docs

Domain documentation uses a single-context layout. See `docs/agents/domain.md`.

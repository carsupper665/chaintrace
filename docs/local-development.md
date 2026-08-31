# Local development

ChainTrace is three processes: the Go API, a Next.js frontend that acts as a
BFF, and an optional Python LLM Agent. The browser never talks to the Go API
directly except for one redirect during login, and never reaches the Agent at
all.

```
browser ──► Next.js :3000 ──► Go API :7794 ──► PostgreSQL / TronGrid
   └──────── /Authentication/verify (login link only) ───────┘
                                    │
                                    └──► Python Agent :7795 (LAN only) ──► Gemini
```

The Agent is optional: with `AGENT_BASE_URL` unset, every conversation request
returns `agent_unavailable` and everything else works as before.

## Configuration

| Process | File | Template |
| --- | --- | --- |
| Go API | `.env` (repository root) | [`.env.example`](../.env.example) |
| Frontend | `frontend/.env` | [`frontend/.env.example`](../frontend/.env.example) |
| LLM Agent | `agent/.env` | [`agent/.env.example`](../agent/.env.example) |

```bash
cp .env.example .env
cp frontend/.env.example frontend/.env
cp agent/.env.example agent/.env
```

The frontend only needs `CHAINTRACE_BACKEND_URL` to point at the Go API
(`http://localhost:7794`). Outside production an unset value falls back to that
same default, so a fresh checkout reaches the API even before `.env` is copied;
a production build still requires it explicitly. Every `/api/*` route in the
frontend is a proxy that attaches the Owner's credential and forwards to
`/api/v1/*` on the Go API.

### Two settings that silently break login if you skip them

1. **`LOGIN_VERIFY_BASE_URL`** must point at the **Go API**. Its built-in
   default is `http://localhost:${PORT:-3000}`, while the server's own port
   default is `7794`. If you set neither variable, the verification email links
   to the frontend origin, which has no `/Authentication/verify` route, and the
   login journey dead-ends on a 404.
2. **`SMTP_*`** must be a working mailbox. Login is an email challenge with no
   development bypass; without SMTP, `POST /Authentication/login` returns 500
   and the UI shows `登入服務暫時無法使用`.

`TRONGRID_API_KEY` is not needed to sign in or to browse Investigations, but
without it every Analysis Run terminates as `failed` with the error code
`provider_unavailable`.

## Running

```bash
go run .
```

```bash
cd frontend && npm install && npm run dev
```

Start the Go API first — the frontend's server-rendered workspace page calls
`/api/v1/me` on every request and redirects to `/login` when it cannot reach it.

## The login journey

There is no self-service registration in the UI; create the Owner first, either
through `ROOT_USER` / `ROOT_USER_EMAIL` / `ROOT_PASSWORD` in `.env` or with
`POST /api/v1/register`.

1. `POST /api/auth/login` (frontend) → `POST /Authentication/login` (Go).
   Verifies the password and emails a one-time verification link. Responds
   `202`; the UI switches to the "check your mail" state.
2. The Owner opens the emailed link: `GET /Authentication/verify` on the **Go
   API**. This consumes the challenge and `302`s to
   `FRONTEND_BASE_URL/login/callback?code=…&id=…`.
3. `GET /login/callback` (frontend, server side) → `GET
   /Authentication/challenge` (Go) exchanges the code for a JWT and stores it in
   the `chaintrace_credential` cookie (`HttpOnly`, `SameSite=Lax`). The raw
   token never reaches browser JavaScript.
4. Every later domain call goes browser → frontend `/api/…` → Go `/api/v1/…`
   with `Authorization: Bearer <cookie>`.
5. `POST /api/auth/logout` revokes the token on the Go side and clears the
   cookie.

Because steps 3–5 originate from the Next.js process, the Go API sees a single
client IP for all Owners. Keep that in mind when tuning
`GLOBAL_MAX_REQUEST_NUM`.

### Skipping the mail round trip while testing

`go run ./devtoken` prints a JWT for the root Owner (`-email you@example.com`
for another, `-out token.txt` to write it to a file, because the shared logger
also writes to stdout). Use it as `Authorization: Bearer <token>` against the Go
API, or set it as the `chaintrace_credential` cookie to enter the UI.

It signs with the key already in `.env`, so it grants nothing an Owner could not
get by logging in — it only skips the email. **It is a developer tool: never
build it into a deployment.** The root `go build .` does not include it; it is
its own `package main` under `devtoken/`.

## Tests

```bash
go test ./...
```

```bash
cd frontend && npm test
```

`npm test` runs `vinext build` before the Node test files, so it also catches
type and build regressions.

## Running the LLM Agent

The Agent is a LAN-only sidecar. It holds the LLM credentials; the Go API and
the frontend never see them (see [development rules](development-rules.md)
section 5).

```bash
python -m venv agent/.venv
agent/.venv/Scripts/python.exe -m pip install -e "agent[gemini,dev]"
```

Fill `LLM_API_KEY` in `agent/.env` with a key from
<https://aistudio.google.com/apikey>, pick the same `AGENT_SHARED_KEY` in both
`.env` and `agent/.env`, then start it:

```bash
agent/.venv/Scripts/python.exe agent/app.py
```

`GET http://127.0.0.1:7795/healthz` should answer. To confirm the key and the
adapter against the real API — this one costs tokens — run
`agent/.venv/Scripts/python.exe agent/smoke.py`.

Set `AGENT_BASE_URL` and `AGENT_SHARED_KEY` in the repository-root `.env` so
the Go API starts using it.

### If a model stops responding

Gemini models go over capacity from time to time; the symptom is a request that
hangs rather than an error. Switch `LLM_MODEL` in `agent/.env` to another model
(`gemini-3.6-flash`, `gemini-3.5-flash-lite`) — that is faster than waiting.

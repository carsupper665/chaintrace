# Middle layer

The browser never calls the Go API or a chain data provider directly. Every
domain read and write goes through a same-origin Next.js route under
`app/api/`, which attaches the Owner's credential cookie as a bearer token and
proxies to `/api/v1/*` on `CHAINTRACE_BACKEND_URL`
(`investigation-backend.ts`). Backend status codes and machine-readable `code`
values are preserved; the proxy never falls back to a public chain source or
fabricates domain data.

`*-contract.ts` holds the types and route constants. `*-client.ts` holds the
browser-side client that validates responses and maps stable codes to display
text. Neither layer computes domain values — risk scores, metrics and totals
are backend-owned.

Everything is keyed by **Investigation ID**. There are no address-based
endpoints; the old `/api/middle/risk-score`, `/api/middle/investigation-metrics`,
`/api/middle/anomaly-analysis`, `/api/middle/transaction-graph` and
`/api/middle/agent/chat` routes were removed with the Ethereum prototype.

## Investigations, Analysis Runs and results

`investigation-contract.ts` / `investigation-client.ts`

| Frontend route | Backend route |
| --- | --- |
| `GET/POST /api/investigations` | `/api/v1/investigations` |
| `GET/PATCH/DELETE /api/investigations/{id}` | `/api/v1/investigations/{id}` |
| `POST /api/investigations/{id}/analysis-runs` | start a run, `202` + run ID |
| `GET/DELETE /api/investigations/{id}/analysis-runs/{runId}` | poll / cancel |
| `GET /api/investigations/{id}/current-result` | current stable result |

- `Investigation.status` is `待處理`, `分析中` or `已完成`. `network` is always
  `TRON_MAINNET`.
- Amounts are `ExactAmount` (`smallestUnit` string + `decimals` + `asset`) and
  must never be coerced to a JavaScript `number`. Use `formatExactAmount`.
- Scope is bounded client-side and server-side: Transfer Limit 1–5000 (default
  500), Traversal Depth 1–4 (default 2).
- `pollAnalysisRun` backs off from 1s up to 5s and treats a shared
  `rate_limited` response as backpressure rather than a failed run.
- Terminal run states are `completed`, `failed` and `cancelled`. A run missing
  after a backend restart polls as `run_lost`; the client then reloads the
  previous stable result and offers resubmission.
- Stable codes handled here: `unauthorized`, `investigation_not_found`,
  `invalid_tron_target`, `invalid_analysis_scope`,
  `immutable_investigation_target`, `analysis_run_active`, `run_lost`,
  `rate_limited`.

## Transaction graph

`transaction-graph-contract.ts` / `transaction-graph-client.ts`

`GET /api/investigations/{id}/graph?datasetId=&cursor=&pageSize=&anchor=`

- Every page is bound to one Analysis Dataset. A request against a Dataset that
  is no longer current returns `stale_dataset`.
- Nodes carry domain identity only; the backend never returns `x`, `y` or
  `group`, and the client rejects a page that does. Layout is added in
  `services/transactionGraphBrowser.ts`.
- Edges are individual TRC20 Transfer events, identified by parent transaction
  hash plus event identity, so several Transfers from one transaction stay
  distinct.
- `anchor` discloses more relationships from the same Dataset. It never calls a
  chain provider, starts a run or changes the assessment.

## Conversation and Agent

`conversation-contract.ts` / `conversation-client.ts`

`GET/POST /api/investigations/{id}/conversation`

- Pagination walks **forward** in durable order: the first page is the oldest,
  and each `nextCursor` discloses newer messages. Pages are appended, never
  prepended.
- Submitting requires an Owner-scoped `idempotencyKey`. Retrying with the same
  key does not duplicate records.
- The MVP configures no Agent provider. A successful submit responds `503` with
  `{"code":"agent_unavailable","persisted":true,"messages":[…]}` — the user
  message and a structured system event, both durably stored. That system event
  is rendered as a system outcome, never as an Agent reply, and no mock reply is
  generated anywhere in the frontend.

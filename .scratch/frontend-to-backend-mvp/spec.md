# Frontend-to-Backend Investigation MVP

Status: ready-for-agent

## Problem Statement

ChainTrace currently presents an Investigation workspace, but its durable domain state is browser-local and its data flow does not match the intended product. The frontend seeds Ethereum and Bitcoin Investigations, validates Ethereum addresses, can fall back directly to an Ethereum data source, derives visible graph totals with floating-point numbers, sends frontend graph data back for assessment, and stores Investigations, graph evidence, assessments, and conversation history in local storage. The Go backend currently exposes user registration and login scaffolding but has no Investigation, Analysis Dataset, graph, assessment, or conversation domain APIs.

This split prevents ChainTrace from providing owner-scoped persistence, a reproducible evidence boundary, exact token amounts, consistent metrics, or trustworthy completed assessments. Graph expansion can change the evidence being analyzed, browser rendering choices can change aggregate values, and a transaction hash cannot uniquely identify every TRC20 Transfer produced by one Blockchain Transaction. Provider failures and empty results also cannot be represented without risking a misleading zero or low-risk display.

The existing authentication path is not a safe Owner identity baseline. Password verification, challenge validation, and token revocation behavior contain severe correctness errors, while protected domain routing is not established. Moving browser-local Investigations into a shared database before establishing a verified Owner identity would risk exposing one Owner's data to another.

The MVP therefore needs one end-to-end, authenticated Investigation workflow in which the backend is authoritative for Investigation records, completed Analysis Datasets, normalized blockchain evidence, Transaction Graph data, Investigation Metrics, completed assessments, and conversation history. It must support TRON mainnet TRC20 USDT first, remain explicit about bounded or partial evidence, and preserve responsive frontend-owned presentation behavior without pretending that deterministic rules or an unavailable Agent are machine learning.

## Solution

Deliver an authenticated, owner-scoped Investigation API and connect the existing frontend middle layer to it. An Owner can create, list, open, rename, retarget while eligible, analyze, cancel, and delete an Investigation. The Investigation is the canonical domain entity, and the Investigation Session is the frontend working context with the same ID, not a separately persisted lifecycle.

Starting analysis creates an in-memory Analysis Run and immediately returns an accepted response. A background goroutine collects a bounded Analysis Dataset for TRON mainnet TRC20 USDT through a network-neutral ChainDataProvider interface whose only production implementation is TronGrid. The collection window is the trailing 30 days ending at a captured confirmed block cutoff. It admits only successful, confirmed TRC20 Transfers, preserves both Blockchain Transactions and individual Transfer events, uses exact smallest-unit strings plus decimals, and obeys the requested Transfer Limit and Traversal Depth within the MVP defaults and maxima.

The backend applies a simple, deterministic, explicitly versioned `rules-v1` analyzer to the completed or usable partial Analysis Dataset. On successful publication, the Analysis Dataset, Transaction Graph source data, Investigation Metrics, Analysis Coverage, confidence, stop reason, and completed assessment are committed atomically to PostgreSQL. The previous stable result remains available until that commit. A cancelled run publishes nothing. A backend restart may lose active in-memory work; polling then returns `RUN_LOST`, and the frontend restores the previous stable result and offers a new run.

The frontend obtains all durable domain data through its BFF/client boundary and authenticated identity. It retains only presentation and transient interaction state, including node coordinates, zoom, pan, selection, panel state, theme, graph disclosure state, segmented rendering, and client-side CSV/PDF generation. Graph pagination and node expansion disclose additional relationships from the same Analysis Dataset and never collect or analyze new blockchain evidence.

The Agent boundary is introduced as a provider interface, but the MVP configures no LLM provider, generates no simulated response, and returns `AGENT_UNAVAILABLE`. Agent Task state remains independent from Investigation Status. Conversation messages and structured system events are stored by the backend in ordered, fixed-size SQL chunks rather than high-frequency token writes.

## User Stories

1. As an Owner, I want to authenticate before accessing ChainTrace domain APIs, so that my Investigations are not exposed to unauthenticated users.
2. As an Owner, I want the backend to derive my identity from verified authentication state rather than request payloads, so that another user cannot impersonate me by supplying an Owner ID.
3. As an Owner, I want to see my authenticated identity in the workspace, so that the interface does not present a hard-coded user as if it were me.
4. As an Owner, I want invalid, expired, malformed, or revoked credentials to be rejected consistently, so that protected Investigation data has a dependable security baseline.
5. As an Owner, I want a valid login flow to issue credentials that protected APIs actually accept, so that authentication correctness can be verified end to end.
6. As an Owner, I want requests for another Owner's Investigation to reveal no domain data, so that ownership boundaries cannot be bypassed or enumerated.
7. As an Owner, I want all Analysis Datasets, transactions, Transfers, assessments, metrics, and conversations to inherit my Investigation's ownership, so that related records cannot leak independently.
8. As a product operator, I want the MVP identity work limited to safe per-user ownership, so that delivery is not blocked by enterprise IAM, organizations, or role administration.
9. As an Owner, I want to create an Investigation with a title and an optional Investigation Target, so that I can establish a workspace before I know the final address.
10. As an Owner, I want a new Investigation to default to `待處理`, so that its status accurately states that it has no completed analysis.
11. As an Owner, I want to list only my Investigations, so that the workspace is populated from authoritative server data rather than seeded browser examples.
12. As an Owner, I want Investigation lists to include the current stable risk summary and Investigation Metrics when available, so that I can compare my work without opening every Investigation.
13. As an Owner, I want to open an Investigation by its stable ID, so that its Investigation Session, graph, analysis, and conversation all refer to one identity.
14. As an Owner, I want to rename my Investigation without changing its evidence or lifecycle, so that I can organize my work safely.
15. As an Owner, I want to edit the address and Network while an Investigation has never published an analysis, so that I can correct an initially incomplete Investigation Target.
16. As an Owner, I want the address and Network to become immutable after the first analysis result is successfully published, so that later evidence and conversations cannot silently refer to another target.
17. As an Owner, I want a clear conflict response if I try to change a locked Investigation Target, so that the UI can direct me to create another Investigation instead.
18. As an Owner, I want to delete an Investigation and make all of its domain records inaccessible, so that obsolete work does not remain in my workspace.
19. As an Owner, I want Investigation Status to use only `待處理`, `分析中`, and `已完成`, so that it communicates the analysis lifecycle rather than task or review state.
20. As an Owner, I want a failed, cancelled, or lost first run to return the Investigation to `待處理`, so that failure is not presented as completion.
21. As an Owner, I want a failed, cancelled, or lost re-analysis to restore `已完成` with the previous stable result, so that transient work does not erase a valid assessment.
22. As an Owner, I want TRON mainnet to be the only selectable Network in the MVP, so that the UI does not promise unsupported Ethereum or Bitcoin behavior.
23. As an Owner, I want TRON address checksum validation and TRON-specific display, so that an Ethereum-shaped string is not accepted for a TRON Investigation Target.
24. As an Owner, I want to start an Analysis Run without holding an HTTP request open, so that a long provider traversal does not block the browser request.
25. As an Owner, I want a newly started Analysis Run to report `queued` and then `running`, so that I can see that background work was accepted and began.
26. As an Owner, I want to poll Analysis Run status and bounded progress, so that the workspace can show useful progress without requiring WebSockets.
27. As an Owner, I want Analysis Run terminal states to be `completed`, `failed`, or `cancelled`, so that the result of each attempt is unambiguous.
28. As an Owner, I want only one `queued` or `running` Analysis Run per Investigation, so that concurrent attempts cannot race to publish different current results.
29. As an Owner, I want a conflict response when another run is already active, so that repeated clicks do not start duplicate provider work.
30. As an Owner, I want to cancel an active Analysis Run, so that I can stop work I no longer need.
31. As an Owner, I want cancellation to publish no new Analysis Dataset, metrics, or assessment, so that incomplete work cannot replace my stable result.
32. As an Owner, I want a completion/cancellation race to have one atomic winner, so that a cancelled run cannot publish after the cancellation response.
33. As an Owner, I want a lost in-memory run to return `RUN_LOST`, so that a backend restart is distinguishable from an ordinary provider failure.
34. As an Owner, I want the previous stable result restored after `RUN_LOST`, so that the workspace does not become blank after a backend restart.
35. As an Owner, I want to resubmit analysis after `RUN_LOST`, so that accepted MVP work loss is recoverable without editing the Investigation.
36. As an Owner, I want an Analysis Scope to use a fixed trailing 30-day Analysis Window, so that the meaning of every result is explicit and comparable.
37. As an Owner, I want the Analysis Window to end at a confirmed block cutoff captured when the run begins, so that provider pagination does not move the evidence boundary during collection.
38. As an Owner, I want the default Transfer Limit to be 500 and the default Traversal Depth to be 2, so that ordinary analysis is predictably bounded.
39. As an Owner, I want the maximum Transfer Limit to be 5,000 and the maximum Traversal Depth to be 4, so that one request cannot create unbounded provider or database work.
40. As an Owner, I want invalid Analysis Scope values rejected before a run starts, so that background workers only receive valid bounded work.
41. As an Owner, I want Traversal Depth to count transaction-relationship hops from the Investigation Target, so that depth has a stable domain meaning.
42. As an Owner, I want Transfer Limit to count unique Eligible Transfers admitted to the Analysis Dataset, so that duplicate provider pages do not consume the limit or distort metrics.
43. As an Owner, I want the backend to collect through a provider-neutral interface, so that a future fallback can replace TronGrid without changing Investigation or analysis contracts.
44. As an Owner, I want only successful and confirmed Blockchain Transactions to contribute TRC20 Transfers, so that pending or reverted activity is never treated as fund flow.
45. As an Owner, I want only TRON mainnet TRC20 USDT events in the MVP Analysis Dataset, so that unrelated assets and Networks do not contaminate the assessment.
46. As an Owner, I want both the Blockchain Transaction and each TRC20 Transfer event preserved, so that every graph relationship retains auditable on-chain provenance.
47. As an Owner, I want multiple TRC20 Transfers from one Blockchain Transaction to remain distinct, so that transaction hash de-duplication does not discard real token movements.
48. As an Owner, I want graph edge identity to represent a Transfer event within its parent Blockchain Transaction, so that pagination and frontend merging de-duplicate the correct domain record.
49. As an Owner, I want every Transfer Amount represented as an exact smallest-unit string with decimals, so that large or fractional USDT values are never rounded by floating-point conversion.
50. As an Owner, I want Investigation Metrics to be derived from the entire Analysis Dataset, so that graph rendering and pagination do not change analytical totals.
51. As an Owner, I want related-address count, Transfer count, total flow, and Asset returned together, so that all metric displays share one dataset and one meaning.
52. As an Owner, I want total flow to sum each unique admitted Transfer exactly once, so that duplicate pages and graph disclosure do not inflate it.
53. As an Owner, I want a bounded collection that reaches an expected scope boundary to explain that boundary, so that a Transfer or Traversal limit is not confused with exhaustive chain history.
54. As an Owner, I want an unexpectedly interrupted but usable collection labeled as a Partial Analysis, so that I can distinguish provisional evidence from a normally bounded result.
55. As an Owner, I want every published result to include partial status, confidence, Analysis Coverage, and a stop reason, so that a score is never detached from its evidence quality.
56. As an Owner, I want Analysis Coverage to report the requested and collected Eligible Transfer counts and reached depth, so that I can understand what the bounded run actually observed.
57. As an Owner, I want provider rate limiting, provider failure, resource limits, and expected scope boundaries to have distinct stop reasons, so that operational failure is not described as normal completion.
58. As an Owner, I want a completed zero-Transfer collection to display insufficient evidence rather than low risk, so that absence of observed evidence is not a safety claim.
59. As an Owner, I want a run that fails before collecting usable evidence to leave the previous stable result unchanged, so that an empty provider failure is not published as an assessment.
60. As an Owner, I want the initial risk assessment to identify itself as deterministic `rules-v1`, so that it is not mistaken for machine learning or an LLM opinion.
61. As an Owner, I want the same normalized Analysis Dataset to produce the same order-independent score, severity, reasons, and address assessments, so that results are reproducible.
62. As an Owner, I want each nonzero Risk Score to include contributing reasons, so that the score can be traced to deterministic evidence.
63. As an Owner, I want a Risk Score interpreted together with confidence and Analysis Coverage, so that a bounded score is not presented as an unconditional address rating.
64. As an Owner, I want human-readable Interpretation to be clearly unavailable when no interpretation provider exists, so that deterministic reasons are not presented as generated Agent advice.
65. As an Owner, I want the backend to publish a new completed result atomically, so that graph data, metrics, assessment, coverage, and the current result pointer never describe different runs.
66. As an Owner, I want completed Analysis Datasets and assessments to survive process restarts, so that only active work, not completed work, is ephemeral.
67. As an Owner, I want Transaction Graph pages to identify their Analysis Dataset, so that the frontend cannot accidentally merge relationships from different analyses.
68. As an Owner, I want graph pagination to return stable, opaque cursors, so that I can reveal a large bounded dataset incrementally.
69. As an Owner, I want node expansion to disclose relationships already in the same Analysis Dataset, so that clicking a node does not call TronGrid or change the assessment.
70. As an Owner, I want graph pages to omit node coordinates and other view state, so that backend evidence is independent of one visualization.
71. As an Owner, I want frontend node coordinates, zoom, pan, selection, panel layout, theme, and disclosure state to survive ordinary UI interactions, so that server authority does not make the workspace unresponsive.
72. As an Owner, I want frontend view state invalidated when the current Analysis Dataset changes, so that old coordinates or disclosure cursors are not applied to new evidence incorrectly.
73. As an Owner, I want visible graph filters and segmented rendering to affect only presentation, so that hidden edges remain included in backend metrics and assessment.
74. As an Owner, I want exact Transfer Amounts formatted for display without converting them to JavaScript floating point, so that the browser preserves backend precision.
75. As an Owner, I want CSV exports generated in the browser from the disclosed backend data and exact amount fields, so that export does not require server-side rendering or lose precision.
76. As an Owner, I want PDF exports generated in the browser from the current stable assessment and graph view, so that the existing presentation workflow remains frontend-owned.
77. As an Owner, I want exported reports to include the Dataset identity, Analysis Window, coverage, confidence, partial status, stop reason, Asset, and analyzer version, so that downloaded evidence retains its analytical context.
78. As an Owner, I want stale browser-local Investigation records to stop overriding server records, so that local examples and old snapshots cannot replace authoritative data.
79. As an Owner, I want harmless local presentation preferences preserved independently from domain data, so that the authority migration does not remove theme and panel preferences.
80. As an Owner, I want to reload or use another authenticated browser and recover my Investigations, completed graph data, metrics, assessments, and conversation, so that domain continuity no longer depends on one local-storage snapshot.
81. As an Owner, I want conversation history associated with the Investigation ID, so that the Investigation Session does not acquire a second identity.
82. As an Owner, I want my submitted conversation messages durably ordered, so that reopening an Investigation restores the same history.
83. As an Owner, I want conversation content written in fixed-size ordered chunks, so that future streamed responses do not create a database write for every token.
84. As an Owner, I want an Agent request to return `AGENT_UNAVAILABLE` while no provider is configured, so that the MVP is truthful about Agent capability.
85. As an Owner, I want no mock Agent message generated by the frontend or backend, so that unavailable functionality cannot be mistaken for analysis.
86. As an Owner, I want `AGENT_UNAVAILABLE` to leave Investigation Status unchanged, so that an Agent interaction is not confused with an Analysis Run.
87. As an Owner, I want any future Agent Task to use `accepted`, `running`, or `completed` independently from Investigation Status, so that the canonical lifecycle separation remains intact.
88. As an Owner, I want an Agent provider to receive structured Investigation evidence through a stable interface, so that a future provider can interpret results without changing scores or inventing evidence.
89. As a product operator, I want production domain persistence to require PostgreSQL, so that supported deployments do not silently use a local SQLite database.
90. As a developer, I want SQLite available only for explicitly selected local tests, so that fast local checks remain possible without treating SQLite behavior as production behavior.
91. As a product operator, I want active Analysis Runs and Agent Tasks kept in process memory for the MVP, so that the first release avoids an external job queue while acknowledging restart loss.
92. As a developer, I want deterministic error codes for authorization, validation, active-run conflicts, immutable targets, stale datasets, lost runs, provider failures, and unavailable Agents, so that frontend behavior does not depend on parsing prose.
93. As a developer, I want the frontend BFF to forward authenticated context and preserve backend error semantics, so that the browser never needs to call chain providers or know the backend deployment URL.
94. As a developer, I want TronGrid behavior verified from recorded responses rather than live CI calls, so that pagination, confirmation filters, rate limits, and precision tests are repeatable.
95. As a developer, I want primary integration tests to exercise the public Go HTTP API against PostgreSQL with fake providers and analyzers, so that domain behavior is tested at the highest practical seam.
96. As a developer, I want frontend tests to exercise BFF/client contracts and critical user flows rather than hook internals, so that refactoring does not break tests that only assert implementation details.

## Implementation Decisions

### Domain Authority And Identity

- The Go backend is the authoritative source for Investigations, current and historical completed Analysis Datasets, normalized Blockchain Transactions and TRC20 Transfers, Transaction Graph data, Investigation Metrics, completed assessments, and conversation history.
- The frontend remains authoritative only for presentation and transient interaction state: node coordinates, graph grouping, zoom, pan, selection, view history, panel dimensions and collapse state, theme, disclosed graph segments, and CSV/PDF generation.
- Investigation is the aggregate root. Every durable child record is reached through and owner-scoped by its Investigation. The Investigation Session uses the Investigation ID and is not a separate persisted entity or lifecycle.
- Owner identity is derived exclusively from verified authentication state. Domain requests never accept an Owner ID as an authority-bearing field.
- Before any Investigation endpoint is enabled, the authentication baseline must correctly verify password credentials and challenges, issue usable credentials, verify the expected signature algorithm and registered claims, reject expired or not-yet-valid credentials, apply token revocation in the correct direction, resolve the referenced User, and place that User ID into request context.
- Missing or invalid authentication returns `401`. Authenticated access to a record not owned by the caller returns a non-enumerating `404`. Authorization checks are applied in database queries, not only after loading an unrestricted record.
- A minimal authenticated identity endpoint returns the stable User ID and display fields needed by the frontend. The hard-coded identity is removed, and BFF requests propagate authenticated context without accepting identity from browser request bodies.
- This baseline does not introduce enterprise single sign-on, organizations, sharing, role-based Investigation permissions, account administration, or a general IAM redesign.

### Investigation Lifecycle

- An Investigation stores a stable opaque ID, Owner ID, title, optional address, explicit Network, Investigation Status, current completed result reference, target-lock state, and timestamps.
- TRON mainnet is the only available MVP Network. Contracts remain Network-explicit and network-neutral so future providers do not require redefining Investigation identity.
- A new Investigation may omit its address and starts in `待處理`. Its Network defaults to TRON mainnet.
- Title is editable throughout the Investigation lifecycle. Address and Network are editable only until the first Analysis Run atomically publishes a result, including a usable Partial Analysis. Failed, cancelled, or lost runs do not lock the Investigation Target.
- Starting a run changes Investigation Status to `分析中`. Successful result publication changes it to `已完成`. A failed, cancelled, or lost run restores `已完成` when a previous stable result exists and otherwise restores `待處理`.
- Agent requests and Agent Task state never mutate Investigation Status.
- List responses are paginated and include the current stable risk summary and Investigation Metrics where available. They do not calculate values from active or partially rendered graph state.
- Deleting an Investigation transactionally makes its completed datasets, evidence, assessments, and conversation inaccessible to the Owner. Retention, audit archive, and recovery policies beyond this product deletion behavior are outside this MVP.

### HTTP And BFF Contracts

- The authenticated API exposes the current identity, Investigation collection CRUD, single-Investigation retrieval and update, Analysis Run start/poll/cancel, current completed analysis retrieval, Dataset-bound graph pagination/expansion, conversation retrieval, and Agent request boundaries.
- Investigation APIs are keyed by Investigation ID, not by free-standing address and Network query pairs. Address-only risk, metrics, graph, and anomaly endpoints are replaced rather than retained as parallel authorities.
- Starting an Analysis Run returns `202 Accepted` with an opaque run ID, `queued` status, accepted scope, and polling reference. It rejects an active run with `409` and `ANALYSIS_RUN_ACTIVE`.
- Polling returns the run ID, Investigation ID, status, bounded progress, accepted scope, and a machine-readable failure code when applicable. A terminal completed response references the newly current Analysis Dataset.
- Cancelling an active run is idempotent while cancellation is pending. The cancellation token is propagated through provider traversal and analysis. Publication and cancellation use a single synchronized terminal transition so only one can win.
- Polling a run that is absent after process restart returns `RUN_LOST`. Since run state is intentionally non-durable, the frontend then fetches the Investigation's current stable result instead of clearing it.
- Current analysis retrieval returns the Dataset identity, Analysis Scope, Analysis Coverage, Investigation Metrics, completed Anomaly Analysis, analyzer version, partial flag, confidence, stop reason, and timestamps as one consistent result boundary.
- Graph requests identify the Investigation and expected Analysis Dataset. Responses include Dataset ID, normalized nodes and Transfer-event edges, an opaque next cursor, and whether more relationships are available.
- A graph request against a Dataset that is no longer current returns `DATASET_STALE`; this prevents pages from different runs being merged. Historical result browsing is not required by the frontend MVP even though completed data may be retained.
- Expansion may include an anchor address or node identifier, but it queries only membership already present in the identified Analysis Dataset. It never invokes ChainDataProvider and never starts analysis.
- Conversation retrieval is cursor-paginated in durable message order. An Agent request uses the Investigation ID as session identity and does not duplicate target fields as an independent source of truth.
- Error responses use stable codes for at least authentication failure, invalid TRON target, invalid Analysis Scope, immutable Investigation Target, active Analysis Run, run not found/lost, stale Dataset, provider failure, insufficient evidence, and unavailable Agent. Human-readable text may be localized independently.
- BFF contracts preserve backend status codes and machine-readable codes, forward authenticated context, disable caching for mutable owner-scoped resources, and never fall back to a public chain provider or fabricated domain data.

### Persistence Model

- PostgreSQL is mandatory for production startup. SQLite is selected only explicitly for local tests and is not an automatic production fallback when a PostgreSQL configuration is absent.
- Durable records include Investigation, Analysis Dataset, Dataset membership/provenance, Blockchain Transaction, TRC20 Transfer, Investigation Metrics, completed Anomaly Analysis and address assessments, conversation message metadata, and conversation chunks.
- Active Analysis Runs and Agent Tasks live only in a concurrency-safe in-memory registry. Their polling/progress state is not required to survive restart.
- An Analysis Dataset is an immutable snapshot belonging to one Investigation and one Analysis Run attempt. It records its Network, Asset, captured block cutoff, Analysis Window, requested Transfer Limit, requested Traversal Depth, collection timestamps, Analysis Coverage, partial flag, confidence, and stop reason.
- Completed publication uses one PostgreSQL transaction to persist the Dataset and evidence, metrics, assessment, target lock, current result pointer, and final Investigation Status. The previous current result remains untouched unless the transaction commits.
- Blockchain Transaction provenance includes Network, transaction hash, confirmed block identity and timestamp, and successful/confirmed state sufficient to audit eligibility.
- A TRC20 Transfer belongs to one Blockchain Transaction and records a stable event identity within that transaction, contract/Asset identity, sender, recipient, exact smallest-unit amount string, decimals, and event timestamp.
- Transfer uniqueness within a Dataset is based on Network, parent transaction hash, and stable event identity. Transaction hash alone is never the Transfer or graph-edge key.
- Dataset membership is explicit so de-duplication, pagination, graph projection, metrics, and assessment all use exactly the same admitted set.
- Transfer Amount strings contain unsigned base-10 smallest units and are validated without exponent notation or decimal points. Decimals are stored separately. Database and JSON boundaries must not convert these values through floating point.
- Investigation Metrics store or return exact total flow in the same smallest-unit-plus-decimals representation as Transfer Amount. Related-address count excludes the Investigation Target, Transfer count counts unique Transfer events, and Asset identifies TRC20 USDT.
- Conversation messages have stable IDs, Investigation ownership, role/event type, timestamps, and ordered chunks. Content uses ordered chunks with a fixed maximum of 4 KiB of UTF-8 content per row, without splitting a code point. Chunks for one completed message or structured event are committed together; per-token SQL writes are prohibited.
- Database constraints and indexes support Owner-scoped Investigation lookup, one current result, Dataset-bound Transfer uniqueness, graph pagination order, and conversation ordering. Database constraints complement, rather than replace, service-level ownership checks.

### Analysis Scope And Collection

- The Analysis Window is exactly the trailing 30 days ending at the confirmed block cutoff captured at run start. The recorded cutoff, start, and end are reused for every provider page in that run.
- Transfer Limit defaults to 500 and accepts values from 1 through 5,000. Traversal Depth defaults to 2 and accepts values from 1 through 4. Invalid values fail synchronously and create no run.
- Traversal Depth counts Transfer relationships from the Investigation Target. The target is depth zero; Transfers incident to it are depth one. Collection maintains visited-address and admitted-Transfer sets to prevent loops and duplicate pages from changing scope.
- Transfer Limit counts unique Eligible Transfers admitted to the Dataset, not provider records fetched, Blockchain Transactions, graph nodes, or visible edges.
- Expected bounded stop reasons include source exhaustion within the window, Transfer Limit reached, Traversal Depth reached, and no Eligible Transfers. Reaching these declared boundaries does not by itself make the analysis partial.
- Unexpected partial stop reasons include provider rate-limit exhaustion, provider unavailability/error after retry, and a backend time or resource budget. A usable subset may be published only with `partial: true`, reduced confidence, complete Analysis Coverage, and the specific stop reason.
- Analysis Coverage includes the captured window/cutoff, requested and collected Eligible Transfer counts, requested and reached Traversal Depth, and stop reason. It describes the bounded dataset and never claims complete blockchain-history coverage.
- A normally completed Dataset with zero Eligible Transfers publishes an explicit insufficient-evidence assessment with a null Risk Score and no low-risk severity. If collection fails before establishing a valid zero-result boundary or before collecting usable evidence, the run fails and publishes nothing.
- Confidence is an integer from 0 through 100 calculated deterministically from coverage and collection quality. Partial results must have lower confidence than an equivalent normally bounded result. A zero-evidence assessment has confidence zero.

### Chain Data Provider

- Chain data acquisition is behind a Network-explicit ChainDataProvider interface that accepts the Investigation Target, Asset, captured cutoff/window, bounded traversal request, and cancellation context, and returns normalized pages plus provider-neutral cursor, rate-limit, confirmation, and error information.
- TronGrid is the only production provider implementation in this MVP. A fallback seam exists through provider selection and normalized contracts, but there is no second provider and no frontend chain-data fallback.
- The TronGrid adapter handles cursor/fingerprint pagination until an expected scope boundary, cancellation, configured request timeout, bounded retries, and `429` responses including `Retry-After` when supplied.
- After bounded retry exhaustion, a rate-limited collection may publish a usable Partial Analysis or fail without publication according to whether usable evidence exists. It must never silently return an apparently complete empty result.
- Only events for the configured TRON mainnet USDT contract are admitted. Token metadata is normalized and checked rather than trusted from frontend display fields.
- Only TRC20 Transfers whose parent Blockchain Transaction is both successful and confirmed are Eligible Transfers. Pending, failed, reverted, malformed, out-of-window, wrong-contract, wrong-Network, and duplicate events are excluded with observable collection accounting.
- The adapter preserves a stable event discriminator for multiple Transfers from one transaction. If provider responses require combining endpoints to establish provenance or success, that normalization remains inside the adapter.
- Amount parsing treats provider values as exact decimal strings in smallest units and validates decimals/token metadata. It never parses Transfer Amount through binary floating point.
- Provider errors are normalized into retryable rate-limit/transient failures, non-retryable invalid requests/data, cancellation, and unavailable-provider outcomes so run state and stop reasons remain provider-neutral.

### Graph And Metrics

- The backend Transaction Graph is a projection of one immutable Analysis Dataset. Nodes represent unique addresses and graph edges represent individual TRC20 Transfer events.
- Backend graph nodes contain domain identity and classification needed for rendering but no coordinates, groups, zoom, selection, or view history.
- Graph edges expose stable Transfer identity, parent Blockchain Transaction provenance, sender, recipient, exact amount, decimals, Asset, and timestamp. Multiple edges between the same addresses and multiple events from one transaction remain distinct.
- Pagination order is stable for the lifetime of a Dataset and its cursor is opaque and Dataset-bound. Repeating a page or expansion is idempotent at the contract level.
- Investigation Metrics are calculated once from all unique Dataset Transfers, not from a graph page. Total flow is the exact sum of every admitted Transfer once; visible graph filters and expansion do not mutate metrics.
- The frontend may add coordinates, visual type, groups, and other rendering fields after receiving backend nodes. It de-duplicates edges by Transfer identity and discards stale responses whose Dataset ID does not match the active stable result.
- Frontend graph expansion and segmented rendering reveal already collected relationships. They do not request graph data by arbitrary address, invoke TronGrid, alter Analysis Scope, recalculate the backend assessment, or change Investigation Status.

### Deterministic Assessment

- Risk evaluation is implemented as one replaceable analyzer interface. The production MVP implementation is a cohesive single-file `rules-v1` module; tests may replace it with a fake at the same interface.
- `rules-v1` consumes only normalized Analysis Dataset evidence and Analysis Coverage. It produces a nullable Risk Score, severity when a score exists, machine-readable contributing reasons, address-level assessments, analyzer version, and deterministic summary fields.
- Risk Score is an integer from 0 through 100. Severity bands and every rule threshold, weight, and reason code are centralized and versioned as part of `rules-v1`, not dispersed through controllers or frontend code.
- `rules-v1` fires each Investigation-level signal at most once: fan-out to at least 10 unique recipients contributes 20; fan-in from at least 10 unique senders contributes 15; forwarding at least 80% of received value within 60 minutes contributes 25; a target-connected path of at least two hops that forwards at least 70% at each hop within 24 hours contributes 25; and a path of two through four hops returning to an earlier address contributes 15. The score is the capped sum of contributions. Address-level assessments apply the same signals to each qualifying address.
- Severity bands are `low` for 0-24, `medium` for 25-49, `high` for 50-74, and `critical` for 75-100. A normal bounded result has confidence 100; a Partial Analysis has confidence `min(75, floor(100 * collected Eligible Transfers / requested Transfer Limit))`; zero evidence has confidence zero and no Risk Score. These values are centralized, versioned wiring defaults rather than validated predictive thresholds.
- Equivalent Dataset content produces identical output regardless of provider page or database row order. Reasons identify the evidence-derived rules that contributed, and their declared contributions reconcile with the final capped score.
- Zero Eligible Transfers produce no Risk Score or low-risk severity. Partial evidence may produce a score only alongside partial status, confidence, coverage, and stop reason.
- The analyzer is described in API and UI output as deterministic rules, not ML, AI, model inference, or an LLM. `rules-v1` calibration is an MVP heuristic and not a compliance or identity attribution claim.
- Interpretation is a separate provider concern and cannot alter score, reasons, evidence, or coverage. With no interpretation provider, the API exposes interpretation as unavailable rather than generating substitute prose.

### Analysis Run Execution

- A concurrency-safe in-memory run manager enforces at most one `queued` or `running` run per Investigation and Owner. It stores cancellation functions, progress, terminal status, and the previous stable Investigation state needed for status restoration.
- The HTTP start operation validates authentication, ownership, target completeness, TRON address, Network support, Analysis Scope, and active-run uniqueness before launching a goroutine.
- Run progress is coarse and monotonic, based on phases and bounded collection counters rather than fabricated percentage precision. Polling does not read or write high-frequency progress rows in PostgreSQL.
- The worker captures the confirmed cutoff, collects and normalizes the Dataset, computes full-Dataset metrics, evaluates `rules-v1`, then attempts atomic publication. Each phase observes cancellation.
- Cancellation or failure before publication leaves PostgreSQL current-result state untouched. The in-memory terminal state records a stable code suitable for polling while the process remains alive.
- On successful commit, the run becomes `completed` and polling references the current Dataset. If the commit fails, the run is `failed`, no partially persisted result becomes current, and the previous stable state remains authoritative.
- Process restart intentionally discards the run registry. The API treats a frontend-polled missing run as `RUN_LOST`; completed PostgreSQL results remain available through Investigation retrieval.

### Frontend Integration

- The frontend canonical Investigation type follows the backend entity and its three-value Investigation Status. Current risk and metrics are server summaries; they are not initialized from sample Ethereum/Bitcoin records or recalculated from visible graph edges.
- The Investigation Session state remains keyed by Investigation ID. A separate `sessionId` does not create a second domain object and target data is not repeated in analysis requests as an authority.
- Frontend middle contracts are updated around authenticated Investigation CRUD, Analysis Runs, current results, Dataset-bound graph pages, conversations, and Agent availability. Existing address-based risk/metrics/graph requests and frontend-submitted anomaly graphs are removed from the active flow.
- TRON mainnet is displayed as the only supported Network. Address entry uses network-aware TRON Base58Check validation, shows TRON guidance, and removes Ethereum/Bitcoin capability labels and public Ethereum fallback behavior.
- The frontend starts analysis, polls until a terminal state with a bounded interval/backoff, stops polling on unmount or Investigation switch, and prevents duplicate start actions while a run is active.
- During re-analysis, the previous stable graph and assessment remain available with a clear in-progress state. They are replaced only after the completed Dataset is fetched successfully.
- On `failed` or `cancelled`, the UI displays the run outcome and restores the correct stable Investigation Status. On `RUN_LOST`, it explains restart-related work loss, reloads the current stable result, and offers resubmission.
- Partial status, confidence, Analysis Coverage, and stop reason are visible beside the score and in exports. An insufficient-evidence result renders no zero/low-risk badge.
- Transfer and metric display uses string/BigInt decimal formatting from smallest units and decimals. Graph calculations, sorting, labels, CSV, and PDF must not coerce exact amounts to JavaScript `number`.
- Frontend local storage no longer persists Investigation records, target fields, graph evidence, metrics, assessments, or conversation as domain authority. Old domain snapshots are ignored rather than uploaded as trusted evidence.
- Theme, panel dimensions/collapse state, graph coordinates, zoom, pan, selection, and disclosed-segment state may remain local. Dataset-specific view state is keyed by authenticated Owner and Dataset ID and is discarded when the Dataset changes.
- CSV generation remains client-side and exports Transfer event identity, parent transaction hash, timestamp, sender, recipient, exact formatted amount, smallest-unit amount, decimals, Asset, Dataset ID, and coverage context.
- PDF generation remains client-side and uses the current stable backend result plus frontend layout. Export is disabled or clearly marked when no stable assessment exists and must label partial or insufficient-evidence results accurately.
- The authenticated identity response supplies the workspace user display. No hard-coded user, unsupported-Network status, mock Investigation, mock graph, mock score, or mock Agent response remains in the production flow.

### Conversation And Agent Boundary

- Conversation is a backend-owned ordered stream associated with one Investigation. It may contain user messages, real provider messages in a future release, and structured system events such as an unavailable-provider code.
- Submitting a message uses an Owner-scoped idempotency key and atomically persists the Owner's user message plus the resulting structured system event. With no production provider configured, the response reports `AGENT_UNAVAILABLE`; retries do not duplicate either record, and no Agent Task or agent-authored message is created.
- A structured unavailable event may be persisted for durable history, but it is identified as a system event and is not presented as an Agent reply.
- The Agent provider interface accepts the Investigation identity, current structured assessment, coverage, and authorized conversation context. A provider may never modify the Analysis Dataset, Investigation Metrics, Risk Score, reasons, or evidence.
- No LLM SDK, remote model, mock response generator, or deterministic pseudo-Agent is configured for this MVP.
- An unavailable provider creates no active Agent Task. The canonical `accepted`, `running`, and `completed` Agent Task states remain reserved for actual provider execution and remain in memory when introduced.

## Testing Decisions

- Tests assert externally observable behavior and stable contracts, not controller/service call order, goroutine implementation, ORM details, private helper functions, React hook internals, or frontend component state shape.
- The primary seam is the Go HTTP API running against a real PostgreSQL test database with a fake ChainDataProvider and fake analyzer. This is the highest practical seam because it exercises authentication, owner scoping, validation, routing, transactions, persistence, run coordination, and response contracts while keeping chain data and scoring deterministic.
- PostgreSQL integration tests isolate data per test and verify actual constraints and transactional publication. SQLite may support supplementary fast local tests but is not accepted as evidence for PostgreSQL ownership, uniqueness, exact-value, concurrency, or rollback behavior.
- Authentication contract tests cover successful credential issuance/use, malformed and expired credentials, incorrect password/challenge input, correct revocation behavior, absent Users, missing credentials, and non-enumerating cross-Owner access before domain tests rely on Owner identity.
- Investigation API tests cover owner-scoped create/list/get/update/delete, optional targets, TRON-only validation, lifecycle statuses, title edits, target edits before publication, target locking after full or partial publication, and preservation of unlocked targets after failed/cancelled runs.
- Analysis Run API tests cover `202` start, queued/running/completed transitions, progress shape, one-active-run conflict, failed state, idempotent cancellation, cancellation-versus-publication races, status restoration, completed result references, and `RUN_LOST` behavior after replacing the in-memory registry.
- Atomicity tests force provider, analyzer, and database publication failures and verify that no new Dataset, metrics, assessment, target lock, or current-result pointer becomes partially visible and that the previous stable result remains unchanged.
- Scope tests verify the exact 30-day captured window, stable cutoff across pages, default and maximum Transfer Limit, default and maximum Traversal Depth, invalid bounds, hop semantics, loop handling, duplicate-page handling, and unique Eligible Transfer counting.
- Collection behavior tests use fake provider scripts for source exhaustion, limit boundaries, partial data followed by rate limiting, provider unavailability, cancellation, malformed events, wrong Asset/Network, pending or failed transactions, duplicate events, and zero-evidence outcomes.
- Persistence and graph contract tests verify separate Blockchain Transaction and TRC20 Transfer records, multiple Transfer events per transaction, Transfer-event edge IDs, Dataset-bound cursors, stable pagination, stale Dataset rejection, idempotent page retrieval, same-Dataset expansion, and metrics that remain constant regardless of graph disclosure.
- Precision tests use amounts beyond JavaScript safe integer range, smallest nonzero units, large values, leading-zero input normalization, multiple Transfers in one transaction, and exact aggregate totals. Expected JSON and PostgreSQL values remain smallest-unit strings plus decimals throughout.
- Coverage and assessment tests verify expected versus unexpected stop reasons, partial flag, confidence bounds, reduced partial confidence, insufficient-evidence null score, deterministic order-independent `rules-v1` output, version labeling, reason reconciliation, and absence of ML/LLM claims.
- Conversation tests verify Owner scoping, stable ordering, UTF-8-safe 4 KiB chunk boundaries, atomic message assembly, cursor pagination, structured unavailable events, no mock Agent message, and no Investigation Status change after `AGENT_UNAVAILABLE`.
- TronGrid is tested separately at the adapter contract seam with recorded fixtures. Fixture suites cover pagination/fingerprints, overlapping pages, multiple events per transaction, success and confirmation filtering, USDT contract filtering, window filtering, token decimals, exact precision, malformed responses, timeouts, cancellation, `429` with and without `Retry-After`, bounded retry, and normalized partial/failure outcomes.
- TronGrid contract tests do not call the live service in normal CI. A manually invoked live smoke check may exist for operator diagnosis but is not a release gate and must not supply production credentials to fixtures.
- Frontend tests use the BFF/client contract seam with controlled HTTP responses. They verify authenticated identity/list loading, Investigation creation and target editing, TRON validation, run start/poll/cancel, duplicate-start prevention, completion cutover, previous-result preservation, partial and insufficient-evidence rendering, `RUN_LOST` recovery, and `AGENT_UNAVAILABLE` truthfulness.
- Frontend graph flow tests verify Dataset ID propagation, stale response rejection, same-Dataset pagination/expansion, no provider call or re-analysis on expansion, view-only filtering, metric stability, exact amount rendering, and Dataset-keyed local presentation restoration.
- Frontend export flow tests verify exact amount text, Transfer-event and parent-transaction identity, Dataset and coverage metadata, partial/insufficient labels, and that CSV/PDF generation remains browser-side.
- Existing server-rendered workspace/build tests remain useful as broad smoke and architecture checks, but they are not the primary behavioral seam and should not be expanded into source-text assertions for new domain behavior.
- End-to-end acceptance requires the Go API integration suite, TronGrid recorded-fixture suite, frontend BFF/client flow suite, frontend build, and existing render smoke tests to pass without live external dependencies.

## Out of Scope

- Ethereum, Bitcoin, EVM, native TRX, non-USDT TRC20 assets, multi-Asset aggregation, and cross-Network Investigations.
- A second chain data provider or automatic production provider failover. Only the fallback interface/selection seam is included.
- Unbounded blockchain history, a user-configurable Analysis Window, Transfer Limits above 5,000, or Traversal Depth above 4.
- Re-analysis triggered by graph pagination, graph expansion, visual filtering, node movement, or segmented rendering.
- Server-owned graph coordinates, graph layout, zoom, pan, selection, panel state, theme, CSV rendering, or PDF rendering.
- A graph database, event-stream platform, Redis, durable job queue, distributed worker fleet, workflow engine, WebSocket progress, or server-sent events.
- Persisting active Analysis Run or Agent Task state across process restarts. `RUN_LOST` and resubmission are the accepted MVP behavior.
- LLM integration, generated Interpretation, simulated Agent replies, Agent tool use, autonomous analysis changes, or any claim that `rules-v1` is ML.
- Enterprise IAM, SSO, organizations, team sharing, granular roles, external collaborators, Investigation assignment, or audit/compliance administration beyond safe Owner isolation.
- Sanctions screening, entity attribution, address labels from commercial intelligence, legal conclusions, compliance certification, or production risk-model validation.
- Real-time monitoring, alerts, continuous address surveillance, automatic scheduled re-analysis, or claims that the 30-day result updates without a new Analysis Run.
- Importing browser-local mock or prior address-derived evidence into PostgreSQL as trusted domain data. Only presentation preferences may survive the authority migration.
- Historical-result comparison UI, branching Investigation Targets, merging Investigations, or changing a locked target in place.
- A general localization redesign. Stable machine codes are required, while broader copy translation can be handled separately.

## Further Notes

- Current backend scope is substantially smaller than this MVP: it has user/authentication scaffolding and database initialization, but no Investigation router, controller, model, service, provider, run manager, or analysis persistence. The service layer is effectively empty.
- Current frontend behavior is a prototype authority rather than a backend client: it seeds local Investigations, persists domain snapshots in the browser, assumes Ethereum/Bitcoin, accepts Ethereum addresses, can query a public Ethereum source, represents edge amounts as numbers, identifies edges by transaction hash, merges new address queries during expansion, and sends visible graph data for assessment. The integration is therefore a contract migration, not merely filling in one provider function.
- The present authentication implementation has release-blocking correctness defects in password comparison, login challenge validation, and revoked-token handling. The safe Owner identity baseline is a prerequisite product capability for this MVP, even though a broader IAM program is not.
- Delivery should establish the identity baseline and owner-scoped PostgreSQL schema first, then prove the full HTTP workflow with fake provider/analyzer implementations, then add in-memory run execution and atomic publication, then validate the TronGrid adapter, and finally cut the frontend over from browser authority to Investigation-ID contracts.
- The fixed 30-day, 5,000-Transfer, depth-4 maxima bound data volume but not necessarily provider call count. The implementation still needs configured request, retry, run-time, and memory budgets that map exhaustion to explicit partial/failure outcomes rather than silent truncation.
- TronGrid quotas, response consistency, confirmation semantics, and stable event discriminators are external delivery risks. Recorded fixtures reduce regression risk, but production monitoring must distinguish rate limiting, malformed provider data, and internal failures.
- In-process goroutines deliberately trade durability and horizontal coordination for MVP simplicity. Multi-instance routing could make polling miss the process that owns a run; production deployment must use a single backend instance or sticky routing until durable workers are introduced.
- `rules-v1` enables reproducible product wiring but has no asserted predictive validity. UI and exports must retain its version, reasons, coverage, and confidence and avoid compliance-grade or ML claims.
- Exact amounts require coordinated contract changes across persistence, API, frontend graph rendering, metrics, CSV, and PDF. Any remaining numeric amount field is a precision regression risk.
- No ADR conflict was identified. This specification implements the existing decisions for backend domain authority, network-neutral TRON-first contracts, backend graph evidence with frontend rendering, immutable targets, rendering-independent analysis, separate transactions and Transfers, bounded best-effort coverage, TronGrid, PostgreSQL, pollable in-process runs, deterministic risk, and Owner scoping.

# Learned risk scoring — implementation plan

The decision and its reasoning are in [ADR-0015](adr/0015-learned-risk-scoring-alongside-rules.md). This is the order of work and what each phase has to prove before the next one starts.

## Where the code goes

```
agent/
├── app.py            the only contact point: one more route
├── llm/  prompt.py  session.py  turn.py     unchanged
└── scoring/          new, self-contained
    ├── features.py   transfers + target -> Address Feature Vector
    ├── schemas.py    request/response models for the route
    ├── service.py    load the model, score a vector
    ├── model/        the trained artifact and its manifest
    └── train/        offline training; the server never imports this

crawler/              standalone, deployable on its own
├── crawl.py
├── trongrid.py
└── README.md
```

`scoring/` imports nothing from `llm/`, `prompt.py`, `session.py` or `turn.py`, and none of those import `scoring/`. Deleting the directory leaves a working Agent minus `POST /v1/score`. A test asserts this boundary rather than trusting it.

The crawler sits outside `agent/` because it shares no code with anything: it fetches raw Transfers and writes them to disk, and never computes a feature. That is what lets the whole folder be copied to a machine with a faster network and run there against stock Python. Feature parity (ADR-0015) binds training to serving, and training is `scoring/train/`, not the crawler.

## Phase 0 — The feature vector

`scoring/features.py` takes a list of Transfers and a Target address and returns a fixed-length vector. It is a pure function: no network, no database, no model.

Twenty-two Depth 0 features. The count is deliberately smaller than the candidate list it came from, because an unsupervised model weighs every feature equally: four ways of saying "how much money" give the amount four times the say of anything else, and the model then ranks exchanges first because they are large rather than because they are odd. Correlated candidates are collapsed into one representative each.

| # | Feature | Note |
| --- | --- | --- |
| 1–2 | Transfer count in, out | `log1p` |
| 3 | In/out count ratio | |
| 4 | Net flow over total inflow | ratio, not an absolute |
| 5 | Median amount | `log1p` |
| 6 | Amount coefficient of variation | replaces separate total/maximum/dispersion |
| 7 | Largest single inflow as a share of total inflow | the ratio form of "maximum" |
| 8 | Pass-through share | inflow forwarded on within a short interval |
| 9 | Median holding time | |
| 10–11 | Distinct counterparties in, out | `log1p` |
| 12–13 | Counterparty HHI in, out | concentration |
| 14 | Counterparty reuse rate | |
| 15 | Active days over window days | |
| 16 | Coefficient of variation of inter-transfer gaps | burstiness and gaps are one feature, not two |
| 17 | Hour-of-day entropy | |
| 18 | Round-amount share | |
| 19 | Repeated-amount share | |
| 20 | **Address age** | first-ever transfer to now; needs one extra provider call |
| 21 | **Inflow/outflow amount matching** | share of inflows that pair with a near-equal outflow |
| 22 | **Structuring share** | amounts clustered just under a round threshold |
| — | Truncation flag | whether the transfer cap was reached |

Features 20–22 are additions, and 20 is the one worth the most for its cost. An address that first appeared three days ago and has moved two million dollars is a different object from a three-year-old address that moved the same amount, and a trailing 30-day window cannot see the difference. One provider call for the first-ever transfer timestamp buys it.

Feature 21 exists because net flow only compares totals. "Received 100, sent 100" and "received 1,000, sent 900" have the same net flow and are not the same behaviour; a forwarding hop shows individual inflows matched by near-equal outflows, which is one of the strongest laundering signals available at Depth 0.

Three rules that decide whether the model works at all:

- **Amounts are integers in the Asset's smallest unit** until the last step, then `log1p`. USDT amounts span several orders of magnitude; without this the model learns "who is rich", not "who is odd".
- **Prefer ratios and shapes over absolute counts.** Absolute counts move with the cap; ratios do not.
- **Truncation is a feature, not a secret.** Whether the transfer cap was reached is one of the inputs.

Hour-of-day entropy replaces any "night-time ratio": chain timestamps are UTC and the actor's timezone is unknown, so a night-time threshold encodes a guess. Entropy asks only whether activity is concentrated in time, which needs no such guess.

Phase 2 checks the correlation matrix before fitting anything. Any pair still above roughly 0.9 means one of them is redundant and the list above was not aggressive enough.

**Goal.** One pure function turning Transfers plus a Target into a 22-number vector, identical whether it runs in training or in a request.

**Accepted when all of these hold:**

1. Each of the 22 features has a test whose expected value was worked out by hand from a small transfer list — not captured from the implementation's own output.
2. Degenerate inputs return a defined value rather than raising: no transfers, inflows only, outflows only, a single transfer, all amounts identical (a zero denominator for every coefficient of variation), and several transfers sharing one timestamp.
3. Amounts stay exact. A test carries an amount above 2^53 in the Asset's smallest unit and asserts no precision is lost before the final `log1p`.
4. The same input produces a bit-identical vector on repeated runs.
5. `features.py` imports no HTTP client, no database driver and no model library. A test asserts this.
6. `scoring/` imports nothing from `llm/`, `prompt`, `session` or `turn`, and none of them import `scoring/`. A test asserts both directions.

## Phase 1 — Crawl a training set

`crawler/`, run by hand on whichever machine has the best network. Standard library only, no virtualenv, no third-party packages — the folder is the deployment unit.

Addresses are sampled from recent USDT transfer traffic — walk the transfers in a block range and take the addresses that appear — rather than by expanding outward from a seed. A seed expansion would train the model on "addresses near the ones we already investigated", which is the population we are trying to judge, not a baseline for it.

Each sampled address gets its full trailing 30-day window up to the model cap (10,000 transfers, not the Analysis Scope's 500). The manifest — crawl date, sampling start, window, cap, address count — is written **before the first fetch**, not at the end: a crawl killed halfway through otherwise loses the window it was using, and a resumed run would silently collect against a different one. Which addresses are already done is read back from the data file itself, so a half-written final line costs one address rather than the whole run.

Measured on a real run: about 10 API calls and 70 KB of gzipped data per address, so 1,000 addresses is roughly 10,000 calls and 70 MB.

**Goal.** A training set that describes ordinary USDT activity, rather than the neighbourhood of addresses somebody already found interesting.

**Accepted when all of these hold:**

1. At least 5,000 addresses are on disk, each with its trailing 30-day window or an explicit truncation flag.
2. A manifest records crawl date, block range, window, transfer cap, sample size and provider endpoint.
3. Re-running from that manifest selects the same addresses.
4. Addresses come from scanning transfers in a block range. A seed expansion is a failed Phase 1 even if it produces more rows.
5. The transfer-count distribution is inspected and is not concentrated in a handful of very large addresses.
6. `crawler/` is unreachable from the server. A test asserts the app's import graph excludes it.

## Phase 2 — Train, and above all validate

Baseline is `IsolationForest` with `RobustScaler` — robust scaling because a single exchange wallet would otherwise drag the mean and variance of every amount feature. Highly correlated features (total, median, maximum move together) get pruned so distance is not charged three times for the same thing.

Labels are used **only for evaluation**, never for training:

| Source | Use |
| --- | --- |
| OFAC SDN list | sanctioned TRON addresses — should score high |
| TRONSCAN address tags | exchanges and contracts — should **not** dominate the top |
| Chainabuse reports | user-reported scam addresses — should score high |

Fifty to a hundred known-bad and twenty known-exchange addresses are enough to tell whether the model does anything.

**Goal.** A model that ranks suspicious addresses above ordinary ones, and demonstrably not merely large ones.

**Accepted when all of these hold:**

1. The correlation matrix is computed before fitting. Any pair above roughly 0.9 is collapsed or dropped, and the decision is recorded.
2. The evaluation set holds at least 50 known-bad addresses (OFAC SDN, Chainabuse) and at least 20 known exchanges or contracts (TRONSCAN tags).
3. **Signal.** Known-bad addresses land in the top decile of the anomaly ranking at several times the rate a random address would — roughly 30% against the 10% a coin flip would give.
4. **Veto, and it outranks the signal.** Known exchanges do not form the majority of the top percentile. If they do, the model has learned "large and busy": return to Phase 0 and change features. Moving the threshold is not a fix, it is a way of hiding the result.
5. An ablation refits without address age and without inflow/outflow matching, and reports how far the signal falls. A feature that costs an extra provider call has to show it earns it.
6. The saved model carries a manifest: training-set hash, feature-list version, library versions.

## Phase 3 — Serve it, next to the rules

`POST /v1/score` in `app.py`, handled in `scoring/`. On the Go side a `RiskEvaluator` implementation calls it and stores the learned score beside the deterministic one.

The published Risk Score does not change in this phase. Both numbers are recorded so they can be compared on real Investigations.

**Goal.** Real Analysis Runs record a Learned Risk Score without changing what an Owner sees and without adding a new way for a run to fail.

**Accepted when all of these hold:**

1. After one Analysis Run the database holds both the deterministic score and the Learned Risk Score.
2. The Owner-visible Risk Score is unchanged: the existing router tests pass unmodified.
3. A scorer returning 5xx, or timing out, leaves the run `completed` with rules only. A test drives that path.
4. **Train/serve parity is checked end to end.** For one real address, the vector computed offline equals the vector computed inside a request, field by field. This is the last place train/serve skew can be caught before it becomes invisible.
5. The scorer call has a timeout below the run's own budget, and exceeding it counts as unavailable rather than failed.
6. Deleting `scoring/` entirely leaves the Agent starting and answering chat, minus one route.

## Phase 4 — Decide which score is published

**Goal.** Decide on evidence whether the Learned Risk Score replaces the published Risk Score, joins it, or stays internal.

**Accepted when all of these hold:**

1. Enough real Investigations carry both scores to compare them on something other than the evaluation set.
2. A written comparison says what each score caught, missed and flagged wrongly — not a single headline number.
3. An ADR records the decision and the evidence behind it.
4. If the published score changes, that is a breaking change to what an Owner reads: CONTEXT.md and the release note say so.

This phase is a decision, not code, and it cannot start until Phase 3 has been running for a while.

## Still open

- **Sampling.** Walking recent blocks weights the sample toward busy addresses. That may be the right baseline — most USDT addresses are busy — but it needs a sanity check against the crawled distribution before Phase 2.
- **Window.** Thirty days matches ADR-0007. Whether it is long enough for holding-time features to mean anything is a Phase 2 finding.
- **Neighbour features.** The seven Depth 1 features are excluded (ADR-0015). Including them needs collection to stop spending its budget in address order. One of them, "correlation with fund flow", also has to say correlation between which two quantities before it can be implemented at all.

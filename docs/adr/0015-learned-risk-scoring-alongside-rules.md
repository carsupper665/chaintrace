# Learned risk scoring alongside the deterministic rules

A second Risk Evaluator scores an Investigation Target with an unsupervised anomaly model trained on TRON USDT activity. It runs alongside the deterministic rules of ADR-0011 rather than replacing them: both scores are stored, and the published Risk Score stays the deterministic one until the learned score has been measured against known-bad and known-benign addresses. Which one gets published is a later decision, and it needs that measurement to make honestly.

One implementation of the Address Feature Vector serves both training and scoring. Training runs offline on evidence this repository crawls itself; scoring runs inside a request. If the two computed features differently the model would be guessing in production with no symptom — it would still return a plausible number — so the same code, the same trailing window and the same transfer cap apply to both, and whether a given collection was truncated is itself a feature rather than a hidden bias.

The model lives in its own package inside the Agent server process, sharing only the process and the HTTP router. It imports nothing from the LLM side and the LLM side imports nothing from it, so deleting the package leaves a working Agent minus one route. A separate service was the alternative; it was rejected because a second process to operate and configure buys no isolation that the import boundary does not already give.

Scoring reads the Target's own transfer history at the model's cap, not the Analysis Dataset assembled under ADR-0007. The first model also excludes neighbour features entirely: collection spends its transfer budget in address order, so at Traversal Depth 1 it censors neighbours alphabetically rather than at random, and a feature computed over that is an artifact of the sort rather than a property of the address.

## Consequences

A learned score carries no reason string, so the deterministic reasons remain the explanation an Owner reads. ADR-0011's rule that interpretation cannot alter scores still holds: the Agent explains both numbers and invents neither.

A trained model is evidence about the traffic it was trained on and nothing else. Its crawl date, window, transfer cap and address sampling are recorded with it, because a model fitted to one month of USDT traffic cannot be quoted as a finding about another.

# Agent operates investigation tools under backend authorization

The Agent may carry out investigation operations itself — reading an address, expanding a node, setting an Investigation Target, and starting an Analysis Run — rather than only describing what the Owner should do next. An Owner who writes "investigate this address" has already decided; making them press a confirmation button afterwards adds a step without adding a decision.

Every Tool Call is still authorized and executed by the backend, never by the Agent process: the backend resolves the Owner from its own records, refuses any address outside the Investigation's current Analysis Dataset, caps rows and calls per turn, and treats the Agent's arguments as untrusted input. The Agent has no database access, no Owner identity, and no credentials.

This supersedes the clause in ADR-0011 stating that the MVP ships no LLM provider. The rest of ADR-0011 stands: an Agent explains Risk Scores and evidence, and can neither change a score nor invent evidence.

## Consequences

The blast radius of a hallucinated address is a refusal, not a wrong answer or an unintended fetch. The cost ceiling is enforced structurally instead of by asking the Owner: ADR-0007 already allows only one active Analysis Run per Investigation, and the Agent cannot widen an Analysis Scope.

# An Agent Turn never waits for an Analysis Run

When an Agent starts an Analysis Run, its turn ends immediately with an answer saying collection has begun. It does not block until results exist. A second turn produces the assessment once the run publishes, and the frontend — which already polls run status under ADR-0010 — starts that turn without the Owner asking again.

Blocking was the alternative: one turn, simpler control flow. It was rejected because a run takes minutes while a turn is budgeted in seconds, so blocking would hold an HTTP request open for the whole collection, show no progress, and contradict ADR-0010's polling model.

## Consequences

The Owner sees one continuous conversation across two turns, and no message is ever attributed to them that they did not write. Agent Evidence must therefore describe an Analysis Run in progress, so a turn arriving mid-run can say so honestly instead of pretending there is nothing to report.

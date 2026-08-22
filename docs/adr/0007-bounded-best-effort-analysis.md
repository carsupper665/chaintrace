# Bounded best-effort analysis with explicit coverage

The MVP analyzes a fixed trailing 30-day window with configurable transaction and traversal limits, initially defaulting to 500 transfers and two hops and capped at 5,000 transfers and four hops. Collection may return a Partial Analysis when a provider or resource constraint stops it early, but every result must expose coverage, stop reason, confidence, and partial status so a provisional score cannot be mistaken for exhaustive blockchain analysis.

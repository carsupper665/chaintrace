# Deterministic risk evaluation with separate interpretation

The first MVP exposes risk evaluation through one replaceable service function that deterministically produces scores, reasons, and address-level assessments from an Analysis Dataset. LLM-based interpretation and Agent responses live behind separate provider interfaces and may explain those structured results but cannot alter scores or invent evidence; the MVP ships no LLM provider and reports Agent unavailability rather than generating simulated replies.

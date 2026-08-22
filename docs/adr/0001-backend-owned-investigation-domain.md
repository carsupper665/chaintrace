# Backend-owned investigation domain

The backend is the authoritative system for investigation records, transaction graph acquisition, derived metrics, risk and anomaly results, agent conversations, and durable lifecycle state. The frontend retains only presentation and transient interaction state, because server ownership enables identity-scoped persistence and consistent analysis while preserving responsive UI interactions.

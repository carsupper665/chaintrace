# Pollable in-process analysis runs

Dataset collection and assessment execute as in-process goroutines rather than synchronous HTTP requests. Starting a run returns `202 Accepted` and a run identifier, and the frontend polls in-memory status and progress until completed results are published to PostgreSQL; if the backend restarts, polling returns `RUN_LOST`, the frontend restores the previous stable result state, and the user may submit the run again. This accepts restart-related work loss to keep MVP execution simple while preserving completed investigation data.

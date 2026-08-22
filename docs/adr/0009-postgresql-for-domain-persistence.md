# PostgreSQL for investigation persistence

PostgreSQL is the production system of record for Investigations, completed Analysis Datasets, blockchain transactions, transfers, assessments, and chunked conversation history. Active Analysis Runs and Agent Tasks execute in memory and publish completed results to PostgreSQL. SQLite may support local tests, but it is not a production deployment option, and the MVP does not add a graph database because the bounded graph can be represented and queried within PostgreSQL without another operational dependency.

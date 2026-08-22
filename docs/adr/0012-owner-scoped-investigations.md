# Owner-scoped investigations

Every Investigation and its datasets, analyses, conversations, and completed results belong to one authenticated User, and all domain APIs enforce that ownership. The frontend's hard-coded user display is replaced by authenticated identity so moving browser-local investigations into PostgreSQL does not create a shared data pool visible to unrelated users.

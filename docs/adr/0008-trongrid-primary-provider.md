# TronGrid as the first chain data provider

The TRC20 MVP uses TronGrid as its only implemented chain data provider, behind a provider interface that normalizes pagination, confirmation filtering, token metadata, rate limits, and errors. A fallback provider seam is retained but no second provider is implemented in the MVP, balancing delivery speed against future provider failure and lock-in risk.

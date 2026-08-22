# Network-neutral contracts with a TRON-first provider

Investigation and analysis contracts identify the blockchain network explicitly and remain neutral to network-specific address and transaction formats. Providers encapsulate network-specific behavior, while the MVP first ships TRON mainnet support for TRC20 USDT transfers; the frontend must use TRON address validation and must not present Ethereum as available until its provider is delivered.

# 06 — 接通 production TronGrid TRC20 USDT

**What to build:** Owner 在 Web 輸入真實 TRON mainnet address 後，既有 Analysis Run path 使用 TronGrid 收集 confirmed TRC20 USDT evidence，保存 PostgreSQL 並顯示與 fixture provider 相同的 Dataset、metrics 與 assessment contracts；一般 CI 不依賴 live network。

**Blocked by:** 05 — 完成 bounded multi-hop Analysis 與 rules-v1.

**Status:** ready-for-agent

- [ ] TronGrid 是唯一 production provider implementation，但位於 network-neutral seam 後；fallback 插槽存在而不實作第二家 provider，frontend 不直接呼叫 TronGrid。
- [ ] Adapter 固定 mainnet USDT contract、30-day timestamps、`only_confirmed`、pagination/fingerprint 與 request timeout，並驗證 parent transaction successful/confirmed provenance。
- [ ] Pending、failed、wrong contract/Asset/Network、out-of-window、malformed 與 duplicate events 不進入 Dataset；同 transaction 多個 Transfer events 維持獨立 identity。
- [ ] 429、403、timeout 與 transient errors 使用 bounded retry/backoff/cancellation並正規化成 provider-neutral stop reason；有 usable evidence 時遵守 Partial Analysis，否則 run failed 且保留前一 stable result。
- [ ] TronGrid token value 全程保留 raw integer string 與 decimals；metadata、response size、page count 與 memory 使用有明確防護，不信任 frontend symbol/value。
- [ ] Recorded fixtures 經過 Web start/poll、Go provider、PostgreSQL publication 與 frontend result rendering 的完整 journey，涵蓋 pagination、overlap、confirmed/success filtering、multi-event、429 與 precision；另可手動執行 live smoke，但不作 CI gate。

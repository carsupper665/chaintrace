# 11 — Harden 並驗證 Investigation MVP 端到端旅程

**What to build:** 以 production-like 設定反覆驗證 Owner 從 Web login、管理 Investigation、TRC20 analysis、graph、Session 到 exports 的完整旅程，補齊安全隔離、provider failures、operational limits 與 release gates，讓 MVP 可交付。

**Blocked by:** 06 — 接通 production TronGrid TRC20 USDT; 07 — 分頁呈現 saved Transaction Graph; 08 — 完成 Analysis Run 取消、遺失與 Target 鎖定; 09 — 保存 Session conversation 並呈現 Agent unavailable; 10 — 以 backend result 產生 client-side CSV 與 PDF.

**Status:** ready-for-agent

- [ ] Production-like PostgreSQL journey 使用兩個 test Owners，從 Web login/callback 開始驗證 create/list/select、Target edit、run/poll/result、graph pagination、conversation、rename/delete、logout 與 export，全程無跨 Owner metadata/data leakage。
- [ ] Deterministic recorded TronGrid journey 涵蓋 confirmed USDT、unconfirmed/failed exclusion、multi-hop/multi-event、exact amount、500/5000 limits、depth 2/4、Partial Analysis、zero evidence 與 provider failure；一般 CI 不依賴 live network。
- [ ] Lifecycle journey 覆蓋 single active run、cancel/publication race、`RUN_LOST`、retry、previous stable result、Target lock、re-analysis、stale Dataset 與 delete/worker race。
- [ ] Security matrix 對每個 nested API 驗證 unauthenticated、wrong Owner、malformed/oversized input、invalid cursor/result、credential revocation 與 upstream error；logs 不記錄 credential、password、challenge 或敏感 payload。
- [ ] Provider calls 具 bounded timeout、context cancellation、rate-limit handling、有限 retries/backoff、response-size 與 memory safeguards；request/Investigation/run IDs 足以關聯 operational failure。
- [ ] Browser journey 同時覆蓋 desktop/mobile 的 auth guard、TRON-only input、polling/cancel/lost、reload persistence、graph expansion不重跑分析、partial warning、Agent unavailable 與 CSV/PDF。
- [ ] Legacy Ethereum/Blockscout fallback、hard-coded identity/demo Investigations、frontend score/metrics authority、browser-local domain persistence、fixed production secrets 與 wildcard credentialed CORS 全部移除。
- [ ] Go PostgreSQL integration tests、recorded provider tests、frontend flow tests、browser E2E、lint 與 production builds 全部通過；每張前置 ticket 的 journey 可單獨執行並指出 frontend、API 或 SQL regression。

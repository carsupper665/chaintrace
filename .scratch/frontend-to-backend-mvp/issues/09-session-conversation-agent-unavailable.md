# 09 — 保存 Session conversation 並呈現 Agent unavailable

**What to build:** Owner 在 Web Investigation Session 提交 command 後，backend 以固定 chunks 保存 durable conversation；MVP 沒有 LLM provider 時原子記錄 user message 與 `AGENT_UNAVAILABLE` system outcome，不建立假 Agent Task 或模擬回覆。

**Blocked by:** 02 — 建立 Owner-scoped Investigation 工作區.

**Status:** ready-for-agent

- [ ] Investigation Session 與 Investigation 共用 ID；backend 由 Owner-scoped Investigation 取得 Target、Dataset 與 assessment context，不信任 frontend 上傳 snapshot。
- [ ] Command 使用 Owner-scoped idempotency key；user message 與 structured unavailable outcome 原子保存 PostgreSQL，retry 不重複建立，concurrent commands 有穩定 ordering。
- [ ] Message content 以固定上限、UTF-8-safe ordered chunks 保存並一次提交，不做 per-token SQL writes；cursor pagination 可在 reload 後重建相同 conversation。
- [ ] AgentProvider 是可替換 seam；MVP 未設定 implementation 時回 stable `AGENT_UNAVAILABLE`，不建立 `accepted/running/completed` Agent Task、不生成 Interpretation、recommendation 或 evidence。
- [ ] Agent request 與 unavailable outcome不改 Investigation Status、Analysis Run、Dataset、metrics、score 或 reasons；frontend 將 system outcome 與 agent-authored message 明確區分。
- [ ] 完整測試從 browser 提交 long Unicode command、收到 unavailable、reload conversation 並查驗 PostgreSQL chunks/order；另驗證 retry、cross-Owner、spoofed context、atomic rollback 與 localStorage 不再保存 chat authority。

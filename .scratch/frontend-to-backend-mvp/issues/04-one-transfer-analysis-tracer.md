# 04 — 打通單筆 Transfer Analysis tracer bullet

**What to build:** Owner 對已設定 TRON Target 的 Investigation 從 Web 啟動 Analysis Run，backend 以 injected fixture provider 收到一筆 confirmed TRC20 USDT Transfer，在 goroutine 中完成 collection、persistence、metrics 與 assessment，frontend polling 後顯示 PostgreSQL current result。這張票以最窄資料打通所有層，不啟用 production fake provider。

**Blocked by:** 03 — 管理 pending Investigation 與 TRON Target.

**Status:** ready-for-agent

- [ ] Start endpoint 驗證 authentication、ownership 與完整 Target，立即回 `202`、run ID 與 `queued`；同 Investigation 同時最多一個 active run，工作不在 HTTP request 內同步完成。
- [ ] Test-only injected ChainDataProvider 回傳一筆 successful、confirmed TRON mainnet USDT Transfer 與其 Blockchain Transaction provenance；production 設定不能選到 fake provider。
- [ ] Worker 將 Blockchain Transaction 與 Transfer 分開保存，edge identity 使用 transaction + event identity；amount 以 smallest-unit string + decimals 經 JSON 與 PostgreSQL round trip，不經 floating point。
- [ ] 一個 immutable Analysis Dataset、Transfer count、related-address count、exact total flow、Asset、coverage 與 replaceable evaluator result 在單一 SQL transaction 發布，Investigation current result 與狀態同步轉為 `已完成`。
- [ ] Frontend 啟動後 polling `queued/running/completed/failed`，成功時載入 authoritative metrics 與 assessment；不提交 frontend graph 給 backend，也不自行計算 domain totals 或 score。
- [ ] Publication 任一步失敗時不留下半套 Dataset、Transfer、metrics、assessment 或 current pointer，Investigation 回到前一 stable 狀態並顯示 stable error code。
- [ ] Go HTTP + PostgreSQL integration test 注入 fixture provider/evaluator並驗證完整 records；frontend flow test 從按下分析、polling 到顯示一筆 Transfer 的結果，且 reload 後仍由 SQL 恢復。

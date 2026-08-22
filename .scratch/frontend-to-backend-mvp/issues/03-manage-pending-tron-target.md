# 03 — 管理 pending Investigation 與 TRON Target

**What to build:** Owner 可在 Web rename 或 delete 自己的 pending Investigation，並設定或修改 TRON mainnet address；所有變更由 Go 驗證並保存 PostgreSQL，frontend 不再把確認地址直接等同抓取 Ethereum graph。

**Blocked by:** 02 — 建立 Owner-scoped Investigation 工作區.

**Status:** ready-for-agent

- [ ] Pending Investigation 可在 Web 設定或修改 address，Go 執行 TRON Base58Check validation，Network 固定為 TRON mainnet；Ethereum、Bitcoin、malformed 或 checksum 錯誤 address 被拒絕。
- [ ] Rename 接受 non-empty bounded title 並立即反映 list/detail；跨 Owner rename、target update 與 delete 均不能成功或洩漏 record 存在。
- [ ] Delete pending Investigation 後 PostgreSQL record 不再可讀，frontend 選取下一筆或顯示 empty state，舊 local view state 不使資料重現。
- [ ] Web 只顯示 TRON mainnet/TRC20 USDT 能力與對應輸入提示，移除 Ethereum regex、Blockscout runtime fallback、Bitcoin capability 與服務正常的靜態宣稱。
- [ ] API 與 BFF 使用 Investigation ID，不接受 address/network 作為另一套 domain authority；Target 來源只能是 Owner-scoped Investigation record。
- [ ] 完整測試從 browser 建立 pending Investigation、rename、輸入 invalid/valid TRON address、reload 驗證 PostgreSQL，再 delete 並驗證 Web/API/SQL 都不存在；同時驗證跨 Owner 防護。

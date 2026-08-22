# 02 — 建立 Owner-scoped Investigation 工作區

**What to build:** 登入 Owner 開啟 Web 工作區時只看見 PostgreSQL 中屬於自己的 Investigation，可建立、搜尋、列出與選取；reload 或另一個已登入 browser 能恢復相同 domain records，localStorage 不再是權威來源。

**Blocked by:** 01 — 完成 Owner Web 登入旅程.

**Status:** ready-for-agent

- [ ] Web 建立 Investigation 後，Go API 以 authenticated Owner 建立穩定 ID、title、可為空的 Target、`待處理` 狀態與空 current result，並保存至 PostgreSQL。
- [ ] List、search、detail 與 select 只回傳目前 Owner 的 Investigation；另一個 test Owner 對 ID 的讀取回 non-enumerating `404`，不能觀察 nested metadata。
- [ ] Web 顯示真實 empty state 與 backend records，不載入 hard-coded `shlee`、sample Ethereum/Bitcoin Investigations 或 browser-local domain snapshot。
- [ ] 舊 localStorage Investigation、Target、graph、assessment、metrics 與 conversation 一律忽略且不上傳；theme、panel 與其他純視覺偏好可保留。
- [ ] API validation、authentication、ownership、not-found 與 database failures 使用 stable machine-readable errors，frontend 不靠解析文字決定狀態。
- [ ] 完整測試從已登入 browser 建立兩筆 Investigation、reload、搜尋及切換，直接驗證 PostgreSQL records；第二個 Owner 登入後看不到第一個 Owner 的任何資料。

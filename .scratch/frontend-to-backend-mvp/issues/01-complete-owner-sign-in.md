# 01 — 完成 Owner Web 登入旅程

**What to build:** 既有 PostgreSQL User 可從 Web 使用 email 或 username 與 password 登入，經過一次性 email challenge、frontend callback 與 token exchange 後進入受保護工作區；登出後 credential 失效。這張票修復既有 auth 骨架並串起 Web、Go 與 SQL，不包含使用者註冊。

**Blocked by:** None — can start immediately.

**Status:** ready-for-agent

- [ ] 測試環境在隔離 PostgreSQL 中建立固定 test-only User，測試結束後清除；production 不建立預設帳號、不包含固定測試密碼，也不依賴 registration flow。
- [ ] Email 與 username 登入都從 PostgreSQL 查到正確 User 並正確驗證 password hash；錯誤帳密使用相同外部錯誤，不洩漏帳號是否存在。
- [ ] Email challenge 同時驗證未過期、正確 code 與完整性資料，只能使用一次；callback query 正確，challenge、交換碼與 salt 使用密碼學安全亂數，SMTP TLS 不停用憑證驗證。
- [ ] JWT 僅接受預期演算法與 claims；正常 token 不被誤判 revoked，失效、過期、撤銷或不存在 User 的 token 回 `401`；logout 使目前 credential 後續不可用。
- [ ] Web 提供登入、等待驗證與 callback 流程；callback 在 server boundary 交換 token 並保存為 HttpOnly、SameSite credential cookie，browser localStorage 與 client JavaScript 不取得原始 token。
- [ ] Frontend BFF 以 cookie 中的 credential 呼叫 protected identity endpoint，工作區顯示真實 Owner；未登入使用者被導向登入頁，登出清除 cookie 並回到登入頁。
- [ ] Production 缺少 PostgreSQL、JWT/session secrets 或允許的 frontend origin 時拒絕啟動；credentialed CORS 不使用 wildcard，既有 OS environment 不因缺少本機 `.env` 而失效。
- [ ] Go HTTP integration test 使用 PostgreSQL test User 與攔截郵件取得驗證 URL；browser flow test 從登入表單走到 callback、Owner workspace、logout，並驗證 SQL User、cookie、identity、錯誤與 revocation 的完整旅程。

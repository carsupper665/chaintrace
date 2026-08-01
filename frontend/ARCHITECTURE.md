# ChainTrace 專案架構與交接指南

## 設計原則

本專案採「薄路由、功能組裝、畫面與邏輯分離、中介層契約固定」的結構：

- `app` 僅處理 Next.js 路由與 HTTP 入口，不放大型畫面或商業邏輯。
- `src/features` 組合一項完整功能，但不直接實作後端資料來源。
- `src/ui` 只處理 JSX 與使用者互動呈現。
- `src/hooks` 管理 React 狀態、事件、工作區 session 與圖譜操作。
- `src/middle` 是前端與後端唯一需要共同遵守的資料邊界。
- `src/styles` 是 CSS 的唯一放置位置。

這種做法沿用 Next.js 官方允許的 `src` 應用程式目錄與「將專案檔案放在
`app` 外」策略，也符合 React 官方將元件拆檔、把可重用狀態邏輯抽成
Custom Hook 的建議。

## 目錄

```text
chaintrace-frontend/
├─ app/
│  ├─ api/middle/             # 對瀏覽器公開的中介 API 路由
│  ├─ layout.tsx              # 全站 HTML、metadata、CSS 入口
│  └─ page.tsx                # 首頁入口，只渲染 ChainTraceApp
├─ src/
│  ├─ features/chaintrace/
│  │  └─ ChainTraceApp.tsx    # 三欄介面與全域狀態的組裝層
│  ├─ ui/                     # Workspace、Agent、Analysis、Dialogs
│  ├─ hooks/
│  │  └─ useChainTrace.ts     # 前端狀態與操作
│  ├─ models/                 # UI 型別、預設調查案例與建議文字
│  ├─ services/               # PDF 與調查工作區本機保存等瀏覽器端服務
│  ├─ utils/                  # 風險色階等純函式
│  ├─ middle/                 # contracts、clients、providers
│  └─ styles/                 # CSS 入口與 ChainTrace 全站樣式
├─ public/                    # 字型、圖示、OG 圖等靜態檔
├─ tests/                     # 建置後驗證
└─ package.json               # 指令與套件版本
```

## 資料流

```text
UI component
  → useChainTrace
  → src/middle/*-client.ts
  → app/api/middle/*/route.ts
  → src/middle/*-provider.ts
  → Go backend / external blockchain data source
```

### 中介層三種檔案

- `*-contract.ts`：前後端共同資料格式。後端串接時應優先確認這裡。
- `*-client.ts`：瀏覽器端呼叫 `/api/middle/*`，UI 不應直接拼後端網址。
- `*-provider.ts`：伺服器端轉接 Go 後端或真實資料來源，不包含 UI。

後端同事只要依 `src/middle/README.md` 與各 `contract` 回傳資料，不需要
閱讀 React 元件或猜測畫面變數名稱。

## 新增或修改功能

1. 新增畫面：放在 `src/ui`，由 `ChainTraceApp.tsx` 組裝。
2. 新增狀態或互動：放在 `src/hooks`，透過 Hook 回傳給 UI。
3. 新增後端資料：先定義 `contract`，再實作 `client`、API route、
   `provider`，最後才接到 Hook。
4. 新增純計算：放在 `src/utils`，不要塞進 UI 元件。
5. 新增匯出或瀏覽器服務：放在 `src/services`。
6. 新增 CSS：放在 `src/styles`，並由 `index.css` 統一匯入。

## 瀏覽器端資料保存

`src/services/investigationStorage.ts` 使用具版本號的 `localStorage`
快照，保存調查案例、錢包地址、交易圖譜、節點位置與展開狀態、AI
對話、分析結果及畫布視角。重新整理頁面時由 `useChainTrace` 還原，
並以短延遲合併連續操作，避免拖曳圖譜時頻繁寫入。

這些資料只保存在目前瀏覽器與裝置；跨裝置同步或多人共享應由後端資料庫
另行實作，不應改用 UI 假資料。

重新載入頁面後，中介層只有在指標或風險接口回傳 `status: "ready"` 時，
才可覆寫已保存的數值。`pending`、`unavailable` 與前端 fallback 的預設
零值不得清除由真實交易圖譜計算出的關聯節點、交易數與總資金流。

## 命名與匯入規則

- React 元件：`PascalCase.tsx`
- Custom Hook：`usePascalCase.ts`
- 一般函式與資料：`camelCase.ts`
- 中介層檔名維持 `kebab-case`，讓同一 API 的 contract/client/provider
  排在一起。
- 跨資料夾使用 `@/src/...` 絕對匯入；同資料夾內才使用相對匯入。
- UI 不得直接呼叫外部 API，provider 不得匯入 React 或 UI。

## 交接前檢查

```bash
npm install
npm run build
npm test
```

環境變數與各中介 API 回應格式請查看 `src/middle/README.md`。

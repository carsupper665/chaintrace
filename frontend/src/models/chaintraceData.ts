import type { Investigation } from "./chaintraceTypes";

export const initialInvestigations: Investigation[] = [
  {
    id: "CT-2041",
    title: "ETH 錢包追蹤 #2041",
    address: "",
    network: "Ethereum",
    risk: 0,
    relatedNodes: 0,
    totalFlow: 0,
    flowAsset: "",
    transactionCount: 0,
    status: "分析中",
  },
  {
    id: "CT-2038",
    title: "可疑扇出交易群",
    address: "",
    network: "Ethereum",
    risk: 0,
    relatedNodes: 0,
    totalFlow: 0,
    flowAsset: "",
    transactionCount: 0,
    status: "已完成",
  },
  {
    id: "CT-2029",
    title: "分層式交易鏈",
    address: "",
    network: "Bitcoin",
    risk: 0,
    relatedNodes: 0,
    totalFlow: 0,
    flowAsset: "",
    transactionCount: 0,
    status: "已完成",
  },
  {
    id: "CT-2017",
    title: "高風險錢包調查",
    address: "",
    network: "Ethereum",
    risk: 0,
    relatedNodes: 0,
    totalFlow: 0,
    flowAsset: "",
    transactionCount: 0,
    status: "待處理",
  },
];

export const investigationSuggestions = [
  "追蹤此地址近 30 天的資金流向",
  "找出高風險節點與異常交易路徑",
  "檢測拆分、聚合與分層交易模式",
];

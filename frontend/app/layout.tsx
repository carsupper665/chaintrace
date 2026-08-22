import type { Metadata } from "next";
import "@/src/styles/index.css";

export const metadata: Metadata = {
  metadataBase: new URL(
    process.env.NEXT_PUBLIC_SITE_URL || "http://localhost:3000",
  ),
  title: "ChainTrace | AI 區塊鏈異常調查",
  description:
    "以 AI Agent、多模態異常偵測與可解釋交易圖譜協助調查區塊鏈地址風險。",
  openGraph: {
    title: "ChainTrace | AI 區塊鏈異常調查",
    description:
      "以 AI Agent、多模態異常偵測與可解釋交易圖譜協助調查區塊鏈地址風險。",
    images: [{ url: "/og.png", width: 1792, height: 932 }],
  },
  twitter: {
    card: "summary_large_image",
    title: "ChainTrace | AI 區塊鏈異常調查",
    description: "AI Agent 驅動的區塊鏈地址異常診斷與資金流向調查。",
    images: ["/og.png"],
  },
  icons: {
    icon: "/favicon.svg",
    shortcut: "/favicon.svg",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="zh-Hant">
      <body>{children}</body>
    </html>
  );
}

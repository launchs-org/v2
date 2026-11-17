import type { Metadata, Viewport } from "next"
import "./globals.css"

export const metadata: Metadata = {
  title: "Launchs — Container Platform",
  description: "コンテナ、デプロイ、ストレージ、Ingressを一元管理する Launchs コントロールプレーン。",
  applicationName: "Launchs",
  robots: { index: false, follow: false },
}

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: "#f5f7fb",
}

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="ja">
      <body>{children}</body>
    </html>
  )
}

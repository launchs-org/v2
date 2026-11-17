/** @type {import('next').NextConfig} */
const nextConfig = {
  // 単独で動かせる形で出力する(本番のイメージを小さくする)。
  output: "standalone",
  // 開発サーバーに、別のホスト名(例: 172.30.1.1)のブラウザからつなぐことを許す(環境変数 DEV_ALLOWED_ORIGINS。カンマ区切り)。
  allowedDevOrigins: (process.env.DEV_ALLOWED_ORIGINS ?? "").split(",").map((origin) => origin.trim()).filter(Boolean),
  async headers() {
    return [
      {
        source: "/(.*)",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "Strict-Transport-Security", value: "max-age=63072000" },
          { key: "X-Frame-Options", value: "SAMEORIGIN" },
          { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
        ],
      },
    ]
  },
}

export default nextConfig

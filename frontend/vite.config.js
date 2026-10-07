import path from "node:path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// 开发：JUKU_BACKEND=http://127.0.0.1:端口 JUKU_TOKEN=令牌 npm run dev
const backend = process.env.JUKU_BACKEND || "http://127.0.0.1:8999"
const token = process.env.JUKU_TOKEN || ""

export default defineConfig({
  base: "./",
  plugins: [react()],
  resolve: { alias: { "@": path.resolve(__dirname, "src") } },
  server: {
    proxy: {
      "/api": {
        target: backend,
        changeOrigin: true,
        headers: token ? { "X-Juku-Token": token } : {},
      },
    },
  },
  build: { outDir: "dist", emptyOutDir: true, chunkSizeWarningLimit: 1200 },
})

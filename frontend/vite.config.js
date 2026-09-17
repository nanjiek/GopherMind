import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

const proxyTarget = process.env.VITE_PROXY_TARGET || "http://localhost:9090";

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      "/auth": proxyTarget,
      "/query": proxyTarget,
      "/sessions": proxyTarget,
      "/session": proxyTarget,
      "/attachments": proxyTarget,
      "/stream": proxyTarget
    }
  }
});

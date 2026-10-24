import { resolve } from "node:path";
import { defineConfig } from "vitest/config";
import preact from "@preact/preset-vite";

// Two HTML entries ("/" and "/tg/") share the same hashed chunks. The output
// goes to internal/web/dist, which the Go binary embeds (webui build tag).
export default defineConfig({
  plugins: [preact()],
  build: {
    outDir: resolve(import.meta.dirname, "../internal/web/dist"),
    emptyOutDir: true,
    target: "es2022",
    cssCodeSplit: false,
    rollupOptions: {
      input: {
        main: resolve(import.meta.dirname, "index.html"),
        tg: resolve(import.meta.dirname, "tg/index.html"),
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8088",
      "/auth": "http://127.0.0.1:8088",
    },
  },
  test: {
    environment: "jsdom",
    include: ["test/**/*.test.{ts,tsx}"],
    restoreMocks: true,
  },
});

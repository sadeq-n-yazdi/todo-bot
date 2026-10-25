import { createHash } from "node:crypto";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import { defineConfig, type Plugin } from "vitest/config";
import preact from "@preact/preset-vite";

const root = import.meta.dirname;

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

// Builds src/sw.ts to /sw.js (no hash in the name, scope "/") and writes the
// list of the shell files and a version into it (docs/WEB-DESIGN.md section
// 7.5). The version is a hash of the file names, of the HTML entries and of
// the files in public/, so any change of the shell gives a new worker.
function shellWorker(): Plugin {
  return {
    name: "todo-shell-worker",
    apply: "build",
    // Run after the plugins that add the CSS and the HTML files to the bundle.
    generateBundle: {
      order: "post",
      handler(_options, bundle) {
        const sw = bundle["sw.js"];
        if (!sw || sw.type !== "chunk") this.error("sw.js was not built; check the 'sw' input");

        const hash = createHash("sha256");
        const files: string[] = [];
        for (const name of Object.keys(bundle).sort()) {
          const out = bundle[name];
          if (name === "sw.js") continue;
          if (name.startsWith("assets/")) {
            files.push("/" + name);
            hash.update(name);
          } else if (out.type === "asset" && name.endsWith(".html")) {
            hash.update(name).update(out.source);
          }
        }
        const pub = join(root, "public");
        for (const f of walk(pub).sort()) {
          const url = "/" + f.slice(pub.length + 1).split("\\").join("/");
          files.push(url);
          hash.update(url).update(readFileSync(f));
        }

        const json = JSON.stringify({ version: hash.digest("hex").slice(0, 12), files });
        const re = /(["'`])__SHELL_JSON__\1/;
        if (!re.test(sw.code)) this.error("sw.js has no SHELL_JSON placeholder");
        sw.code = sw.code.replace(re, () => JSON.stringify(json));
      },
    },
  };
}

// Two HTML entries ("/" and "/tg/") share the same hashed chunks. The output
// goes to internal/web/dist, which the Go binary embeds (webui build tag).
export default defineConfig({
  plugins: [preact(), shellWorker()],
  build: {
    outDir: resolve(root, "../internal/web/dist"),
    emptyOutDir: true,
    target: "es2022",
    cssCodeSplit: false,
    rollupOptions: {
      input: {
        main: resolve(root, "index.html"),
        tg: resolve(root, "tg/index.html"),
        sw: resolve(root, "src/sw.ts"),
      },
      output: {
        // The worker keeps a fixed name, because the browser looks for /sw.js.
        entryFileNames: (chunk) => (chunk.name === "sw" ? "sw.js" : "assets/[name]-[hash].js"),
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
    setupFiles: ["test/setup.ts"],
    restoreMocks: true,
  },
});

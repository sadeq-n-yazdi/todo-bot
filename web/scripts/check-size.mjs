// Size budget of docs/WEB-DESIGN.md section 8.5. It reads internal/web/dist, so
// run "npm run build" first. It prints the sizes and exits 1 above a limit.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";

const dist = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "internal", "web", "dist");
const KB = 1024;
const limits = {
  "js gzip": 45 * KB,
  "js raw": 120 * KB,
  "css gzip": 10 * KB,
  "icons and manifest": 150 * KB,
  "dist total": 400 * KB,
};

function walk(dir) {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

let files;
try {
  files = walk(dist);
} catch {
  console.error(`size: ${dist} does not exist; run "npm run build" first`);
  process.exit(1);
}

const sum = { "js gzip": 0, "js raw": 0, "css gzip": 0, "icons and manifest": 0, "dist total": 0 };
for (const f of files) {
  const raw = readFileSync(f);
  const rel = relative(dist, f);
  sum["dist total"] += raw.length;
  if (f.endsWith(".js")) {
    sum["js raw"] += raw.length;
    sum["js gzip"] += gzipSync(raw).length;
  } else if (f.endsWith(".css")) {
    sum["css gzip"] += gzipSync(raw).length;
  } else if (rel.startsWith("icons") || f.endsWith(".webmanifest")) {
    sum["icons and manifest"] += raw.length;
  }
}

let bad = false;
for (const [name, limit] of Object.entries(limits)) {
  const over = sum[name] > limit;
  bad ||= over;
  console.log(`size: ${name.padEnd(20)} ${String(sum[name]).padStart(7)} B  limit ${limit} B  ${over ? "OVER" : "ok"}`);
}
process.exit(bad ? 1 : 0);

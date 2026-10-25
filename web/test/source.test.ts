import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    return statSync(p).isDirectory() ? walk(p) : [p];
  });
}

const src = join(__dirname, "..", "src");

// docs/WEB-DESIGN.md section 11.3: the app shows text as text.
describe("source rules", () => {
  const files = walk(src).filter((f) => /\.(ts|tsx)$/.test(f));

  it("finds the source files", () => {
    expect(files.length).toBeGreaterThan(5);
  });

  it("never uses innerHTML, outerHTML or dangerouslySetInnerHTML", () => {
    for (const f of files) {
      const text = readFileSync(f, "utf8");
      expect(text, f).not.toMatch(/innerHTML|outerHTML|dangerouslySetInnerHTML|insertAdjacentHTML|document\.write/);
    }
  });

  it("never uses eval", () => {
    for (const f of files) {
      expect(readFileSync(f, "utf8"), f).not.toMatch(/\beval\(|new Function\(/);
    }
  });

  it("keeps inline scripts and styles out of the HTML entries", () => {
    for (const f of [join(src, "..", "index.html"), join(src, "..", "tg", "index.html")]) {
      const html = readFileSync(f, "utf8");
      expect(html, f).not.toMatch(/<script(?![^>]*\bsrc=)[^>]*>/);
      expect(html, f).not.toMatch(/<style|\sstyle=|\sonclick=|\sonload=/);
    }
  });

  it("registers the service worker in one place only", () => {
    // pwa.ts holds the function; exactly one other file calls it.
    const callers = files.filter((f) => !f.endsWith(join("tabs", "pwa.ts")) && /registerServiceWorker\(/.test(readFileSync(f, "utf8")));
    expect(callers.map((f) => f.slice(src.length + 1))).toEqual(["app.tsx"]);
    for (const f of files.filter((f) => !f.endsWith(join("tabs", "pwa.ts")))) {
      expect(readFileSync(f, "utf8"), f).not.toMatch(/serviceWorker\.register\(/);
    }
  });

  it("links the apple-touch-icon from the HTML entry of the PWA", () => {
    const html = readFileSync(join(src, "..", "index.html"), "utf8");
    expect(html).toContain('<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png" />');
    for (const f of files) expect(readFileSync(f, "utf8"), f).not.toMatch(/apple-touch-icon/);
  });

  it("defines each global class in one CSS file only", () => {
    // A global class rule is a selector of one class, with only pseudo-classes
    // or attributes after it: ".row", ".btn[disabled]", ".row-check:hover".
    // Selectors with a parent, such as ".bar .btn", are scoped and can repeat.
    const owner = new Map<string, string>();
    for (const f of walk(src).filter((f) => f.endsWith(".css"))) {
      const css = readFileSync(f, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
      for (const m of css.matchAll(/([^{}]+)\{/g)) {
        for (const sel of m[1].split(",").map((s) => s.trim())) {
          const g = /^\.([A-Za-z0-9_-]+)(?:[:[][^\s.]*)?$/.exec(sel);
          if (!g) continue;
          const name = f.slice(src.length + 1);
          const was = owner.get(g[1]);
          expect(was === undefined || was === name, `.${g[1]} is in ${was} and ${name}`).toBe(true);
          owner.set(g[1], name);
        }
      }
    }
    expect(owner.has("row")).toBe(true);
  });

  it("uses logical CSS properties only", () => {
    const css = readFileSync(join(src, "styles.css"), "utf8");
    expect(css).not.toMatch(/(margin|padding|border)-(left|right)\b|(^|[\s;{])(left|right)\s*:|text-align:\s*(left|right)/);
  });
});

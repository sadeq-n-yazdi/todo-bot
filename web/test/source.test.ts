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

  it("uses logical CSS properties only", () => {
    const css = readFileSync(join(src, "styles.css"), "utf8");
    expect(css).not.toMatch(/(margin|padding|border)-(left|right)\b|(^|[\s;{])(left|right)\s*:|text-align:\s*(left|right)/);
  });
});

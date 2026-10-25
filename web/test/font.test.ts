import { beforeEach, describe, expect, it } from "vitest";
import { FONT_CSS, loadFont } from "../src/font";
import { readFileSync } from "node:fs";
import { join } from "node:path";

describe("font", () => {
  beforeEach(() => {
    document.head.replaceChildren();
  });

  it("adds the preconnect and the stylesheet of Vazirmatn", () => {
    loadFont();
    const pre = document.head.querySelector('link[rel="preconnect"]') as HTMLLinkElement;
    const css = document.head.querySelector('link[rel="stylesheet"]') as HTMLLinkElement;
    expect(pre.href).toBe("https://fonts.gstatic.com/");
    expect(pre.crossOrigin).toBe("anonymous");
    expect(css.href).toBe(FONT_CSS);
    expect(FONT_CSS).toMatch(/family=Vazirmatn:/);
    expect(FONT_CSS).toMatch(/display=swap/);
  });

  it("adds the links only once", () => {
    loadFont();
    loadFont();
    expect(document.head.querySelectorAll("link")).toHaveLength(2);
  });

  it("puts Vazirmatn first in --font and keeps the system fonts", () => {
    const css = readFileSync(join(__dirname, "..", "src", "styles.css"), "utf8");
    expect(css).toMatch(/--font:\s*"Vazirmatn",\s*system-ui,/);
  });
});

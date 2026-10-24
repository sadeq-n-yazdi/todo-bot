import { beforeEach, describe, expect, it } from "vitest";
import { applyTheme, themeVars } from "../src/theme";

describe("theme", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("style");
  });

  it("maps Telegram theme parameters to CSS variables", () => {
    expect(
      themeVars({
        bg_color: "#111111",
        text_color: "#eeeeee",
        hint_color: "#999999",
        link_color: "#3399ff",
        button_color: "#2288cc",
        button_text_color: "#ffffff",
        secondary_bg_color: "#222222",
        destructive_text_color: "#ff5555",
      }),
    ).toEqual({
      "--tg-theme-bg-color": "#111111",
      "--tg-theme-text-color": "#eeeeee",
      "--tg-theme-hint-color": "#999999",
      "--tg-theme-link-color": "#3399ff",
      "--tg-theme-button-color": "#2288cc",
      "--tg-theme-button-text-color": "#ffffff",
      "--tg-theme-secondary-bg-color": "#222222",
      "--tg-theme-destructive-text-color": "#ff5555",
    });
  });

  it("ignores unknown keys and values that are not colors", () => {
    expect(themeVars({ bg_color: "red; background: url(x)", other: "#fff" })).toEqual({});
  });

  it("sets the variables on the root element", () => {
    applyTheme({ bg_color: "#102030" });
    expect(document.documentElement.style.getPropertyValue("--tg-theme-bg-color")).toBe("#102030");
  });
});

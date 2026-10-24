import { beforeEach, describe, expect, it } from "vitest";
import { dirFor, num, setCatalog, setLang, t } from "../src/i18n";

describe("i18n", () => {
  beforeEach(() => {
    setLang("en");
    setCatalog({});
  });

  it("fills %s and %d in order", () => {
    setCatalog({ "web.x": "%s has %d items" });
    expect(t("web.x", "Sam", 3)).toBe("Sam has 3 items");
  });

  it("returns the key for a missing message", () => {
    expect(t("web.missing")).toBe("web.missing");
  });

  it("uses Persian digits in Farsi only", () => {
    expect(num(1234567890)).toBe("1234567890");
    setLang("fa");
    expect(num(1234567890)).toBe("۱۲۳۴۵۶۷۸۹۰");
    setCatalog({ "web.n": "%d مورد" });
    expect(t("web.n", 12)).toBe("۱۲ مورد");
  });

  it("gives the direction of a language", () => {
    expect(dirFor("fa")).toBe("rtl");
    expect(dirFor("fa-IR")).toBe("rtl");
    expect(dirFor("en")).toBe("ltr");
    expect(dirFor(undefined)).toBe("ltr");
  });
});

import { beforeEach, describe, expect, it } from "vitest";
import { clearCache, clearOtherUsers, readCache, writeCache } from "../src/cache";

describe("cache", () => {
  beforeEach(() => localStorage.clear());

  it("reads back what it wrote, per user", () => {
    writeCache(1, "mine", { items: [1] });
    writeCache(2, "mine", { items: [2] });
    expect(readCache(1, "mine")).toEqual({ items: [1] });
    expect(readCache(2, "mine")).toEqual({ items: [2] });
    expect(readCache(3, "mine")).toBeNull();
  });

  it("deletes every entry of the user on logout", () => {
    writeCache(1, "mine", 1);
    writeCache(1, "assigned", 2);
    writeCache(2, "mine", 3);
    clearCache(1);
    expect(readCache(1, "mine")).toBeNull();
    expect(readCache(1, "assigned")).toBeNull();
    expect(readCache(2, "mine")).toBe(3);
  });

  it("deletes the entries of other users when the user changes", () => {
    writeCache(1, "mine", 1);
    writeCache(2, "mine", 2);
    clearOtherUsers(2);
    expect(readCache(1, "mine")).toBeNull();
    expect(readCache(2, "mine")).toBe(2);
  });

  it("returns null for damaged data", () => {
    localStorage.setItem("todo.cache.1.mine", "{not json");
    expect(readCache(1, "mine")).toBeNull();
  });
});

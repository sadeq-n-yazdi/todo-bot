import { cleanup, fireEvent, render, screen } from "@testing-library/preact";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setCatalog, setLang } from "../src/i18n";
import { Tabs } from "../src/tabs/index";
import { resetPwa } from "../src/tabs/pwa";
import type { ListResponse } from "../src/tabs/model";
import type { User } from "../src/types";

// The golden files of the Go tests (internal/web/testdata/api) are the one
// source of truth for the shape of the lists.
const dir = join(__dirname, "..", "..", "internal", "web", "testdata", "api");
const golden = (name: string): ListResponse => JSON.parse(readFileSync(join(dir, name), "utf8")) as ListResponse;
const body = (name: string) => JSON.stringify(golden(name));

const user = (over: Partial<User> = {}): User => ({
  id: 42,
  username: "alice",
  lang: "en",
  calendar: "gregorian",
  tz: "UTC",
  dir: "ltr",
  ...over,
});

const fetchMock = vi.fn();
let table: Record<string, string> = {};

beforeEach(() => {
  table = {};
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (url: string) => {
    if (!(url in table)) throw new Error("unexpected " + url);
    return new Response(table[url], { headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetchMock);
  localStorage.clear();
  setLang("en");
  setCatalog({
    "web.tabs.noname": "(no username)",
    "web.tabs.more": "Load more",
    "web.tabs.assigned": "Assigned to me",
    "web.tabs.empty.mine": "You have no open items.",
  });
  resetPwa();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("golden lists", () => {
  it("chat_title is a string in every golden file", () => {
    for (const f of ["list_mine_en_gregorian.json", "assigned_fa_jalali.json", "list_group_fa_gregorian.json", "list_page2.json"]) {
      for (const it of golden(f).items) expect(typeof it.chat_title, f).toBe("string");
    }
  });

  for (const [lang, cal] of [
    ["en", "gregorian"],
    ["en", "jalali"],
    ["fa", "gregorian"],
    ["fa", "jalali"],
  ] as const) {
    it(`Mine, ${lang} ${cal}: shows every item with the text of the server`, async () => {
      setLang(lang);
      const name = `list_mine_${lang}_${cal}.json`;
      table["/api/chats/42/items?page=0"] = body(name);
      render(<Tabs user={user({ lang })} />);
      const g = golden(name);
      await screen.findByText(g.items[0].text);
      for (const it of g.items) {
        expect(screen.getByText(it.text).getAttribute("dir")).toBe("auto");
        if (it.due_text) expect(screen.getAllByText(it.due_text).length).toBeGreaterThan(0);
      }
      expect(document.querySelectorAll(".overdue")).toHaveLength(g.items.filter((i) => i.overdue).length);
    });

    it(`Assigned, ${lang} ${cal}: shows the chat titles and no "no username" arrow for the viewer`, async () => {
      setLang(lang);
      table["/api/chats/42/items?page=0"] = body("list_empty.json");
      table["/api/assigned"] = body(`assigned_${lang}_${cal}.json`);
      render(<Tabs user={user({ lang })} />);
      fireEvent.click(screen.getByRole("tab", { name: "Assigned to me" }));
      await screen.findByText("book the flight");
      expect(screen.getByText("Family").tagName).toBe("BDI");
      expect(screen.queryByText("(no username)")).toBeNull();
    });
  }

  it("shows (no username) for another person without a username, in Mine", async () => {
    table["/api/chats/42/items?page=0"] = body("list_mine_en_gregorian.json");
    render(<Tabs user={user()} />);
    await screen.findByText("ask the neighbour");
    expect(screen.getByText("(no username)").tagName).toBe("BDI");
    expect(screen.getByText("@bob").tagName).toBe("BDI");
  });

  it("shows the empty state from list_empty.json", async () => {
    table["/api/chats/42/items?page=0"] = body("list_empty.json");
    render(<Tabs user={user()} />);
    await screen.findByText("You have no open items.");
  });

  it("Load more reads page 2 of the golden files", async () => {
    const p1 = { ...golden("list_mine_en_gregorian.json"), pages: 2 };
    table["/api/chats/42/items?page=0"] = JSON.stringify(p1);
    table["/api/chats/42/items?page=1"] = body("list_page2.json");
    render(<Tabs user={user()} />);
    await screen.findByText("Load more");
    fireEvent.click(screen.getByText("Load more"));
    await screen.findByText("item 11");
    expect(screen.queryByText("Load more")).toBeNull();
  });
});

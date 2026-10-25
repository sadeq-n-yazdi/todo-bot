import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "../src/app";
import { setPreviewDelay } from "../src/actions/add";
import { closeSheet } from "../src/actions/store";
import { setToken } from "../src/api";
import { readCache, writeCache } from "../src/cache";
import { getLang, setCatalog, setLang } from "../src/i18n";
import { item, list } from "../src/tabs/fixtures";
import { resetPwa } from "../src/tabs/pwa";
import type { User } from "../src/types";

// The settings menu, the Add button, logout and the offline start of app.tsx.

const base: User = { id: 11111, username: "alice", lang: "en", calendar: "gregorian", tz: "UTC", dir: "ltr" };
const me = (over: Partial<User> = {}, kind = "browser") => ({
  user: { ...base, ...over },
  session: { kind, expires_at: "2026-11-25T00:00:00Z" },
  bot: { username: "todo_bot" },
});

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type Handler = (body: unknown) => Response | Promise<Response>;
const fetchMock = vi.fn();
let routes: Record<string, Handler> = {};
let down = false;

function sent(key: string): unknown[] {
  return fetchMock.mock.calls
    .filter((c) => `${(c[1]?.method as string) ?? "GET"} ${c[0] as string}` === key)
    .map((c) => (c[1]?.body ? JSON.parse(c[1].body as string) : undefined));
}

const MINE = `GET /api/chats/${base.id}/items?page=0`;

beforeEach(() => {
  routes = {
    "GET /api/me": () => json(me()),
    "GET /api/i18n?lang=en": () => json({ lang: "en", dir: "ltr", messages: {} }),
    [MINE]: () => json(list([item({ text: "buy milk" })])),
  };
  down = false;
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
    if (down) throw new TypeError("Failed to fetch");
    const key = `${init?.method ?? "GET"} ${url}`;
    const h = routes[key];
    if (!h) throw new Error("unexpected " + key);
    return h(init?.body ? JSON.parse(init.body as string) : undefined);
  });
  vi.stubGlobal("fetch", fetchMock);
  localStorage.clear();
  sessionStorage.clear();
  setToken(null);
  setLang("en");
  setCatalog({});
  setPreviewDelay(10);
  resetPwa();
  closeSheet();
  document.documentElement.lang = "en";
  document.documentElement.dir = "ltr";
});

const settle = () => new Promise((r) => setTimeout(r, 60));

afterEach(async () => {
  cleanup();
  closeSheet();
  await settle();
  vi.unstubAllGlobals();
  delete (window as unknown as { Telegram?: unknown }).Telegram;
});

async function start(mode: "pwa" | "miniapp" = "pwa") {
  render(<App mode={mode} initData={mode === "miniapp" ? "query_id=Q&hash=h" : undefined} />);
  await screen.findByText("buy milk");
}

describe("the settings menu", () => {
  it("changes the language: dir and lang change at once, and the lists load again", async () => {
    routes["PUT /api/me/settings"] = () => json(me({ lang: "fa", dir: "rtl" }));
    routes["GET /api/i18n?lang=fa"] = () =>
      json({ lang: "fa", dir: "rtl", messages: { "web.actions.settings": "تنظیمات", "web.actions.add": "افزودن" } });
    await start();
    expect(sent(MINE)).toHaveLength(1);

    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    const en = screen.getByRole("button", { name: "English" });
    expect(en.getAttribute("aria-pressed")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "فارسی" }));

    await waitFor(() => expect(document.documentElement.dir).toBe("rtl"));
    expect(document.documentElement.lang).toBe("fa");
    expect(getLang()).toBe("fa");
    expect(sent("PUT /api/me/settings")).toEqual([{ lang: "fa" }]);
    // The new catalog is in use, and the list loads again in the new language.
    await screen.findByRole("button", { name: "افزودن" });
    await waitFor(() => expect(sent(MINE)).toHaveLength(2));
    await screen.findByText("buy milk");
    expect(screen.getByRole("button", { name: "فارسی" }).getAttribute("aria-pressed")).toBe("true");
    expect(JSON.parse(localStorage.getItem("todo.me")!).lang).toBe("fa");
  });

  it("changes the calendar", async () => {
    routes["PUT /api/me/settings"] = () => json(me({ calendar: "jalali" }));
    await start();
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.cal_jalali" }));
    await waitFor(() => expect(sent(MINE)).toHaveLength(2));
    await screen.findByText("buy milk");
    expect(sent("PUT /api/me/settings")).toEqual([{ calendar: "jalali" }]);
    expect(screen.getByRole("button", { name: "web.actions.cal_jalali" }).getAttribute("aria-pressed")).toBe("true");
  });

  it("shows the message of the server when the change fails", async () => {
    routes["PUT /api/me/settings"] = () => json({ error: { code: "bad_calendar", message: "Unknown calendar." } }, 422);
    await start();
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.cal_jalali" }));
    expect((await screen.findByRole("alert")).textContent).toBe("Unknown calendar.");
    expect(document.documentElement.dir).toBe("ltr");
  });

  it("has no Sign in a browser in the PWA", async () => {
    await start();
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    expect(screen.queryByRole("button", { name: "web.actions.signin_browser" })).toBeNull();
  });

  it("opens the code screen from Sign in a browser in the Mini App, and BackButton closes it", async () => {
    const clicks = new Set<() => void>();
    const back = { show: vi.fn(), hide: vi.fn(), onClick: (f: () => void) => clicks.add(f), offClick: (f: () => void) => clicks.delete(f) };
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { initData: "query_id=Q&hash=h", BackButton: back } };
    routes["POST /api/auth/miniapp"] = () => json({ token: "tb1_tok", expires_at: "x", user: { id: base.id }, start: "" });
    routes["GET /api/me"] = () => json(me({}, "miniapp"));
    routes["POST /api/auth/code"] = () => json({ code: "ABCD-1234", expires_at: new Date(Date.now() + 600000).toISOString() }, 201);
    await start("miniapp");

    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.signin_browser" }));
    await screen.findByText("ABCD-1234");
    expect(screen.queryByTestId("settings-sheet")).toBeNull();

    await waitFor(() => expect(clicks.size).toBe(1));
    clicks.forEach((f) => f());
    await screen.findByText("buy milk");
    expect(screen.queryByTestId("code-screen")).toBeNull();
  });
});

describe("the Add button", () => {
  it("adds an item and loads the list again", async () => {
    routes["GET /api/chats"] = () => json({ chats: [{ id: base.id, kind: "private", title: "" }] });
    routes[`GET /api/chats/${base.id}/members`] = () => json({ members: [] });
    routes["POST /api/parse"] = () => json({ text: "call mum", due_at: null, due_text: null, recur: "", recur_text: "" });
    routes[`POST /api/chats/${base.id}/items`] = () => {
      routes[MINE] = () => json(list([item({ text: "buy milk" }), item({ id: 43, num: 4, text: "call mum" })]));
      return json(item({ id: 43, text: "call mum" }), 201);
    };
    await start();
    fireEvent.click(screen.getByRole("button", { name: "web.actions.add" }));
    const sheet = await screen.findByTestId("add-sheet");
    await waitFor(() => expect(sent("GET /api/chats")).toHaveLength(1));
    await waitFor(() => expect(sent(`GET /api/chats/${base.id}/members`)).toHaveLength(1));
    fireEvent.input(screen.getByRole("textbox"), { target: { value: "call mum" } });
    await screen.findByText("web.actions.preview_none");
    fireEvent.click(within(sheet).getByRole("button", { name: "web.actions.add" }));
    await waitFor(() => expect(screen.queryByTestId("add-sheet")).toBeNull());
    await screen.findByText("call mum");
    expect(sent(`POST /api/chats/${base.id}/items`)).toEqual([{ text: "call mum", assignee_id: 0 }]);
  });
});

describe("logout", () => {
  it("in the PWA: ends the session, deletes the saved user and lists, and shows the login screen", async () => {
    routes["POST /api/auth/logout"] = () => new Response(null, { status: 204 });
    await start();
    expect(localStorage.getItem("todo.me")).not.toBeNull();
    writeCache(base.id, "mine", list([item()]));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.logout" }));
    await screen.findByTestId("login");
    expect(sent("POST /api/auth/logout")).toHaveLength(1);
    expect(localStorage.getItem("todo.me")).toBeNull();
    expect(readCache(base.id, "mine")).toBeNull();
  });

  it("everywhere: calls logout-all", async () => {
    routes["POST /api/auth/logout-all"] = () => new Response(null, { status: 204 });
    await start();
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.logout_all" }));
    await screen.findByTestId("login");
    expect(sent("POST /api/auth/logout-all")).toHaveLength(1);
  });

  it("in the Mini App: forgets the token and shows that the user is logged out", async () => {
    routes["POST /api/auth/miniapp"] = () => json({ token: "tb1_tok", expires_at: "x", user: { id: base.id }, start: "" });
    routes["GET /api/me"] = () => json(me({}, "miniapp"));
    routes["POST /api/auth/logout"] = () => new Response(null, { status: 204 });
    await start("miniapp");
    expect(sessionStorage.getItem("todo.token")).toBe("tb1_tok");
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.logout" }));
    expect((await screen.findByRole("status")).textContent).toBe("web.actions.logged_out");
    expect(sessionStorage.getItem("todo.token")).toBeNull();
  });

  it("shows the error and stays signed in when the network is down", async () => {
    await start();
    down = true;
    fireEvent.click(screen.getByRole("button", { name: "web.actions.settings" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.logout" }));
    expect((await screen.findByRole("alert")).textContent).toBe("web.actions.offline");
    expect(localStorage.getItem("todo.me")).not.toBeNull();
  });
});

describe("the start without a network", () => {
  it("uses the last user and the saved lists, without the Add button", async () => {
    localStorage.setItem("todo.me", JSON.stringify(base));
    writeCache(base.id, "mine", list([item({ text: "saved item" })]));
    down = true;
    render(<App mode="pwa" />);
    await screen.findByText("saved item");
    expect(screen.getByText("@alice")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "web.actions.add" })).toBeNull();
    expect(screen.getByText("saved item").closest("li")!.querySelectorAll("button")).toHaveLength(0);
  });

  it("shows the error when there is no saved user", async () => {
    down = true;
    render(<App mode="pwa" />);
    expect((await screen.findByRole("alert")).textContent).toBe("Something went wrong.");
  });

  it("a 401 at the start deletes the saved user and the lists", async () => {
    localStorage.setItem("todo.me", JSON.stringify(base));
    writeCache(base.id, "mine", list([item()]));
    routes["GET /api/me"] = () => json({ error: { code: "session_revoked", message: "x" } }, 401);
    render(<App mode="pwa" />);
    await screen.findByTestId("login");
    expect(localStorage.getItem("todo.me")).toBeNull();
    expect(readCache(base.id, "mine")).toBeNull();
  });
});

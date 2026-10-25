import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Actions } from "../src/actions/index";
import { closeSheet, openItem } from "../src/actions/store";
import { setToken } from "../src/api";
import { changes, Edit, type EditItem } from "../src/edit/index";
import { closeEdit, getEdit, openEdit } from "../src/edit/store";
import { setCatalog, setLang } from "../src/i18n";
import { onListsChanged } from "../src/live";
import { item } from "../src/tabs/fixtures";
import type { User } from "../src/types";

// Builds against endpoints 22 and 23 of docs/WEB-DESIGN.md section 5.3.

const user: User = { id: 11111, username: "alice", lang: "en", calendar: "gregorian", tz: "UTC", dir: "ltr" };
const GROUP = -1001234567890;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const editItem = (over: Partial<EditItem> = {}): EditItem => ({
  ...item(),
  status: "open",
  when_text: "2026-10-25 18:00",
  ...over,
});

type Handler = (body: unknown) => Response | Promise<Response>;
const fetchMock = vi.fn();
let routes: Record<string, Handler> = {};

function sent(key: string): unknown[] {
  return fetchMock.mock.calls
    .filter((c) => `${(c[1]?.method as string) ?? "GET"} ${c[0] as string}` === key)
    .map((c) => (c[1]?.body ? JSON.parse(c[1].body as string) : undefined));
}

beforeEach(() => {
  routes = {};
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (url: string, init?: RequestInit) => {
    const key = `${init?.method ?? "GET"} ${url}`;
    const h = routes[key];
    if (!h) throw new Error("unexpected " + key);
    return h(init?.body ? JSON.parse(init.body as string) : undefined);
  });
  vi.stubGlobal("fetch", fetchMock);
  localStorage.clear();
  setToken(null);
  setLang("en");
  setCatalog({});
  closeSheet();
  closeEdit();
});

afterEach(() => {
  cleanup();
  closeSheet();
  closeEdit();
  vi.unstubAllGlobals();
});

const base = `/api/chats/${user.id}/items/42`;

function withItem(it: EditItem, members = [{ id: 22222, username: "bob" }], chat = user.id) {
  routes[`GET /api/chats/${chat}/items/42`] = () => json(it);
  routes[`GET /api/chats/${chat}/members`] = () => json({ members });
}

async function openForm(it = editItem(), chat = user.id) {
  render(<Edit />);
  openEdit(item({ chat_id: it.chat_id }), chat, false);
  await screen.findByRole("dialog");
  await waitFor(() => expect((screen.getByRole("textbox", { name: "web.edit.text" }) as HTMLTextAreaElement).value).toBe(it.text));
}

describe("changes", () => {
  const orig = editItem({ assignee: { id: 22222, username: "bob" } });
  const same = { text: orig.text, when: orig.when_text ?? "", assignee: 22222 };

  it("is empty when nothing changed", () => {
    expect(changes(orig, same)).toEqual({});
    expect(changes(orig, { ...same, when: " 2026-10-25 18:00 " })).toEqual({});
  });

  it("has only the changed fields", () => {
    expect(changes(orig, { ...same, text: "buy bread" })).toEqual({ text: "buy bread" });
    expect(changes(orig, { ...same, when: "friday 9:00" })).toEqual({ when: "friday 9:00" });
    expect(changes(orig, { ...same, assignee: 0 })).toEqual({ assignee_id: 0 });
  });

  it("sends when: null when the date field is cleared", () => {
    expect(changes(orig, { ...same, when: "  " })).toEqual({ when: null });
  });

  it("sends when: null for No date, also when the item has no phrase", () => {
    expect(changes(orig, { ...same, noDate: true })).toEqual({ when: null });
    const every3 = editItem({ when_text: "", recur: "every 3 days", recur_text: "every 3 days" });
    expect(changes(every3, { text: every3.text, when: "", assignee: 0 })).toEqual({});
    expect(changes(every3, { text: every3.text, when: "", assignee: 0, noDate: true })).toEqual({ when: null });
    const none = editItem({ when_text: null, due_at: null, due_text: null, due_full: null });
    expect(changes(none, { text: none.text, when: "", assignee: 0, noDate: true })).toEqual({});
  });

  it("sends no when for an item without a date and an empty field", () => {
    const none = editItem({ when_text: null, due_at: null, due_text: null, due_full: null });
    expect(changes(none, { text: none.text, when: "", assignee: 0 })).toEqual({});
  });
});

describe("the edit form", () => {
  it("fills from endpoint 22", async () => {
    withItem(editItem({ assignee: { id: 22222, username: "bob" } }));
    await openForm();
    expect((screen.getByRole("textbox", { name: "web.edit.when" }) as HTMLInputElement).value).toBe("2026-10-25 18:00");
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("22222"));
    expect(sent(`GET ${base}`)).toHaveLength(1);
  });

  it("sends only the changed text, then closes and reloads the lists", async () => {
    withItem(editItem());
    let body: unknown;
    routes[`PATCH ${base}`] = (b) => {
      body = b;
      return json(item({ text: "buy bread" }));
    };
    const reload = vi.fn();
    const off = onListsChanged(reload);
    await openForm();

    fireEvent.input(screen.getByRole("textbox", { name: "web.edit.text" }), { target: { value: "buy bread" } });
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));

    await waitFor(() => expect(getEdit()).toBeNull());
    expect(body).toEqual({ text: "buy bread" });
    expect(reload).toHaveBeenCalledTimes(1);
    expect(screen.getByRole("status").textContent).toBe("web.edit.saved");
    off();
  });

  it("removes the date with when: null and changes the assignee", async () => {
    withItem(editItem());
    let body: unknown;
    routes[`PATCH ${base}`] = (b) => {
      body = b;
      return json(item());
    };
    await openForm();
    await screen.findByRole("option", { name: "@bob" });

    fireEvent.input(screen.getByRole("textbox", { name: "web.edit.when" }), { target: { value: "" } });
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "22222" } });
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));

    await waitFor(() => expect(body).toEqual({ when: null, assignee_id: 22222 }));
  });

  it("sends no PATCH when nothing changed, and says so", async () => {
    withItem(editItem());
    const reload = vi.fn();
    const off = onListsChanged(reload);
    await openForm();
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));
    await waitFor(() => expect(getEdit()).toBeNull());
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === "PATCH")).toBe(false);
    expect(screen.getByRole("status").textContent).toBe("web.edit.unchanged");
    expect(reload).not.toHaveBeenCalled();
    off();
  });

  it("shows the note of the PATCH answer after the save", async () => {
    withItem(editItem());
    routes[`PATCH ${base}`] = () => json({ ...editItem(), note: "@bob has not started the bot." });
    await openForm();
    await screen.findByRole("option", { name: "@bob" });
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "22222" } });
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));
    await waitFor(() => expect(getEdit()).toBeNull());
    expect(screen.getByRole("status").textContent).toBe("web.edit.saved @bob has not started the bot.");
  });

  it("shows the schedule as text and removes the date of an every-3-days item with No date", async () => {
    const every3 = editItem({
      when_text: "",
      recur: "every 3 days",
      recur_text: "every 3 days",
      due_full: "Sun 25 Oct 2026 18:00",
    });
    withItem(every3);
    let body: unknown;
    routes[`PATCH ${base}`] = (b) => {
      body = b;
      return json({ ...every3, due_at: null, when_text: "" });
    };
    setCatalog({ "web.edit.repeat": "Repeats: %s" });
    await openForm(every3);
    expect(screen.getByTestId("edit-schedule").textContent).toBe("Sun 25 Oct 2026 18:00 · Repeats: every 3 days");
    expect((screen.getByRole("textbox", { name: "web.edit.when" }) as HTMLInputElement).value).toBe("");

    const noDate = screen.getByRole("button", { name: "web.edit.no_date" });
    fireEvent.click(noDate);
    expect(noDate.getAttribute("aria-pressed")).toBe("true");
    expect((screen.getByRole("textbox", { name: "web.edit.when" }) as HTMLInputElement).disabled).toBe(true);
    expect(screen.queryByTestId("edit-schedule")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));

    await waitFor(() => expect(body).toEqual({ when: null }));
  });

  it("has no No date control for an item without a date", async () => {
    const none = editItem({ when_text: null, due_at: null, due_text: null, due_full: null });
    withItem(none);
    await openForm(none);
    expect(screen.queryByRole("button", { name: "web.edit.no_date" })).toBeNull();
    expect(screen.queryByTestId("edit-schedule")).toBeNull();
  });

  it("shows the item number in the title, in Persian digits in Farsi", async () => {
    setLang("fa");
    setCatalog({ "web.edit.title": "ویرایش کار %s" });
    withItem(editItem({ num: 17 }));
    render(<Edit />);
    openEdit(item({ num: 17 }), user.id, false);
    const dialog = await screen.findByRole("dialog");
    expect(dialog.getAttribute("aria-label")).toBe("ویرایش کار ۱۷");
    expect(screen.getByRole("heading").textContent).toBe("ویرایش کار ۱۷");
  });

  it("shows the item number in the title in English", async () => {
    setCatalog({ "web.edit.title": "Edit item %s" });
    withItem(editItem());
    await openForm();
    expect(screen.getByRole("heading").textContent).toBe("Edit item 3");
  });

  it("closes with web.edit.gone when the item went away after the form opened", async () => {
    withItem(editItem());
    routes[`PATCH ${base}`] = () => json({ error: { code: "not_found", message: "Not found." } }, 404);
    const reload = vi.fn();
    const off = onListsChanged(reload);
    await openForm();
    fireEvent.input(screen.getByRole("textbox", { name: "web.edit.text" }), { target: { value: "buy bread" } });
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));
    await waitFor(() => expect(getEdit()).toBeNull());
    expect(screen.getByRole("status").textContent).toBe("web.edit.gone");
    expect(reload).toHaveBeenCalledTimes(1);
    off();
  });

  it("shows the error of the server and stays open", async () => {
    withItem(editItem());
    routes[`PATCH ${base}`] = () => json({ error: { code: "bad_time", message: "I cannot read that time." } }, 422);
    await openForm();
    fireEvent.input(screen.getByRole("textbox", { name: "web.edit.when" }), { target: { value: "blah" } });
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));
    expect((await screen.findByRole("alert")).textContent).toBe("I cannot read that time.");
    expect(getEdit()).not.toBeNull();
  });

  it("shows the error when the item is gone", async () => {
    routes[`GET ${base}`] = () => json({ error: { code: "not_found", message: "Not found." } }, 404);
    routes[`GET /api/chats/${user.id}/members`] = () => json({ members: [] });
    render(<Edit />);
    openEdit(item(), user.id, false);
    expect((await screen.findByRole("alert")).textContent).toBe("Not found.");
    expect(screen.queryByRole("button", { name: "web.edit.save" })).toBeNull();
  });

  it("has no assignee picker in Assigned to me, where the actor is the private chat", async () => {
    const it = editItem({ chat_id: GROUP });
    routes[`GET ${base}`] = () => json(it);
    render(<Edit />);
    openEdit(item({ chat_id: GROUP }), user.id, false);
    await screen.findByRole("textbox", { name: "web.edit.when" });
    expect(screen.queryByRole("combobox")).toBeNull();
    expect(sent(`GET /api/chats/${user.id}/members`)).toHaveLength(0);
  });
});

describe("the Edit entry of the item sheet", () => {
  it("closes the item sheet and opens the edit form", async () => {
    withItem(editItem());
    render(
      <>
        <Actions
          user={user}
          miniapp={false}
          onChanged={() => {}}
          onSaved={() => {}}
          onSignInBrowser={() => {}}
          onLogout={async () => {}}
        />
        <Edit />
      </>,
    );
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.edit.open" }));
    await screen.findByTestId("edit-sheet");
    expect(screen.queryByTestId("item-sheet")).toBeNull();
    expect(getEdit()?.chat).toBe(user.id);
  });
});

describe("the edit form in the Mini App", () => {
  function fakeTelegram() {
    const button = () => {
      const clicks = new Set<() => void>();
      const calls: string[] = [];
      return {
        clicks,
        calls,
        setText: vi.fn(),
        show: vi.fn(() => calls.push("show")),
        hide: vi.fn(() => calls.push("hide")),
        enable: vi.fn(),
        disable: vi.fn(),
        showProgress: vi.fn(),
        hideProgress: vi.fn(),
        onClick: vi.fn((fn: () => void) => clicks.add(fn)),
        offClick: vi.fn((fn: () => void) => clicks.delete(fn)),
        tap() {
          clicks.forEach((fn) => fn());
        },
      };
    };
    const tg = { MainButton: button(), BackButton: button(), HapticFeedback: { notificationOccurred: vi.fn() } };
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: tg };
    return tg;
  }

  afterEach(() => {
    delete (window as unknown as { Telegram?: unknown }).Telegram;
  });

  it("keeps the BackButton shown after the item sheet, and Back closes the form", async () => {
    const tg = fakeTelegram();
    withItem(editItem());
    render(
      <>
        <Actions
          user={user}
          miniapp={true}
          onChanged={() => {}}
          onSaved={() => {}}
          onSignInBrowser={() => {}}
          onLogout={async () => {}}
        />
        <Edit />
      </>,
    );
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.edit.open" }));
    await screen.findByRole("textbox", { name: "web.edit.when" });
    // Let the deferred cleanups of the item sheet run.
    await new Promise((r) => setTimeout(r, 80));

    expect(tg.BackButton.calls.at(-1)).toBe("show");
    expect(tg.MainButton.calls.at(-1)).toBe("show");
    tg.BackButton.tap();
    await waitFor(() => expect(getEdit()).toBeNull());
  });

  it("saves with the MainButton", async () => {
    const tg = fakeTelegram();
    withItem(editItem());
    let body: unknown;
    routes[`PATCH ${base}`] = (b) => {
      body = b;
      return json(item());
    };
    render(<Edit />);
    openEdit(item(), user.id, true);
    const box = await screen.findByRole("textbox", { name: "web.edit.text" });
    await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe("buy milk"));
    fireEvent.input(box, { target: { value: "buy bread" } });
    tg.MainButton.tap();
    await waitFor(() => expect(body).toEqual({ text: "buy bread" }));
    await waitFor(() => expect(getEdit()).toBeNull());
  });
});

describe("the web.edit strings", () => {
  const root = join(__dirname, "..", "..");
  const walk = (dir: string): string[] =>
    readdirSync(dir).flatMap((n) => {
      const p = join(dir, n);
      return statSync(p).isDirectory() ? walk(p) : [p];
    });
  const messages = readFileSync(join(root, "internal", "web", "messages_edit.go"), "utf8");
  const enBlock = messages.slice(messages.indexOf("i18n.En"), messages.indexOf("i18n.Fa"));
  const keys = [...enBlock.matchAll(/"(web\.edit\.[a-z_]+)"/g)].map((m) => m[1]);
  const web = walk(join(root, "web", "src"))
    .filter((f) => /\.tsx?$/.test(f))
    .map((f) => readFileSync(f, "utf8"))
    .join("\n");
  const server = readdirSync(join(root, "internal", "web"))
    .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go") && f !== "messages_edit.go")
    .map((f) => readFileSync(join(root, "internal", "web", f), "utf8"))
    .join("\n");

  it("finds the keys", () => {
    expect(keys.length).toBeGreaterThan(5);
  });

  it("uses every web.edit key, in the app or in the server", () => {
    for (const k of keys) expect(web.includes(`"${k}"`) || server.includes(`"${k}"`), k).toBe(true);
  });

  it("calls only keys that the server registers, in en and fa", () => {
    const registered = walk(join(root, "internal"))
      .filter((f) => /\/messages[^/]*\.go$/.test(f))
      .map((f) => readFileSync(f, "utf8"))
      .join("\n");
    const form = walk(join(root, "web", "src", "edit"))
      .map((f) => readFileSync(f, "utf8"))
      .join("\n");
    const used = [...form.matchAll(/\bt\("([a-z_.]+)"/g)].map((m) => m[1]);
    expect(used.length).toBeGreaterThan(5);
    for (const k of used) {
      const n = registered.split(`"${k}":`).length - 1;
      expect(n, k).toBeGreaterThanOrEqual(2);
    }
  });

  it("uses no web.actions key for the labels of the form", () => {
    const form = readFileSync(join(root, "web", "src", "edit", "index.tsx"), "utf8");
    expect(form).not.toMatch(/"web\.actions\.(text|assignee)"/);
  });
});

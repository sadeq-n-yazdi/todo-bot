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
  await waitFor(() => expect((screen.getByRole("textbox", { name: "web.actions.text" }) as HTMLTextAreaElement).value).toBe(it.text));
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

    fireEvent.input(screen.getByRole("textbox", { name: "web.actions.text" }), { target: { value: "buy bread" } });
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

  it("sends no PATCH when nothing changed", async () => {
    withItem(editItem());
    await openForm();
    fireEvent.click(screen.getByRole("button", { name: "web.edit.save" }));
    await waitFor(() => expect(getEdit()).toBeNull());
    expect(fetchMock.mock.calls.some((c) => c[1]?.method === "PATCH")).toBe(false);
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

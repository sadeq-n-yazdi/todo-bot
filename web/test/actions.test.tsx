import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Actions } from "../src/actions/index";
import { setPreviewDelay } from "../src/actions/add";
import { closeSheet, openAdd, openItem } from "../src/actions/store";
import { setToken } from "../src/api";
import { writeCache } from "../src/cache";
import { setCatalog, setLang } from "../src/i18n";
import { chats, item } from "../src/tabs/fixtures";
import { Row } from "../src/tabs/row";
import type { User } from "../src/types";

// Builds against the API of docs/WEB-DESIGN.md section 5.3 (endpoints 15 to 21).

const user: User = { id: 11111, username: "alice", lang: "en", calendar: "gregorian", tz: "UTC", dir: "ltr" };
const GROUP = -1001234567890;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
const fail = (status: number, code: string, message: string) => json({ error: { code, message } }, status);

type Handler = (body: unknown) => Response | Promise<Response>;
const fetchMock = vi.fn();
let routes: Record<string, Handler> = {};

/** The requests to "METHOD path", with their JSON bodies. */
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
  setPreviewDelay(10);
  closeSheet();
});

afterEach(() => {
  cleanup();
  closeSheet();
  vi.unstubAllGlobals();
  delete (window as unknown as { Telegram?: unknown }).Telegram;
});

function renderActions(over: Partial<Parameters<typeof Actions>[0]> = {}) {
  const onChanged = vi.fn();
  render(
    <Actions
      user={user}
      miniapp={false}
      onChanged={onChanged}
      onSaved={() => {}}
      onSignInBrowser={() => {}}
      onLogout={async () => {}}
      {...over}
    />,
  );
  return { onChanged };
}

/** A fake Telegram.WebApp with the buttons and the haptic feedback. */
function fakeTelegram() {
  const button = () => {
    const clicks = new Set<() => void>();
    return {
      clicks,
      setText: vi.fn(),
      show: vi.fn(),
      hide: vi.fn(),
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

function addRoutes(members: { id: number; username: string }[] = [{ id: 22222, username: "wife" }]) {
  routes["GET /api/chats"] = () => json(chats);
  routes[`GET /api/chats/${user.id}/members`] = () => json({ members });
  routes[`GET /api/chats/${GROUP}/members`] = () => json({ members: [{ id: 33333, username: "bob" }] });
  routes["POST /api/parse"] = (b) => {
    const text = (b as { text: string }).text;
    return json({ text: "buy milk", due_at: "2026-10-25T17:00:00Z", due_text: `parsed: ${text}`, recur: "", recur_text: "" });
  };
}

async function typeText(value: string) {
  const box = screen.getByRole("textbox");
  fireEvent.input(box, { target: { value } });
}

describe("the add sheet", () => {
  it("shows the live preview, offers the members and adds with the assignee", async () => {
    addRoutes();
    routes[`POST /api/chats/${user.id}/items`] = () => json(item({ id: 99 }), 201);
    const { onChanged } = renderActions();
    openAdd();
    const sheet = await screen.findByTestId("add-sheet");
    await within(sheet).findByRole("option", { name: "@wife" });

    await typeText("buy milk tomorrow 18:00");
    await within(sheet).findByText("parsed: buy milk tomorrow 18:00");
    expect(sent("POST /api/parse")).toEqual([{ text: "buy milk tomorrow 18:00" }]);

    fireEvent.change(within(sheet).getByRole("combobox", { name: "web.actions.assignee" }), { target: { value: "22222" } });
    fireEvent.click(within(sheet).getByRole("button", { name: "web.actions.add" }));

    await screen.findByText("web.actions.added");
    expect(sent(`POST /api/chats/${user.id}/items`)).toEqual([{ text: "buy milk tomorrow 18:00", assignee_id: 22222 }]);
    expect(onChanged).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId("add-sheet")).toBeNull();
  });

  it("asks for the preview only after the pause, and only for the last text", async () => {
    setPreviewDelay(60);
    addRoutes();
    renderActions();
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    await typeText("b");
    await typeText("bu");
    await typeText("buy");
    await screen.findByText("parsed: buy");
    expect(sent("POST /api/parse")).toEqual([{ text: "buy" }]);
  });

  it("asks for no preview for empty text, and Add stays off", async () => {
    addRoutes();
    renderActions();
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    await typeText("   ");
    await new Promise((r) => setTimeout(r, 30));
    expect(sent("POST /api/parse")).toEqual([]);
    expect((screen.getByRole("button", { name: "web.actions.add" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("starts in the last group when the Groups tab is open, and loads its members", async () => {
    addRoutes();
    writeCache(user.id, "pref.tab", "groups");
    writeCache(user.id, "pref.group", GROUP);
    routes[`POST /api/chats/${GROUP}/items`] = () => json(item({ chat_id: GROUP }), 201);
    renderActions();
    openAdd();
    await screen.findByRole("option", { name: "@bob" });
    expect((screen.getByRole("combobox", { name: "web.actions.chat" }) as HTMLSelectElement).value).toBe(String(GROUP));
    await typeText("call mum");
    await screen.findByText("parsed: call mum");
    fireEvent.click(screen.getByRole("button", { name: "web.actions.add" }));
    await screen.findByText("web.actions.added");
    expect(sent(`POST /api/chats/${GROUP}/items`)).toEqual([{ text: "call mum", assignee_id: 0 }]);
  });

  it("changes the members when the list changes", async () => {
    addRoutes();
    renderActions();
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    fireEvent.change(screen.getByRole("combobox", { name: "web.actions.chat" }), { target: { value: String(GROUP) } });
    await screen.findByRole("option", { name: "@bob" });
    expect(screen.queryByRole("option", { name: "@wife" })).toBeNull();
  });

  it("shows the message of the server on an error and keeps the sheet open", async () => {
    addRoutes();
    routes[`POST /api/chats/${user.id}/items`] = () => fail(422, "too_long", "The text is too long.");
    const { onChanged } = renderActions();
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    await typeText("x");
    await screen.findByText("parsed: x");
    fireEvent.click(screen.getByRole("button", { name: "web.actions.add" }));
    expect((await screen.findByRole("alert")).textContent).toBe("The text is too long.");
    expect(onChanged).not.toHaveBeenCalled();
    expect(screen.getByTestId("add-sheet")).toBeTruthy();
  });

  it("shows the preview error of the server", async () => {
    addRoutes();
    routes["POST /api/parse"] = () => fail(422, "too_long", "Too long.");
    renderActions();
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    await typeText("x");
    await within(screen.getByTestId("preview")).findByText("Too long.");
  });

  it("uses the MainButton and the BackButton in the Mini App", async () => {
    const tg = fakeTelegram();
    addRoutes();
    routes[`POST /api/chats/${user.id}/items`] = () => json(item(), 201);
    const { onChanged } = renderActions({ miniapp: true });
    openAdd();
    await screen.findByRole("option", { name: "@wife" });

    // No Add or Close button in the page: Telegram has them.
    expect(screen.queryByRole("button", { name: "web.actions.add" })).toBeNull();
    expect(screen.queryByRole("button", { name: "web.actions.close" })).toBeNull();
    expect(tg.MainButton.show).toHaveBeenCalled();
    expect(tg.MainButton.setText).toHaveBeenLastCalledWith("web.actions.add");
    expect(tg.MainButton.disable).toHaveBeenCalled();
    expect(tg.BackButton.show).toHaveBeenCalled();

    await typeText("buy milk");
    await screen.findByText("parsed: buy milk");
    expect(tg.MainButton.enable).toHaveBeenCalled();
    tg.MainButton.tap();
    await screen.findByText("web.actions.added");
    expect(sent(`POST /api/chats/${user.id}/items`)).toEqual([{ text: "buy milk", assignee_id: 0 }]);
    expect(onChanged).toHaveBeenCalled();
    await waitFor(() => expect(tg.MainButton.hide).toHaveBeenCalled());
    expect(tg.MainButton.clicks.size).toBe(0);
    expect(tg.BackButton.clicks.size).toBe(0);
  });

  it("closes with the BackButton in the Mini App", async () => {
    const tg = fakeTelegram();
    addRoutes();
    renderActions({ miniapp: true });
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    tg.BackButton.tap();
    await waitFor(() => expect(screen.queryByTestId("add-sheet")).toBeNull());
    await waitFor(() => expect(tg.BackButton.hide).toHaveBeenCalled());
    expect(tg.MainButton.hide).toHaveBeenCalled();
  });
});

describe("the item sheet", () => {
  const base = `/api/chats/${user.id}/items/42`;

  it("marks the item as done, with the haptic feedback in the Mini App", async () => {
    const tg = fakeTelegram();
    routes[`POST ${base}/done`] = () => json({ item: item(), still_open: false });
    const { onChanged } = renderActions({ miniapp: true });
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.done" }));
    await screen.findByText("web.actions.done_ok");
    expect(sent(`POST ${base}/done`)).toEqual([{}]);
    expect(tg.HapticFeedback.notificationOccurred).toHaveBeenCalledWith("success");
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("shows the next date of an item that repeats", async () => {
    setCatalog({ "web.actions.next": "Next: %s" });
    routes[`POST ${base}/done`] = () => json({ item: item({ due_text: "Sun 18:00" }), still_open: true });
    renderActions();
    openItem(item({ recur_text: "every Sun" }), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.done" }));
    await screen.findByText("Next: Sun 18:00");
  });

  for (const preset of ["10m", "1h", "tomorrow"] as const) {
    it(`reminds later with the ${preset} preset`, async () => {
      setCatalog({ "web.actions.reminded": "Reminder %s." });
      routes[`POST ${base}/remind`] = () => json({ fire_at: "2026-10-25T17:00:00Z", fire_text: "at 18:00" }, 201);
      const { onChanged } = renderActions();
      openItem(item(), user.id);
      const keys = { "10m": "web.actions.remind_10m", "1h": "web.actions.remind_1h", tomorrow: "web.actions.remind_tomorrow" };
      fireEvent.click(await screen.findByRole("button", { name: keys[preset] }));
      await screen.findByText("Reminder at 18:00.");
      expect(sent(`POST ${base}/remind`)).toEqual([{ preset }]);
      expect(onChanged).toHaveBeenCalledTimes(1);
    });
  }

  it("reminds later at a time that the user types", async () => {
    routes[`POST ${base}/remind`] = () => json({ fire_at: "x", fire_text: "fri 09:00" }, 201);
    renderActions();
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.remind_custom" }));
    const set = screen.getByRole("button", { name: "web.actions.remind_set" }) as HTMLButtonElement;
    expect(set.disabled).toBe(true);
    fireEvent.input(screen.getByRole("textbox", { name: "web.actions.remind_when" }), { target: { value: "friday 9:00" } });
    fireEvent.click(set);
    await screen.findByText("web.actions.reminded");
    expect(sent(`POST ${base}/remind`)).toEqual([{ when: "friday 9:00" }]);
  });

  it("shows the message of the server for a bad time", async () => {
    routes[`POST ${base}/remind`] = () => fail(422, "in_past", "That time is in the past.");
    renderActions();
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.remind_custom" }));
    fireEvent.input(screen.getByRole("textbox"), { target: { value: "yesterday" } });
    fireEvent.click(screen.getByRole("button", { name: "web.actions.remind_set" }));
    expect((await screen.findByRole("alert")).textContent).toBe("That time is in the past.");
    expect(screen.getByTestId("item-sheet")).toBeTruthy();
  });

  it("deletes only after the confirmation", async () => {
    routes[`DELETE ${base}`] = () => new Response(null, { status: 204 });
    const { onChanged } = renderActions();
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.delete" }));
    expect(screen.getByText("web.actions.delete_ask")).toBeTruthy();
    expect(sent(`DELETE ${base}`)).toEqual([]);

    // Cancel goes back, and nothing is deleted.
    fireEvent.click(screen.getByRole("button", { name: "web.actions.cancel" }));
    expect(screen.queryByText("web.actions.delete_ask")).toBeNull();
    expect(sent(`DELETE ${base}`)).toEqual([]);

    fireEvent.click(screen.getByRole("button", { name: "web.actions.delete" }));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.delete_yes" }));
    await screen.findByText("web.actions.deleted");
    expect(sent(`DELETE ${base}`)).toHaveLength(1);
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("the BackButton leaves the confirmation first, then closes the sheet", async () => {
    const tg = fakeTelegram();
    renderActions({ miniapp: true });
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.delete" }));
    tg.BackButton.tap();
    await screen.findByRole("button", { name: "web.actions.done" });
    tg.BackButton.tap();
    await waitFor(() => expect(screen.queryByTestId("item-sheet")).toBeNull());
  });

  it("shows the message of the server for a group the user left", async () => {
    routes[`POST /api/chats/${GROUP}/items/42/done`] = () => fail(403, "not_member", "You are not in this group.");
    renderActions();
    openItem(item({ chat_id: GROUP }), GROUP);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.done" }));
    expect((await screen.findByRole("alert")).textContent).toBe("You are not in this group.");
  });

  it("sends one Done request for two fast taps", async () => {
    let finish: (r: Response) => void = () => {};
    routes[`POST ${base}/done`] = () => new Promise<Response>((r) => (finish = r));
    renderActions();
    openItem(item(), user.id);
    const done = await screen.findByRole("button", { name: "web.actions.done" });
    fireEvent.click(done);
    fireEvent.click(done);
    await waitFor(() => expect((done as HTMLButtonElement).disabled).toBe(true));
    expect(screen.getByRole("button", { name: "web.actions.delete" }).hasAttribute("disabled")).toBe(true);
    expect(sent(`POST ${base}/done`)).toHaveLength(1);
    finish(json({ item: item(), still_open: false }));
    await screen.findByText("web.actions.done_ok");
  });

  it("tells the parent of the change when the sheet closed during the request", async () => {
    let finish: (r: Response) => void = () => {};
    routes[`POST ${base}/done`] = () => new Promise<Response>((r) => (finish = r));
    addRoutes();
    const { onChanged } = renderActions();
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.done" }));
    await waitFor(() => expect(sent(`POST ${base}/done`)).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "web.actions.close" }));
    // The user opens the add sheet meanwhile. The late answer must not close it.
    openAdd();
    await screen.findByRole("option", { name: "@wife" });
    finish(json({ item: item(), still_open: false }));
    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(screen.getByTestId("add-sheet")).toBeTruthy();
  });

  it("shows a local text for a network error", async () => {
    routes[`POST ${base}/done`] = () => {
      throw new TypeError("Failed to fetch");
    };
    renderActions();
    openItem(item(), user.id);
    fireEvent.click(await screen.findByRole("button", { name: "web.actions.done" }));
    expect((await screen.findByRole("alert")).textContent).toBe("web.actions.offline");
  });
});

describe("the row", () => {
  it("opens the item sheet, and the check mark marks the item as done", async () => {
    routes[`POST /api/chats/${user.id}/items/42/done`] = () => json({ item: item(), still_open: false });
    renderActions();
    render(
      <ul>
        <Row item={item()} offline={false} showChat={false} viewerId={user.id} />
      </ul>,
    );
    fireEvent.click(screen.getByText("buy milk"));
    await screen.findByTestId("item-sheet");
    fireEvent.click(screen.getByRole("button", { name: "web.actions.close" }));
    expect(screen.queryByTestId("item-sheet")).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "web.actions.mark_done" }));
    await screen.findByText("web.actions.done_ok");
    expect(sent(`POST /api/chats/${user.id}/items/42/done`)).toEqual([{}]);
  });

  it("acts through the private chat of the viewer in Assigned to me", async () => {
    routes[`POST /api/chats/${user.id}/items/42/done`] = () => json({ item: item(), still_open: false });
    renderActions();
    const assigned = item({ chat_id: GROUP, chat_title: "Family", assignee: { id: user.id, username: "alice" } });
    render(
      <ul>
        <Row item={assigned} offline={false} showChat viewerId={user.id} />
      </ul>,
    );
    fireEvent.click(screen.getByRole("button", { name: "web.actions.mark_done" }));
    await screen.findByText("web.actions.done_ok");
    expect(sent(`POST /api/chats/${user.id}/items/42/done`)).toEqual([{}]);
  });

  it("has no buttons offline", () => {
    render(
      <ul>
        <Row item={item()} offline={true} showChat={false} viewerId={user.id} />
      </ul>,
    );
    expect(document.querySelectorAll("li button, li input")).toHaveLength(0);
  });
});

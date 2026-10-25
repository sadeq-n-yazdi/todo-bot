import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setCatalog, setLang } from "../src/i18n";
import { readCache, writeCache } from "../src/cache";
import { chats, farsiItem, item, list } from "../src/tabs/fixtures";
import { LOGOUT_EVENT, Tabs } from "../src/tabs/index";
import { resetPwa } from "../src/tabs/pwa";
import type { User } from "../src/types";

const user: User = { id: 11111, username: "alice", lang: "en", calendar: "gregorian", tz: "UTC", dir: "ltr" };

const en: Record<string, string> = {
  "web.tabs.mine": "Mine",
  "web.tabs.assigned": "Assigned to me",
  "web.tabs.groups": "Groups",
  "web.tabs.empty.mine": "You have no open items.",
  "web.tabs.empty.assigned": "Nobody gave you an item.",
  "web.tabs.empty.groups": "You are not in a group with this bot.",
  "web.tabs.empty.group": "This group has no open items.",
  "web.tabs.more": "Load more",
  "web.tabs.offline": "No connection. This list is from %s.",
  "web.tabs.offline_none": "No connection, and there is no saved list.",
  "web.tabs.noname": "(no username)",
};

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

type Route = (url: string) => Response | Promise<Response> | undefined;
const fetchMock = vi.fn();
let routes: Route[] = [];
let down = false;

function calls(prefix: string): string[] {
  return fetchMock.mock.calls.map((c) => c[0] as string).filter((u) => u.startsWith(prefix));
}

function setVisibility(v: "visible" | "hidden") {
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => v });
}

beforeEach(() => {
  routes = [];
  down = false;
  fetchMock.mockReset();
  fetchMock.mockImplementation(async (url: string) => {
    if (down) throw new TypeError("Failed to fetch");
    for (const r of routes) {
      const res = r(url);
      if (res) return res;
    }
    throw new Error("unexpected " + url);
  });
  vi.stubGlobal("fetch", fetchMock);
  localStorage.clear();
  setLang("en");
  setCatalog(en);
  resetPwa();
  setVisibility("visible");
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const mine = (r: ReturnType<typeof list>, page = 0) => (url: string) =>
  url === `/api/chats/11111/items?page=${page}` ? json(r) : undefined;

describe("Mine tab", () => {
  it("shows the items with the relative due text, recurrence and assignee", async () => {
    routes.push(
      mine(
        list([
          item({ num: 1, text: "buy milk", due_text: "today 18:00", overdue: true }),
          item({ id: 43, num: 2, text: "call mom", recur_text: "every Sun", assignee: { id: 2, username: "wife" } }),
        ]),
      ),
    );
    render(<Tabs user={user} />);
    await screen.findByText("buy milk");
    expect(screen.getByText("today 18:00").className).toContain("overdue");
    expect(screen.getByText("every Sun")).toBeTruthy();
    // The username sits in <bdi>, so a Farsi page keeps "@wife" in order.
    const who = screen.getByText("@wife");
    expect(who.tagName).toBe("BDI");
    // Item text has dir="auto".
    expect(screen.getByText("call mom").getAttribute("dir")).toBe("auto");
  });

  it("shows the number and a placeholder when the assignee has no username", async () => {
    routes.push(mine(list([item({ assignee: { id: 5, username: "" } })])));
    render(<Tabs user={user} />);
    await screen.findByText("(no username)");
  });

  it("shows the empty state", async () => {
    routes.push(mine(list([])));
    render(<Tabs user={user} />);
    await screen.findByText("You have no open items.");
  });

  it("loads the next page with Load more, and then hides the button", async () => {
    routes.push(mine(list([item({ id: 1, text: "first" })], { pages: 2 }), 0));
    routes.push(mine(list([item({ id: 2, text: "second" })], { page: 1, pages: 2 }), 1));
    render(<Tabs user={user} />);
    await screen.findByText("first");
    expect(calls("/api/chats/11111/items")).toEqual(["/api/chats/11111/items?page=0"]);

    fireEvent.click(screen.getByText("Load more"));
    await screen.findByText("second");
    expect(screen.getByText("first")).toBeTruthy();
    expect(screen.queryByText("Load more")).toBeNull();
  });

  it("sends the Todo header and no body", async () => {
    routes.push(mine(list([])));
    render(<Tabs user={user} />);
    await screen.findByText("You have no open items.");
    const init = fetchMock.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe("GET");
    expect(new Headers(init.headers).get("X-Todo-Request")).toBe("1");
  });
});

describe("Assigned tab", () => {
  it("shows the chat title of each item and the empty state", async () => {
    routes.push((u) =>
      u === "/api/assigned" ? json(list([item({ text: "pay rent", chat_title: "Family", chat_id: -100 })], { pages: 1 })) : undefined,
    );
    routes.push(mine(list([])));
    render(<Tabs user={user} />);
    fireEvent.click(screen.getByRole("tab", { name: "Assigned to me" }));
    await screen.findByText("pay rent");
    expect(screen.getByText("Family").tagName).toBe("BDI");
  });

  it("shows its own empty state", async () => {
    routes.push((u) => (u === "/api/assigned" ? json(list([])) : undefined));
    routes.push(mine(list([])));
    render(<Tabs user={user} />);
    fireEvent.click(screen.getByRole("tab", { name: "Assigned to me" }));
    await screen.findByText("Nobody gave you an item.");
  });

  it("remembers the tab of the user", async () => {
    routes.push((u) => (u === "/api/assigned" ? json(list([])) : undefined));
    routes.push(mine(list([])));
    const first = render(<Tabs user={user} />);
    fireEvent.click(screen.getByRole("tab", { name: "Assigned to me" }));
    await screen.findByText("Nobody gave you an item.");
    first.unmount();
    render(<Tabs user={user} />);
    await screen.findByText("Nobody gave you an item.");
  });
});

describe("Groups tab", () => {
  const groupRoute = (id: number, items: ReturnType<typeof item>[], over = {}) => (u: string) =>
    u === `/api/chats/${id}/items?page=0` ? json(list(items, over)) : undefined;

  it("shows one chip for each group and the list of the first group", async () => {
    routes.push((u) => (u === "/api/chats" ? json(chats) : undefined));
    routes.push(groupRoute(-1001234567890, [item({ text: "family task" })]));
    routes.push(groupRoute(-1009876543210, [item({ text: "work task" })]));
    routes.push(mine(list([])));
    render(<Tabs user={user} />);
    fireEvent.click(screen.getByRole("tab", { name: "Groups" }));
    await screen.findByText("family task");
    const family = screen.getByRole("button", { name: "Family" });
    expect(family.getAttribute("aria-pressed")).toBe("true");
    // The private chat is not a chip.
    expect(screen.getAllByRole("button", { pressed: false }).map((b) => b.textContent)).toEqual(["Work"]);

    fireEvent.click(screen.getByRole("button", { name: "Work" }));
    await screen.findByText("work task");
    expect(screen.queryByText("family task")).toBeNull();
    expect(readCache(user.id, "pref.group")).toBe(-1009876543210);
  });

  it("opens the last group again", async () => {
    writeCache(user.id, "pref.group", -1009876543210);
    routes.push((u) => (u === "/api/chats" ? json(chats) : undefined));
    routes.push(groupRoute(-1009876543210, [item({ text: "work task" })]));
    routes.push(mine(list([])));
    writeCache(user.id, "pref.tab", "groups");
    render(<Tabs user={user} />);
    await screen.findByText("work task");
  });

  it("forgets a stored group that is not in the list any more", async () => {
    writeCache(user.id, "pref.group", -555);
    writeCache(user.id, "pref.tab", "groups");
    routes.push((u) => (u === "/api/chats" ? json(chats) : undefined));
    routes.push(groupRoute(-1001234567890, [item({ text: "family task" })]));
    render(<Tabs user={user} />);
    await screen.findByText("family task");
  });

  it("shows the empty state when the user has no group, and the partial note", async () => {
    writeCache(user.id, "pref.tab", "groups");
    routes.push((u) =>
      u === "/api/chats" ? json({ chats: [{ id: user.id, kind: "private", title: "" }], partial: false }) : undefined,
    );
    render(<Tabs user={user} />);
    await screen.findByText("You are not in a group with this bot.");
  });

  it("shows the empty state of one group", async () => {
    writeCache(user.id, "pref.tab", "groups");
    routes.push((u) => (u === "/api/chats" ? json(chats) : undefined));
    routes.push(groupRoute(-1001234567890, []));
    render(<Tabs user={user} />);
    await screen.findByText("This group has no open items.");
  });

  it("shows the note when the answer is partial", async () => {
    writeCache(user.id, "pref.tab", "groups");
    setCatalog({ ...en, "web.tabs.partial": "Some groups may be missing." });
    routes.push((u) => (u === "/api/chats" ? json({ ...chats, partial: true }) : undefined));
    routes.push(groupRoute(-1001234567890, []));
    render(<Tabs user={user} />);
    await screen.findByText("Some groups may be missing.");
  });

  it("shows an error from the server with a retry button", async () => {
    writeCache(user.id, "pref.tab", "groups");
    setCatalog({ ...en, "web.tabs.retry": "Try again" });
    let fail = true;
    routes.push((u) => {
      if (u !== "/api/chats") return undefined;
      return fail ? json({ error: { code: "internal", message: "Boom" } }, 500) : json({ chats: [], partial: false });
    });
    render(<Tabs user={user} />);
    await screen.findByText("Boom");
    fail = false;
    fireEvent.click(screen.getByText("Try again"));
    await screen.findByText("You are not in a group with this bot.");
  });

  it("shows the error of a group list, for example 403 not_member", async () => {
    writeCache(user.id, "pref.tab", "groups");
    routes.push((u) => (u === "/api/chats" ? json(chats) : undefined));
    routes.push((u) =>
      u === "/api/chats/-1001234567890/items?page=0"
        ? json({ error: { code: "not_member", message: "You are not a member of this group." } }, 403)
        : undefined,
    );
    render(<Tabs user={user} />);
    await screen.findByText("You are not a member of this group.");
  });
});

describe("refetch", () => {
  async function setup() {
    let n = 0;
    routes.push((u) => (u === "/api/chats/11111/items?page=0" ? json(list([item({ text: `v${++n}` })])) : undefined));
    render(<Tabs user={user} />);
    await screen.findByText("v1");
    return () => calls("/api/chats/11111/items").length;
  }

  it("loads again when the page becomes visible", async () => {
    const count = await setup();
    expect(count()).toBe(1);
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await screen.findByText("v2");
    expect(count()).toBe(2);
  });

  it("does not load when the page becomes hidden", async () => {
    const count = await setup();
    setVisibility("hidden");
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(count()).toBe(1);
  });

  it("loads again on focus, without a spinner", async () => {
    const count = await setup();
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await screen.findByText("v2");
    expect(count()).toBe(2);
    expect(screen.queryByRole("status")).toBeNull();
  });

  it("loads again on the activated event of the Mini App, and stops after unmount", async () => {
    const handlers: Record<string, () => void> = {};
    (window as unknown as { Telegram: unknown }).Telegram = {
      WebApp: {
        onEvent: (n: string, fn: () => void) => (handlers[n] = fn),
        offEvent: (n: string, fn: () => void) => {
          if (handlers[n] === fn) delete handlers[n];
        },
      },
    };
    try {
      let n = 0;
      routes.push((u) => (u === "/api/chats/11111/items?page=0" ? json(list([item({ text: `v${++n}` })])) : undefined));
      const r = render(<Tabs user={user} />);
      await screen.findByText("v1");
      await act(async () => handlers["activated"]());
      await screen.findByText("v2");
      await act(async () => {
        r.unmount();
      });
      expect(handlers["activated"]).toBeUndefined();
    } finally {
      delete (window as unknown as { Telegram?: unknown }).Telegram;
    }
  });

  it("keeps the pages that the user opened", async () => {
    routes.push(mine(list([item({ id: 1, text: "first" })], { pages: 2 }), 0));
    routes.push(mine(list([item({ id: 2, text: "second" })], { page: 1, pages: 2 }), 1));
    render(<Tabs user={user} />);
    await screen.findByText("first");
    fireEvent.click(screen.getByText("Load more"));
    await screen.findByText("second");
    fetchMock.mockClear();
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(calls("/api/chats/11111/items")).toEqual(["/api/chats/11111/items?page=0", "/api/chats/11111/items?page=1"]));
    expect(screen.getByText("second")).toBeTruthy();
  });

  it("does not start a second load while one is on the way", async () => {
    let release: (r: Response) => void = () => {};
    let hits = 0;
    routes.push((u) => {
      if (u !== "/api/chats/11111/items?page=0") return undefined;
      hits++;
      if (hits === 1) return json(list([item({ text: "v1" })]));
      return new Promise<Response>((res) => (release = res));
    });
    render(<Tabs user={user} />);
    await screen.findByText("v1");
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(hits).toBe(2);
    await act(async () => release(json(list([item({ text: "v2" })]))));
    await screen.findByText("v2");
  });

  it("loads the groups again too", async () => {
    writeCache(user.id, "pref.tab", "groups");
    let n = 0;
    routes.push((u) => (u === "/api/chats" ? json({ chats: chats.chats.slice(0, 2), partial: false }) : undefined));
    routes.push((u) => (u === "/api/chats/-1001234567890/items?page=0" ? json(list([item({ text: `g${++n}` })])) : undefined));
    render(<Tabs user={user} />);
    await screen.findByText("g1");
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await screen.findByText("g2");
    expect(calls("/api/chats").filter((u) => u === "/api/chats")).toHaveLength(2);
  });
});

describe("offline", () => {
  it("shows the saved list with due_full, a banner with the time and no buttons", async () => {
    routes.push(
      mine(
        list([item({ text: "buy milk", due_text: "tomorrow 18:00", due_full: "Sun 25 Oct 2026 18:00" })], {
          rendered_at: "2026-10-24T08:30:00Z",
        }),
      ),
    );
    render(<Tabs user={user} />);
    await screen.findByText("tomorrow 18:00");

    down = true;
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await screen.findByText("Sun 25 Oct 2026 18:00");
    expect(screen.queryByText("tomorrow 18:00")).toBeNull();
    expect(screen.getByText("No connection. This list is from 08:30.")).toBeTruthy();
    expect(within(document.body).queryByRole("button", { name: "Load more" })).toBeNull();
    expect(screen.getByText("buy milk").closest("li")!.querySelectorAll("button, input")).toHaveLength(0);

    // The network is back: the list is live again and the banner goes.
    down = false;
    await act(async () => {
      window.dispatchEvent(new Event("online"));
    });
    await screen.findByText("tomorrow 18:00");
    expect(screen.queryByText(/No connection/)).toBeNull();
  });

  it("starts offline from the saved list, with Persian digits in Farsi", async () => {
    setLang("fa");
    setCatalog({ ...en, "web.tabs.offline": "اتصال ندارید. ساعت %s" });
    writeCache(user.id, "mine", { items: [farsiItem], page: 0, pages: 1, rendered_at: "2026-10-24T08:30:00Z" });
    down = true;
    render(<Tabs user={{ ...user, lang: "fa" }} />);
    await screen.findByText("یکشنبه ۳ آبان ۱۴۰۵ ۱۸:۰۰");
    expect(screen.getByText("اتصال ندارید. ساعت ۰۸:۳۰")).toBeTruthy();
    expect(screen.getByText("@thesadeq").tagName).toBe("BDI");
    expect(screen.getByText("خرید شیر و نان").getAttribute("dir")).toBe("auto");
  });

  it("says so when it is offline and nothing is saved", async () => {
    down = true;
    render(<Tabs user={user} />);
    await screen.findByText("No connection, and there is no saved list.");
  });

  it("does not use another user's list", async () => {
    writeCache(99, "mine", list([item({ text: "secret" })]));
    down = true;
    render(<Tabs user={user} />);
    await screen.findByText("No connection, and there is no saved list.");
    expect(screen.queryByText("secret")).toBeNull();
    // The entries of other users are deleted at the start.
    expect(readCache(99, "mine")).toBeNull();
  });

  it("keeps the list that was loaded with Load more for offline use", async () => {
    routes.push(mine(list([item({ id: 1, text: "first" })], { pages: 2 }), 0));
    routes.push(mine(list([item({ id: 2, text: "second" })], { page: 1, pages: 2 }), 1));
    render(<Tabs user={user} />);
    await screen.findByText("first");
    fireEvent.click(screen.getByText("Load more"));
    await screen.findByText("second");
    expect(readCache<{ items: unknown[] }>(user.id, "mine")!.items).toHaveLength(2);
  });
});

describe("logout", () => {
  it("deletes the offline lists of the user on the logout event", async () => {
    routes.push(mine(list([item({ text: "buy milk" })])));
    render(<Tabs user={user} />);
    await screen.findByText("buy milk");
    expect(readCache(user.id, "mine")).not.toBeNull();
    writeCache(77, "mine", 1); // another user's entry is not this user's concern
    await act(async () => {
      window.dispatchEvent(new Event(LOGOUT_EVENT));
    });
    expect(readCache(user.id, "mine")).toBeNull();
    expect(readCache(user.id, "pref.tab")).toBeNull();
  });

  it("deletes them after a 401", async () => {
    routes.push(mine(list([item({ text: "buy milk" })])));
    render(<Tabs user={user} />);
    await screen.findByText("buy milk");
    routes.length = 0;
    routes.push(() => json({ error: { code: "session_revoked", message: "Gone" } }, 401));
    await act(async () => {
      window.dispatchEvent(new Event("focus"));
    });
    await waitFor(() => expect(readCache(user.id, "mine")).toBeNull());
  });
});

describe("the page shell", () => {
  it("has a tablist with the three tabs, and the first is selected", async () => {
    routes.push(mine(list([])));
    render(<Tabs user={user} />);
    const tabs = screen.getAllByRole("tab");
    expect(tabs.map((x) => x.textContent)).toEqual(["Mine", "Assigned to me", "Groups"]);
    expect(tabs[0].getAttribute("aria-selected")).toBe("true");
    await screen.findByText("You have no open items.");
  });
});

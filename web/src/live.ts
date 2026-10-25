// Live updates (docs/WEB-DESIGN.md section 7.6, phase 4).
//
// - While the page is visible and the network is online, the app asks for the
//   current list every 30 seconds with If-None-Match. A 304 costs one small
//   response. The poll stops when the page is hidden or the network is offline.
// - A save in the app (the edit form) tells the app to load the lists again.
//
// The poll uses its own fetch, because api() treats a 304 as an error and does
// not give the ETag header.

import { api, getToken } from "./api";
import { readCache } from "./cache";
import type { Chat, ChatsResponse, ListResponse, SavedList } from "./tabs/model";

export const POLL_MS = 30_000;

/** The list that the tabs show now: its URL and its name in the offline cache. */
export interface PollTarget {
  path: string;
  cache: string;
}

/**
 * The current list, from the choices that the tabs keep in the cache
 * ("pref.tab" and "pref.group"). Groups without a known group give null.
 */
export function pollTarget(userId: number): PollTarget | null {
  const tab = readCache<string>(userId, "pref.tab");
  if (tab === "assigned") return { path: "/api/assigned", cache: "assigned" };
  if (tab !== "groups") return { path: `/api/chats/${userId}/items?page=0`, cache: "mine" };

  // The Groups tab shows the stored group, or the first group when that one is gone.
  const groups = (readCache<ChatsResponse>(userId, "chats")?.chats ?? []).filter(
    (c: Chat) => c.kind !== "private" && c.id !== userId,
  );
  const picked = readCache<number>(userId, "pref.group");
  const g = groups.find((c) => c.id === picked) ?? groups[0];
  return g ? { path: `/api/chats/${g.id}/items?page=0`, cache: `group-${g.id}` } : null;
}

type Answer = { kind: "same" } | { kind: "new"; body: ListResponse; etag: string } | { kind: "unauthorized" } | { kind: "error" };

async function ask(path: string, etag: string | undefined): Promise<Answer> {
  const headers = new Headers({ "X-Todo-Request": "1", Accept: "application/json" });
  const token = getToken();
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (etag) headers.set("If-None-Match", etag);
  let res: Response;
  try {
    // no-store: the page sends If-None-Match itself, and sees the 304.
    res = await fetch(path, { method: "GET", headers, credentials: "same-origin", cache: "no-store" });
  } catch {
    return { kind: "error" };
  }
  if (res.status === 304) return { kind: "same" };
  if (res.status === 401) return { kind: "unauthorized" };
  if (!res.ok) return { kind: "error" };
  try {
    return { kind: "new", body: (await res.json()) as ListResponse, etag: res.headers.get("ETag") ?? "" };
  } catch {
    return { kind: "error" };
  }
}

/** True when the answer shows the same first page as the list on the screen. */
function sameAsSaved(body: ListResponse, saved: SavedList): boolean {
  const items = body.items ?? [];
  return (
    (body.pages || 1) === (saved.pages || 1) &&
    JSON.stringify(items) === JSON.stringify((saved.items ?? []).slice(0, items.length)) &&
    (saved.page > 0 || items.length === (saved.items ?? []).length)
  );
}

/**
 * Start the poll for a user. `onChange` runs when the current list changed on
 * the server. The returned function stops the poll.
 *
 * - The first request goes out one period after the start, because the tabs
 *   have just loaded the list.
 * - A 200 is compared with the list in the offline cache, which the tabs wrote
 *   at their last load. Without a cache entry, only a new ETag counts.
 * - A 401 stops the poll. A call of api() then takes the app to sign-in.
 */
export function startPoll(userId: number, onChange: () => void, every: number = POLL_MS): () => void {
  const etags = new Map<string, string>();
  let timer: ReturnType<typeof setInterval> | undefined;
  let stopped = false;
  let busy = false;

  const active = () => !stopped && document.visibilityState === "visible" && navigator.onLine;

  const tick = async () => {
    if (busy || !active()) return;
    const target = pollTarget(userId);
    if (!target) return;
    busy = true;
    try {
      const prev = etags.get(target.path);
      const r = await ask(target.path, prev);
      if (stopped) return;
      if (r.kind === "unauthorized") {
        stop();
        void api("GET", "/api/me").catch(() => {});
        return;
      }
      if (r.kind !== "new") return;
      if (r.etag) etags.set(target.path, r.etag);
      const saved = readCache<SavedList>(userId, target.cache);
      const changed = saved ? !sameAsSaved(r.body, saved) : prev !== undefined && r.etag !== prev;
      if (changed) onChange();
    } finally {
      busy = false;
    }
  };

  // Run the timer only while the page is visible and online.
  const sync = () => {
    if (active()) {
      if (timer === undefined) timer = setInterval(() => void tick(), every);
    } else if (timer !== undefined) {
      clearInterval(timer);
      timer = undefined;
    }
  };

  document.addEventListener("visibilitychange", sync);
  window.addEventListener("online", sync);
  window.addEventListener("offline", sync);
  sync();

  function stop() {
    stopped = true;
    sync();
    document.removeEventListener("visibilitychange", sync);
    window.removeEventListener("online", sync);
    window.removeEventListener("offline", sync);
  }
  return stop;
}

const changeListeners = new Set<() => void>();

/** Tell the app that a save changed the lists, so it loads them again. */
export function listsChanged(): void {
  changeListeners.forEach((fn) => fn());
}

/** Register a function for listsChanged. It returns a function that removes it. */
export function onListsChanged(fn: () => void): () => void {
  changeListeners.add(fn);
  return () => changeListeners.delete(fn);
}

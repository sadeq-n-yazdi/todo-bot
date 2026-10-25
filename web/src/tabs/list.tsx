import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { api, ApiError } from "../api";
import { readCache, writeCache } from "../cache";
import { num, t } from "../i18n";
import type { User } from "../types";
import type { ListResponse, SavedList } from "./model";
import { Row } from "./row";

type Status = "loading" | "ok" | "offline" | "error";

interface ListState extends SavedList {
  status: Status;
  /** Offline, and the cache has no entry for this list. */
  noSaved: boolean;
  message: string;
  moreBusy: boolean;
}

const empty = (status: Status): ListState => ({
  items: [],
  page: 0,
  pages: 1,
  rendered_at: "",
  status,
  noSaved: false,
  message: "",
  moreBusy: false,
});

/**
 * Loads one list (docs/WEB-DESIGN.md sections 5.3 and 7.5).
 *
 * - `pathOf(page)` is the URL of a page. A list that has no paging ignores `page`.
 * - A change of `tick` loads the pages again in the background. It loads every
 *   page that the user has opened, so "Load more" keeps its place.
 * - Each good answer goes to the offline cache. A network error shows that cache.
 */
export function useList(userId: number, cacheName: string, pathOf: (page: number) => string, tick: number) {
  const [state, setState] = useState<ListState>(empty("loading"));
  const current = useRef(state);
  current.current = state;
  const seq = useRef(0); // each reset or new load gets a number; an old answer is dropped
  const busy = useRef(false);
  const pathRef = useRef(pathOf);
  pathRef.current = pathOf;

  const fromNetworkError = useCallback(
    (e: unknown): ListState | null => {
      if (!(e instanceof ApiError) || e.status !== 0) return null;
      const saved = readCache<SavedList>(userId, cacheName);
      if (!saved) return { ...empty("offline"), noSaved: true };
      return { ...empty("offline"), ...saved, status: "offline" };
    },
    [userId, cacheName],
  );

  const load = useCallback(
    async (upTo: number, quiet: boolean) => {
      if (busy.current) return;
      busy.current = true;
      const my = ++seq.current;
      if (!quiet) setState(empty("loading"));
      try {
        let items: ListResponse["items"] = [];
        let pages = 1;
        let page = 0;
        let rendered = "";
        for (let p = 0; p <= upTo; p++) {
          const r = await api<ListResponse>("GET", pathRef.current(p));
          if (seq.current !== my) return;
          items = items.concat(r.items ?? []);
          pages = r.pages || 1;
          page = p;
          rendered = r.rendered_at;
          if (p + 1 >= pages) break;
        }
        const saved: SavedList = { items, page, pages, rendered_at: rendered };
        writeCache(userId, cacheName, saved);
        setState({ ...empty("ok"), ...saved });
      } catch (e) {
        if (seq.current !== my) return;
        const off = fromNetworkError(e);
        if (off) setState(off);
        else setState({ ...current.current, status: "error", message: e instanceof Error ? e.message : t("web.tabs.error"), moreBusy: false });
      } finally {
        if (seq.current === my) busy.current = false;
      }
    },
    [userId, cacheName, fromNetworkError],
  );

  // A new list (the tab or the group changed): start again.
  useEffect(() => {
    void load(0, false);
    return () => {
      seq.current++; // drop the answers that are still on the way
      busy.current = false;
    };
  }, [load]);

  // A new tick: load again without the spinner.
  const lastTick = useRef(tick);
  useEffect(() => {
    if (lastTick.current === tick) return;
    lastTick.current = tick;
    void load(current.current.page, true);
  }, [tick, load]);

  const more = useCallback(async () => {
    const s = current.current;
    if (busy.current || s.page + 1 >= s.pages) return;
    busy.current = true;
    const my = ++seq.current;
    setState({ ...s, moreBusy: true });
    try {
      const r = await api<ListResponse>("GET", pathRef.current(s.page + 1));
      if (seq.current !== my) return;
      const saved: SavedList = {
        items: s.items.concat(r.items ?? []),
        page: s.page + 1,
        pages: r.pages || s.pages,
        rendered_at: r.rendered_at,
      };
      writeCache(userId, cacheName, saved);
      setState({ ...empty("ok"), ...saved });
    } catch (e) {
      if (seq.current !== my) return;
      const off = fromNetworkError(e);
      if (off) setState(off);
      else setState({ ...s, status: "error", message: e instanceof Error ? e.message : t("web.tabs.error"), moreBusy: false });
    } finally {
      if (seq.current === my) busy.current = false;
    }
  }, [userId, cacheName, fromNetworkError]);

  const retry = useCallback(() => void load(current.current.page, false), [load]);

  return { state, more, retry };
}

/** The time of the saved list, in the time zone of the user. This is a clock, not a calendar date. */
export function clockOf(iso: string, tz: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const opts: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hourCycle: "h23" };
  let s: string;
  try {
    s = new Intl.DateTimeFormat("en-US", { ...opts, timeZone: tz || "UTC" }).format(d);
  } catch {
    s = new Intl.DateTimeFormat("en-US", { ...opts, timeZone: "UTC" }).format(d);
  }
  return num(s);
}

export function ItemList(props: {
  user: User;
  cacheName: string;
  pathOf: (page: number) => string;
  tick: number;
  emptyKey: string;
  showChat?: boolean;
}) {
  const { state, more, retry } = useList(props.user.id, props.cacheName, props.pathOf, props.tick);
  const offline = state.status === "offline";

  if (state.status === "loading") return <div class="spinner" role="status" aria-busy="true" />;

  return (
    <section class="list">
      {offline ? (
        <p class="banner offline" role="status">
          {state.noSaved ? t("web.tabs.offline_none") : t("web.tabs.offline", clockOf(state.rendered_at, props.user.tz))}
        </p>
      ) : null}
      {state.status === "error" ? (
        <div class="banner error" role="alert">
          <span>{state.message || t("web.tabs.error")}</span>{" "}
          <button type="button" class="link" onClick={retry}>
            {t("web.tabs.retry")}
          </button>
        </div>
      ) : null}
      {state.items.length === 0 && state.status !== "error" && !state.noSaved ? (
        <p class="empty">{t(props.emptyKey)}</p>
      ) : (
        <ul class="rows">
          {state.items.map((it) => (
            <Row key={`${it.chat_id}:${it.id}`} item={it} offline={offline} showChat={props.showChat === true} />
          ))}
        </ul>
      )}
      {!offline && state.page + 1 < state.pages ? (
        <button type="button" class="more" disabled={state.moreBusy} onClick={() => void more()}>
          {t("web.tabs.more")}
        </button>
      ) : null}
    </section>
  );
}

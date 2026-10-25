import { useEffect, useRef, useState } from "preact/hooks";
import { api, ApiError } from "../api";
import { readCache, writeCache } from "../cache";
import { t } from "../i18n";
import type { User } from "../types";
import { ItemList } from "./list";
import type { Chat, ChatsResponse } from "./model";

// The last group is a convenience of the user. It lives in the same store as
// the offline lists, so that logout deletes it with them.
const PREF = "pref.group";

type Phase = "loading" | "ok" | "error";

export function Groups(props: { user: User; tick: number }) {
  const uid = props.user.id;
  const [phase, setPhase] = useState<Phase>("loading");
  const [groups, setGroups] = useState<Chat[]>([]);
  const [partial, setPartial] = useState(false);
  const [message, setMessage] = useState("");
  const [picked, setPicked] = useState<number | null>(() => readCache<number>(uid, PREF));
  const seq = useRef(0);

  const loadChats = (quiet: boolean) => {
    const my = ++seq.current;
    if (!quiet) setPhase("loading");
    const only = (r: ChatsResponse) => (r.chats ?? []).filter((c) => c.kind !== "private" && c.id !== uid);
    api<ChatsResponse>("GET", "/api/chats").then(
      (r) => {
        if (seq.current !== my) return;
        writeCache(uid, "chats", r);
        setGroups(only(r));
        setPartial(r.partial === true);
        setPhase("ok");
      },
      (e: unknown) => {
        if (seq.current !== my) return;
        const saved = e instanceof ApiError && e.status === 0 ? readCache<ChatsResponse>(uid, "chats") : null;
        if (saved) {
          setGroups(only(saved));
          setPartial(false);
          setPhase("ok");
        } else {
          setMessage(e instanceof Error ? e.message : t("web.tabs.error"));
          setPhase("error");
        }
      },
    );
  };

  useEffect(() => {
    loadChats(false);
    return () => {
      seq.current++;
    };
  }, [uid]);

  const lastTick = useRef(props.tick);
  useEffect(() => {
    if (lastTick.current === props.tick) return;
    lastTick.current = props.tick;
    loadChats(true);
  }, [props.tick]);

  if (phase === "loading") return <div class="spinner" role="status" aria-busy="true" />;
  if (phase === "error") {
    return (
      <div class="banner error" role="alert">
        <span>{message || t("web.tabs.error")}</span>{" "}
        <button type="button" class="link" onClick={() => loadChats(false)}>
          {t("web.tabs.retry")}
        </button>
      </div>
    );
  }
  if (groups.length === 0) return <p class="empty">{t("web.tabs.empty.groups")}</p>;

  // A stored group that is gone from the list falls back to the first group.
  const current = groups.find((g) => g.id === picked) ?? groups[0];
  const choose = (id: number) => {
    setPicked(id);
    writeCache(uid, PREF, id);
  };

  return (
    <div>
      {partial ? (
        <p class="banner" role="status">
          {t("web.tabs.partial")}
        </p>
      ) : null}
      <div class="chips" role="group" aria-label={t("web.tabs.chips")}>
        {groups.map((g) => (
          <button
            key={g.id}
            type="button"
            class={g.id === current.id ? "chip on" : "chip"}
            aria-pressed={g.id === current.id}
            onClick={() => choose(g.id)}
          >
            <bdi>{g.title}</bdi>
          </button>
        ))}
      </div>
      <ItemList
        key={current.id}
        user={props.user}
        cacheName={`group-${current.id}`}
        pathOf={(page) => `/api/chats/${current.id}/items?page=${page}`}
        tick={props.tick}
        emptyKey="web.tabs.empty.group"
      />
    </div>
  );
}

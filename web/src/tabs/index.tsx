// The three tabs (docs/WEB-DESIGN.md sections 7.3, 7.5 and 7.6).
import { useEffect, useState } from "preact/hooks";
import { onUnauthorized } from "../api";
import { clearCache, clearOtherUsers, readCache, writeCache } from "../cache";
import { t } from "../i18n";
import type { User } from "../types";
import { Groups } from "./groups";
import { ItemList } from "./list";
import {
  applyUpdate,
  checkIosHint,
  dismissIosHint,
  ensureTouchIcon,
  promptInstall,
  registerServiceWorker,
  usePwa,
} from "./pwa";
import "./tabs.css";

export type TabId = "mine" | "assigned" | "groups";
const TABS: TabId[] = ["mine", "assigned", "groups"];

/**
 * The app that handles logout dispatches this event on window after
 * POST /api/auth/logout. The tabs then delete the offline lists of the user.
 */
export const LOGOUT_EVENT = "todo:logout";

interface TelegramEvents {
  onEvent?(name: string, fn: () => void): void;
  offEvent?(name: string, fn: () => void): void;
}

const PREF = "pref.tab";

export function Tabs(props: { user: User }) {
  const user = props.user;
  const [tab, setTab] = useState<TabId>(() => {
    const saved = readCache<string>(user.id, PREF);
    return TABS.find((x) => x === saved) ?? "mine";
  });
  const [tick, setTick] = useState(0);
  const pwa = usePwa();

  // A different user than the last one: delete the offline lists of the others.
  // Logout and a 401 delete the lists of this user.
  useEffect(() => {
    clearOtherUsers(user.id);
    const wipe = () => clearCache(user.id);
    window.addEventListener(LOGOUT_EVENT, wipe);
    const off = onUnauthorized(wipe);
    return () => {
      window.removeEventListener(LOGOUT_EVENT, wipe);
      off();
    };
  }, [user.id]);

  // Load the current tab again when the user comes back (section 7.6).
  useEffect(() => {
    const bump = () => setTick((n) => n + 1);
    const onVisible = () => {
      if (document.visibilityState === "visible") bump();
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", bump);
    window.addEventListener("online", bump);
    const tg = (window as unknown as { Telegram?: { WebApp?: TelegramEvents } }).Telegram?.WebApp;
    tg?.onEvent?.("activated", bump);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", bump);
      window.removeEventListener("online", bump);
      tg?.offEvent?.("activated", bump);
    };
  }, []);

  // The PWA at "/" has the service worker. The Mini App at "/tg/" has none.
  useEffect(() => {
    if (location.pathname.startsWith("/tg/")) return;
    ensureTouchIcon();
    checkIosHint();
    if (import.meta.env.PROD) void registerServiceWorker();
  }, []);

  const choose = (id: TabId) => {
    setTab(id);
    writeCache(user.id, PREF, id);
  };

  return (
    <div class="tabs">
      {pwa.updateReady ? (
        <div class="banner update" role="status">
          <span>{t("web.tabs.update")}</span>{" "}
          <button type="button" class="link" onClick={() => applyUpdate()}>
            {t("web.tabs.reload")}
          </button>
        </div>
      ) : null}
      {pwa.iosHint ? (
        <div class="banner" role="status">
          <span>{t("web.tabs.ios_hint")}</span>{" "}
          <button type="button" class="link" onClick={dismissIosHint}>
            {t("web.tabs.close")}
          </button>
        </div>
      ) : null}
      {pwa.canInstall ? (
        <div class="banner">
          <button type="button" class="link" onClick={() => void promptInstall()}>
            {t("web.tabs.install")}
          </button>
        </div>
      ) : null}
      <div class="tablist" role="tablist" aria-label={t("web.tabs.label")}>
        {TABS.map((id) => (
          <button
            key={id}
            type="button"
            role="tab"
            id={`tab-${id}`}
            aria-selected={tab === id}
            aria-controls="tabpanel"
            class={tab === id ? "tab on" : "tab"}
            onClick={() => choose(id)}
          >
            {t(`web.tabs.${id}`)}
          </button>
        ))}
      </div>
      <div id="tabpanel" role="tabpanel" aria-labelledby={`tab-${tab}`} class="panel">
        {tab === "mine" ? (
          <ItemList
            key="mine"
            user={user}
            cacheName="mine"
            pathOf={(page) => `/api/chats/${user.id}/items?page=${page}`}
            tick={tick}
            emptyKey="web.tabs.empty.mine"
          />
        ) : null}
        {tab === "assigned" ? (
          <ItemList
            key="assigned"
            user={user}
            cacheName="assigned"
            pathOf={() => "/api/assigned"}
            tick={tick}
            emptyKey="web.tabs.empty.assigned"
            showChat
          />
        ) : null}
        {tab === "groups" ? <Groups key="groups" user={user} tick={tick} /> : null}
      </div>
    </div>
  );
}

import { useEffect, useState } from "preact/hooks";
import { api, ApiError, onUnauthorized } from "./api";
import { fetchMe, forgetToken, signInMiniApp } from "./auth";
import { clearCache } from "./cache";
import { applyLang, loadCatalog, num, t } from "./i18n";
import type { Me, User } from "./types";
import { Actions, closeSheet, openAdd, openSettings } from "./actions/index";
import { ShownError } from "./actions/sheet";
import { useBackButton } from "./actions/telegram";
import { CodeScreen } from "./code/index";
import { Edit } from "./edit/index";
import { Login } from "./login/index";
import { LOGOUT_EVENT, Tabs } from "./tabs/index";
import { ensureTouchIcon, registerServiceWorker } from "./tabs/pwa";

export type Mode = "miniapp" | "pwa";

type State =
  | { phase: "loading" }
  | { phase: "login" }
  | { phase: "signedout" }
  | { phase: "error"; message: string }
  /** `offline`: /api/me failed on the network, and the app uses the last stored user. */
  | { phase: "ready"; user: User; start: string; offline: boolean };

// The last /api/me user, for a start without a network (docs/WEB-DESIGN.md
// section 7.5). Logout and a 401 delete it.
const ME_KEY = "todo.me";

function storeUser(u: User): void {
  try {
    localStorage.setItem(ME_KEY, JSON.stringify(u));
  } catch {
    /* full or blocked */
  }
}

function storedUser(): User | null {
  try {
    const raw = localStorage.getItem(ME_KEY);
    const u = raw ? (JSON.parse(raw) as User) : null;
    return u && typeof u.id === "number" ? u : null;
  } catch {
    return null;
  }
}

/** Delete the stored user and the offline lists of that user. */
function forgetUser(): void {
  const u = storedUser();
  if (u) clearCache(u.id);
  try {
    localStorage.removeItem(ME_KEY);
  } catch {
    /* blocked */
  }
}

export function App(props: { mode: Mode; initData?: string; screen?: "signout" }) {
  const [state, setState] = useState<State>({ phase: "loading" });
  const [attempt, setAttempt] = useState(0);
  // A new value loads the lists again: after a write, or a change of settings.
  const [rev, setRev] = useState(0);
  // "Sign in a browser" from the settings menu (Mini App).
  const [codeOpen, setCodeOpen] = useState(false);
  const miniapp = props.mode === "miniapp";

  // The PWA registers its service worker and the iOS icon before sign-in too.
  useEffect(() => {
    if (miniapp) return;
    ensureTouchIcon();
    if (import.meta.env.PROD) void registerServiceWorker();
  }, [miniapp]);

  useEffect(() => {
    let live = true;
    const set = (s: State) => live && setState(s);

    (async () => {
      try {
        let start = "";
        if (miniapp) {
          if (!props.initData) {
            set({ phase: "error", message: t("web.app.open_in_telegram") });
            return;
          }
          start = (await signInMiniApp(props.initData)).start ?? "";
        }
        const me = await fetchMe();
        applyLang(me.user.lang, me.user.dir);
        await loadCatalog(me.user.lang);
        if (!miniapp) storeUser(me.user);
        set({ phase: "ready", user: me.user, start, offline: false });
      } catch (e) {
        const saved = !miniapp && e instanceof ApiError && e.status === 0 ? storedUser() : null;
        if (saved) {
          // Offline cold start: show the saved lists of the last user.
          applyLang(saved.lang, saved.dir);
          await loadCatalog(saved.lang);
          set({ phase: "ready", user: saved, start: "", offline: true });
        } else if (!(e instanceof ApiError)) {
          set({ phase: "error", message: t("web.app.error") });
        } else if (e.status === 403 && e.code === "not_allowed") {
          set({ phase: "error", message: t("web.app.notallowed", num(String(e.data.user_id ?? ""))) });
        } else if (e.status === 401 && !miniapp) {
          forgetUser();
          set({ phase: "login" });
        } else {
          set({ phase: "error", message: e.status === 0 ? t("web.app.error") : e.message });
        }
      }
    })();

    return () => {
      live = false;
    };
  }, [props.mode, props.initData, attempt]);

  // Back online after an offline start: start again with the network.
  const offline = state.phase === "ready" && state.offline;
  useEffect(() => {
    if (!offline) return;
    const again = () => setAttempt((n) => n + 1);
    window.addEventListener("online", again);
    return () => window.removeEventListener("online", again);
  }, [offline]);

  // A 401 after the start (for example a revoked session) goes back to sign-in.
  useEffect(
    () =>
      onUnauthorized(() => {
        forgetToken();
        if (!miniapp) forgetUser();
        setState((s) => (s.phase === "ready" && !miniapp ? { phase: "login" } : s));
      }),
    [miniapp],
  );

  // The code screen comes from Settings (codeOpen) or from a start_param=code
  // launch (start). Back leaves both.
  const showCode = codeOpen || (state.phase === "ready" && state.start === "code");
  useBackButton(miniapp && showCode && !props.screen, () => {
    setCodeOpen(false);
    setState((s) => (s.phase === "ready" ? { ...s, start: "" } : s));
  });

  // The settings menu: apply the answer of PUT /api/me/settings at once.
  const saved = async (me: Me) => {
    applyLang(me.user.lang, me.user.dir);
    await loadCatalog(me.user.lang);
    if (!miniapp) storeUser(me.user);
    setState((s) => (s.phase === "ready" ? { ...s, user: me.user } : s));
  };

  const logout = async (everywhere: boolean) => {
    const user = state.phase === "ready" ? state.user : null;
    try {
      await api("POST", everywhere ? "/api/auth/logout-all" : "/api/auth/logout");
    } catch (e) {
      // A 401 means that the session has already ended.
      // Offline: say so, and keep the session. The user can try again later.
      if (e instanceof ApiError && e.status === 0) throw new ShownError(t("web.actions.logout_offline"));
      if (!(e instanceof ApiError) || e.status !== 401) throw e;
    }
    if (user) clearCache(user.id);
    forgetUser();
    window.dispatchEvent(new Event(LOGOUT_EVENT));
    forgetToken();
    closeSheet();
    setCodeOpen(false);
    setState(miniapp ? { phase: "signedout" } : { phase: "login" });
  };

  switch (state.phase) {
    case "loading":
      return <div class="spinner" role="status" aria-busy="true" />;
    case "login":
      return <Login onSignedIn={() => setAttempt((n) => n + 1)} />;
    case "signedout":
      return (
        <main class="screen">
          <p role="status">{t("web.actions.logged_out")}</p>
        </main>
      );
    case "error":
      return (
        <main class="screen">
          <p role="alert" class="error">
            {state.message}
          </p>
        </main>
      );
    case "ready": {
      const u = state.user;
      const screen = props.screen ?? (showCode ? "code" : undefined);
      return (
        <div class="app">
          <header class="bar">
            <span class="who"><bdi dir="ltr">{u.username ? `@${u.username}` : num(u.id)}</bdi></span>
            {screen ? null : (
              <span class="bar-buttons">
                {state.offline ? null : (
                  <button type="button" class="btn add" onClick={openAdd}>
                    <span aria-hidden="true">+ </span>
                    {t("web.actions.add")}
                  </button>
                )}
                <button
                  type="button"
                  class="icon-btn"
                  aria-label={t("web.actions.settings")}
                  title={t("web.actions.settings")}
                  onClick={openSettings}
                >
                  <span aria-hidden="true">⚙</span>
                </button>
              </span>
            )}
          </header>
          {screen ? (
            <CodeScreen screen={screen} />
          ) : (
            <Tabs key={`${u.lang}.${u.calendar}.${rev}`} user={u} />
          )}
          <Actions
            user={u}
            miniapp={miniapp}
            onChanged={() => setRev((n) => n + 1)}
            onSaved={saved}
            onSignInBrowser={() => setCodeOpen(true)}
            onLogout={logout}
          />
          <Edit />
        </div>
      );
    }
  }
}

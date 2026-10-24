import { useEffect, useState } from "preact/hooks";
import { ApiError, onUnauthorized } from "./api";
import { fetchMe, forgetToken, signInMiniApp } from "./auth";
import { applyLang, loadCatalog, num, t } from "./i18n";
import type { User } from "./types";
import { Actions } from "./actions/index";
import { CodeScreen } from "./code/index";
import { Edit } from "./edit/index";
import { Login } from "./login/index";
import { Tabs } from "./tabs/index";

export type Mode = "miniapp" | "pwa";

type State =
  | { phase: "loading" }
  | { phase: "login" }
  | { phase: "error"; message: string }
  | { phase: "ready"; user: User; start: string };

export function App(props: { mode: Mode; initData?: string; screen?: "signout" }) {
  const [state, setState] = useState<State>({ phase: "loading" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    const set = (s: State) => live && setState(s);

    (async () => {
      try {
        let start = "";
        if (props.mode === "miniapp") {
          if (!props.initData) {
            set({ phase: "error", message: t("web.app.open_in_telegram") });
            return;
          }
          start = (await signInMiniApp(props.initData)).start ?? "";
        }
        const me = await fetchMe();
        applyLang(me.user.lang, me.user.dir);
        await loadCatalog(me.user.lang);
        set({ phase: "ready", user: me.user, start });
      } catch (e) {
        if (!(e instanceof ApiError)) {
          set({ phase: "error", message: t("web.app.error") });
        } else if (e.status === 403 && e.code === "not_allowed") {
          set({ phase: "error", message: t("web.app.notallowed", num(String(e.data.user_id ?? ""))) });
        } else if (e.status === 401 && props.mode === "pwa") {
          set({ phase: "login" });
        } else {
          set({ phase: "error", message: e.message });
        }
      }
    })();

    return () => {
      live = false;
    };
  }, [props.mode, props.initData, attempt]);

  // A 401 after the start (for example a revoked session) goes back to sign-in.
  useEffect(
    () =>
      onUnauthorized(() => {
        forgetToken();
        setState((s) => (s.phase === "ready" && props.mode === "pwa" ? { phase: "login" } : s));
      }),
    [props.mode],
  );

  switch (state.phase) {
    case "loading":
      return <div class="spinner" role="status" aria-busy="true" />;
    case "login":
      return <Login onSignedIn={() => setAttempt((n) => n + 1)} />;
    case "error":
      return (
        <main class="screen">
          <p role="alert" class="error">
            {state.message}
          </p>
        </main>
      );
    case "ready": {
      const screen = props.screen ?? (state.start === "code" ? "code" : undefined);
      return (
        <div class="app">
          <header class="bar">
            <span class="who">{state.user.username ? `@${state.user.username}` : num(state.user.id)}</span>
          </header>
          {screen ? <CodeScreen screen={screen} /> : <Tabs user={state.user} />}
          <Actions />
          <Edit />
        </div>
      );
    }
  }
}

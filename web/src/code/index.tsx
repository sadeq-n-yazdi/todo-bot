import { useEffect, useRef, useState } from "preact/hooks";
import { ApiError, api } from "../api";
import { forgetToken } from "../auth";
import { num } from "../i18n";
import "./code.css";
import { tr } from "./text";

interface NewCode {
  code: string;
  expires_at: string;
}

type CodeState =
  | { phase: "loading" }
  | { phase: "shown"; code: string; expires: number }
  | { phase: "expired" }
  | { phase: "canceled" }
  | { phase: "error"; message: string };

function initData(): string {
  return window.Telegram?.WebApp?.initData ?? "";
}

/** mm:ss in the digits of the language. */
function clock(ms: number): string {
  const s = Math.max(0, Math.ceil(ms / 1000));
  return num(`${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`);
}

function message(e: unknown): string {
  return e instanceof ApiError ? e.message : tr("web.app.error");
}

/** A ref that is false after the screen is gone, so that late requests set no state. */
function useMounted() {
  const m = useRef(true);
  useEffect(() => {
    m.current = true;
    return () => void (m.current = false);
  }, []);
  return m;
}

/** Mini App screens: the login code (Settings or start_param=code), and the sign-out of the sign-in notice. */
export function CodeScreen(props: { screen: "code" | "signout" }) {
  return props.screen === "signout" ? <SignOut /> : <ShowCode />;
}

function ShowCode() {
  const [state, setState] = useState<CodeState>({ phase: "loading" });
  const [now, setNow] = useState(() => Date.now());
  const [round, setRound] = useState(0);

  // Each round asks the server for a new code. The request carries the initData
  // of the Mini App, and the server checks that it is at most 10 minutes old.
  useEffect(() => {
    let live = true;
    setState({ phase: "loading" });
    api<NewCode>("POST", "/api/auth/code", { init_data: initData() }).then(
      (r) => live && setState({ phase: "shown", code: r.code, expires: Date.parse(r.expires_at) }),
      (e) => live && setState({ phase: "error", message: message(e) }),
    );
    return () => {
      live = false;
    };
  }, [round]);

  // The clock runs only while a code shows.
  useEffect(() => {
    if (state.phase !== "shown") return;
    const id = setInterval(() => {
      const n = Date.now();
      setNow(n);
      if (n >= state.expires) setState({ phase: "expired" });
    }, 1000);
    return () => clearInterval(id);
  }, [state]);

  const mounted = useMounted();
  async function cancel() {
    try {
      await api("DELETE", "/api/auth/code");
      if (mounted.current) setState({ phase: "canceled" });
    } catch (e) {
      if (mounted.current) setState({ phase: "error", message: message(e) });
    }
  }

  const again = (
    <button class="btn" type="button" onClick={() => setRound((n) => n + 1)}>
      {tr("web.login.code_new")}
    </button>
  );
  const [before, after = ""] = tr("web.login.code_left", "\u0000").split("\u0000");
  return (
    <main class="screen codescreen" data-testid="code-screen">
      <h1>{tr("web.login.code_title")}</h1>
      {state.phase === "loading" && <div class="spinner" role="status" aria-busy="true" />}
      {state.phase === "shown" && (
        <>
          <p class="bigcode">
            <bdi dir="ltr">{state.code}</bdi>
          </p>
          <p data-testid="left">
            {before}
            <bdi dir="ltr">{clock(state.expires - now)}</bdi>
            {after}
          </p>
          <p class="warning">{tr("web.login.code_warning")}</p>
          <div class="row">
            <button class="btn plain" type="button" onClick={cancel}>
              {tr("web.login.code_cancel")}
            </button>
          </div>
        </>
      )}
      {state.phase === "expired" && (
        <>
          <p>{tr("web.login.code_expired")}</p>
          <div class="row">{again}</div>
        </>
      )}
      {state.phase === "canceled" && (
        <>
          <p>{tr("web.login.code_canceled")}</p>
          <div class="row">{again}</div>
        </>
      )}
      {state.phase === "error" && (
        <>
          <p role="alert" class="error">
            {state.message}
          </p>
          <div class="row">{again}</div>
        </>
      )}
    </main>
  );
}

function SignOut() {
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  const mounted = useMounted();
  async function confirm() {
    setBusy(true);
    setError("");
    try {
      await api("POST", "/api/auth/logout-all");
      forgetToken(); // the session of this Mini App is gone too
      if (mounted.current) setDone(true);
    } catch (e) {
      if (mounted.current) {
        setError(message(e));
        setBusy(false);
      }
    }
  }

  return (
    <main class="screen codescreen" data-testid="signout-screen">
      <h1>{tr("web.login.signout_title")}</h1>
      {done ? (
        <p role="status">{tr("web.login.signout_done")}</p>
      ) : (
        <>
          <p>{tr("web.login.signout_text")}</p>
          {error && (
            <p role="alert" class="error">
              {error}
            </p>
          )}
          <div class="row">
            <button class="btn" type="button" disabled={busy} onClick={confirm}>
              {tr("web.login.signout_ok")}
            </button>
          </div>
        </>
      )}
    </main>
  );
}

import { useEffect, useRef, useState } from "preact/hooks";
import { ApiError, api } from "../api";
import "../code/code.css";
import { tr } from "../code/text";
import { getLang, loadCatalog } from "../i18n";

// The bot name is known only after a sign-in, so the link to the Mini App
// needs it from the page: <meta name="todo-bot" content="bot_username">.
function botName(): string {
  const m = document.querySelector('meta[name="todo-bot"]');
  const v = m?.getAttribute("content") ?? "";
  return /^[A-Za-z0-9_]{3,64}$/.test(v) ? v : "";
}

/** The login screen of the PWA: the steps, and the field for the code from the Mini App. */
export function Login(props: { onSignedIn: () => void }) {
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [, setReady] = useState(0);

  // The catalog is public. Load it, so that the screen has its text before a session exists.
  useEffect(() => {
    let live = true;
    const l = getLang().slice(0, 2);
    void loadCatalog(l === "fa" ? "fa" : "en").then(() => live && setReady((n) => n + 1));
    return () => {
      live = false;
    };
  }, []);

  // A request that ends after the screen is gone must not set state.
  const mounted = useRef(true);
  useEffect(() => () => void (mounted.current = false), []);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (busy || code.trim() === "") return;
    setBusy(true);
    setError("");
    try {
      // The code goes only in the JSON body, never in a URL.
      await api("POST", "/api/auth/code/redeem", { code });
      if (!mounted.current) return;
      setCode("");
      props.onSignedIn();
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof ApiError ? err.message : tr("web.app.error"));
      setBusy(false);
    }
  }

  const bot = botName();
  return (
    <main data-testid="login" class="screen login">
      <h1>{tr("web.login.title")}</h1>
      <p class="steps">{tr("web.login.steps")}</p>
      {bot && (
        <p>
          <a class="btn" href={`https://t.me/${bot}?startapp=code`}>
            {tr("web.login.open")}
          </a>
        </p>
      )}
      <form onSubmit={submit}>
        <label for="login-code">{tr("web.login.code_label")}</label>
        <input
          id="login-code"
          class="codefield"
          type="text"
          dir="ltr"
          inputMode="text"
          autocomplete="off"
          autocapitalize="characters"
          autocorrect="off"
          spellcheck={false}
          maxLength={32}
          value={code}
          onInput={(e) => setCode((e.target as HTMLInputElement).value)}
        />
        {error && (
          <p role="alert" class="error">
            {error}
          </p>
        )}
        <button class="btn" type="submit" disabled={busy || code.trim() === ""}>
          {busy ? tr("web.login.busy") : tr("web.login.submit")}
        </button>
      </form>
    </main>
  );
}

// The Telegram Login Widget on the login screen of the PWA (docs/WEB-DESIGN.md
// section 3.4). It uses the redirect mode: Telegram sends the browser to
// GET /auth/telegram, and the server answers 303 to "/" with the session
// cookie, or to "/#login-error=<code>".

import { useEffect, useRef } from "preact/hooks";
import { getLang, t } from "../i18n";

/** The widget script. Section 11.2 allows https://telegram.org in script-src on "/" only. */
export const WIDGET_SRC = "https://telegram.org/js/telegram-widget.js?22";

// The codes of /#login-error=<code> (internal/web/auth_widget.go).
const CODES = ["bad_request", "bad_signature", "expired", "not_allowed", "used", "internal"];

// Used until the catalog arrives, or when it has no such key.
const english: Record<string, string> = {
  "web.widget.hint": "On a computer, you can also sign in with your Telegram account:",
  "web.widget.error_bad_request": "Telegram sent sign-in data that is not correct. Try again.",
  "web.widget.error_bad_signature": "Telegram did not confirm this sign-in. Try again.",
  "web.widget.error_expired": "The sign-in took too long. Try again.",
  "web.widget.error_not_allowed": "This Telegram account cannot use this app. Ask the owner to add it.",
  "web.widget.error_used": "This sign-in was already used. Sign in again.",
  "web.widget.error_internal": "Something went wrong with the sign-in. Try again.",
};

function wt(key: string): string {
  const got = t(key);
  return got !== key ? got : (english[key] ?? key);
}

/**
 * The bot name. It is known only after a sign-in, so the login screen needs
 * it from the page: <meta name="todo-bot" content="bot_username">.
 */
export function botName(): string {
  const m = document.querySelector('meta[name="todo-bot"]');
  const v = m?.getAttribute("content") ?? "";
  return /^[A-Za-z0-9_]{3,64}$/.test(v) ? v : "";
}

/**
 * An installed app on iOS (navigator.standalone). It has its own storage, and
 * the sign-in of Telegram does not come back to it, so it gets no widget.
 */
export function installedIOS(): boolean {
  return (navigator as Navigator & { standalone?: boolean }).standalone === true;
}

/**
 * Read the error of a widget sign-in from the URL fragment, and remove the
 * fragment. It returns one of the known codes ("internal" for any other
 * value), or "" when there is no error.
 */
export function takeLoginError(): string {
  const m = /^#login-error=(.*)$/.exec(location.hash);
  if (!m) return "";
  history.replaceState(history.state, "", location.pathname + location.search);
  return CODES.includes(m[1]) ? m[1] : "internal";
}

/** The text of a code of takeLoginError. It is always a fixed string, never text from the URL. */
export function loginErrorText(code: string): string {
  return wt("web.widget.error_" + (CODES.includes(code) ? code : "internal"));
}

/** The widget button of Telegram, in the redirect mode. */
export function Widget(props: { bot: string }) {
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const s = document.createElement("script");
    s.async = true;
    s.src = WIDGET_SRC;
    s.setAttribute("data-telegram-login", props.bot);
    s.setAttribute("data-size", "large");
    s.setAttribute("data-auth-url", location.origin + "/auth/telegram");
    s.setAttribute("data-request-access", "write");
    s.setAttribute("data-lang", getLang().startsWith("fa") ? "fa" : "en");
    el.append(s);
    // The script puts its iframe next to itself. Remove both.
    return () => el.replaceChildren();
  }, [props.bot]);

  return (
    <section class="widget">
      <p>{wt("web.widget.hint")}</p>
      <div ref={box} data-testid="widget" />
    </section>
  );
}

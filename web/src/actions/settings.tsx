import { useState } from "preact/hooks";
import { api } from "../api";
import { t } from "../i18n";
import type { Me, User } from "../types";
import { errText, Sheet, useLive } from "./sheet";

// Each language has its own name in every language.
const LANGS: { id: string; name: string }[] = [
  { id: "en", name: "English" },
  { id: "fa", name: "فارسی" },
];
const CALENDARS: { id: string; key: string }[] = [
  { id: "gregorian", key: "web.actions.cal_gregorian" },
  { id: "jalali", key: "web.actions.cal_jalali" },
];

/**
 * The settings menu (docs/WEB-DESIGN.md section 7.3): the language, the
 * calendar, "Sign in a browser" (Mini App only), Log out and Log out
 * everywhere. A change goes to PUT /api/me/settings, and `onSaved` applies
 * the answer at once.
 */
export function SettingsSheet(props: {
  user: User;
  miniapp: boolean;
  onClose: () => void;
  onSaved: (me: Me) => Promise<void> | void;
  onSignInBrowser: () => void;
  onLogout: (everywhere: boolean) => Promise<void>;
}) {
  const live = useLive();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function work(fn: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      if (live.current) setError(errText(e));
    } finally {
      if (live.current) setBusy(false);
    }
  }

  const save = (patch: { lang?: string; calendar?: string }) =>
    work(async () => {
      const me = await api<Me>("PUT", "/api/me/settings", patch);
      await props.onSaved(me);
    });

  const u = props.user;
  return (
    <Sheet title={t("web.actions.settings")} miniapp={props.miniapp} onBack={props.onClose} testId="settings-sheet">
      <fieldset class="choice" disabled={busy}>
        <legend>{t("web.actions.language")}</legend>
        {LANGS.map((l) => (
          <button
            key={l.id}
            type="button"
            class={u.lang === l.id ? "chip on" : "chip"}
            lang={l.id}
            aria-pressed={u.lang === l.id}
            onClick={() => u.lang !== l.id && void save({ lang: l.id })}
          >
            {l.name}
          </button>
        ))}
      </fieldset>

      <fieldset class="choice" disabled={busy}>
        <legend>{t("web.actions.calendar")}</legend>
        {CALENDARS.map((c) => (
          <button
            key={c.id}
            type="button"
            class={u.calendar === c.id ? "chip on" : "chip"}
            aria-pressed={u.calendar === c.id}
            onClick={() => u.calendar !== c.id && void save({ calendar: c.id })}
          >
            {t(c.key)}
          </button>
        ))}
      </fieldset>

      {error ? (
        <p role="alert" class="error">
          {error}
        </p>
      ) : null}

      <div class="sheet-menu">
        {props.miniapp ? (
          <button type="button" class="btn plain" disabled={busy} onClick={props.onSignInBrowser}>
            {t("web.actions.signin_browser")}
          </button>
        ) : null}
        <button type="button" class="btn plain" disabled={busy} onClick={() => void work(() => props.onLogout(false))}>
          {t("web.actions.logout")}
        </button>
        <button type="button" class="btn danger" disabled={busy} onClick={() => void work(() => props.onLogout(true))}>
          {t("web.actions.logout_all")}
        </button>
      </div>
    </Sheet>
  );
}

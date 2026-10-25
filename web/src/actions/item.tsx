import { useEffect, useState } from "preact/hooks";
import { api } from "../api";
import { t } from "../i18n";
import type { Item } from "../tabs/model";
import { errText, Sheet, useLive } from "./sheet";
import { haptic } from "./telegram";

type Step = "main" | "when" | "confirm";

export type Preset = "10m" | "1h" | "tomorrow";
const PRESETS: { preset: Preset; key: string }[] = [
  { preset: "10m", key: "web.actions.remind_10m" },
  { preset: "1h", key: "web.actions.remind_1h" },
  { preset: "tomorrow", key: "web.actions.remind_tomorrow" },
];

/**
 * The item sheet: Done, Remind later (10 min, 1 h, tomorrow, another time)
 * and Delete with a confirmation (docs/WEB-DESIGN.md sections 5.3 and 7.3).
 * `chat` is the chat of the actor. With `run="done"` (the check mark of a
 * row) the sheet marks the item as done at once.
 */
export function ItemSheet(props: {
  item: Item;
  chat: number;
  miniapp: boolean;
  run?: "done";
  onClose: () => void;
  /** A change was saved. `note` is the text for the toast. */
  onChanged: (note: string) => void;
}) {
  const it = props.item;
  const base = `/api/chats/${props.chat}/items/${it.id}`;
  const live = useLive();
  const [step, setStep] = useState<Step>("main");
  const [when, setWhen] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function run(fn: () => Promise<string>) {
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const note = await fn();
      if (live.current) props.onChanged(note);
    } catch (e) {
      if (live.current) {
        setError(errText(e));
        setBusy(false);
      }
      haptic("error");
    }
  }

  const done = () =>
    run(async () => {
      const r = await api<{ item: Item; still_open: boolean }>("POST", `${base}/done`, {});
      haptic("success");
      return r.still_open && r.item?.due_text ? t("web.actions.next", r.item.due_text) : t("web.actions.done_ok");
    });

  const remind = (body: { preset: Preset } | { when: string }) =>
    run(async () => {
      const r = await api<{ fire_at: string; fire_text: string }>("POST", `${base}/remind`, body);
      return t("web.actions.reminded", r.fire_text);
    });

  const del = () =>
    run(async () => {
      await api("DELETE", base);
      return t("web.actions.deleted");
    });

  useEffect(() => {
    if (props.run === "done") void done();
  }, []);

  // Back goes one step back, and from the first step it closes the sheet.
  const back = () => {
    if (step !== "main" && !busy) {
      setStep("main");
      setError("");
    } else props.onClose();
  };

  const due = it.due_text;
  return (
    <Sheet title={t("web.actions.open")} miniapp={props.miniapp} onBack={back} testId="item-sheet">
      <div class="sheet-item">
        <p class="sheet-item-text" dir="auto">
          {it.text}
        </p>
        {due ? <p class={it.overdue ? "due overdue" : "due"}>{due}</p> : null}
        {it.recur_text ? (
          <p class="recur">
            <span aria-hidden="true">🔁 </span>
            <bdi>{it.recur_text}</bdi>
          </p>
        ) : null}
      </div>

      {error ? (
        <p role="alert" class="error">
          {error}
        </p>
      ) : null}

      {step === "main" ? (
        <div class="sheet-actions">
          <button type="button" class="btn" disabled={busy} onClick={() => void done()}>
            {t("web.actions.done")}
          </button>
          <fieldset class="presets" disabled={busy}>
            <legend>{t("web.actions.remind")}</legend>
            {PRESETS.map((p) => (
              <button key={p.preset} type="button" class="btn plain" onClick={() => void remind({ preset: p.preset })}>
                {t(p.key)}
              </button>
            ))}
            <button type="button" class="btn plain" onClick={() => setStep("when")}>
              {t("web.actions.remind_custom")}
            </button>
          </fieldset>
          <button type="button" class="btn danger" disabled={busy} onClick={() => setStep("confirm")}>
            {t("web.actions.delete")}
          </button>
        </div>
      ) : null}

      {step === "when" ? (
        <form
          class="sheet-form"
          onSubmit={(e) => {
            e.preventDefault();
            if (when.trim() !== "") void remind({ when });
          }}
        >
          <label class="field">
            <span>{t("web.actions.remind_when")}</span>
            <input
              name="when"
              type="text"
              dir="auto"
              autoComplete="off"
              placeholder={t("web.actions.remind_when_hint")}
              value={when}
              onInput={(e) => setWhen((e.target as HTMLInputElement).value)}
            />
          </label>
          <div class="sheet-buttons">
            <button type="submit" class="btn" disabled={busy || when.trim() === ""}>
              {t("web.actions.remind_set")}
            </button>
            <button type="button" class="btn plain" disabled={busy} onClick={back}>
              {t("web.actions.cancel")}
            </button>
          </div>
        </form>
      ) : null}

      {step === "confirm" ? (
        <div class="sheet-confirm" role="group" aria-label={t("web.actions.delete")}>
          <p>{t("web.actions.delete_ask")}</p>
          <div class="sheet-buttons">
            <button type="button" class="btn danger" disabled={busy} onClick={() => void del()}>
              {t("web.actions.delete_yes")}
            </button>
            <button type="button" class="btn plain" disabled={busy} onClick={back}>
              {t("web.actions.cancel")}
            </button>
          </div>
        </div>
      ) : null}
    </Sheet>
  );
}

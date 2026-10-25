// The edit form (docs/WEB-DESIGN.md sections 5.3 and 7.3, endpoints 22 and 23).
// The item sheet opens it through the store of this folder.
import { useEffect, useRef, useState } from "preact/hooks";
import { ApiError, api } from "../api";
import { errText, Sheet, useLive } from "../actions/sheet";
import { haptic, useMainButton } from "../actions/telegram";
import { t } from "../i18n";
import { listsChanged } from "../live";
import type { Item } from "../tabs/model";
import { closeEdit, type EditTarget, getEdit, useEditTarget } from "./store";

export { openEdit } from "./store";

/** Endpoint 22: the item, with its status and the date as text for the form. */
export interface EditItem extends Item {
  status: string;
  /** The date and the repeat as text that the parser reads again. "" or null when the item has no date. */
  when_text: string | null;
  /** Only in the answer of PATCH: a plain text for the editor, for example web.edit.nodm. */
  note?: string;
}

interface Member {
  id: number;
  username: string;
}

/** The body of PATCH (endpoint 23). Only the changed fields are in it. */
export interface Patch {
  text?: string;
  when?: string | null;
  assignee_id?: number;
}

export interface Form {
  text: string;
  when: string;
  assignee: number;
  /** "No date" is on: remove the date, whatever the date field has. */
  noDate?: boolean;
}

const formOf = (it: EditItem): Form => ({ text: it.text, when: it.when_text ?? "", assignee: it.assignee?.id ?? 0 });

const hasDate = (it: EditItem) => it.due_at !== null || (it.when_text ?? "").trim() !== "";

/**
 * The fields that differ from the loaded item. "No date", or an empty date
 * field when the item had a phrase, removes the date. When when_text is ""
 * (for example "every 3 days"), an empty field keeps the schedule.
 */
export function changes(orig: EditItem, f: Form): Patch {
  const was = formOf(orig);
  const p: Patch = {};
  if (f.text !== was.text) p.text = f.text;
  const when = f.when.trim();
  if (f.noDate) {
    if (hasDate(orig)) p.when = null;
  } else if (when !== was.when.trim()) {
    if (when !== "") p.when = when;
    else if (was.when.trim() !== "" || orig.due_at !== null) p.when = null;
  }
  if (f.assignee !== was.assignee) p.assignee_id = f.assignee;
  return p;
}

type Load = { kind: "loading" } | { kind: "ok"; item: EditItem } | { kind: "error"; message: string };

function EditSheet(props: { target: NonNullable<EditTarget>; onSaved: (msg: string, reload: boolean) => void }) {
  const { item: row, chat, miniapp } = props.target;
  const base = `/api/chats/${chat}/items/${row.id}`;
  const live = useLive();
  const [load, setLoad] = useState<Load>({ kind: "loading" });
  const [form, setForm] = useState<Form>({ text: "", when: "", assignee: 0 });
  const [members, setMembers] = useState<Member[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // In "Assigned to me" the actor is the private chat of the viewer, so the
  // members of that chat are not the people of the item: no picker there.
  const canAssign = chat === row.chat_id;

  useEffect(() => {
    api<EditItem>("GET", base).then(
      (it) => {
        if (!live.current) return;
        setLoad({ kind: "ok", item: it });
        setForm(formOf(it));
      },
      (e) => live.current && setLoad({ kind: "error", message: errText(e) }),
    );
    if (canAssign) {
      api<{ members: Member[] }>("GET", `/api/chats/${chat}/members`).then(
        (r) => live.current && setMembers(r.members ?? []),
        () => {
          /* the picker then offers only "Nobody" and the current assignee */
        },
      );
    }
  }, []);

  const orig = load.kind === "ok" ? load.item : null;
  const canSave = orig !== null && form.text.trim() !== "" && !busy;

  const inFlight = useRef(false);
  const save = async () => {
    if (!orig || !canSave || inFlight.current) return;
    const patch = changes(orig, form);
    if (Object.keys(patch).length === 0) {
      props.onSaved(t("web.edit.unchanged"), false);
      return;
    }
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const out = await api<EditItem>("PATCH", base, patch);
      haptic("success");
      props.onSaved(out?.note ? `${t("web.edit.saved")} ${out.note}` : t("web.edit.saved"), true);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) {
        // Done or deleted since the form opened (endpoint 23 gives 404).
        haptic("error");
        props.onSaved(t("web.edit.gone"), true);
        return;
      }
      inFlight.current = false;
      if (live.current) {
        setError(errText(e));
        setBusy(false);
      }
      haptic("error");
    }
  };

  useMainButton(miniapp && orig !== null, t("web.edit.save"), form.text.trim() !== "", busy, () => void save());

  const memberName = (m: Member) => (m.username ? `@${m.username}` : t("web.actions.noname"));
  // The current assignee stays in the picker, also when the member list does not have it.
  const people: Member[] =
    orig?.assignee && !members.some((m) => m.id === orig.assignee?.id) ? [orig.assignee, ...members] : members;
  const set = (k: keyof Form, v: string | number | boolean) => setForm((f) => ({ ...f, [k]: v }));
  // The schedule as read-only text, when the parser has no phrase for it.
  const schedule =
    orig && hasDate(orig) && (orig.when_text ?? "") === ""
      ? [orig.due_full ?? orig.due_text, orig.recur_text ? t("web.edit.repeat", orig.recur_text) : ""]
          .filter(Boolean)
          .join(" · ")
      : "";

  return (
    <Sheet title={t("web.edit.title", orig?.num ?? row.num)} miniapp={miniapp} onBack={closeEdit} testId="edit-sheet">
      {load.kind === "loading" ? <div class="spinner" role="status" aria-busy="true" /> : null}
      {load.kind === "error" ? (
        <p role="alert" class="error">
          {load.message}
        </p>
      ) : null}
      {orig ? (
        <form
          class="sheet-form"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <label class="field">
            <span>{t("web.edit.text")}</span>
            <textarea
              name="text"
              dir="auto"
              rows={3}
              maxLength={1000}
              value={form.text}
              onInput={(e) => set("text", (e.target as HTMLTextAreaElement).value)}
            />
          </label>

          <label class="field">
            <span>{t("web.edit.when")}</span>
            <input
              name="when"
              type="text"
              dir="auto"
              autoComplete="off"
              placeholder={t("web.edit.when_hint")}
              value={form.when}
              disabled={form.noDate}
              onInput={(e) => set("when", (e.target as HTMLInputElement).value)}
            />
          </label>

          {schedule && !form.noDate ? (
            <p class="preview preview-text" dir="auto" data-testid="edit-schedule">
              {schedule}
            </p>
          ) : null}

          {hasDate(orig) ? (
            <div class="presets">
              <button
                type="button"
                class={form.noDate ? "chip on" : "chip"}
                aria-pressed={!!form.noDate}
                onClick={() => set("noDate", !form.noDate)}
              >
                {t("web.edit.no_date")}
              </button>
            </div>
          ) : null}

          {canAssign ? (
            <label class="field">
              <span>{t("web.edit.assignee")}</span>
              <select
                name="assignee"
                value={String(form.assignee)}
                onChange={(e) => set("assignee", Number((e.target as HTMLSelectElement).value))}
              >
                <option value="0">{t("web.actions.nobody")}</option>
                {people.map((m) => (
                  <option key={m.id} value={String(m.id)} dir="auto">
                    {memberName(m)}
                  </option>
                ))}
              </select>
            </label>
          ) : null}

          {error ? (
            <p role="alert" class="error">
              {error}
            </p>
          ) : null}

          {miniapp ? null : (
            <div class="sheet-buttons">
              <button type="submit" class="btn" disabled={!canSave}>
                {busy ? t("web.actions.busy") : t("web.edit.save")}
              </button>
              <button type="button" class="btn plain" disabled={busy} onClick={closeEdit}>
                {t("web.actions.cancel")}
              </button>
            </div>
          )}
        </form>
      ) : null}
    </Sheet>
  );
}

/**
 * The edit form, mounted once in app.tsx. It shows the item that openEdit
 * chose. After a save it closes, shows a toast (with the note of the server,
 * if any) and loads the lists again.
 */
export function Edit() {
  const target = useEditTarget();
  const [toast, setToast] = useState("");

  // A new sign-in must not open the item of the last session.
  useEffect(() => closeEdit, []);

  useEffect(() => {
    if (!toast) return;
    const id = setTimeout(() => setToast(""), 4000);
    return () => clearTimeout(id);
  }, [toast]);

  const live = useLive();
  const saved = (from: EditTarget, msg: string, reload: boolean) => {
    // The user can close the form (or open another one) while the request runs.
    if (from !== null && from === getEdit()) closeEdit();
    if (live.current) setToast(msg);
    if (reload) listsChanged();
  };

  return (
    <>
      {target ? (
        <EditSheet key={`${target.chat}:${target.item.id}`} target={target} onSaved={(msg, reload) => saved(target, msg, reload)}
        />
      ) : null}
      {toast ? (
        <p class="toast" role="status">
          {toast}
        </p>
      ) : null}
    </>
  );
}

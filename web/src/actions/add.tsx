import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "../api";
import { t } from "../i18n";
import type { Chat, ChatsResponse, Item } from "../tabs/model";
import type { User } from "../types";
import { errText, Sheet, useLive } from "./sheet";
import { useMainButton } from "./telegram";

interface Parsed {
  text: string;
  due_at: string | null;
  due_text: string | null;
  recur: string;
  recur_text: string;
}

interface Member {
  id: number;
  username: string;
}

/** The pause after the last key before the preview asks the server (section 7.3). */
let previewDelay = 400;

/** For tests. */
export function setPreviewDelay(ms: number): void {
  previewDelay = ms;
}

type Preview = { kind: "none" } | { kind: "ok"; p: Parsed } | { kind: "error"; message: string };

/**
 * The add sheet: the text, a live preview from POST /api/parse, the list, the
 * assignee picker and Add (docs/WEB-DESIGN.md sections 5.3 and 7.3). In the
 * Mini App, Add is the Telegram MainButton.
 */
export function AddSheet(props: {
  user: User;
  miniapp: boolean;
  /** The chat that the sheet starts with: the private chat or the last group. */
  chat: number;
  onClose: () => void;
  onAdded: (item: Item) => void;
}) {
  const uid = props.user.id;
  const live = useLive();
  const [chats, setChats] = useState<Chat[]>([]);
  const [chat, setChat] = useState(props.chat);
  const [text, setText] = useState("");
  const [preview, setPreview] = useState<Preview>({ kind: "none" });
  const [members, setMembers] = useState<Member[]>([]);
  const [assignee, setAssignee] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // The lists the user can add to. The private chat is first.
  useEffect(() => {
    api<ChatsResponse>("GET", "/api/chats").then(
      (r) => {
        if (!live.current) return;
        const list = r.chats ?? [];
        setChats(list);
        if (!list.some((c) => c.id === props.chat)) setChat(uid);
      },
      () => {
        if (live.current) setChat(uid);
      },
    );
  }, []);

  // The people of the chosen chat, for the assignee picker.
  useEffect(() => {
    let mine = true;
    setMembers([]);
    setAssignee(0);
    api<{ members: Member[] }>("GET", `/api/chats/${chat}/members`).then(
      (r) => mine && live.current && setMembers(r.members ?? []),
      () => {
        /* the picker then offers only "Nobody" */
      },
    );
    return () => {
      mine = false;
    };
  }, [chat]);

  // The live preview: 400 ms after the last key. An old answer is dropped.
  const seq = useRef(0);
  useEffect(() => {
    const my = ++seq.current;
    if (text.trim() === "") {
      setPreview({ kind: "none" });
      return;
    }
    const id = setTimeout(() => {
      api<Parsed>("POST", "/api/parse", { text }).then(
        (p) => live.current && seq.current === my && setPreview({ kind: "ok", p }),
        (e) => live.current && seq.current === my && setPreview({ kind: "error", message: errText(e) }),
      );
    }, previewDelay);
    return () => clearTimeout(id);
  }, [text]);

  const canAdd = text.trim() !== "" && !busy;
  // One request at a time, also for two fast taps before the button turns off.
  const inFlight = useRef(false);
  const add = async () => {
    if (!canAdd || inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError("");
    try {
      const item = await api<Item>("POST", `/api/chats/${chat}/items`, { text, assignee_id: assignee });
      // The parent stays mounted: it must load the lists again even when the
      // sheet closed while the request ran.
      props.onAdded(item);
    } catch (e) {
      inFlight.current = false;
      if (live.current) {
        setError(errText(e));
        setBusy(false);
      }
    }
  };

  useMainButton(props.miniapp, t("web.actions.add"), text.trim() !== "", busy, () => void add());

  const chatName = (c: Chat) => (c.id === uid || c.kind === "private" ? t("web.actions.chat_private") : c.title);
  const memberName = (m: Member) => (m.username ? `@${m.username}` : t("web.actions.noname"));
  const shownChats = chats.length > 0 ? chats : [{ id: uid, kind: "private", title: "" }];

  return (
    <Sheet title={t("web.actions.add_title")} miniapp={props.miniapp} onBack={props.onClose} testId="add-sheet">
      <form
        class="sheet-form"
        onSubmit={(e) => {
          e.preventDefault();
          void add();
        }}
      >
        <label class="field">
          <span>{t("web.actions.text")}</span>
          <textarea
            name="text"
            dir="auto"
            rows={3}
            maxLength={1000}
            placeholder={t("web.actions.text_hint")}
            value={text}
            onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          />
        </label>

        <div class="preview" aria-live="polite" data-testid="preview">
          <span class="preview-label">{t("web.actions.preview")}</span>
          {preview.kind === "ok" ? (
            <>
              <span class="preview-text" dir="auto">
                {preview.p.text}
              </span>
              <span class="preview-due">{preview.p.due_text ?? t("web.actions.preview_none")}</span>
              {preview.p.recur_text ? (
                <span class="recur">
                  <span aria-hidden="true">🔁 </span>
                  <bdi>{preview.p.recur_text}</bdi>
                </span>
              ) : null}
            </>
          ) : null}
          {preview.kind === "error" ? <span class="error">{preview.message}</span> : null}
        </div>

        {shownChats.length > 1 ? (
          <label class="field">
            <span>{t("web.actions.chat")}</span>
            <select name="chat" value={String(chat)} onChange={(e) => setChat(Number((e.target as HTMLSelectElement).value))}>
              {shownChats.map((c) => (
                <option key={c.id} value={String(c.id)} dir="auto">
                  {chatName(c)}
                </option>
              ))}
            </select>
          </label>
        ) : null}

        <label class="field">
          <span>{t("web.actions.assignee")}</span>
          <select
            name="assignee"
            value={String(assignee)}
            onChange={(e) => setAssignee(Number((e.target as HTMLSelectElement).value))}
          >
            <option value="0">{t("web.actions.nobody")}</option>
            {members.map((m) => (
              <option key={m.id} value={String(m.id)} dir="auto">
                {memberName(m)}
              </option>
            ))}
          </select>
        </label>

        {error ? (
          <p role="alert" class="error">
            {error}
          </p>
        ) : null}

        {props.miniapp ? null : (
          <div class="sheet-buttons">
            <button type="submit" class="btn" disabled={!canAdd}>
              {busy ? t("web.actions.busy") : t("web.actions.add")}
            </button>
          </div>
        )}
      </form>
    </Sheet>
  );
}

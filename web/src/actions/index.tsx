// The add sheet, the item sheet and the settings menu (docs/WEB-DESIGN.md
// section 7.3). The rows and the header open a sheet through the store.
import { useEffect, useState } from "preact/hooks";
import { readCache } from "../cache";
import { t } from "../i18n";
import type { Me, User } from "../types";
import { AddSheet } from "./add";
import { ItemSheet } from "./item";
import { SettingsSheet } from "./settings";
import { useLive } from "./sheet";
import { closeSheet, getSheet, openAdd, openSettings, type Sheet, useSheet } from "./store";
import "./actions.css";

export { closeSheet, openAdd, openSettings };

/**
 * The chat that the add sheet starts with: the last group when the Groups
 * tab is open, else the private chat. The tabs keep both choices in the cache.
 */
export function startChat(userId: number): number {
  if (readCache<string>(userId, "pref.tab") !== "groups") return userId;
  const g = readCache<number>(userId, "pref.group");
  return typeof g === "number" && g < 0 ? g : userId;
}

export function Actions(props: {
  user: User;
  miniapp: boolean;
  /** An item was added or changed: load the lists again. */
  onChanged: () => void;
  /** The settings were saved: apply them at once. */
  onSaved: (me: Me) => Promise<void> | void;
  onSignInBrowser: () => void;
  onLogout: (everywhere: boolean) => Promise<void>;
}) {
  const sheet = useSheet();
  const [toast, setToast] = useState("");

  useEffect(() => {
    if (!toast) return;
    const id = setTimeout(() => setToast(""), 4000);
    return () => clearTimeout(id);
  }, [toast]);

  // A new user (or logout) closes any open sheet.
  useEffect(() => closeSheet, [props.user.id]);

  const live = useLive();

  // `from` is the sheet that saved the change. If the user closed it (or
  // opened another one) while the request ran, the open sheet stays.
  const changed = (note: string, from: Sheet) => {
    if (getSheet() === from) closeSheet();
    if (live.current) setToast(note);
    props.onChanged();
  };

  return (
    <>
      {sheet.kind === "add" ? (
        <AddSheet
          user={props.user}
          miniapp={props.miniapp}
          chat={startChat(props.user.id)}
          onClose={closeSheet}
          onAdded={() => changed(t("web.actions.added"), sheet)}
        />
      ) : null}
      {sheet.kind === "item" ? (
        <ItemSheet
          key={`${sheet.chat}:${sheet.item.id}:${sheet.run ?? ""}`}
          item={sheet.item}
          chat={sheet.chat}
          run={sheet.run}
          miniapp={props.miniapp}
          onClose={closeSheet}
          onChanged={(note) => changed(note, sheet)}
        />
      ) : null}
      {sheet.kind === "settings" ? (
        <SettingsSheet
          user={props.user}
          miniapp={props.miniapp}
          onClose={closeSheet}
          onSaved={props.onSaved}
          onSignInBrowser={() => {
            closeSheet();
            props.onSignInBrowser();
          }}
          onLogout={props.onLogout}
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

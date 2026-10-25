// Which sheet is open (docs/WEB-DESIGN.md section 7.3). The rows live in the
// tabs, and the sheets live in <Actions/>, so a small store joins them. It
// keeps no data of the server: the lists load again after each change.

import { useEffect, useState } from "preact/hooks";
import type { Item } from "../tabs/model";

export type Sheet =
  | { kind: "none" }
  | { kind: "add" }
  /**
   * `chat` is the chat of the actor (section 5.1), not always item.chat_id.
   * `run: "done"` is the check mark of a row: the sheet marks the item as done
   * at once and closes.
   */
  | { kind: "item"; item: Item; chat: number; run?: "done" }
  | { kind: "settings" };

let sheet: Sheet = { kind: "none" };
const listeners = new Set<() => void>();

function set(s: Sheet): void {
  sheet = s;
  listeners.forEach((fn) => fn());
}

export function getSheet(): Sheet {
  return sheet;
}

export function openAdd(): void {
  set({ kind: "add" });
}

export function openItem(item: Item, chat: number, run?: "done"): void {
  set(run ? { kind: "item", item, chat, run } : { kind: "item", item, chat });
}

export function openSettings(): void {
  set({ kind: "settings" });
}

export function closeSheet(): void {
  set({ kind: "none" });
}

export function useSheet(): Sheet {
  const [s, setS] = useState(sheet);
  useEffect(() => {
    const fn = () => setS(sheet);
    listeners.add(fn);
    fn();
    return () => {
      listeners.delete(fn);
    };
  }, []);
  return s;
}

/**
 * The chat of the actor for an item of a list. In "Assigned to me" the actor
 * is the private chat of the viewer, because the assignee acts only from
 * there (section 5.1). Elsewhere it is the chat of the item.
 */
export function actorChat(item: Item, assignedList: boolean, viewerId: number): number {
  return assignedList ? viewerId : item.chat_id;
}

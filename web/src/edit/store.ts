// Which item the edit form shows (docs/WEB-DESIGN.md section 7.3). The item
// sheet opens it, and <Edit/> in app.tsx shows it. It keeps no data of the
// server: the form loads the item again from endpoint 22.

import { useEffect, useState } from "preact/hooks";
import type { Item } from "../tabs/model";

/** `chat` is the chat of the actor (section 5.1), not always item.chat_id. */
export type EditTarget = { item: Item; chat: number; miniapp: boolean } | null;

let target: EditTarget = null;
const listeners = new Set<() => void>();

function set(t: EditTarget): void {
  target = t;
  listeners.forEach((fn) => fn());
}

export function openEdit(item: Item, chat: number, miniapp: boolean): void {
  set({ item, chat, miniapp });
}

export function closeEdit(): void {
  set(null);
}

export function getEdit(): EditTarget {
  return target;
}

export function useEditTarget(): EditTarget {
  const [s, setS] = useState(target);
  useEffect(() => {
    const fn = () => setS(target);
    listeners.add(fn);
    fn();
    return () => {
      listeners.delete(fn);
    };
  }, []);
  return s;
}

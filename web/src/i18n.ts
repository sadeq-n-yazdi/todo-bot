// UI strings come from GET /api/i18n (docs/WEB-DESIGN.md section 7.4). The only
// locale logic here is the digits and the direction. JavaScript never works
// out calendar dates.

import { api } from "./api";

export type Dir = "ltr" | "rtl";

let lang = "en";
let catalog: Record<string, string> = {};

// Used until the catalog arrives, or when it has no such key.
const fallback: Record<string, string> = {
  "web.app.notallowed": "Your Telegram ID is %s. Ask the owner to add it.",
  "web.app.open_in_telegram": "Open this page from the bot in Telegram.",
  "web.app.error": "Something went wrong.",
};

const persian = "۰۱۲۳۴۵۶۷۸۹";

export function setLang(l: string): void {
  lang = l;
}

export function getLang(): string {
  return lang;
}

export function setCatalog(c: Record<string, string>): void {
  catalog = c;
}

export function dirFor(l: string | undefined): Dir {
  return l !== undefined && /^(fa|ar|he|ur)(-|$)/i.test(l) ? "rtl" : "ltr";
}

/** Digits of a number or string, in Persian digits when the language is Farsi. */
export function num(n: number | string): string {
  const s = String(n);
  return lang.startsWith("fa") ? s.replace(/\d/g, (d) => persian[Number(d)]) : s;
}

/** Fill %s and %d, in order. A missing key gives the key. */
export function t(key: string, ...args: (string | number)[]): string {
  const tpl = catalog[key] ?? fallback[key] ?? key;
  let i = 0;
  return tpl.replace(/%[sd]/g, () => {
    const a = args[i++];
    return a === undefined ? "" : typeof a === "number" ? num(a) : a;
  });
}

const storeKey = (l: string) => `todo.i18n.${l}`;

/**
 * Load the catalog of a language. A failure keeps the last stored catalog, or
 * an empty one, so that the app still starts.
 */
export async function loadCatalog(l: string): Promise<void> {
  setLang(l);
  try {
    const r = await api<{ messages: Record<string, string> }>("GET", `/api/i18n?lang=${encodeURIComponent(l)}`);
    setCatalog(r.messages ?? {});
    try {
      localStorage.setItem(storeKey(l), JSON.stringify(r.messages ?? {}));
    } catch {
      /* storage can be blocked */
    }
  } catch {
    try {
      const saved = localStorage.getItem(storeKey(l));
      setCatalog(saved ? (JSON.parse(saved) as Record<string, string>) : {});
    } catch {
      setCatalog({});
    }
  }
}

/** Set <html lang dir>. */
export function applyLang(l: string, dir: Dir = dirFor(l)): void {
  setLang(l);
  document.documentElement.lang = l;
  document.documentElement.dir = dir;
}

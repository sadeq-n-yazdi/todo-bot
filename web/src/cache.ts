// The last response of each list, per user (docs/WEB-DESIGN.md section 7.5).
// Storage can fail or be blocked, so every call is safe to fail.

const prefix = "todo.cache.";
const keyOf = (userId: number, name: string) => `${prefix}${userId}.${name}`;

export function writeCache(userId: number, name: string, value: unknown): void {
  try {
    localStorage.setItem(keyOf(userId, name), JSON.stringify(value));
  } catch {
    /* full or blocked */
  }
}

export function readCache<T = unknown>(userId: number, name: string): T | null {
  try {
    const raw = localStorage.getItem(keyOf(userId, name));
    return raw === null ? null : (JSON.parse(raw) as T);
  } catch {
    return null;
  }
}

function removeWhere(match: (userId: string) => boolean): void {
  try {
    for (const k of Object.keys(localStorage)) {
      if (!k.startsWith(prefix)) continue;
      const user = k.slice(prefix.length).split(".")[0];
      if (match(user)) localStorage.removeItem(k);
    }
  } catch {
    /* blocked */
  }
}

/** Logout: delete every entry of the user. */
export function clearCache(userId: number): void {
  removeWhere((u) => u === String(userId));
}

/** A different user signed in: delete the entries of all other users. */
export function clearOtherUsers(userId: number): void {
  removeWhere((u) => u !== String(userId));
}

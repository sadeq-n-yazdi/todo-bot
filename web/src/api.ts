// fetch wrapper (docs/WEB-DESIGN.md sections 3.5, 3.6 and 5.1).
//
// Every request carries X-Todo-Request. The Mini App adds a bearer token. The
// browser and the PWA send the session cookie, so they set no token.

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  /** The whole "error" object of the response, for extra fields such as user_id. */
  readonly data: Record<string, unknown>;

  constructor(status: number, code: string, message: string, data: Record<string, unknown> = {}) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.data = data;
  }
}

let token: string | null = null;
const unauthorized = new Set<() => void>();

export function setToken(t: string | null): void {
  token = t;
}

export function getToken(): string | null {
  return token;
}

/** Register a function that runs after each 401. It returns a function that removes it. */
export function onUnauthorized(fn: () => void): () => void {
  unauthorized.add(fn);
  return () => unauthorized.delete(fn);
}

export async function api<T = unknown>(method: string, path: string, body?: unknown): Promise<T> {
  const headers = new Headers({ "X-Todo-Request": "1", Accept: "application/json" });
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const init: RequestInit = { method, headers, credentials: "same-origin" };
  if (body !== undefined) {
    headers.set("Content-Type", "application/json");
    init.body = JSON.stringify(body);
  }

  let res: Response;
  try {
    res = await fetch(path, init);
  } catch (e) {
    throw new ApiError(0, "network", e instanceof Error ? e.message : "network error");
  }

  if (res.status === 204) return undefined as T;

  let payload: unknown;
  try {
    payload = await res.json();
  } catch {
    payload = undefined;
  }

  if (!res.ok) {
    const err = (payload as { error?: Record<string, unknown> } | undefined)?.error ?? {};
    const code = typeof err.code === "string" ? err.code : "internal";
    const message = typeof err.message === "string" ? err.message : res.statusText;
    if (res.status === 401) unauthorized.forEach((fn) => fn());
    throw new ApiError(res.status, code, message, err);
  }
  return payload as T;
}

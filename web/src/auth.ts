// Mini App sign-in (docs/WEB-DESIGN.md sections 3.2 and 3.5). The token is kept
// in memory and in sessionStorage, so that a reload in the same webview keeps
// it. The app still signs in again with fresh initData at each start.

import { api, setToken } from "./api";
import type { Me, MiniAppSession } from "./types";

const tokenKey = "todo.token";

function store(token: string | null): void {
  try {
    if (token) sessionStorage.setItem(tokenKey, token);
    else sessionStorage.removeItem(tokenKey);
  } catch {
    /* blocked in a private window or an iframe */
  }
}

export async function signInMiniApp(initData: string): Promise<MiniAppSession> {
  const s = await api<MiniAppSession>("POST", "/api/auth/miniapp", { init_data: initData });
  setToken(s.token);
  store(s.token);
  return s;
}

export function fetchMe(): Promise<Me> {
  return api<Me>("GET", "/api/me");
}

export function forgetToken(): void {
  setToken(null);
  store(null);
}

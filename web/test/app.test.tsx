import { cleanup, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "../src/app";
import { setToken } from "../src/api";
import { setCatalog, setLang } from "../src/i18n";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const me = (over: Record<string, unknown> = {}) => ({
  user: { id: 42, username: "alice", lang: "en", calendar: "gregorian", tz: "UTC", dir: "ltr", ...over },
  session: { kind: "miniapp", expires_at: "2026-10-25T00:00:00Z" },
  bot: { username: "todo_bot" },
});

describe("App in the Mini App", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    setToken(null);
    setLang("en");
    setCatalog({});
    sessionStorage.clear();
    document.documentElement.removeAttribute("dir");
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("signs in with initData, then shows the user name from /api/me", async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/miniapp") {
        return json(200, { token: "tb1_tok", expires_at: "2026-10-25T00:00:00Z", user: { id: 42 }, start: "" });
      }
      if (url === "/api/me") return json(200, me());
      if (url.startsWith("/api/i18n")) return json(200, { lang: "en", dir: "ltr", messages: {} });
      throw new Error("unexpected " + url);
    });

    render(<App mode="miniapp" initData="query_id=Q&hash=h" />);
    await screen.findByText("@alice");

    const signIn = fetchMock.mock.calls.find((c) => c[0] === "/api/auth/miniapp")!;
    expect(signIn[1].method).toBe("POST");
    expect(JSON.parse(signIn[1].body)).toEqual({ init_data: "query_id=Q&hash=h" });
    expect(new Headers(signIn[1].headers).get("X-Todo-Request")).toBe("1");
    expect(new Headers(signIn[1].headers).has("Authorization")).toBe(false);

    const meCall = fetchMock.mock.calls.find((c) => c[0] === "/api/me")!;
    expect(new Headers(meCall[1].headers).get("Authorization")).toBe("Bearer tb1_tok");
    expect(sessionStorage.getItem("todo.token")).toBe("tb1_tok");
  });

  it("sets dir=rtl and lang on <html> for a Farsi user", async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/miniapp") return json(200, { token: "tb1_tok", expires_at: "x", user: { id: 42 }, start: "" });
      if (url === "/api/me") return json(200, me({ lang: "fa", dir: "rtl" }));
      return json(200, { lang: "fa", dir: "rtl", messages: {} });
    });
    render(<App mode="miniapp" initData="x" />);
    await screen.findByText("@alice");
    expect(document.documentElement.dir).toBe("rtl");
    expect(document.documentElement.lang).toBe("fa");
  });

  it("shows the user ID when the user is not allowed", async () => {
    fetchMock.mockResolvedValue(json(403, { error: { code: "not_allowed", message: "no", user_id: 777 } }));
    render(<App mode="miniapp" initData="x" />);
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("777"));
  });

  it("shows an error when sign-in fails", async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: "bad_signature", message: "bad" } }));
    render(<App mode="miniapp" initData="x" />);
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("bad"));
  });

  it("shows an error and does not call the API when there is no initData", async () => {
    render(<App mode="miniapp" initData="" />);
    await screen.findByRole("alert");
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("App in the browser", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    setToken(null);
    setLang("en");
    setCatalog({});
  });
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("uses the cookie session: /api/me without a bearer token", async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/me") return json(200, me());
      return json(200, { lang: "en", dir: "ltr", messages: {} });
    });
    render(<App mode="pwa" />);
    await screen.findByText("@alice");
    const meCall = fetchMock.mock.calls.find((c) => c[0] === "/api/me")!;
    expect(new Headers(meCall[1].headers).has("Authorization")).toBe(false);
    expect(fetchMock.mock.calls.some((c) => c[0] === "/api/auth/miniapp")).toBe(false);
  });

  it("shows the login screen on 401", async () => {
    fetchMock.mockResolvedValue(json(401, { error: { code: "unauthenticated", message: "x" } }));
    render(<App mode="pwa" />);
    await screen.findByTestId("login");
  });
});

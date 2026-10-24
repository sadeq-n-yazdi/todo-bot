import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, api, onUnauthorized, setToken } from "../src/api";

function reply(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api", () => {
  const fetchMock = vi.fn();
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    setToken(null);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("always sends the X-Todo-Request header", async () => {
    fetchMock.mockResolvedValue(reply(200, { ok: true }));
    await api("GET", "/api/me");
    const [, init] = fetchMock.mock.calls[0];
    expect(new Headers(init.headers).get("X-Todo-Request")).toBe("1");
  });

  it("sends the bearer token when there is one, and no header when not", async () => {
    fetchMock.mockResolvedValue(reply(200, {}));
    await api("GET", "/api/me");
    expect(new Headers(fetchMock.mock.calls[0][1].headers).has("Authorization")).toBe(false);
    setToken("tb1_abc");
    await api("GET", "/api/me");
    expect(new Headers(fetchMock.mock.calls[1][1].headers).get("Authorization")).toBe("Bearer tb1_abc");
  });

  it("sends a JSON body with the JSON content type", async () => {
    fetchMock.mockResolvedValue(reply(200, {}));
    await api("POST", "/api/auth/miniapp", { init_data: "a=b" });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/auth/miniapp");
    expect(init.method).toBe("POST");
    expect(init.body).toBe('{"init_data":"a=b"}');
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
    expect(init.credentials).toBe("same-origin");
  });

  it("maps an error body to ApiError", async () => {
    fetchMock.mockResolvedValue(reply(403, { error: { code: "not_allowed", message: "no" } }));
    await expect(api("POST", "/api/auth/miniapp", {})).rejects.toMatchObject({
      name: "ApiError",
      status: 403,
      code: "not_allowed",
      message: "no",
    });
  });

  it("returns undefined for 204", async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    await expect(api("POST", "/api/auth/logout")).resolves.toBeUndefined();
  });

  it("calls the unauthorized handlers on 401 and still throws", async () => {
    const seen = vi.fn();
    const off = onUnauthorized(seen);
    fetchMock.mockResolvedValue(reply(401, { error: { code: "unauthenticated", message: "x" } }));
    await expect(api("GET", "/api/me")).rejects.toBeInstanceOf(ApiError);
    expect(seen).toHaveBeenCalledOnce();
    off();
  });

  it("turns a network failure into ApiError with status 0", async () => {
    fetchMock.mockRejectedValue(new TypeError("offline"));
    await expect(api("GET", "/api/me")).rejects.toMatchObject({ status: 0, code: "network" });
  });
});

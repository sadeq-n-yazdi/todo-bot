import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { onUnauthorized, setToken } from "../src/api";
import { writeCache } from "../src/cache";
import { POLL_MS, pollTarget, startPoll } from "../src/live";
import { chats, item, list } from "../src/tabs/fixtures";

// docs/WEB-DESIGN.md section 7.6: the ETag poll of phase 4.

const UID = 11111;
const GROUP = -1001234567890;
const MINE = `/api/chats/${UID}/items?page=0`;

const fetchMock = vi.fn();
let visibility: DocumentVisibilityState = "visible";
let online = true;

function listResponse(body: unknown, etag = 'W/"a"', status = 200): Response {
  return new Response(status === 304 ? null : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ETag: etag },
  });
}

function setVisible(v: DocumentVisibilityState) {
  visibility = v;
  document.dispatchEvent(new Event("visibilitychange"));
}

function setOnline(v: boolean) {
  online = v;
  window.dispatchEvent(new Event(v ? "online" : "offline"));
}

const header = (i: number, name: string) => new Headers(fetchMock.mock.calls[i][1].headers).get(name);

let stop: (() => void) | undefined;

beforeEach(() => {
  vi.useFakeTimers();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  visibility = "visible";
  online = true;
  Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility });
  Object.defineProperty(navigator, "onLine", { configurable: true, get: () => online });
  localStorage.clear();
  setToken(null);
});

afterEach(() => {
  stop?.();
  stop = undefined;
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("pollTarget", () => {
  it("follows the tab and the group that the tabs keep", () => {
    expect(pollTarget(UID)).toEqual({ path: MINE, cache: "mine" });
    writeCache(UID, "pref.tab", "assigned");
    expect(pollTarget(UID)).toEqual({ path: "/api/assigned", cache: "assigned" });
    writeCache(UID, "pref.tab", "groups");
    expect(pollTarget(UID)).toBeNull();
    writeCache(UID, "chats", chats);
    expect(pollTarget(UID)?.path).toBe(`/api/chats/${GROUP}/items?page=0`);
    writeCache(UID, "pref.group", -1009876543210);
    expect(pollTarget(UID)).toEqual({ path: "/api/chats/-1009876543210/items?page=0", cache: "group--1009876543210" });
  });
});

describe("startPoll", () => {
  it("asks every 30 s with If-None-Match, and a 304 changes nothing", async () => {
    const saved = list([item()]);
    writeCache(UID, "mine", saved);
    fetchMock.mockResolvedValueOnce(listResponse(saved)).mockResolvedValue(listResponse(null, 'W/"a"', 304));
    const onChange = vi.fn();
    setToken("tb1_tok");
    stop = startPoll(UID, onChange);

    await vi.advanceTimersByTimeAsync(POLL_MS - 1);
    expect(fetchMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][0]).toBe(MINE);
    expect(header(0, "If-None-Match")).toBeNull();
    expect(header(0, "X-Todo-Request")).toBe("1");
    expect(header(0, "Authorization")).toBe("Bearer tb1_tok");

    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(header(1, "If-None-Match")).toBe('W/"a"');
    expect(onChange).not.toHaveBeenCalled();
  });

  it("calls onChange when the list differs from the one on the screen", async () => {
    writeCache(UID, "mine", list([item()]));
    fetchMock.mockResolvedValue(listResponse(list([item(), item({ id: 43, num: 4, text: "call mom" })]), 'W/"b"'));
    const onChange = vi.fn();
    stop = startPoll(UID, onChange);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("without a cache entry, only a new ETag counts", async () => {
    fetchMock
      .mockResolvedValueOnce(listResponse(list([]), 'W/"a"'))
      .mockResolvedValueOnce(listResponse(list([]), 'W/"a"'))
      .mockResolvedValueOnce(listResponse(list([item()]), 'W/"b"'));
    const onChange = vi.fn();
    stop = startPoll(UID, onChange);
    await vi.advanceTimersByTimeAsync(POLL_MS * 2);
    expect(onChange).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("stops while the page is hidden or offline, and starts again", async () => {
    fetchMock.mockResolvedValue(listResponse(null, 'W/"a"', 304));
    stop = startPoll(UID, () => {});

    setVisible("hidden");
    await vi.advanceTimersByTimeAsync(POLL_MS * 3);
    expect(fetchMock).not.toHaveBeenCalled();

    setVisible("visible");
    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    setOnline(false);
    await vi.advanceTimersByTimeAsync(POLL_MS * 3);
    expect(fetchMock).toHaveBeenCalledTimes(1);

    setOnline(true);
    await vi.advanceTimersByTimeAsync(POLL_MS);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("does not start while the page is hidden", async () => {
    visibility = "hidden";
    stop = startPoll(UID, () => {});
    await vi.advanceTimersByTimeAsync(POLL_MS * 3);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("stops for good after stop()", async () => {
    fetchMock.mockResolvedValue(listResponse(null, 'W/"a"', 304));
    const s = startPoll(UID, () => {});
    s();
    setVisible("visible");
    setOnline(true);
    await vi.advanceTimersByTimeAsync(POLL_MS * 3);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("keeps going after a network error", async () => {
    fetchMock.mockRejectedValueOnce(new TypeError("offline")).mockResolvedValue(listResponse(null, 'W/"a"', 304));
    stop = startPoll(UID, () => {});
    await vi.advanceTimersByTimeAsync(POLL_MS * 2);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("stops after a 401 and lets api() send the app to sign-in", async () => {
    fetchMock.mockImplementation(async () =>
      new Response(JSON.stringify({ error: { code: "session_revoked", message: "x" } }), { status: 401 }),
    );
    const out = vi.fn();
    const off = onUnauthorized(out);
    stop = startPoll(UID, () => {});
    await vi.advanceTimersByTimeAsync(POLL_MS);
    await vi.advanceTimersByTimeAsync(0);
    expect(out).toHaveBeenCalledTimes(1);
    const n = fetchMock.mock.calls.length;
    await vi.advanceTimersByTimeAsync(POLL_MS * 3);
    expect(fetchMock.mock.calls.length).toBe(n);
    off();
  });
});

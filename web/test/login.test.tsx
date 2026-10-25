import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setToken } from "../src/api";
import { CodeScreen } from "../src/code/index";
import { setCatalog, setLang } from "../src/i18n";
import { Login } from "../src/login/index";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const fetchMock = vi.fn();
const emptyCatalog = (url: string) => (url.startsWith("/api/i18n") ? json(200, { lang: "en", dir: "ltr", messages: {} }) : undefined);

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  setToken(null);
  setLang("en");
  setCatalog({});
  document.head.querySelectorAll("meta[name=todo-bot]").forEach((m) => m.remove());
  fetchMock.mockImplementation(async (url: string) => {
    const r = emptyCatalog(url);
    if (r) return r;
    throw new Error("unexpected " + url);
  });
});
afterEach(async () => {
  vi.useRealTimers();
  // Let the pending requests of the screen end before jsdom goes.
  await new Promise((r) => setTimeout(r, 0));
  cleanup();
  vi.unstubAllGlobals();
  delete window.Telegram;
});

const calls = (path: string) => fetchMock.mock.calls.filter((c) => c[0] === path);

describe("Login", () => {
  it("sends the code in the JSON body, never in a URL, then signs in", async () => {
    const done = vi.fn();
    fetchMock.mockImplementation(async (url: string) => {
      const r = emptyCatalog(url);
      if (r) return r;
      if (url === "/api/auth/code/redeem") return json(200, { user: { id: 42 }, expires_at: "2026-10-25T00:00:00Z" });
      throw new Error("unexpected " + url);
    });
    render(<Login onSignedIn={done} />);
    fireEvent.input(screen.getByLabelText("Code"), { target: { value: "abcd-efgh" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await waitFor(() => expect(done).toHaveBeenCalled());
    const [url, init] = calls("/api/auth/code/redeem")[0];
    expect(url).toBe("/api/auth/code/redeem");
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ code: "abcd-efgh" });
    expect(new Headers(init.headers).get("X-Todo-Request")).toBe("1");
    expect(fetchMock.mock.calls.every((c) => !String(c[0]).includes("abcd"))).toBe(true);
  });

  it("shows the message of the server for a wrong code and stays on the screen", async () => {
    const done = vi.fn();
    fetchMock.mockImplementation(async (url: string) => {
      const r = emptyCatalog(url);
      if (r) return r;
      return json(401, { error: { code: "bad_code", message: "The code is wrong, used or expired." } });
    });
    render(<Login onSignedIn={done} />);
    fireEvent.input(screen.getByLabelText("Code"), { target: { value: "ZZZZZZZZ" } });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    expect((await screen.findByRole("alert")).textContent).toBe("The code is wrong, used or expired.");
    expect(done).not.toHaveBeenCalled();
    await waitFor(() => expect((screen.getByRole("button", { name: "Sign in" }) as HTMLButtonElement).disabled).toBe(false));
  });

  it("does not send an empty code, and sets the field left to right", async () => {
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const field = screen.getByLabelText("Code") as HTMLInputElement;
    expect(field.getAttribute("dir")).toBe("ltr");
    expect(field.autocomplete).toBe("off");
    expect((screen.getByRole("button", { name: "Sign in" }) as HTMLButtonElement).disabled).toBe(true);
    expect(calls("/api/auth/code/redeem")).toHaveLength(0);
  });

  it("links to the Mini App only when the page names the bot", async () => {
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(screen.queryByText("Open the app in Telegram")).toBeNull();
    cleanup();
    const meta = document.createElement("meta");
    meta.name = "todo-bot";
    meta.content = "todo_bot";
    document.head.append(meta);
    render(<Login onSignedIn={() => {}} />);
    // The widget also sends its start request now; count the catalog loads only.
    await waitFor(() => expect(fetchMock.mock.calls.filter((c) => String(c[0]).startsWith("/api/i18n"))).toHaveLength(2));
    expect(screen.getByText("Open the app in Telegram").getAttribute("href")).toBe("https://t.me/todo_bot?startapp=code");
  });

  it("uses the catalog text when it arrives", async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url.startsWith("/api/i18n")) return json(200, { lang: "en", dir: "ltr", messages: { "web.login.title": "Welcome" } });
      throw new Error("unexpected " + url);
    });
    render(<Login onSignedIn={() => {}} />);
    await screen.findByText("Welcome");
  });
});

describe("CodeScreen", () => {
  beforeEach(() => {
    window.Telegram = { WebApp: { initData: "query_id=Q&hash=h" } } as unknown as Window["Telegram"];
  });

  it("asks for a code with the initData and shows it with a left-to-right isolate", async () => {
    const expires = new Date(Date.now() + 5 * 60_000).toISOString();
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/code") return json(201, { code: "ABCD-EFGH", expires_at: expires });
      throw new Error("unexpected " + url);
    });
    render(<CodeScreen screen="code" />);
    const code = await screen.findByText("ABCD-EFGH");
    expect(code.tagName).toBe("BDI");
    expect(code.getAttribute("dir")).toBe("ltr");
    const [, init] = calls("/api/auth/code")[0];
    expect(init.method).toBe("POST");
    expect(JSON.parse(init.body)).toEqual({ init_data: "query_id=Q&hash=h" });
    expect(screen.getByText(/Never send it to another person/)).toBeTruthy();
    expect(screen.getByTestId("left").textContent).toMatch(/Time left: \d:\d\d/);
  });

  it("counts down and then offers a new code", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
    const expires = new Date(Date.now() + 3000).toISOString();
    fetchMock.mockImplementation(async () => json(201, { code: "ABCD-EFGH", expires_at: expires }));
    render(<CodeScreen screen="code" />);
    await vi.waitFor(() => expect(screen.queryByText("ABCD-EFGH")).not.toBeNull());
    await vi.advanceTimersByTimeAsync(1000);
    expect(screen.getByTestId("left").textContent).toContain("0:02");
    await vi.advanceTimersByTimeAsync(2500);
    expect(screen.getByText("The code expired.")).toBeTruthy();
    expect(screen.queryByText("ABCD-EFGH")).toBeNull();
    expect(screen.getByRole("button", { name: "Get a new code" })).toBeTruthy();
  });

  it("cancels the code", async () => {
    const expires = new Date(Date.now() + 5 * 60_000).toISOString();
    fetchMock.mockImplementation(async (url: string, init: RequestInit) => {
      if (url === "/api/auth/code" && init.method === "POST") return json(201, { code: "ABCD-EFGH", expires_at: expires });
      if (url === "/api/auth/code" && init.method === "DELETE") return new Response(null, { status: 204 });
      throw new Error("unexpected " + url);
    });
    render(<CodeScreen screen="code" />);
    await screen.findByText("ABCD-EFGH");
    fireEvent.click(screen.getByRole("button", { name: "Cancel the code" }));
    await screen.findByText("The code is canceled.");
    expect(screen.queryByText("ABCD-EFGH")).toBeNull();
  });

  it("shows the error of the server", async () => {
    fetchMock.mockImplementation(async () =>
      json(503, { error: { code: "too_many_codes", message: "Too many sign-in codes are open now." } }),
    );
    render(<CodeScreen screen="code" />);
    expect((await screen.findByRole("alert")).textContent).toBe("Too many sign-in codes are open now.");
  });

  it("asks for a confirmation, then logs out everywhere", async () => {
    fetchMock.mockImplementation(async (url: string) => {
      if (url === "/api/auth/logout-all") return new Response(null, { status: 204 });
      throw new Error("unexpected " + url);
    });
    render(<CodeScreen screen="signout" />);
    expect(calls("/api/auth/logout-all")).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Log out" }));
    await screen.findByText("You are logged out everywhere.");
    expect(calls("/api/auth/logout-all")[0][1].method).toBe("POST");
  });
});

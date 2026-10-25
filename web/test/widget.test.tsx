import { cleanup, render, screen, waitFor } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setToken } from "../src/api";
import { setCatalog, setLang } from "../src/i18n";
import { Login } from "../src/login/index";
import { WIDGET_SRC, WIDGET_START, loginErrorText, takeLoginError } from "../src/login/widget";

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

const fetchMock = vi.fn();

function nameBot(name: string): void {
  const meta = document.createElement("meta");
  meta.name = "todo-bot";
  meta.content = name;
  document.head.append(meta);
}

function setStandalone(v: boolean | undefined): void {
  Object.defineProperty(navigator, "standalone", { value: v, configurable: true });
}

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
  setToken(null);
  setLang("en");
  setCatalog({});
  document.head.querySelectorAll("meta[name=todo-bot]").forEach((m) => m.remove());
  history.replaceState(null, "", "/");
  setStandalone(undefined);
  fetchMock.mockImplementation(async (url: string) => {
    if (url.startsWith("/api/i18n")) return json(200, { lang: "en", dir: "ltr", messages: {} });
    if (url === WIDGET_START) return new Response(null, { status: 204 });
    throw new Error("unexpected " + url);
  });
});

const starts = () => fetchMock.mock.calls.filter((c) => c[0] === WIDGET_START);
afterEach(async () => {
  await new Promise((r) => setTimeout(r, 0));
  cleanup();
  vi.unstubAllGlobals();
  setStandalone(undefined);
  history.replaceState(null, "", "/");
});

const widgetScript = () => document.querySelector<HTMLScriptElement>(`script[src="${WIDGET_SRC}"]`);

describe("Login Widget", () => {
  it("loads the widget script in the redirect mode", async () => {
    nameBot("todo_bot");
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(widgetScript()).not.toBeNull());
    const s = widgetScript()!;
    expect(s.src).toBe("https://telegram.org/js/telegram-widget.js?22");
    expect(s.async).toBe(true);
    expect(s.dataset.telegramLogin).toBe("todo_bot");
    expect(s.dataset.authUrl).toBe(location.origin + "/auth/telegram");
    // The redirect mode only: no popup callback.
    expect(s.dataset.onauth).toBeUndefined();
    expect(screen.getByTestId("widget").contains(s)).toBe(true);
    expect(screen.getByText(/sign in with your Telegram account/)).toBeTruthy();
    // The start request came first: it sets the nonce cookie of the server.
    expect(starts()).toHaveLength(1);
    const [, init] = starts()[0];
    expect(init.method).toBe("GET");
    expect(new Headers(init.headers).get("X-Todo-Request")).toBe("1");
  });

  it("does not show when the start request fails", async () => {
    nameBot("todo_bot");
    fetchMock.mockImplementation(async (url: string) => {
      if (url.startsWith("/api/i18n")) return json(200, { lang: "en", dir: "ltr", messages: {} });
      return json(403, { error: { code: "wrong_session", message: "x" } });
    });
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(starts()).toHaveLength(1));
    await new Promise((r) => setTimeout(r, 10));
    expect(screen.queryByTestId("widget")).toBeNull();
    expect(widgetScript()).toBeNull();
  });

  it("removes the script when the screen goes", async () => {
    nameBot("todo_bot");
    const { unmount } = render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(widgetScript()).not.toBeNull());
    unmount();
    await waitFor(() => expect(widgetScript()).toBeNull());
  });

  it("does not show in an installed iOS app", async () => {
    nameBot("todo_bot");
    setStandalone(true);
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByTestId("widget")).toBeNull();
    expect(widgetScript()).toBeNull();
    expect(starts()).toHaveLength(0);
    // The code field stays.
    expect(screen.getByLabelText("Code")).toBeTruthy();
  });

  it("shows in a browser that is not standalone", async () => {
    nameBot("todo_bot");
    setStandalone(false);
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(widgetScript()).not.toBeNull());
  });

  it("does not show when the page does not name the bot", async () => {
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(screen.queryByTestId("widget")).toBeNull();
    expect(widgetScript()).toBeNull();
  });

  it("does not show for a bad bot name", async () => {
    nameBot('x" onload="alert(1)');
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(widgetScript()).toBeNull();
  });
});

describe("login errors of the widget", () => {
  it("shows the error of the fragment and removes the fragment", async () => {
    history.replaceState(null, "", "/?x=1#login-error=used");
    render(<Login onSignedIn={() => {}} />);
    expect((await screen.findByRole("alert")).textContent).toBe("This sign-in was already used. Sign in again.");
    expect(location.hash).toBe("");
    expect(location.search).toBe("?x=1");
  });

  it("uses the catalog text", async () => {
    fetchMock.mockImplementation(async () =>
      json(200, { lang: "en", dir: "ltr", messages: { "web.widget.error_expired": "Too late." } }),
    );
    history.replaceState(null, "", "/#login-error=expired");
    render(<Login onSignedIn={() => {}} />);
    await waitFor(() => expect(screen.getByRole("alert").textContent).toBe("Too late."));
  });

  it("never shows text from the URL", async () => {
    history.replaceState(null, "", "/#login-error=%3Cb%3Eyou%20were%20hacked%3C/b%3E");
    render(<Login onSignedIn={() => {}} />);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toBe("Something went wrong with the sign-in. Try again.");
    expect(alert.textContent).not.toMatch(/hacked/);
    expect(location.hash).toBe("");
  });

  it("knows every code of the server", () => {
    for (const code of ["bad_request", "bad_signature", "expired", "not_allowed", "used", "internal", "restart", "other_user"]) {
      expect(loginErrorText(code)).not.toMatch(/^web\./);
    }
    expect(loginErrorText("not_allowed")).toMatch(/Ask the owner/);
  });

  it("gives no error and keeps other fragments", () => {
    history.replaceState(null, "", "/#tab=2");
    expect(takeLoginError()).toBe("");
    expect(location.hash).toBe("#tab=2");
  });
});

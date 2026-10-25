import { act, cleanup, fireEvent, render, screen } from "@testing-library/preact";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setCatalog, setLang } from "../src/i18n";
import { item, list } from "../src/tabs/fixtures";
import { Tabs } from "../src/tabs/index";
import {
  applyUpdate,
  checkIosHint,
  dismissIosHint,
  ensureTouchIcon,
  getPwaState,
  promptInstall,
  registerServiceWorker,
  resetPwa,
} from "../src/tabs/pwa";
import type { User } from "../src/types";

const user: User = { id: 11111, username: "alice", lang: "en", calendar: "gregorian", tz: "UTC", dir: "ltr" };

class FakeWorker extends EventTarget {
  state = "installing";
  postMessage = vi.fn();
}

class FakeRegistration extends EventTarget {
  waiting: FakeWorker | null = null;
  installing: FakeWorker | null = null;
  update = vi.fn(async () => {});
}

class FakeContainer extends EventTarget {
  controller: object | null = {};
  reg = new FakeRegistration();
  register = vi.fn(async (_url: string, _opts: unknown) => this.reg);
}

const asContainer = (c: FakeContainer) => c as unknown as ServiceWorkerContainer;

beforeEach(() => {
  resetPwa();
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  document.head.querySelectorAll("link[rel=apple-touch-icon]").forEach((l) => l.remove());
});

describe("service worker registration", () => {
  it("registers /sw.js with scope /", async () => {
    const c = new FakeContainer();
    await registerServiceWorker(asContainer(c));
    expect(c.register).toHaveBeenCalledWith("/sw.js", { scope: "/" });
    expect(getPwaState().updateReady).toBe(false);
  });

  it("survives a failed registration, and a browser with no support", async () => {
    const c = new FakeContainer();
    c.register.mockRejectedValue(new Error("no"));
    await registerServiceWorker(asContainer(c));
    await registerServiceWorker(undefined);
    expect(getPwaState().updateReady).toBe(false);
  });

  it("flags an update that is already waiting", async () => {
    const c = new FakeContainer();
    c.reg.waiting = new FakeWorker();
    await registerServiceWorker(asContainer(c));
    expect(getPwaState().updateReady).toBe(true);
  });

  it("flags a new worker when it is installed", async () => {
    const c = new FakeContainer();
    await registerServiceWorker(asContainer(c));
    const w = new FakeWorker();
    c.reg.installing = w;
    c.reg.dispatchEvent(new Event("updatefound"));
    expect(getPwaState().updateReady).toBe(false);
    w.state = "installed";
    w.dispatchEvent(new Event("statechange"));
    expect(getPwaState().updateReady).toBe(true);
  });

  it("does not call the first install an update", async () => {
    const c = new FakeContainer();
    c.controller = null;
    await registerServiceWorker(asContainer(c));
    const w = new FakeWorker();
    c.reg.installing = w;
    c.reg.dispatchEvent(new Event("updatefound"));
    w.state = "installed";
    w.dispatchEvent(new Event("statechange"));
    expect(getPwaState().updateReady).toBe(false);
  });

  it("looks for a new version each time the page becomes visible", async () => {
    const c = new FakeContainer();
    await registerServiceWorker(asContainer(c));
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "hidden" });
    document.dispatchEvent(new Event("visibilitychange"));
    expect(c.reg.update).not.toHaveBeenCalled();
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
    expect(c.reg.update).toHaveBeenCalledOnce();
    // A second registration call adds no second listener.
    await registerServiceWorker(asContainer(c));
    document.dispatchEvent(new Event("visibilitychange"));
    expect(c.reg.update).toHaveBeenCalledTimes(2);
  });

  it("tells the waiting worker to take over, and reloads when it has", async () => {
    const reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload, pathname: "/" });
    const c = new FakeContainer();
    c.reg.waiting = new FakeWorker();
    await registerServiceWorker(asContainer(c));
    applyUpdate(asContainer(c));
    expect(c.reg.waiting.postMessage).toHaveBeenCalledWith({ type: "SKIP_WAITING" });
    expect(reload).not.toHaveBeenCalled();
    c.dispatchEvent(new Event("controllerchange"));
    expect(reload).toHaveBeenCalledOnce();
  });
});

describe("the update banner", () => {
  it("shows when a new version waits, with a Reload button", async () => {
    setLang("en");
    setCatalog({ "web.tabs.update": "A new version is ready.", "web.tabs.reload": "Reload" });
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(list([item()])), { headers: { "Content-Type": "application/json" } })),
    );
    render(<Tabs user={user} />);
    expect(screen.queryByText("A new version is ready.")).toBeNull();

    const c = new FakeContainer();
    c.reg.waiting = new FakeWorker();
    await act(async () => {
      await registerServiceWorker(asContainer(c));
    });
    await screen.findByText("A new version is ready.");

    vi.stubGlobal("navigator", Object.assign(Object.create(navigator), { serviceWorker: asContainer(c) }));
    fireEvent.click(screen.getByText("Reload"));
    expect(c.reg.waiting.postMessage).toHaveBeenCalledWith({ type: "SKIP_WAITING" });
  });
});

describe("install", () => {
  it("keeps the install event and shows it once", async () => {
    const prompt = vi.fn(async () => {});
    const ev = Object.assign(new Event("beforeinstallprompt", { cancelable: true }), { prompt });
    window.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
    expect(getPwaState().canInstall).toBe(true);
    await promptInstall();
    expect(prompt).toHaveBeenCalledOnce();
    expect(getPwaState().canInstall).toBe(false);
    await promptInstall();
    expect(prompt).toHaveBeenCalledOnce();
  });
});

describe("iOS hint", () => {
  const safari = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1";

  it("shows one time on iOS in the browser", () => {
    checkIosHint(safari, false);
    expect(getPwaState().iosHint).toBe(true);
    dismissIosHint();
    expect(getPwaState().iosHint).toBe(false);
    checkIosHint(safari, false);
    expect(getPwaState().iosHint).toBe(false);
  });

  it("does not show on other systems, or in the installed app", () => {
    checkIosHint("Mozilla/5.0 (X11; Linux x86_64) Chrome/130", false);
    checkIosHint(safari, true);
    expect(getPwaState().iosHint).toBe(false);
  });
});

describe("apple-touch-icon", () => {
  it("adds the link one time", () => {
    ensureTouchIcon();
    ensureTouchIcon();
    const links = document.head.querySelectorAll('link[rel="apple-touch-icon"]');
    expect(links).toHaveLength(1);
    expect(links[0].getAttribute("href")).toBe("/icons/apple-touch-icon.png");
  });
});

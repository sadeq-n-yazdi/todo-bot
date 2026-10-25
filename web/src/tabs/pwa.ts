// PWA parts of docs/WEB-DESIGN.md section 7.5: the service worker, its update
// banner, the Install button and the iOS hint.

import { useEffect, useState } from "preact/hooks";

export interface PwaState {
  /** A new service worker is waiting. */
  updateReady: boolean;
  /** The browser can show its install dialog (Chrome and Edge). */
  canInstall: boolean;
  /** Show the "Share, then Add to Home Screen" hint (iOS Safari, one time). */
  iosHint: boolean;
}

let state: PwaState = { updateReady: false, canInstall: false, iosHint: false };
const listeners = new Set<() => void>();
let waiting: ServiceWorker | null = null;
let watching = false;
let deferredPrompt: (Event & { prompt(): Promise<void> }) | null = null;

function set(patch: Partial<PwaState>): void {
  state = { ...state, ...patch };
  listeners.forEach((fn) => fn());
}

export function getPwaState(): PwaState {
  return state;
}

/** For tests. */
export function resetPwa(): void {
  state = { updateReady: false, canInstall: false, iosHint: false };
  waiting = null;
  watching = false;
  deferredPrompt = null;
}

export function usePwa(): PwaState {
  const [s, setS] = useState(state);
  useEffect(() => {
    const fn = () => setS(state);
    listeners.add(fn);
    fn();
    return () => {
      listeners.delete(fn);
    };
  }, []);
  return s;
}

// The browser can send this event before the app has loaded its data, so the
// listener starts when the module loads.
if (typeof window !== "undefined") {
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    deferredPrompt = e as Event & { prompt(): Promise<void> };
    set({ canInstall: true });
  });
  window.addEventListener("appinstalled", () => {
    deferredPrompt = null;
    set({ canInstall: false });
  });
}

export async function promptInstall(): Promise<void> {
  const p = deferredPrompt;
  if (!p) return;
  deferredPrompt = null; // the browser allows one use of each event
  set({ canInstall: false });
  await p.prompt();
}

/**
 * Register /sw.js (the PWA only; the Mini App at /tg/ has no service worker).
 * A new version waits until the user taps Reload (updateReady). Then the page
 * tells the waiting worker to take over and reloads when it has.
 */
export async function registerServiceWorker(
  container: ServiceWorkerContainer | undefined = typeof navigator === "undefined" ? undefined : navigator.serviceWorker,
): Promise<void> {
  if (!container) return;
  let reg: ServiceWorkerRegistration;
  try {
    reg = await container.register("/sw.js", { scope: "/" });
  } catch {
    return; // the app works without it
  }
  const flag = (w: ServiceWorker | null) => {
    // With no controller this is the first install, not an update.
    if (w && container.controller) {
      waiting = w;
      set({ updateReady: true });
    }
  };
  // A PWA can stay open for days, so look for a new version each time it comes back.
  if (!watching) {
    watching = true;
    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") reg.update().catch(() => {});
    });
  }
  flag(reg.waiting);
  reg.addEventListener("updatefound", () => {
    const w = reg.installing;
    w?.addEventListener("statechange", () => {
      if (w.state === "installed") flag(w);
    });
  });
}

/** The Reload button of the update banner. */
export function applyUpdate(container: ServiceWorkerContainer | undefined = navigator.serviceWorker): void {
  const w = waiting;
  if (!w || !container) {
    location.reload();
    return;
  }
  container.addEventListener("controllerchange", () => location.reload(), { once: true });
  w.postMessage({ type: "SKIP_WAITING" });
}

const IOS_FLAG = "todo.ios-hint";

/** Show the iOS hint one time, in Safari and not yet installed. */
export function checkIosHint(ua: string = navigator.userAgent, standalone: boolean = isStandalone()): void {
  if (!/iPad|iPhone|iPod/.test(ua) || standalone) return;
  try {
    if (localStorage.getItem(IOS_FLAG)) return;
    localStorage.setItem(IOS_FLAG, "1");
  } catch {
    return; // no storage: we cannot show it only one time
  }
  set({ iosHint: true });
}

export function dismissIosHint(): void {
  set({ iosHint: false });
}

function isStandalone(): boolean {
  return (navigator as Navigator & { standalone?: boolean }).standalone === true;
}

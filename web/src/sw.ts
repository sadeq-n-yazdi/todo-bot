// The service worker (docs/WEB-DESIGN.md section 7.5). It caches the app shell
// only. It never handles /api/, /auth/ or /tg/, and it never keeps API data.
//
// The build (the todo-sw plugin in vite.config.ts) replaces SHELL_JSON with
// {"version": "<hash>", "files": ["/assets/...", "/manifest.webmanifest", ...]}.
// The file has no import and no export, because the browser loads it as a
// classic script.

interface Ev {
  waitUntil(p: Promise<unknown>): void;
}
interface FetchEv extends Ev {
  request: Request;
  respondWith(r: Promise<Response> | Response): void;
}
interface MsgEv {
  data?: { type?: string };
}
interface Scope {
  addEventListener(type: string, fn: (e: never) => void): void;
  skipWaiting(): Promise<void>;
  clients: { claim(): Promise<void> };
  location: { origin: string };
}

const sw = self as unknown as Scope;

const SHELL_JSON = "__SHELL_JSON__";

function readShell(): { version: string; files: string[] } {
  try {
    const s = JSON.parse(SHELL_JSON) as { version?: string; files?: string[] };
    return { version: String(s.version ?? "dev"), files: s.files ?? [] };
  } catch {
    return { version: "dev", files: [] }; // not built by the plugin (a test)
  }
}

const shell = readShell();
const PREFIX = "shell-";
const CACHE = PREFIX + shell.version;

// Paths that the worker leaves to the browser.
const SKIP = ["/api/", "/auth/", "/tg/"];

sw.addEventListener("install", ((e: Ev) => {
  // No skipWaiting here. A new version waits until the page asks for it.
  e.waitUntil(
    caches.open(CACHE).then((c) =>
      // "reload" skips the HTTP cache, so the shell and its hashed files match.
      c.addAll(["/", ...shell.files].map((u) => new Request(u, { cache: "reload" }))),
    ),
  );
}) as (e: never) => void);

sw.addEventListener("activate", ((e: Ev) => {
  e.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k.startsWith(PREFIX) && k !== CACHE).map((k) => caches.delete(k))))
      .then(() => sw.clients.claim()),
  );
}) as (e: never) => void);

sw.addEventListener("message", ((e: MsgEv) => {
  if (e.data?.type === "SKIP_WAITING") void sw.skipWaiting();
}) as (e: never) => void);

sw.addEventListener("fetch", ((e: FetchEv) => {
  const req = e.request;
  if (req.method !== "GET") return;
  const url = new URL(req.url);
  if (url.origin !== sw.location.origin) return;
  if (SKIP.some((p) => url.pathname.startsWith(p))) return;

  // The page "/": network first, the saved copy when offline.
  if (req.mode === "navigate") {
    if (url.pathname !== "/" && url.pathname !== "/index.html") return;
    e.respondWith(
      fetch(req).catch(async () => (await caches.match("/", { cacheName: CACHE })) ?? Response.error()),
    );
    return;
  }

  // Hashed files: cache first. The manifest and the icons: the copy of this version.
  if (url.pathname.startsWith("/assets/") || url.pathname.startsWith("/icons/") || url.pathname === "/manifest.webmanifest") {
    e.respondWith(
      caches.match(req, { cacheName: CACHE }).then(async (hit) => {
        if (hit) return hit;
        const res = await fetch(req);
        // A hashed file never changes, so the copy is safe to keep.
        if (res.ok && url.pathname.startsWith("/assets/")) {
          const copy = res.clone();
          e.waitUntil(caches.open(CACHE).then((c) => c.put(req, copy)));
        }
        return res;
      }),
    );
  }
}) as (e: never) => void);

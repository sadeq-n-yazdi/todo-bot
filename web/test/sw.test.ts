import { readFileSync } from "node:fs";
import { stripTypeScriptTypes } from "node:module";
import { join } from "node:path";
import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";

// Runs src/sw.ts as a classic script, the way the browser runs /sw.js, with a
// fake worker scope and a fake Cache Storage. The build puts the file list in
// place of "__SHELL_JSON__"; the test does the same.
const source = readFileSync(join(__dirname, "..", "src", "sw.ts"), "utf8");
const origin = "https://todo.test";

type Handler = (e: unknown) => void;

function makeWorker(shell = { version: "v1", files: ["/assets/app-abc.js", "/assets/style-abc.css", "/manifest.webmanifest"] }) {
  const store = new Map<string, Map<string, Response>>();
  const handlers: Record<string, Handler> = {};
  const key = (x: string | { url: string }) => new URL(typeof x === "string" ? x : x.url, origin).pathname;
  const fetched: string[] = [];
  let online = true;

  const caches = {
    open: async (name: string) => {
      const c = store.get(name) ?? new Map<string, Response>();
      store.set(name, c);
      return {
        addAll: async (reqs: { url: string; cache?: string }[]) => {
          for (const r of reqs) {
            expect(r.cache).toBe("reload");
            c.set(key(r), new Response("shell " + r.url));
          }
        },
        put: async (r: { url: string }, res: Response) => void c.set(key(r), res),
      };
    },
    keys: async () => [...store.keys()],
    delete: async (n: string) => store.delete(n),
    match: async (r: string | { url: string }, o?: { cacheName?: string }) => store.get(o?.cacheName ?? "")?.get(key(r))?.clone(),
  };

  const self = {
    addEventListener: (t: string, fn: Handler) => (handlers[t] = fn),
    skipWaiting: vi.fn(async () => {}),
    clients: { claim: vi.fn(async () => {}) },
    location: { origin },
  };
  class FakeRequest {
    url: string;
    cache?: string;
    constructor(u: string, init?: { cache?: string }) {
      this.url = new URL(u, origin).href;
      this.cache = init?.cache;
    }
  }
  const fetchFn = async (r: { url: string }) => {
    fetched.push(key(r));
    if (!online) throw new TypeError("offline");
    return new Response("net " + r.url);
  };

  const code = stripTypeScriptTypes(source).replace('"__SHELL_JSON__"', JSON.stringify(JSON.stringify(shell)));
  runInNewContext(code, { self, caches, Request: FakeRequest, Response, URL, fetch: fetchFn, JSON });

  function fetchEvent(url: string, init: { method?: string; mode?: string } = {}) {
    const waits: Promise<unknown>[] = [];
    let answer: Promise<Response> | Response | undefined;
    const e = {
      request: { url: new URL(url, origin).href, method: init.method ?? "GET", mode: init.mode ?? "no-cors" },
      respondWith: (r: Promise<Response> | Response) => (answer = r),
      waitUntil: (p: Promise<unknown>) => waits.push(p),
    };
    handlers.fetch(e);
    return { handled: answer !== undefined, text: async () => (await answer)!.text(), waits };
  }
  function lifecycle(type: "install" | "activate") {
    const waits: Promise<unknown>[] = [];
    handlers[type]({ waitUntil: (p: Promise<unknown>) => waits.push(p) });
    return Promise.all(waits);
  }
  return { store, self, handlers, fetched, fetchEvent, lifecycle, setOnline: (v: boolean) => (online = v) };
}

describe("service worker", () => {
  it("is a classic script: no import and no export", () => {
    expect(stripTypeScriptTypes(source)).not.toMatch(/^\s*(import|export)\s/m);
  });

  it("puts the page and the shell files in the cache of its version, and does not skip waiting", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    expect([...w.store.get("shell-v1")!.keys()].sort()).toEqual(["/", "/assets/app-abc.js", "/assets/style-abc.css", "/manifest.webmanifest"]);
    expect(w.self.skipWaiting).not.toHaveBeenCalled();
  });

  it("deletes the shell caches of old versions on activate, and no other cache", async () => {
    const w = makeWorker();
    w.store.set("shell-old", new Map());
    w.store.set("other", new Map());
    await w.lifecycle("install");
    await w.lifecycle("activate");
    expect([...w.store.keys()].sort()).toEqual(["other", "shell-v1"]);
    expect(w.self.clients.claim).toHaveBeenCalled();
  });

  it("skips waiting only when the page asks", () => {
    const w = makeWorker();
    w.handlers.message({ data: { type: "other" } });
    expect(w.self.skipWaiting).not.toHaveBeenCalled();
    w.handlers.message({ data: { type: "SKIP_WAITING" } });
    expect(w.self.skipWaiting).toHaveBeenCalledOnce();
  });

  it("does not handle /api/, /auth/ or /tg/", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    for (const p of ["/api/me", "/api/chats/1/items?page=0", "/auth/telegram?id=1", "/tg/", "/tg/index.html", "/api/i18n?lang=en"]) {
      expect(w.fetchEvent(p).handled, p).toBe(false);
      expect(w.fetchEvent(p, { mode: "navigate" }).handled, p + " (navigate)").toBe(false);
    }
  });

  it("does not handle other methods, other origins or other pages", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    expect(w.fetchEvent("/assets/app-abc.js", { method: "POST" }).handled).toBe(false);
    expect(w.fetchEvent("https://elsewhere.test/assets/app-abc.js").handled).toBe(false);
    expect(w.fetchEvent("/sw.js").handled).toBe(false);
    expect(w.fetchEvent("/healthz").handled).toBe(false);
    expect(w.fetchEvent("/other", { mode: "navigate" }).handled).toBe(false);
  });

  it("loads the page from the network first, and from the cache when offline", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    expect(await w.fetchEvent("/", { mode: "navigate" }).text()).toBe("net https://todo.test/");
    w.setOnline(false);
    expect(await w.fetchEvent("/", { mode: "navigate" }).text()).toBe("shell https://todo.test/");
    expect(await w.fetchEvent("/?a=1", { mode: "navigate" }).text()).toBe("shell https://todo.test/");
  });

  it("serves hashed files from the cache first", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    expect(await w.fetchEvent("/assets/app-abc.js").text()).toBe("shell https://todo.test/assets/app-abc.js");
    expect(await w.fetchEvent("/manifest.webmanifest").text()).toBe("shell https://todo.test/manifest.webmanifest");
    expect(w.fetched).toEqual([]);
  });

  it("keeps a hashed file that was not in the list, after the first fetch", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    const ev = w.fetchEvent("/assets/late-def.js");
    expect(await ev.text()).toBe("net https://todo.test/assets/late-def.js");
    await Promise.all(ev.waits);
    expect(w.store.get("shell-v1")!.has("/assets/late-def.js")).toBe(true);
  });

  it("never keeps a file outside /assets/ after a fetch", async () => {
    const w = makeWorker();
    await w.lifecycle("install");
    const ev = w.fetchEvent("/icons/new.png");
    await ev.text();
    await Promise.all(ev.waits);
    expect(w.store.get("shell-v1")!.has("/icons/new.png")).toBe(false);
  });

  it("works with no build data, for example in the dev server", async () => {
    const w = makeWorker({ version: "dev", files: [] });
    await w.lifecycle("install");
    expect([...w.store.keys()]).toEqual(["shell-dev"]);
  });
});

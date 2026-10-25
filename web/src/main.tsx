import { render } from "preact";
import { App, type Mode } from "./app";
import { loadFont } from "./font";
import { applyLang, dirFor } from "./i18n";
import { applyTheme } from "./theme";
import "./styles.css";

// The minimal part of the Telegram script that the shell uses.
interface TelegramWebApp {
  initData: string;
  themeParams: Record<string, string>;
  ready(): void;
  expand(): void;
  onEvent(name: string, fn: () => void): void;
  initDataUnsafe?: { user?: { language_code?: string } };
}

declare global {
  interface Window {
    Telegram?: { WebApp?: TelegramWebApp };
  }
}

function start(): void {
  loadFont();
  const tg = window.Telegram?.WebApp;
  // /tg/ is the Mini App. The PWA at / never loads the Telegram script.
  const mode: Mode = location.pathname.startsWith("/tg/") ? "miniapp" : "pwa";

  if (mode === "miniapp" && tg) {
    tg.ready();
    tg.expand();
    applyTheme(tg.themeParams);
    tg.onEvent("themeChanged", () => applyTheme(tg.themeParams));
  }

  // Before the session is known: the Telegram language, or the browser language.
  applyLang(
    (mode === "miniapp" && tg?.initDataUnsafe?.user?.language_code) || navigator.language || "en",
    dirFor((mode === "miniapp" && tg?.initDataUnsafe?.user?.language_code) || navigator.language),
  );

  const screen = new URLSearchParams(location.search).get("a") === "signout" ? "signout" : undefined;
  render(<App mode={mode} initData={tg?.initData ?? ""} screen={screen} />, document.getElementById("app")!);
}

start();

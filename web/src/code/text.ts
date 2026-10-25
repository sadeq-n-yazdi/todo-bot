// Text of the login screens (web.login.*). The catalog of GET /api/i18n is
// public, but it can be missing (offline, first start). Then these English
// strings stand in, so that no screen shows a bare key.

import { t } from "../i18n";

const english: Record<string, string> = {
  "web.login.title": "Sign in",
  "web.login.steps":
    "1. In Telegram, open the chat with the bot.\n2. Tap the Open button next to the message field. The web app of the bot opens.\n3. In the web app, tap the ⚙ button (Settings) at the top. Do not open the settings of Telegram.\n4. Tap Sign in a browser. The web app shows a code.\n5. Type the code here, then tap Sign in.",
  "web.login.open": "Open the app in Telegram",
  "web.login.code_label": "Code",
  "web.login.submit": "Sign in",
  "web.login.busy": "Checking the code...",
  "web.login.code_title": "Sign in a browser",
  "web.login.code_warning": "Type this code only into your own browser. Never send it to another person.",
  "web.login.code_left": "Time left: %s",
  "web.login.code_expired": "The code expired.",
  "web.login.code_cancel": "Cancel the code",
  "web.login.code_canceled": "The code is canceled.",
  "web.login.code_new": "Get a new code",
  "web.login.signout_title": "Log out everywhere",
  "web.login.signout_text": "This logs out every browser and app that is signed in to your account, this one too.",
  "web.login.signout_ok": "Log out",
  "web.login.signout_done": "You are logged out everywhere.",
  "web.app.error": "Something went wrong.",
};

/** The string of a key, from the catalog, else in English. */
export function tr(key: string, ...args: (string | number)[]): string {
  const got = t(key, ...args);
  if (got !== key) return got;
  let i = 0;
  return (english[key] ?? key).replace(/%[sd]/g, () => String(args[i++] ?? ""));
}

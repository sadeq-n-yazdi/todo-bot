// Shapes of the API responses (docs/WEB-DESIGN.md section 5.3).

export interface User {
  id: number;
  username: string;
  lang: string;
  calendar: string;
  tz: string;
  dir: "ltr" | "rtl";
}

export interface Me {
  user: User;
  session: { kind: "miniapp" | "browser"; expires_at: string };
  bot: { username: string };
}

export interface MiniAppSession {
  token: string;
  expires_at: string;
  user: { id: number };
  start: string;
}

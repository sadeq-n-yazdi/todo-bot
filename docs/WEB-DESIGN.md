# Web UI design (#102)

This document is the design of the web UI of `todo-bot`. One single-page app (SPA) runs in two places:

- a **Telegram Mini App**, at `https://todo.sadeq.uk/tg/`, opened from the bot;
- an **installable web app (PWA)**, at `https://todo.sadeq.uk/`, for desktop, Android and iOS.

Read `docs/ARCHITECTURE.md` first. This document uses its section numbers (§3, §9, §11, §12). The sub-issues in §14 implement this design. Each sub-issue owns only the files that §14 lists for it.

## 1. Goals and non-goals

### Goals

- Show three tabs:
  - **Mine**: the open items of the user's private chat.
  - **Assigned to me**: the open items that other chats assigned to the user (§11).
  - **Groups**: the open items of each group that the user is a member of now.
- Let the user add an item, mark it done, delete it, ask for a reminder later and, after #98, edit it.
- Apply the same rules as the bot: the allowlist, the item authorisation of §11, the locales and the calendars of §3a.
- Keep one binary, one process and one SQLite file. The web is off unless `TODO_WEB_ADDR` has a value.
- Keep the cost small on vps03: at most 2 MiB more binary size and at most 8 MiB more idle memory.

### Non-goals

- Offline changes. The PWA shows the last loaded lists, but it cannot change them offline.
- Web push notifications. Reminders stay in Telegram.
- The step-by-step wizard of §12. The web has a form.
- Time zone and digest settings. The bot keeps them. The web lets the user change only the language and the calendar.
- A list of done items, and undo.
- Server-sent events and WebSockets (see §10).
- Accounts without Telegram, and passwords.
- The Caddy configuration. It is in a separate repository. §9 gives only the contract.
- The move of the bot from vps00 to vps03. It is a separate operations step.

## 2. Overview

```
  Telegram app (webview)        Telegram Web            Browser or installed PWA
  Mini App /tg/                 iframe of /tg/          /
         \                           |                          /
          \__________________________|_________________________/
                                     |  HTTPS
                          Cloudflare proxy (orange cloud)
                                     |  HTTPS
                     vps03 :443  Caddy (TLS with DNS-01, routes by host name)
                                     |  HTTP on loopback
  +----------------------- todo-bot process, 127.0.0.1:8088 ------------------------+
  |                                                                                 |
  |  internal/web   http.Server                                                     |
  |    /healthz       liveness                                                      |
  |    / /tg/ /assets  static files from go:embed                                   |
  |    /api/...        JSON API                                                     |
  |      auth: initData, login code, Login Widget  -> web_sessions                  |
  |      group membership: chat_members + getChatMember                             |
  |            |                                                                    |
  |            v                                                                    |
  |  service.Service + service.Web  -->  store.Store (SQLite, 1 connection)         |
  |            ^                              ^                                     |
  |            |                              |                                     |
  |  handler <-+-- lease "poller" -- long polling <--- Telegram Bot API             |
  |  scheduler ------------------------------------------> sends reminders          |
  +---------------------------------------------------------------------------------+
```

`internal/web` is a new package in the same process. It calls `service.Service` in the same way as `internal/handler`. Thus a change from the web wakes the scheduler of this process, and no second process opens the SQLite file.

The dependency direction becomes `handler/scheduler/web → service → store, recur, nlparse → model`. `web` also imports `ui` (display strings), `i18n`, `nlparse`, `tg` and `clock`. It does not import `handler`.

## 3. Authentication and sessions

### 3.1 Rules

- There are no passwords. Telegram proves who the user is, in one of three ways:
  - **Mini App `initData`** (phase 1). Telegram signs it with a key that comes from the bot token.
  - **Login code** (phase 2). The PWA shows a code. The user confirms it in the Mini App.
  - **Telegram Login Widget** (phase 4). This is for desktop browsers.
- After a proof, the server checks `TODO_ALLOWED_USERS`. Then it creates a session.
- Sessions live only in SQLite (`web_sessions`), never in memory (§9).
- The server checks the allowlist again on **every** API request, not only at login.

### 3.2 Mini App: verify `initData`

The Mini App reads `Telegram.WebApp.initData`. This is the raw query string that Telegram gives to the page. The app sends it once, at start, to `POST /api/auth/miniapp`. The server does these steps:

1. Refuse a body larger than 4 KiB, or an `init_data` value that is empty.
2. Parse `init_data` with `url.ParseQuery`. Refuse it if a key occurs more than once.
3. Remove the `hash` field and keep its value. Keep every other field, `signature` included.
4. Sort the other keys by byte order. Make the data-check string: each `key=value` (the decoded value), joined with `"\n"`, with no line feed at the end.
5. Calculate `secret = HMAC_SHA256(key = []byte("WebAppData"), message = []byte(botToken))`.
6. Calculate `want = HMAC_SHA256(key = secret, message = []byte(dataCheckString))`.
7. Decode `hash` from hex. Compare it with `want` through `hmac.Equal`. If they differ, answer 401 `bad_signature`.
8. Read `auth_date` (Unix seconds). If it is older than 1 hour, answer 401 `expired`. If it is more than 60 seconds in the future, also answer 401 `expired`.
9. Parse the `user` field as JSON. Refuse it if `id` is not a positive integer.
10. If `user.id` is not in `TODO_ALLOWED_USERS`, answer 403 `not_allowed`. The body also gives the user ID, so that the user can send it to the owner.
11. Call `Service.Prefs(id, user.language_code)`. This creates the user on first contact, as the bot does.
12. Call `Service.NoteUsername(id, user.username)`. A failure gives only a warning in the log.
13. Create a session of kind `miniapp` (§3.5) and return its token in the JSON body.

`start_param` is a navigation hint only, and it never grants access:

- `g<digits>` or `g-<digits>` opens the tab of that group. The membership check (§4) still applies.
- `login_<code>` opens the approval screen of §3.3.

The test suite signs its own `initData` with a test token, through the steps above. Before the phase 1 PR merges, the developer also checks one real `initData` from the development bot by hand. The PR records the result. The log shows only the result, never the data.

> **Note:** Telegram added the `signature` field in Bot API 8.0. It is for checks with a public key, and this design does not use it. For the HMAC check, all fields except `hash` go into the data-check string. The manual check with real data must confirm this.

### 3.3 Login code: sign in a browser through the Mini App

The Login Widget is weak in an installed PWA on iOS. The PWA there has its own storage, and the widget popup does not return to it. Thus the main way to sign in a browser is a code that the user confirms in the Mini App. This works on every device that has Telegram, and it needs no BotFather `/setdomain`.

1. The PWA calls `POST /api/auth/code`. The server creates a row in `web_login_codes`:
   - `code`: 8 characters from the Crockford base32 alphabet (40 bits), shown as `ABCD-EFGH`;
   - `poll_token`: 32 random bytes, base64url;
   - `device`: the `User-Agent` header, cut to 120 characters;
   - an expiry 5 minutes after now.

   The row keeps only the SHA-256 of the code and of the poll token.
2. The PWA shows the code and a button. The button opens `https://t.me/<bot>?startapp=login_<code>`.
3. Telegram opens the Mini App. The Mini App signs in with `initData` (§3.2).
4. The Mini App calls `GET /api/auth/code/{code}`. It shows the device text, the code and a warning: "Confirm only if you started this on your own device."
5. **Confirm** calls `POST /api/auth/code/{code}/approve`. The store sets `user_id` only where `user_id IS NULL` and the code is live. Thus only one approval can win.
6. **Cancel** calls `DELETE /api/auth/code/{code}`.
7. While this occurs, the PWA calls `POST /api/auth/code/poll` with its `poll_token` every 2 seconds:
   - `202 {"status":"pending"}` while nobody approved the code;
   - `200` after the Mini App approved the code. In one conditional delete, the server removes the row. Then it checks the allowlist, creates a `browser` session and sets the cookie. Only one poll can win the delete;
   - `410 gone` if the code expired or the user cancelled it.

The code itself is not a secret that gives a session. Only the holder of the `poll_token` gets the session, and that token has 256 bits.

### 3.4 Telegram Login Widget (phase 4, desktop)

The widget is a quick way for a desktop browser with no Telegram app. It uses the **redirect mode** (`data-auth-url`), not the popup mode. The PWA does not show it in an installed iOS app (`navigator.standalone`).

1. The login screen loads `https://telegram.org/js/telegram-widget.js?22` with `data-telegram-login="<bot>"` and `data-auth-url="https://todo.sadeq.uk/auth/telegram"`.
2. Telegram sends the browser to `GET /auth/telegram?id=…&first_name=…&username=…&photo_url=…&auth_date=…&hash=…`.
3. The server removes `hash`, and sorts and joins the other query fields as in §3.2 step 4.
4. It calculates `secret = SHA256(botToken)` and `want = HMAC_SHA256(key = secret, message = dataCheckString)`. Then it compares `hash` through `hmac.Equal`.
5. It applies the age limit of §3.2 step 8 and the allowlist of step 10.
6. It creates a `browser` session, sets the cookie, and answers `303 See Other` to `/`.
7. On an error, it answers `303` to `/#login-error=<code>`. It never puts the received fields in the URL or in the log.

The widget needs BotFather `/setdomain todo.sadeq.uk` (§16).

### 3.5 Session tokens

- **Format.** `tb1_` and then 32 random bytes from `crypto/rand`, as base64url without padding. That is 47 characters. The prefix lets the server refuse other strings fast, and it lets secret scanners find leaked tokens.
- **Storage.** The `web_sessions.id` column holds the hex SHA-256 of the token. The database never holds the token itself, so a copy of the database gives no session.
- **Transport.**
  - Mini App: the header `Authorization: Bearer tb1_…`. Telegram Web runs the Mini App in an iframe, and the browser blocks third-party cookies there. The app keeps the token in memory and in `sessionStorage`, so that a reload in the same webview keeps it. It signs in again with fresh `initData` at each start.
  - Browser and PWA: the cookie `__Host-tb_session=tb1_…; Path=/; Secure; HttpOnly; SameSite=Strict; Max-Age=<seconds>`. The `__Host-` prefix makes the browser refuse the cookie unless it is `Secure`, has `Path=/` and has no `Domain`.
  - If a request has both, the header wins.
- **Lifetimes.**

  | Kind | Idle limit | Absolute limit | Slides |
  |---|---|---|---|
  | `miniapp` | 12 h | 12 h | no |
  | `browser` | `TODO_WEB_SESSION_TTL`, default 30 days | `TODO_WEB_SESSION_MAX`, default 90 days | yes |

- **Expiry update.** For a `browser` session, the server writes `seen_at = now` and `expires_at = min(now + idle, max_at)` only when the last write is 1 hour old or more. Then it sends the cookie again with the new `Max-Age`. Thus a busy user causes at most one write per hour.
- **Check on each request.**
  1. Read the token. If it does not start with `tb1_` or has the wrong length, answer 401 `unauthenticated`.
  2. Find the row by the hash. If there is no row, or `expires_at <= now`, or `max_at <= now`, answer 401 `unauthenticated`.
  3. If the user is not in `TODO_ALLOWED_USERS`, delete all sessions of that user, and answer 401 `session_revoked`.
  4. Do the expiry update if it is due.
- **Logout.** `POST /api/auth/logout` deletes the row of this session and clears the cookie (`Max-Age=0`). `POST /api/auth/logout-all` deletes every session of the user.
- **Allowlist revocation.**
  - The check in step 3 stops a removed user on the next request.
  - At start, the web deletes the sessions of every user who is not in `TODO_ALLOWED_USERS` (`Web.PruneWebSessions`). An allowlist change needs a restart, so this removes the old rows at once.
- **Limit.** A user has at most 20 sessions. A new session deletes the oldest ones above that limit.
- **Bot token change.** Sessions do not depend on the bot token after login. If the token leaks, the owner changes it in BotFather and deletes all sessions (§16).

### 3.6 CSRF guard

- A request with a bearer token needs no CSRF guard. The browser never adds that header by itself.
- A request with the cookie and an unsafe method (`POST`, `PUT`, `PATCH`, `DELETE`) must pass all these checks, or it gets 403 `csrf`:
  1. It has the header `X-Todo-Request: 1`. A cross-site page cannot send a custom header without a CORS preflight, and the server never answers a preflight.
  2. If `Sec-Fetch-Site` is present, it is `same-origin`.
  3. If `Origin` is present, it is equal to `TODO_WEB_ORIGIN`. At least one of `Sec-Fetch-Site` and `Origin` must be present.
  4. A request with a body has `Content-Type: application/json`.
- The server never sends `Access-Control-Allow-*` headers.
- The cookie is `SameSite=Strict`, which also stops cross-site requests.
- `GET /auth/telegram` is the only `GET` that changes state, because it creates a session. The hash protects it. A forced login can only sign the victim in to an allowlisted account of the attacker. This design accepts that risk.

## 4. Group membership

### 4.1 The problem

The bot never had to prove that a user is a member of a group. A message or a button press in the group was the proof. The web has no such proof. If the web shows the items of a group, it must first prove that the user is a member of that group now.

The bot asks Telegram only for `message` and `callback_query` updates (`tgbot.New`), and privacy mode is on (§6). Thus the bot does not see join and leave events, and it sees only commands and replies in groups. This design does not add `chat_member` or `my_chat_member` updates, because they need a change of `tg.Update`.

### 4.2 Sources of truth

1. **`getChatMember` is the only source that grants access.** A new interface `tg.MemberChecker` (§13) calls it. `tg.Sender` does not change, so the fakes of the scheduler and of the handler do not change.
2. **The `chats` table gives the candidates.** Each row of kind `group` is a group the bot knows.
3. **Updates give a hint.** An observer sees each update from an allowlisted user in a group. It writes a `chat_members` row with status `unknown` and the time in `seen_at`. A hint never grants access. It only adds a group to the candidates, also when the group has no items yet.

### 4.3 The observer

- `web.Observe(next tg.Handler, svc service.Web, allowed map[int64]bool, log) tg.Handler` wraps `handler.Handle`. `cmd/todo-bot/run.go` passes the wrapped handler to `Poller().Run`.
- It calls `next` first, so the bot does not wait. If `next` returns an error, `MarkUpdate` failed, and the observer does nothing.
- After that, for a group message or a group button press from an allowlisted user, it calls `Web.NoteMember(actor)`.
- `NoteMember` reads the row first. It writes only when there is no row or `seen_at` is 1 hour old or more. It also calls `EnsureChat` with the title from Telegram, which is never empty for a group.
- An error gives only a warning in the log.
- The observer does not change `handler/router.go`, so this is not a contract change of the handler.

### 4.4 Check of one group: `groupActor`

Every API call with a group in its path calls `groupActor(ctx, userID, chatID)` before it builds `service.Actor{UserID, ChatID: chatID, ChatKind: group, ChatTitle: <stored title>}`. The title comes from the `chats` row, never from the request. `AddTodo` calls `EnsureChat`, which writes the title. Thus an empty title from the web can erase the name of the group.

1. If `chatID >= 0`, answer 404 `not_found`.
2. Read the `chat_members` row. If its status is `member` and `checked_at` is less than `TODO_WEB_MEMBER_TTL` (default 10 minutes) old, allow the call.
3. If there is no fresh answer, call `getChatMember(chatID, userID)` with a 3-second timeout. Then use the result:

   | Telegram result | Action | API result |
   |---|---|---|
   | status `creator`, `administrator` or `member` | write `member`, `checked_at = now` | allow |
   | status `restricted` with `is_member = true` | write `member` | allow |
   | status `restricted` with `is_member = false`, `left` or `kicked` | write `left` | 403 `not_member` |
   | 400 "user not found", "member not found" or "PARTICIPANT_ID_INVALID" | write `left` | 403 `not_member` |
   | `tg.IsPermanent(err)`: an admin removed the bot, or the chat does not exist | `Web.ChatGone(chatID)`: every row of the chat becomes `left` | 404 `not_found` |
   | `APIError.MigrateTo != 0`: the group became a supergroup | call `Service.MigrateChat(chatID, newID)`, as the handler does, then check `newID` | 409 `group_moved` with `new_id` |
   | 429, 5xx, timeout or network error | write nothing | see step 4 |

4. If Telegram has a temporary error: allow the call if the row is `member` and `checked_at` is less than 1 hour old (`memberGrace`). Otherwise answer 503 `membership_unverified`.

Telegram says that `getChatMember` is "guaranteed to work for other users" only when the bot is an administrator. In practice, it works for the members of small groups. If it fails for a real member, that member gets 403 until the owner makes the bot an administrator (§15, D7).

### 4.5 The list of groups: `GET /api/chats`

1. Collect the candidates: every group in `chats` (`Web.GroupChats`, at most 50), and every `chat_members` row of the user.
2. For each candidate, use the row if it is fresh. A `left` row is fresh for 24 hours (`leftTTL`), so the server does not ask again for each group the user has left.
3. Check the other candidates as in §4.4 step 3. Do at most 4 checks at the same time, and stop all checks after 5 seconds.
4. Return the groups with status `member`, with their stored titles, sorted by title.
5. If a temporary error stopped some checks, the groups in their grace window stay in the list. The response then has `"partial": true`, and the app shows a small note.

Each pair of user and group causes at most one `getChatMember` call per 10 minutes. A family has a few groups, so this is far below the Telegram limits.

### 4.6 Edge cases

- **The user left, or an admin removed the user.** The next check after `TODO_WEB_MEMBER_TTL` sees `left` or `kicked`. The user loses access within 10 minutes, or within 1 hour while Telegram has an outage.
- **An admin removed the bot from the group.** `getChatMember` returns a permanent error, and `ChatGone` marks every row of the group as `left`. The items stay in the database, as they do today.
- **Group to supergroup.** The handler moves the chat when it sees the `migrate_to_chat_id` message (§6). The web does not change `MigrateChat`. Old rows with the old ID get a `MigrateTo` error on their next check. The new ID is in `chats`, so the next `GET /api/chats` checks it.
- **A user removed from the allowlist.** §3.5 stops the user before any membership check.
- **Private chats.** The user's own private chat (`chatID == userID`) needs no membership check. A private chat of another user is always 404.

### 4.7 Privacy

- The web shows the items of a group only to a confirmed member. Telegram must confirm the membership within `TODO_WEB_MEMBER_TTL`, or within the 1-hour grace when Telegram is down.
- For a group, `GET /api/chats/{chat}/members` returns the same list as the assignee step of the wizard: the named, allowlisted users. This is the same as in the bot (§12).

## 5. API

### 5.1 Conventions

- **Format.** All requests and responses are JSON in UTF-8.
  - Times are RFC 3339 in UTC.
  - IDs are JSON numbers. Telegram IDs have at most 52 bits, so JavaScript keeps them exact.
- **Body.** At most 16 KiB. The decoder refuses unknown fields.
- **Errors.** `{"error": {"code": "not_member", "message": "<text in the user's language>"}}`. The `message` comes from the `web.err.<code>` strings.
- **Locale.** The server reads `Service.Prefs` of the user on each request. All display strings use the user's language, calendar and time zone.
- **`{chat}` is the chat of the actor.** It is the user's own ID for **Mine** and for **Assigned to me**, and a group ID for **Groups**. For an assigned item, the actor is the user's private chat, because §11 allows the assignee to act only from there (`a.ChatID == a.UserID`).
- **Auth column.** "S" means that a session is necessary. "C" means that the CSRF guard of §3.6 applies to cookie sessions. "MA" means that the endpoint accepts only a `miniapp` session.
- **Cache.** API responses have `Cache-Control: no-store`, except the lists. The lists have `Cache-Control: private, no-cache`, a weak `ETag` and `Vary: Authorization, Cookie`. A request with an `If-None-Match` equal to the ETag gets `304`. The ETag is the first 16 hex characters of the SHA-256 of `items`, `page` and `pages`. It does not include `rendered_at`.

### 5.2 The item object

```json
{
  "id": 42,
  "chat_id": -1001234567890,
  "chat_title": "Family",
  "num": 3,
  "text": "buy milk",
  "due_at": "2026-10-25T17:00:00Z",
  "due_text": "tomorrow 18:00",
  "due_full": "Sun 25 Oct 2026 18:00",
  "overdue": false,
  "recur": "weekly sun 18:00",
  "recur_text": "every Sun",
  "assignee": {"id": 22222, "username": "wife"},
  "created_by": 11111
}
```

- `due_at`, `due_text` and `due_full` are `null` when the item has no date.
- `due_text` is `ui.FormatWhen` in the user's locale, time zone and calendar. It is relative to now, for example "today 18:00".
- `due_full` is an absolute form: weekday, day, month name, year and time. It is for offline views, where the relative form is out of date. `web/format.go` builds it from `calendar.Kind.Split`, `Locale.MonthName`, `Locale.WeekdayName` and `Locale.Num`.
- `recur_text` is `ui.DescribeRule`. It is `""` when the item does not repeat.
- `assignee` is `null` when the item has no assignee. `username` is `""` when the bot does not know it. The app then shows the `web.item.noname` string.
- Only the **Assigned to me** list fills `chat_title`.
- Text values are plain text, not HTML. The app shows them as text and never as HTML.

A list response is `{"items": [...], "page": 0, "pages": 3, "rendered_at": "<RFC 3339>"}`. The page size is `ui.PageSize` (10).

### 5.3 Endpoints

| # | Method and path | Auth | Request | Response | Errors | Calls | Phase |
|---|---|---|---|---|---|---|---|
| 1 | `GET /healthz` | none | – | 200 `ok` (text) | – | nothing | 1 |
| 2 | `GET /readyz` | none | – | 200 `ok` | 503 | `Web.GroupChats(ctx, 1)` | 1 |
| 3 | `GET /`, `/tg/`, `/assets/*`, `/manifest.webmanifest`, `/sw.js`, `/icons/*` | none | – | static files | 404 | `fs.FS` | 1 |
| 4 | `POST /api/auth/miniapp` | none | `{"init_data": "…"}` | 200 `{"token", "expires_at", "user", "start"}` | 400 `bad_request`, 401 `bad_signature`, 401 `expired`, 403 `not_allowed`, 429 | `Prefs`, `NoteUsername`, `Web.StartWebSession` | 1 |
| 5 | `POST /api/auth/logout` | S, C | – | 204 | 401 | `Web.EndWebSession` | 1 |
| 6 | `POST /api/auth/logout-all` | S, C | – | 204 | 401 | `Web.EndUserWebSessions` | 1 |
| 7 | `GET /api/me` | S | – | 200 `{"user": {"id", "username", "lang", "calendar", "tz", "dir"}, "session": {"kind", "expires_at"}, "bot": {"username"}}` | 401 | `Prefs`, `Web.User` | 1 |
| 8 | `GET /api/i18n?lang=en` | none | – | 200 `{"lang", "dir", "messages": {"web.…": "…"}}` | 400 `bad_lang` | `i18n.Keys`, `Locale.T` | 1 |
| 9 | `GET /api/chats` | S | – | 200 `{"chats": [{"id", "kind", "title"}], "partial": false}`. The private chat is first. | 401 | `Web.GroupChats`, `Web.MembershipsOf`, `MemberChecker`, `Web.RecordMembership` | 1 (private only), 2 |
| 10 | `GET /api/chats/{chat}/items?page=N` | S | – | list | 403 `not_member`, 404, 409 `group_moved`, 503 `membership_unverified` | `groupActor`, `List` | 2 |
| 11 | `GET /api/assigned` | S | – | list, without paging, at most 200 items | 401 | `ListAssigned`, `Web.Chat` (titles) | 2 |
| 12 | `POST /api/auth/code` | none | `{}` | 201 `{"code", "poll_token", "link", "expires_at"}` | 429 | `Web.NewLoginCode` | 2 |
| 13 | `POST /api/auth/code/poll` | none | `{"poll_token"}` | 202 `{"status": "pending"}`, or 200 `{"user", "expires_at"}` and `Set-Cookie` | 403 `not_allowed`, 410 `gone`, 429 | `Web.TakeLoginCode`, `Web.StartWebSession` | 2 |
| 14 | `GET /api/auth/code/{code}` | S, MA | – | 200 `{"code", "device", "created_at", "expires_at"}` | 404, 410 | `Web.LoginCode` | 2 |
| 15 | `POST /api/auth/code/{code}/approve` | S, MA | `{}` | 204 | 404, 409 `used`, 410 | `Web.ApproveLoginCode` | 2 |
| 16 | `DELETE /api/auth/code/{code}` | S, MA | – | 204 | 404 | `Web.CancelLoginCode` | 2 |
| 17 | `POST /api/parse` | S, C | `{"text"}` | 200 `{"text", "due_at", "due_text", "recur", "recur_text"}` | 422 `empty_text`, 422 `too_long` | `nlparse.Parser.Parse` only | 3 |
| 18 | `GET /api/chats/{chat}/members` | S | – | 200 `{"members": [{"id", "username"}]}` | 403, 404 | `groupActor`, `NamedUsers` and the allowlist | 3 |
| 19 | `POST /api/chats/{chat}/items` | S, C | `{"text", "assignee_id"}` (`assignee_id` is optional, 0 = nobody) | 201 item | 422 `empty_text`, `too_long`, `unknown_assignee`; 403; 404 | `groupActor`, `AddTodoFor` | 3 |
| 20 | `POST /api/chats/{chat}/items/{id}/done` | S, C | `{}` | 200 `{"item", "still_open"}` | 404 | `Complete`, then the "did" notice | 3 |
| 21 | `DELETE /api/chats/{chat}/items/{id}` | S, C | – | 204 | 404 | `Delete` | 3 |
| 22 | `POST /api/chats/{chat}/items/{id}/remind` | S, C | `{"preset": "10m"}`, `{"preset": "1h"}`, `{"preset": "tomorrow"}` or `{"when": "<text>"}` | 201 `{"fire_at", "fire_text"}` | 404, 422 `in_past`, 422 `bad_time` | `nlparse.Parser.ParseWhen`, `Remind` | 3 |
| 23 | `PUT /api/me/settings` | S, C | `{"lang"}`, `{"calendar"}` or both | 200, as `/api/me` | 422 `bad_lang`, 422 `bad_calendar` | `SetLang`, `SetCalendar` | 3 |
| 24 | `GET /api/chats/{chat}/items/{id}` | S | – | 200 item, with `"status"` and `"when_text"` for the edit form | 404 | `Todo` | 4 |
| 25 | `PATCH /api/chats/{chat}/items/{id}` | S, C | `{"text"}`, `{"when": "<text>"}`, `{"when": null}` (no date), `{"assignee_id"}`, or a mix | 200 item | 404, 422 `empty_text`, `bad_time`, `in_past`, `unknown_assignee` | `UpdateTodo` (#98), then the assignee DM | 4 |
| 26 | `GET /auth/telegram?…` | none | the widget fields | 303 to `/` with `Set-Cookie` | 303 to `/#login-error=<code>` | `Web.StartWebSession` | 4 |

Error codes for every endpoint with a session: 401 `unauthenticated`, 401 `session_revoked`, 403 `csrf`, 413 `too_large`, 429 `rate_limited` (with `Retry-After`) and 500 `internal`.

`service.ErrForeignChat` and `service.ErrNotFound` both give 404 `not_found`. Thus a user cannot learn whether an item of another chat exists.

### 5.4 Rules of the endpoints

- **Add (19).** The server follows the same steps as `/add`:
  1. Apply `i18n.NormalizeDigits`, then `strings.TrimSpace`.
  2. Refuse more than 1000 runes (`too_long`), the same limit as `maxTodoChars` in the handler.
  3. Call `Parser.Parse(text, now)` with `now` in the user's time zone.
  4. Check `assignee_id`. It must be 0, or a user in `TODO_ALLOWED_USERS` that `NamedUsers` returns. Otherwise answer 422 `unknown_assignee`.
  5. Call `AddTodoFor(actor, assigneeID, result)`.

  The web does not split an `@name` at the start of the text. The form has an assignee picker for that (§15, D9). Text with the same words gives the same item as `/add` (acceptance test in W10).
- **Done (20).** `Complete` follows §5 of ARCHITECTURE. An item that repeats can stay open with a new date. `still_open` then is `true`, and `item` has the new date.
  - The "did" notice follows §11. The server sends it when `item.AssigneeID == userID` and `item.ChatID != actor.ChatID`.
  - It goes to `item.ChatID`, in the locale of the creator (`Prefs(item.CreatedBy)`), with the `web.tg.did` string and `ui.Mention`.
  - A send error gives only a warning in the log, and the response stays 200.
- **Remind later (22).** `Service.Snooze` takes a reminder ID, and the web has only items. Thus the web uses `Service.Remind(actor, todoID, at)`, which adds a `custom` reminder. The presets are:
  - `10m`: now + 10 minutes;
  - `1h`: now + 1 hour;
  - `tomorrow`: tomorrow at `TODO_DEFAULT_TIME` in the user's time zone, with `time.Date`;
  - `when`: the text, after `i18n.NormalizeDigits`, through `Parser.ParseWhen`, the same as `/remind`.
- **Edit (25).** This needs #98. The server builds a `service.TodoPatch`:
  - a `text` field gives `Text`;
  - a `when` text goes through `Parser.Parse` and gives `When`. `when: null` gives `When` with `Due = nil`;
  - an `assignee_id` field gives `AssigneeID`, checked as in Add.

  After a successful save, the server sends the DM to a new assignee. The rules are the rules of #98 §4 (decision 3), with the string `web.tg.assigned`. Fields that are not in the request stay `nil`, so they do not change.
- **Lists (10, 11).** `Service.List(chatID, page)` takes no actor. Thus the web must call `groupActor` before `List` for a group, and must use the user's own ID for **Mine**.
- **i18n (8).** It returns every key that starts with `web.`, in the language asked, with `Locale.T` and no arguments. That gives the raw templates. The `web.` strings can use only `%s` and `%d`. The app fills them in order. It is public, because the login screen needs it before a session exists. It has `Cache-Control: public, max-age=300` and an `ETag`.

## 6. Data and migrations

### 6.1 `migrations/sqlite/0005_web.sql`

Make sure that version `0005` is still free when W1 merges. If another migration took it, use the next free number. Never edit a merged migration.

```sql
-- Web UI (#102, docs/WEB-DESIGN.md). Times are INTEGER Unix seconds (UTC).

-- One row per web session. id is the hex SHA-256 of the token: the table
-- never holds the token itself. expires_at slides for browser sessions and never
-- passes max_at.
CREATE TABLE web_sessions (
  id         TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  kind       TEXT    NOT NULL CHECK (kind IN ('miniapp','browser')),
  device     TEXT    NOT NULL DEFAULT '', -- User-Agent, at most 120 characters
  created_at INTEGER NOT NULL,
  seen_at    INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  max_at     INTEGER NOT NULL
);
CREATE INDEX web_sessions_user ON web_sessions (user_id);
CREATE INDEX web_sessions_expires ON web_sessions (expires_at);

-- A browser login that waits for approval in the Mini App. user_id is NULL
-- until the approval. Both hashes are hex SHA-256; the table never holds
-- the code or the poll token.
CREATE TABLE web_login_codes (
  code_hash  TEXT    PRIMARY KEY,
  poll_hash  TEXT    NOT NULL UNIQUE,
  device     TEXT    NOT NULL DEFAULT '',
  user_id    INTEGER,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX web_login_codes_expires ON web_login_codes (expires_at);

-- What the web knows about a user in a group. Only getChatMember writes
-- 'member' or 'left'; an update seen in the group writes 'unknown' (a hint,
-- never access). No foreign keys, as for drafts: a group can have no chats
-- row yet, and nothing must stop MigrateChat.
CREATE TABLE chat_members (
  chat_id    INTEGER NOT NULL,
  user_id    INTEGER NOT NULL,
  status     TEXT    NOT NULL CHECK (status IN ('member','left','unknown')),
  seen_at    INTEGER NOT NULL DEFAULT 0, -- last update from the user in the chat
  checked_at INTEGER NOT NULL DEFAULT 0, -- last getChatMember answer; 0 = never
  PRIMARY KEY (chat_id, user_id)
);
CREATE INDEX chat_members_user ON chat_members (user_id, status);
```

The Postgres and MySQL providers of v0.2 must ship the same version:

- Postgres: `BIGINT` for IDs and times.
- MySQL:
  - `VARCHAR(64)` for `id`, `code_hash` and `poll_hash`, because a `TEXT` column cannot be a key without a prefix length;
  - `VARCHAR(16)` for `kind` and `status`;
  - `VARCHAR(120)` for `device`.
- No foreign keys, as in SQLite.

### 6.2 Model types (`internal/model/web.go`)

```go
type WebSessionKind string

const (
	WebMiniApp WebSessionKind = "miniapp"
	WebBrowser WebSessionKind = "browser"
)

// WebSession is a row of web_sessions. ID is the hex SHA-256 of the token.
type WebSession struct {
	ID        string
	UserID    int64
	Kind      WebSessionKind
	Device    string
	CreatedAt time.Time
	SeenAt    time.Time
	ExpiresAt time.Time
	MaxAt     time.Time
}

// LoginCode is a row of web_login_codes. UserID is 0 until approved.
type LoginCode struct {
	CodeHash  string
	PollHash  string
	Device    string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
}

type MemberStatus string

const (
	MemberYes     MemberStatus = "member"
	MemberLeft    MemberStatus = "left"
	MemberUnknown MemberStatus = "unknown"
)

// ChatMember is a row of chat_members. CheckedAt is zero if never checked.
type ChatMember struct {
	ChatID    int64
	UserID    int64
	Status    MemberStatus
	SeenAt    time.Time
	CheckedAt time.Time
}
```

### 6.3 Store methods (added to `store.Store`)

All providers share `sqlstore`, so `internal/store/sqlstore/web.go` implements these methods one time. `storetest/web.go` adds conformance cases for them.

```go
// ---- Web UI (#102, docs/WEB-DESIGN.md) ----
PutWebSession(ctx context.Context, s model.WebSession) error
// GetWebSession: ErrNotFound if none, or expires_at <= now, or max_at <= now.
GetWebSession(ctx context.Context, id string, now time.Time) (model.WebSession, error)
TouchWebSession(ctx context.Context, id string, seenAt, expiresAt time.Time) error
DeleteWebSession(ctx context.Context, id string) error
DeleteUserWebSessions(ctx context.Context, userID int64) error
// TrimUserWebSessions keeps the newest keep sessions of the user.
TrimUserWebSessions(ctx context.Context, userID int64, keep int) error
// DeleteWebSessionsExcept deletes the sessions of every user not in keep.
DeleteWebSessionsExcept(ctx context.Context, keep []int64) (n int, err error)

PutLoginCode(ctx context.Context, c model.LoginCode) error
// GetLoginCode: ErrNotFound if none or expired.
GetLoginCode(ctx context.Context, codeHash string, now time.Time) (model.LoginCode, error)
// ApproveLoginCode sets user_id only WHERE user_id IS NULL AND expires_at > now.
// ok=false if it was absent, expired or already approved; of several
// concurrent callers exactly one gets ok=true.
ApproveLoginCode(ctx context.Context, codeHash string, userID int64, now time.Time) (ok bool, err error)
// TakeLoginCode: if the code is approved and live, it deletes the row and
// returns it (exactly one concurrent caller gets it). If it is live but not
// approved, it returns it with UserID 0 and keeps it. ErrNotFound if
// absent or expired.
TakeLoginCode(ctx context.Context, pollHash string, now time.Time) (model.LoginCode, error)
DeleteLoginCode(ctx context.Context, codeHash string) error

// NoteChatMember inserts (chat, user) with status 'unknown' and seen_at,
// or updates only seen_at. It never changes status or checked_at.
NoteChatMember(ctx context.Context, chatID, userID int64, seenAt time.Time) error
// SetChatMember writes status and checked_at, and keeps seen_at.
SetChatMember(ctx context.Context, m model.ChatMember) error
GetChatMember(ctx context.Context, chatID, userID int64) (model.ChatMember, error)
// ListChatMembersOf returns every row of the user, ordered by chat_id.
ListChatMembersOf(ctx context.Context, userID int64) ([]model.ChatMember, error)
// SetChatGone sets every row of the chat to 'left' with checked_at = at.
SetChatGone(ctx context.Context, chatID int64, at time.Time) error
// ListGroupChats returns up to limit chats of kind group, ordered by id.
ListGroupChats(ctx context.Context, limit int) ([]model.Chat, error)
```

`TakeLoginCode` uses one `DELETE … WHERE poll_hash = ? AND user_id IS NOT NULL AND expires_at > ?` and reads the row in the same transaction. MySQL has no `RETURNING`, so it uses `WithTx`.

### 6.4 Retention

`Purge` also deletes:

- sessions with `expires_at <= now` or `max_at <= now`;
- login codes with `expires_at <= now`;
- `chat_members` rows with a status other than `member`, when both `seen_at` and `checked_at` are more than 30 days old.

A `member` row stays until a check finds that the user left. The tables stay small: a family has a few users, a few groups and at most 20 sessions per user.

## 7. Frontend

### 7.1 Structure

```
web/
  .nvmrc                  24 (Node LTS)
  .npmrc                  save-exact=true, ignore-scripts=true
  package.json            exact versions, no ^ or ~
  package-lock.json
  tsconfig.json
  vite.config.ts          two entries; output to ../internal/web/dist; sw.js plugin
  index.html              PWA entry ("/")
  tg/index.html           Mini App entry ("/tg/"); loads telegram-web-app.js
  public/
    manifest.webmanifest
    icons/                icon-192.png, icon-512.png, icon-maskable-512.png, apple-touch-icon.png
  src/
    main.tsx              start: mode, auth, i18n, theme, then <App/>
    app.tsx               header, tabs, hash routes, toasts
    api.ts                fetch wrapper, errors, bearer or cookie with X-Todo-Request
    auth.ts               Mini App sign-in, session token in sessionStorage
    i18n.ts               load /api/i18n, t(key, ...args), digits, dir
    theme.ts              Telegram themeParams or prefers-color-scheme
    cache.ts              last lists per user in localStorage
    tabs/                 mine, assigned, groups, list, row      (W6)
    login/                code login screen, widget              (W7, W13)
    approve/              Mini App approval screen               (W7)
    actions/              add sheet, item sheet, remind, confirm (W9)
    edit/                 edit form                              (W12)
    live.ts               refetch and ETag poll                  (W12)
    sw.ts                 service worker                         (W6)
    styles.css
  test/                   vitest tests
```

W2 makes `app.tsx` with fixed imports from `tabs/index.tsx`, `login/index.tsx`, `approve/index.tsx` and `actions/index.tsx`, as stubs. Later sub-issues replace their own stubs. Thus two parallel sub-issues never edit one file.

### 7.2 Two entries, one bundle

- `/` is the PWA. It registers the service worker. It loads no third-party script until phase 4, and then only on the login screen (the widget).
- `/tg/` is the Mini App.
  - It loads `https://telegram.org/js/telegram-web-app.js`, calls `Telegram.WebApp.ready()` and `expand()`, and signs in with `initData`.
  - It does not register a service worker.
- Both pages load the same JavaScript chunks. `main.tsx` selects the mode from the path and from `Telegram.WebApp.initData`.
- Each entry has its own CSP (§11.2).

### 7.3 Screens

| Screen | Where | Content |
|---|---|---|
| Login | PWA, no session | the code and a button "Open Telegram to confirm"; in phase 4 also the widget (not in an installed iOS app); the wait state; errors |
| Approve | Mini App, `start_param=login_…` | the device text, the code, the warning, Confirm and Cancel |
| Not allowed | both | "Your Telegram ID is N. Ask the owner to add it." |
| Tabs | both | **Mine**, **Assigned to me**, **Groups** |
| Groups | both | one chip for each group; the app keeps the last group in `localStorage`; an empty state when the user has no group |
| List | both | one row for each item: a done checkbox, the text (`dir="auto"`), `due_text` (red when `overdue`), 🔁 `recur_text`, `→ @username`; "Load more" for the next page |
| Item sheet | both | Done, Remind later (10 min, 1 h, tomorrow, custom text), Edit (phase 4), Delete with a confirmation |
| Add sheet | both | the text field, a live preview (`POST /api/parse`, 400 ms after the last key), the assignee picker, Add |
| Settings | header menu | language (en, fa), calendar (Gregorian, Jalali), Log out, Log out everywhere |

In the Mini App, the Add sheet uses `Telegram.WebApp.MainButton`, and the sheets use `BackButton`. "Done" calls `HapticFeedback.notificationOccurred("success")`.

### 7.4 Languages, RTL and theme

- **Strings.** All UI strings come from `GET /api/i18n`. Go registers them with `i18n.Register` under the `web.` prefix, in English and Farsi.
  - Each sub-issue registers its strings in its own `internal/web/messages_<area>.go` file, with its own prefix: `web.app.`, `web.err.`, `web.tabs.`, `web.login.`, `web.item.`, `web.edit.`, `web.tg.` (messages to Telegram).
  - The app keeps the last catalog in `localStorage` and sends `If-None-Match`.
  - It shows a spinner without text until the catalog arrives.
- **Digits.** The server gives every date and time with the correct digits. For numbers that the app makes itself, such as counts, `i18n.ts` converts the 10 ASCII digits to Persian digits in Farsi. That is the only locale logic in JavaScript. JavaScript never calculates calendar dates.
- **Direction.** `<html lang dir>` comes from `/api/me` (`dir` is `rtl` for Farsi). Before the session, the app uses the Telegram `language_code` (Mini App) or `navigator.language` (PWA).
  - The CSS uses only logical properties (`margin-inline-start`, `padding-inline`, `inset-inline-end`, `text-align: start`).
  - Arrow icons turn around under `[dir=rtl]`.
  - Item text has `dir="auto"`, because users mix Farsi and English.
- **Fonts.** The app uses the system fonts: `system-ui, "Segoe UI", Roboto, "Noto Sans Arabic", Tahoma, sans-serif`. The binary embeds no font file, to keep the size small.
- **Theme.**
  - All colors are CSS tokens on `:root`. A dark set applies under `prefers-color-scheme: dark`.
  - In the Mini App, the tokens use the `--tg-theme-*` variables that Telegram sets, with the PWA colors as fallback values. The app follows the `themeChanged` event.
  - The layout uses `--tg-viewport-stable-height` and the safe-area insets.

### 7.5 PWA

- **Manifest.**
  - `name` "Todo", `short_name` "Todo", `start_url` `/`, `scope` `/`, `display` `standalone`;
  - `theme_color` `#147A7A` and `background_color` `#0E3B57`, the colors of `assets/README.md`;
  - icons: 192 and 512 (`any`), and `icon-maskable-512.png` (`maskable`).
  - `assets/README.md` says that all important shapes are in the central 70% circle. Thus the current art fits the maskable safe zone. W6 adds the PNG sizes to `assets/export.sh` and commits the new PNGs.
- **iOS.** The page has `<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png">` (180 px) and `<meta name="apple-mobile-web-app-capable" content="yes">`. The app shows the "Share, then Add to Home Screen" hint one time on iOS Safari.
- **Install.** On Chrome and Edge, the app keeps the `beforeinstallprompt` event and shows an **Install** button in the settings menu.
- **Service worker** (`sw.ts`, built to `/sw.js`, scope `/`):
  - At install, it puts the shell in its cache: `/`, the hashed files in `/assets/`, the manifest and the icons. A small Vite plugin writes the file list and the build hash into `sw.js`.
  - Navigation to `/`: network first, then the cached `/` when offline.
  - `/assets/*`: cache first, because the names contain a hash.
  - `/api/*`, `/auth/*` and `/tg/*`: the service worker does not handle them.
  - A new version waits. The app shows "A new version is ready. Reload." On reload, the service worker calls `skipWaiting`.
- **Offline.** The app keeps the last response of each list in `localStorage`, under a key with the user ID. The service worker never keeps API data.
  - Offline, the app shows these lists with `due_full` instead of `due_text`, a banner with the `rendered_at` time, and no action buttons.
  - Logout, and a different user in `/api/me`, delete these entries.

### 7.6 Updates

- **Phase 2.** The app loads the current tab again on `visibilitychange` (visible), on `focus`, and on the Mini App `activated` event.
- **Phase 4.** While the page is visible, the app asks for the current list every 30 seconds with `If-None-Match`. A `304` costs one small response. The poll stops when the page is not visible or the network is offline.

## 8. Build, CI, embedding and binary size

### 8.1 Toolchain

- Node 24 LTS (`web/.nvmrc`) and npm, at build time only. The binary does not need Node.
- **Dependencies.**
  - `preact` is the only runtime dependency.
  - The dev dependencies are `vite`, `@preact/preset-vite`, `typescript`, `vitest`, `@testing-library/preact` and `jsdom`.
  - There is no CSS framework, no router library and no state library.
- **Versions.** W2 pins the newest stable version of each package that is at least 14 days old, as an exact version. It commits `package-lock.json`.
  - CI and `make web` use `npm ci --ignore-scripts`.
  - A version change is a normal PR.
- **Why Preact and Vite.**
  - Preact is about 4 KB gzipped, and it has components and hooks.
  - Vite builds two HTML entries into shared, hashed chunks. It has a fast development server with a proxy to the Go server.
  - Plain JavaScript without a framework is smaller, but it needs more code for the sheets and lists.

### 8.2 Embedding and the build tag

`go:embed` fails when the directory does not exist, and it ignores a directory with only dot files. Thus a build without Node uses a placeholder:

- `internal/web/assets_webui.go` has `//go:build webui` and `//go:embed all:dist`. Vite writes `internal/web/dist/`, and `.gitignore` excludes it.
- `internal/web/assets_placeholder.go` has `//go:build !webui` and embeds the committed `internal/web/placeholder/index.html`. That page says "The web UI is not built into this binary".
- Both files define `func Assets() fs.FS` and `func Embedded() bool`.
- `todo-bot version` adds `web: embedded` or `web: placeholder` to its output.

### 8.3 Make targets

| Target | Node | What it does |
|---|---|---|
| `make ci` | no | the same as today: vet, lint, `test -race`, check-deploy, `build-linux`. It builds the placeholder. |
| `make build-linux` | no | the same as today, with the placeholder |
| `make web` | yes | `cd web && npm ci --ignore-scripts && npm run build` |
| `make web-test` | yes | `npm run typecheck`, `npm test`, the size check |
| `make release-linux` | yes | `make web`, then `build-linux` with `-tags webui` |

A Go-only contributor runs `make ci` as today, and needs no Node.

### 8.4 CI and release

- **The `ci` job.** It does not change. It stays Go only.
- **A new `web` job.**
  1. Run `actions/setup-node` with `node-version-file: web/.nvmrc`.
  2. Run `make web-test` and `make web`.
  3. Run `go vet -tags webui ./internal/web/...` and `go test -tags webui ./internal/web/...`.
- **`release.yml`.** It adds `setup-node`, and it uses `make release-linux` for amd64 and arm64.
- **`scripts/deploy.sh`.** `build()` uses `make release-linux`. After the build, it runs `go version -m` on the binary. If `-tags=webui` is not in the build settings, it stops. Thus production never gets the placeholder.

### 8.5 Size and memory budget

| Item | Limit | Check |
|---|---|---|
| JavaScript, all chunks | 45 KB gzipped (120 KB raw) | the size check of `make web-test` |
| CSS | 10 KB gzipped | the size check |
| Icons and manifest | 150 KB | the size check |
| `internal/web/dist` total | 400 KB | the size check |
| Binary growth (`build-linux -tags webui` against `main`) | 2 MiB | W3 records both sizes in its PR |
| Idle RSS growth | 8 MiB | W3 records `VmRSS` before and after, on a local run |

- The server serves the files directly from the embedded `fs.FS`. It does not copy them to memory.
- The server does not compress responses. Caddy (`encode zstd gzip`) and Cloudflare compress them.
- `GOMEMLIMIT=40MiB` and `MemoryMax=96M` do not change. vps03 has about 149 MB of free memory and 608 MB of free disk. The binary and its `.prev` copy need about 40 MB.

## 9. Configuration and the contract with Caddy

### 9.1 Environment variables

| Variable | Default | Rule |
|---|---|---|
| `TODO_WEB_ADDR` | `""` (web off) | `host:port` with a loopback IP literal, for example `127.0.0.1:8088`. `config.Load` refuses other addresses. |
| `TODO_WEB_ORIGIN` | – | Necessary when `TODO_WEB_ADDR` has a value. An `https://` URL with no path, for example `https://todo.sadeq.uk`. |
| `TODO_WEB_SESSION_TTL` | `720h` | The idle limit of a browser session, from 1h to 2160h. |
| `TODO_WEB_SESSION_MAX` | `2160h` | The absolute limit of a browser session. At least `TODO_WEB_SESSION_TTL`. |
| `TODO_WEB_MEMBER_TTL` | `10m` | How long a `getChatMember` answer stays valid, from 1m to 1h. |
| `TODO_WEB_TRUSTED_PROXIES` | `127.0.0.1/32,::1/128` | The CIDR list of proxies whose forwarded headers the server reads. |

`config.Config` gets the fields `WebAddr`, `WebOrigin`, `WebSessionTTL`, `WebSessionMax`, `WebMemberTTL` and `WebTrustedProxies []netip.Prefix`. These values are constants in `internal/web`:

- the Mini App session: 12 h;
- the `initData` age limit: 1 h;
- the life of a login code: 5 min;
- the membership grace: 1 h.

`TODO_HTTP_ADDR` stays reserved for the webhook of v0.2 (#31). The web has its own listener now. `config.Load` refuses a `TODO_WEB_ADDR` that is equal to `TODO_HTTP_ADDR`. When #31 starts, it can serve both on one mux.

### 9.2 The listener

- `cmd/todo-bot/run.go` starts the web server only when `TODO_WEB_ADDR` has a value. It starts the server after the service exists and before the lease loop.
- At shutdown, it stops the web first, with `Server.Shutdown` and a 3-second budget. Then it stops the lease loop and the scheduler, as today.
- **Server limits.** `ReadHeaderTimeout` 5 s, `ReadTimeout` 10 s, `WriteTimeout` 15 s, `IdleTimeout` 60 s, `MaxHeaderBytes` 8 KiB. At most 64 requests run at the same time. A request above that limit gets 503.

### 9.3 The contract with Caddy

| Item | Contract |
|---|---|
| Upstream | `http://127.0.0.1:8088`, plain HTTP on loopback |
| Host | Caddy sends `Host: todo.sadeq.uk` without a change. The bot refuses another host with 421, except on `/healthz` and `/readyz`. |
| Forwarded headers | Caddy sets `X-Forwarded-For` and `X-Forwarded-Proto: https`. Cloudflare sets `CF-Connecting-IP`. |
| Client IP | Only when the TCP peer is in `TODO_WEB_TRUSTED_PROXIES`, the bot uses `CF-Connecting-IP`. If that header is absent, it uses the last entry of `X-Forwarded-For`. Otherwise it uses the TCP peer. The bot uses the client IP only for rate limits and logs. |
| Cloudflare only | Caddy accepts `todo.sadeq.uk` only from the Cloudflare IP ranges, or with Authenticated Origin Pulls. Otherwise an attacker can send a false `CF-Connecting-IP`. |
| Health | `GET /healthz` gives 200 `ok` and does not touch the database. `GET /readyz` gives 200 after one small database read, or 503. |
| Response headers | Caddy passes `Content-Security-Policy`, `Set-Cookie`, `Cache-Control` and `ETag` without a change. It adds no cache. |
| Compression | Caddy can use `encode zstd gzip`. |
| Body and time | Requests are at most 16 KiB. No request takes more than 15 s. There are no WebSockets. |
| TLS | Caddy and Cloudflare end TLS. The bot always sets `Secure` cookies and HSTS, because `TODO_WEB_ORIGIN` is `https`. |

## 10. Multi-node notes

- **State.** All web state is in the database: sessions, login codes and membership. Any node can answer any request, so Caddy can send requests to more than one node.
- **Coordination.** The web adds only these conditional writes:
  - `ApproveLoginCode` (`WHERE user_id IS NULL`): one approval wins.
  - `TakeLoginCode`, a conditional delete: one poll gets the session.
  - Session and membership writes are single-row upserts, where the last writer wins. Both values are correct, because they come from Telegram or from the same user.
- **Rate limits are an exception to §9.** Each node keeps its rate limits in memory. This is not coordination, because the limits only slow down abuse. With N nodes, the real limit is N times larger. This design accepts that.
- **No SSE.** A server-sent event to many nodes needs a shared message bus, or in-memory state on each node. The ETag poll of §7.6 works on any node and needs no new table.
- **The scheduler.** A web change calls `Service.wake()` in its own process. Other nodes see the change at their next wake-up, at most `TODO_SCHED_MAX_SLEEP` later. That is the same as for a bot command today.
- **One SQLite connection.** Web requests use the same connection as the bot. Each request makes at most 4 queries and 1 write, and no long transaction.
- **Upgrade of one node at a time.** `0005` adds tables only, so an older binary is not affected. Caddy must send web traffic only to nodes with the web on.

## 11. Security and threat model

### 11.1 Headers on every response

- `X-Content-Type-Options: nosniff`
- `Referrer-Policy: no-referrer`
- `Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=()`
- `Strict-Transport-Security: max-age=31536000`
- `Cross-Origin-Opener-Policy: same-origin` on `/` only. The widget uses the redirect mode, so it needs no popup.
- API responses: `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'`.

### 11.2 Content Security Policy of the pages

`/` (the PWA):

```
default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:;
connect-src 'self'; manifest-src 'self'; worker-src 'self'; font-src 'self';
base-uri 'none'; form-action 'self'; frame-ancestors 'none'
```

In phase 4, the login screen needs the widget. W13 then adds `https://telegram.org` to `script-src` and `frame-src https://oauth.telegram.org`. Only `/` gets them.

`/tg/` (the Mini App):

```
default-src 'none'; script-src 'self' https://telegram.org; style-src 'self';
img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none';
form-action 'self'; frame-ancestors https://web.telegram.org
```

- Telegram Web runs the Mini App in an iframe on `https://web.telegram.org` (the K and A versions). The Telegram apps use a webview, which `frame-ancestors` does not affect. W3 checks both Telegram Web versions by hand. If one of them uses another host, W3 adds that host.
- No page uses inline scripts or inline styles. Preact and the Telegram script set styles through the CSSOM, which `style-src` does not block.
- Telegram changes `telegram-web-app.js` without notice, so the page cannot use Subresource Integrity for it. Only `/tg/` loads it.

### 11.3 Limits and input rules

- **Rate limits.** Each node keeps token buckets in memory (§10). The map holds at most 10,000 keys, and the server removes the oldest keys first.

  | Endpoint | Limit |
  |---|---|
  | `POST /api/auth/miniapp` | 10 per minute per IP |
  | `POST /api/auth/code` | 5 per minute per IP |
  | `POST /api/auth/code/poll` | 60 per minute per IP |
  | `POST /api/auth/code/{code}/approve` | 10 per minute per user |
  | `GET /auth/telegram` | 10 per minute per IP |
  | all other `/api/*` with a session | 300 per minute per user |

- **Input.**
  - A body has at most 16 KiB, and the decoder refuses unknown fields.
  - Item text has at most 1000 runes, after `i18n.NormalizeDigits`.
  - Path IDs must parse as `int64`.
- **Output.** The API sends JSON only. The app shows text as text. No code calls `innerHTML` or `dangerouslySetInnerHTML`. A unit test in W2 searches the source for these names.
- **Host.** The server refuses a `Host` other than the host of `TODO_WEB_ORIGIN` (421). This stops DNS rebinding to `127.0.0.1:8088`.
- **Logs.** The log never contains a session token, `initData`, a `hash`, a cookie, an `Authorization` header, a poll token or a login code. It can contain the user ID, the method, the route pattern, the status and the duration. A test captures the log of a full login and checks that no secret is in it.

### 11.4 Threat model

| # | Threat | Attack | Control | Residual risk |
|---|---|---|---|---|
| 1 | False identity | A false `initData` or false widget fields | HMAC with a key from the bot token, compared in constant time; the age limit | Low. It needs the bot token. |
| 2 | Replay of a proof | A leaked `initData` or widget URL | Age limit of 1 h; never in logs; TLS only | Low, for 1 h |
| 3 | Session theft from a copy of the database | A backup leaks | The table holds only SHA-256 hashes of tokens | Low |
| 4 | Session theft through XSS | Script in an item text | No HTML output; strict CSP without `unsafe-inline`; `HttpOnly` cookie | Medium for the Mini App token in `sessionStorage`. The CSP is the control. |
| 5 | CSRF | A cross-site form or `fetch` with the cookie | `SameSite=Strict`, the `X-Todo-Request` header, `Origin` and `Sec-Fetch-Site` checks, no CORS | Low |
| 6 | Clickjacking | Another site frames the app | `frame-ancestors 'none'` on `/`; only Telegram Web on `/tg/` | Low |
| 7 | Read of a group by a person who is not a member | A request with another group ID | `groupActor` with a `getChatMember` answer at most 10 min old; the hint never grants access | Low |
| 8 | Access after the owner removes a user from the allowlist | An old session | Allowlist check on every request; sessions deleted; pruned at start | None after the restart |
| 9 | Access after a user leaves a group | A cached `member` row | `TODO_WEB_MEMBER_TTL` of 10 min, and 1 h of grace during a Telegram outage | Medium for at most 1 h |
| 10 | Guess of a login code or poll token | Brute force | 256-bit poll token; approval needs a signed-in Mini App; 5 min life; rate limits | Low |
| 11 | Phishing with a login code | The attacker sends a victim a link to approve the attacker's code | The approval screen shows the device text, the code and a warning; a button press is necessary; "Log out everywhere" | Medium. It needs a mistake by the user. |
| 12 | False client IP | A false `CF-Connecting-IP` | The server reads forwarded headers only from trusted proxies; Caddy accepts only Cloudflare | Low. The IP controls only rate limits. |
| 13 | Direct access to the port, or DNS rebinding | A local process, or a page that points a host name at 127.0.0.1 | Loopback bind only; the `Host` check | Low |
| 14 | A bad npm package | Supply chain | Few dependencies; exact versions; lockfile; `npm ci --ignore-scripts`; nothing from npm runs at run time except the bundle | Medium. This design accepts it. |
| 15 | A changed Telegram script | `telegram-web-app.js` or the widget script | Loaded only on `/tg/`, and on the login screen in phase 4 | Medium. This design accepts it, because Telegram clients have the same trust. |
| 16 | Denial of service | Many requests or large bodies | Rate limits, the body limit, timeouts, 64 requests at the same time, `MemoryMax` | Medium. Cloudflare in front helps. |
| 17 | Cloudflare keeps personal data in its cache | Wrong cache headers | `no-store` or `private` on all API responses; a Cloudflare bypass rule for `/api/*` | Low |
| 18 | Access to an item of another chat (IDOR) | An item ID from another chat | The service checks `t.ChatID == a.ChatID` or the assignee rule; `groupActor` checks the chat; both give 404 | Low |
| 19 | Secrets in logs | Debug log | The rules of §11.3, and a test | Low |
| 20 | The bot token leaks | Theft of the env file | All proofs fail when the owner changes the token. The owner also deletes all web sessions (§16). | Medium until the owner acts |

## 12. Testing

All Go tests follow the rules of `CLAUDE.md`: `clocktest.Fake` for time, `tgtest.FakeSender` for Telegram, and a real SQLite database in `t.TempDir()`.

### 12.1 Go unit and HTTP tests

- **Server.** `httptest.NewServer(web.New(deps))` with the real `service.Svc` on SQLite, `clocktest.Fake`, `tgtest.FakeSender`, the new `tgtest.FakeMembers`, and `fstest.MapFS` for the static files.
- **`initData`.** A test helper signs fields with a test token, with the steps of §3.2. Cases:
  - a correct proof;
  - a wrong `hash`, and no `hash`;
  - a key that occurs two times;
  - `auth_date` 1 h + 1 s old, and 61 s in the future;
  - `signature` present;
  - a user who is not allowlisted;
  - `user.id` 0;
  - a body above 4 KiB.
- **The widget.** The same cases, with the key `SHA256(token)`.
- **Sessions.**
  - Expiry with `Fake.Advance`.
  - The expiry update at most one time per hour, and the absolute limit.
  - Logout and "log out everywhere".
  - Allowlist revocation and the prune at start.
  - The limit of 20 sessions.
  - Bearer and cookie, and the header wins when both are present.
- **CSRF.** A cookie `POST` without `X-Todo-Request` gives 403. A cross-site `Sec-Fetch-Site` gives 403. A bearer request does not need the header.
- **Membership.**
  - A fresh row: no Telegram call.
  - An old row: exactly one call.
  - `left` and `kicked`: 403, and the row becomes `left`.
  - `restricted` with `is_member` true and false.
  - An admin removed the bot: every row of the chat becomes `left`.
  - `MigrateTo`: the server calls `MigrateChat`, and the answer is 409 with the new ID.
  - A temporary error inside and outside the grace window.
  - A hint row alone never grants access.
  - Discovery: at most 4 checks at the same time, and the 5 s stop.
- **API.**
  - Every endpoint, with a correct request and each error of §5.3.
  - Golden files of the item JSON in `internal/web/testdata/api/`: en and fa, Gregorian and Jalali, with and without a date, a repeat and an assignee.
  - The ETag gives `304`.
  - The actor for an assigned item.
  - The server sends the "did" notice one time.
  - The "remind later" presets across a DST change, with `time.Date`.
- **Headers.** The CSP of each route, HSTS, the `Host` check, and the client IP with and without a trusted proxy.
- **Logs.** No secret in the log of a full login (§11.3).
- **Store.** The conformance suite `storetest/web.go`:
  - session CRUD and expiry;
  - `ApproveLoginCode` and `TakeLoginCode` from two handles at the same time, where exactly one wins;
  - the upsert rules of `chat_members`;
  - the retention of `Purge`;
  - `ListGroupChats`.
- **Config.** Every new variable, and the refused values.

### 12.2 Frontend tests

- Vitest with `@testing-library/preact` and `jsdom`.
- `api.ts`: bearer or cookie, the `X-Todo-Request` header, the error map, and a 401 that goes to the login screen.
- `i18n.ts`: the `%s` and `%d` replacement, the digits, and `dir`.
- The list renders the golden JSON files of §12.1. The frontend tests read the same files as the Go tests, so the DTO has one source of truth.
- `cache.ts`: read, write, and delete on logout and on a change of user.
- The RTL layout: `dir` on `<html>`.
- The theme: the map from the Telegram theme parameters to the CSS tokens.

### 12.3 The e2e test

`internal/e2e/web_test.go` (W10) starts the full process in the current harness, with the mock Telegram server and the fake clock. It starts the web on `127.0.0.1:0`. Then it does these steps:

1. Sign in with a signed `initData`.
2. Add `buy milk tomorrow 18:00 every week` through `POST /api/chats/{me}/items`. Compare the stored row with the row that `/add buy milk tomorrow 18:00 every week` gives in a second chat.
3. Read the list.
4. Advance the clock. Make sure that the reminder arrives in the mock Telegram server one time.
5. Mark the item done through the API.
6. In a group: the mock answers `getChatMember` with `member`. List the group, and add an item assigned to a second user.
7. The second user completes it from **Assigned to me**. Make sure that the "did" notice arrives in the group one time.
8. The mock now answers `left`. After `TODO_WEB_MEMBER_TTL`, make sure that the group list gives 403.

### 12.4 Manual checks before each phase closes

- The Mini App in Telegram for iOS, Android and Desktop, and in Telegram Web K and A.
- PWA install on Chrome for desktop, Chrome for Android and Safari for iOS.
- Farsi and RTL, and the dark theme.

## 13. CONTRACT CHANGES

The changes below are necessary. W1 makes all of them except the last two lines. W14 makes `tg.Button`, and #98 makes `TodoPatch`.

| Package or file | Change | Sub-issue |
|---|---|---|
| `migrations/sqlite/0005_web.sql` | a new migration file: `web_sessions`, `web_login_codes`, `chat_members` | W1 |
| `internal/model` | a new file `web.go`: `WebSession`, `WebSessionKind`, `LoginCode`, `ChatMember`, `MemberStatus` | W1 |
| `internal/store` | `Store` gets the 17 methods of §6.3. The doc comment of `Purge` lists the new deletes. `MigrateChat` does not change. | W1 |
| `internal/service` | a new interface `service.Web` (below), implemented by `*Svc`. `service.Service` does **not** change, so the handler fakes do not change. | W1 |
| `internal/tg` | a new file `members.go`: `MemberChecker`, `ChatMember`, `MemberStatus` (below). `Sender` does not change. | W1 |
| `internal/tg/tgbot` | `(*Bot).ChatMember` implements `tg.MemberChecker` | W1 |
| `internal/tg/tgtest` | a new `FakeMembers` | W1 |
| `internal/config` | `Config` gets the fields of §9.1 | W1 |
| `internal/ui` | no change. The web uses `FormatWhen`, `DescribeRule` and `Mention`. | – |
| `internal/i18n` | no change. The web uses `Keys` and `T`. | – |
| `internal/handler/router.go` | no change. The observer wraps `Handle` in `cmd/todo-bot`. | – |
| `internal/tg` | `Button` gets `URL string` and `WebAppURL string` for the "Open app" buttons. `tgbot` maps them. | W14 |
| `internal/service` | `UpdateTodo` and `TodoPatch` come from #98. The web only calls them. | #98 |

```go
// internal/service/web.go
// Web is the API of internal/web (docs/WEB-DESIGN.md). *Svc implements it.
type Web interface {
	// Sessions. The token is never stored; the store keeps its SHA-256.
	StartWebSession(ctx context.Context, userID int64, kind model.WebSessionKind,
		device string, idle, max time.Duration) (token string, s model.WebSession, err error)
	// WebSession returns the live session of token (ErrNotFound if none). For a
	// browser session it slides the expiry, with at most one write per hour.
	WebSession(ctx context.Context, token string, idle time.Duration) (model.WebSession, error)
	EndWebSession(ctx context.Context, token string) error
	EndUserWebSessions(ctx context.Context, userID int64) error
	PruneWebSessions(ctx context.Context, allowed map[int64]bool) (int, error)

	// Login codes (§3.3).
	NewLoginCode(ctx context.Context, device string) (code, pollToken string, c model.LoginCode, err error)
	LoginCode(ctx context.Context, code string) (model.LoginCode, error)
	ApproveLoginCode(ctx context.Context, code string, userID int64) error // ErrNotFound, ErrCodeUsed
	CancelLoginCode(ctx context.Context, code string) error
	TakeLoginCode(ctx context.Context, pollToken string) (model.LoginCode, error)

	// Membership (§4). The service only stores; internal/web calls Telegram.
	NoteMember(ctx context.Context, a Actor) error
	Membership(ctx context.Context, chatID, userID int64) (model.ChatMember, error)
	MembershipsOf(ctx context.Context, userID int64) ([]model.ChatMember, error)
	RecordMembership(ctx context.Context, m model.ChatMember) error
	ChatGone(ctx context.Context, chatID int64) error
	GroupChats(ctx context.Context, limit int) ([]model.Chat, error)

	// Reads that the bot gets from Telegram updates.
	Chat(ctx context.Context, chatID int64) (model.Chat, error)
	User(ctx context.Context, userID int64) (model.User, error)
}

var ErrCodeUsed = errors.New("login code already approved")
```

```go
// internal/tg/members.go
type MemberStatus string // "creator", "administrator", "member", "restricted", "left", "kicked"

type ChatMember struct {
	Status   MemberStatus
	IsMember bool // only meaningful for "restricted"
}

// MemberChecker calls getChatMember. *tgbot.Bot implements it. Errors are
// *APIError where Telegram answered, as for Sender.
type MemberChecker interface {
	ChatMember(ctx context.Context, chatID, userID int64) (ChatMember, error)
}
```

`internal/web` has the `Deps` below. It is a new package, so its API is not frozen yet. W1 adds it to the frozen list in ARCHITECTURE §3 after W3 merges.

```go
type Deps struct {
	Svc      service.Service
	Web      service.Web
	Send     tg.Sender
	Members  tg.MemberChecker
	Parser   nlparse.Parser
	Clock    clock.Clock
	Allowed  map[int64]bool
	Token    string // bot token, for the HMAC checks only; never logged
	BotName  string // the bot @username, for t.me links
	Config   config.Config
	Assets   fs.FS
	Log      *slog.Logger
}
```

## 14. Work split

### 14.1 Rules

- At most 4 sub-issues run at the same time. One Sonnet agent can do each one.
- Sub-issues that run at the same time never edit the same file. A later sub-issue can edit a file that an earlier, merged sub-issue made. The lists below name each such file.
- **Owners of shared files.**
  - `cmd/todo-bot/run.go`: W3, then W4, then W14.
  - `docs/ARCHITECTURE.md`: W1 only.
  - `web/vite.config.ts`: W2, then W6.
  - `web/src/app.tsx`: W2, then W9, then W12.
- Each sub-issue copies its acceptance criteria into its PR as a checklist, and `make ci` must pass. A sub-issue that touches `web/` must also pass `make web-test`.
- Branch names follow `issue-<N>-<slug>`.

### 14.2 Phase 1: plumbing, auth and the Mini App shell

**W1. Web contracts and store** (Go)
- **Depends on:** #98 part A merged, because both edit `docs/ARCHITECTURE.md` §3.
- **Owns:**
  - `migrations/sqlite/0005_web.sql`;
  - `internal/model/web.go`;
  - `internal/store/store.go`, `internal/store/sqlstore/web.go`, the `Purge` change in `internal/store/sqlstore/multinode.go`;
  - `internal/store/storetest/web.go` and `suite.go`, `internal/store/sqlite/sqlite_test.go` (the version checks);
  - `internal/service/web.go` and `web_test.go`;
  - `internal/tg/members.go`, `internal/tg/tgbot/members.go` with its test, `internal/tg/tgtest/members.go`;
  - `internal/config/config.go` and `config_test.go`, `deploy/env.example`;
  - `docs/ARCHITECTURE.md`: the §3 lines, and a §14 that points to this document.
- **Acceptance:**
  - [ ] The migration has version 0005 (or the next free number), and the parity test passes.
  - [ ] W1 implements every store method of §6.3, and `storetest` covers each one. The two-handle cases for `ApproveLoginCode` and `TakeLoginCode` give exactly one winner.
  - [ ] `Purge` applies the retention of §6.4.
  - [ ] `*Svc` implements `service.Web`. Tests use SQLite in `t.TempDir()` and `clocktest.Fake`. They cover the expiry update and the hash-only storage.
  - [ ] `tgbot` maps `getChatMember` and its errors. The test uses an `httptest` Telegram server.
  - [ ] `config.Load` parses and checks the variables of §9.1. It refuses a `TODO_WEB_ADDR` that is not on loopback.
  - [ ] The PR has a `CONTRACT CHANGE` section with the W1 rows of §13.

**W2. Frontend scaffold and build** (Node and build files)
- **Depends on:** nothing. It runs at the same time as W1.
- **Owns:**
  - `web/**`: the files of §7.1 except `tabs/`, `login/`, `approve/`, `actions/` and `edit/`, which it makes only as stubs;
  - `internal/web/doc.go`, `internal/web/assets_webui.go`, `internal/web/assets_placeholder.go`, `internal/web/placeholder/index.html`;
  - `.gitignore`, `Makefile`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`;
  - `scripts/deploy.sh` (the `build()` step and the `-tags=webui` guard) and `scripts/test-deploy.sh`.
- **Acceptance:**
  - [ ] `make ci` passes without Node, and builds the placeholder.
  - [ ] `make release-linux` builds a binary with `-tags=webui`. `go version -m` shows it.
  - [ ] `deploy.sh` stops if the binary has no `webui` tag.
  - [ ] The `web` CI job passes: typecheck, vitest, build, size check.
  - [ ] Exact versions, a lockfile, `npm ci --ignore-scripts`.
  - [ ] The Mini App shell signs in with `POST /api/auth/miniapp` and shows the user name from `GET /api/me`. Tests mock `fetch`.
  - [ ] RTL and the theme work in the shell. A test checks that the source has no `innerHTML`.

**W3. Web server core and Mini App sign-in** (Go)
- **Depends on:** W1 and W2 merged.
- **Owns:**
  - `internal/web/server.go`, `deps.go`, `static.go`, `headers.go`, `clientip.go`, `ratelimit.go`, `errors.go`;
  - `internal/web/initdata.go`, `session.go`, `auth.go`, `me.go`, `i18n_api.go`;
  - `internal/web/membership.go`: a simple `groupActor` that reads only the row (no Telegram call). W4 replaces it;
  - `internal/web/chats_api.go`: `GET /api/chats` with the private chat only. W4 replaces it;
  - `internal/web/messages_web.go` (`web.app.*`, `web.err.*`) and the tests of all these files;
  - `cmd/todo-bot/run.go` and `run_test.go` (start and stop the server, `PruneWebSessions` at start), `cmd/todo-bot/main.go` (the `version` line).
- **Acceptance:**
  - [ ] Endpoints 1 to 9 of §5.3 work as specified, with the error codes.
  - [ ] Every `initData` and session case of §12.1 passes.
  - [ ] The CSRF guard, the headers and CSP of §11, the `Host` check, the client IP rule and the rate limits have tests.
  - [ ] No secret is in the log of a full sign-in.
  - [ ] With `TODO_WEB_ADDR` empty, no listener starts, and the bot works as before.
  - [ ] Shutdown stops the web first, inside the 10 s budget.
  - [ ] The PR records the binary size and the idle RSS before and after (§8.5).
  - [ ] One real `initData` from the development bot passes, checked by hand (§3.2).

### 14.3 Phase 2: read-only tabs, membership and the PWA

All four sub-issues depend on W3 merged. They run at the same time.

**W4. Group membership** (Go)
- **Owns:** `internal/web/membership.go` (it replaces the W3 stub), `internal/web/observe.go`, `internal/web/chats_api.go` (it replaces the W3 stub), their tests, and `cmd/todo-bot/run.go` (the observer wraps `h.Handle`).
- **Acceptance:**
  - [ ] `groupActor` follows §4.4, with every row of its table tested.
  - [ ] `GET /api/chats` follows §4.5: at most 4 checks at the same time, the 5 s stop, `partial`.
  - [ ] The observer writes hints only for allowlisted users in groups, at most one time per hour, and never grants access.
  - [ ] The title for the actor comes from the `chats` row.

**W5. Read API** (Go)
- **Owns:** `internal/web/items_read.go`, `dto.go`, `format.go` (`due_full`), `etag.go`, `messages_items.go` (`web.item.*`), `internal/web/testdata/api/*.json`, and their tests.
- **Acceptance:**
  - [ ] Endpoints 10 and 11 work. **Mine** uses the user's own ID. A group calls `groupActor` before `List`.
  - [ ] The golden files cover en and fa, Gregorian and Jalali, with Persian digits.
  - [ ] `If-None-Match` gives `304`. The ETag does not change with `rendered_at`.
  - [ ] **Assigned to me** has `chat_title` and at most 200 items.

**W6. Frontend: tabs and PWA** (Node)
- **Owns:** `web/src/tabs/**`, `web/src/sw.ts`, `web/public/**`, `web/vite.config.ts` (the service worker plugin), `assets/export.sh` and the new PNG files in `assets/`, `internal/web/messages_tabs.go` (`web.tabs.*`).
- **Acceptance:**
  - [ ] The three tabs, the group chips, the empty states and "Load more" work against the golden JSON.
  - [ ] Refetch on `visibilitychange`, on `focus` and on `activated`.
  - [ ] The manifest and the icons (192, 512, maskable, apple-touch) pass the Chrome install check.
  - [ ] The service worker caches the shell only. It does not handle `/api/`, `/auth/` or `/tg/`. The update banner works.
  - [ ] Offline, the app shows the cached lists with `due_full`, a banner and no actions. Logout clears them.

**W7. Login code** (Go and Node)
- **Owns:** `internal/web/auth_code.go`, its tests, `internal/web/messages_login.go` (`web.login.*`), `web/src/login/**` (the code screen) and `web/src/approve/**`.
- **Acceptance:**
  - [ ] Endpoints 12 to 16 follow §3.3.
  - [ ] Two polls at the same time give one session. Two approvals at the same time give one winner.
  - [ ] A code expires after 5 min. A cancelled code gives 410 to the poll.
  - [ ] The approval screen shows the device text, the code and the warning.
  - [ ] The PWA gets the cookie and opens the tabs.

### 14.4 Phase 3: add, done, delete and remind later

**W8. Write API** (Go)
- **Depends on:** W4 and W5 merged.
- **Owns:** `internal/web/items_write.go`, `parse_api.go`, `members_api.go`, `settings_api.go`, `notify.go` (the "did" notice), `messages_write.go` (`web.item.*` write strings, `web.tg.did`), and their tests.
- **Acceptance:**
  - [ ] Endpoints 17 to 23 work as specified, with every error code.
  - [ ] Add follows §5.4: digits, the 1000-rune limit, the parse in the user's time zone, the assignee check.
  - [ ] "Remind later" `tomorrow` uses `TODO_DEFAULT_TIME` with `time.Date`, and it is correct across a DST change.
  - [ ] The server sends the "did" notice one time, only for the assignee from another chat, in the creator's locale.
  - [ ] Every write wakes the scheduler: a test with the fake clock sees the reminder.

**W9. Frontend: actions** (Node)
- **Depends on:** W6 merged. It can run at the same time as W8, against the API of §5.
- **Owns:** `web/src/actions/**` (it replaces the stub), `web/src/tabs/row.tsx` (the row opens the item sheet), `web/src/app.tsx` (the settings menu and the Add button), `internal/web/messages_actions.go` (`web.actions.*`).
- **Acceptance:**
  - [ ] Add with the live preview and the assignee picker. Done, Delete with a confirmation, and Remind later with all presets.
  - [ ] The Mini App uses `MainButton` and `BackButton`.
  - [ ] The settings menu changes the language and the calendar, and the direction changes at once.
  - [ ] An error shows the `message` of the server.

**W10. e2e and user docs**
- **Depends on:** W8 merged.
- **Owns:** `internal/e2e/web_test.go`, `internal/e2e/mocktg_test.go` (`getChatMember`), `internal/e2e/harness_test.go` (if it needs a helper), `docs/USAGE.md`, `docs/DEPLOYMENT.md` (env, the Caddy contract, the steps of §16), `README.md`, `README.fa.md`.
- **Acceptance:**
  - [ ] The e2e test of §12.3 passes with `-race`.
  - [ ] The docs follow the STE rules, and the checker shows no errors.

### 14.5 Phase 4: edit, the widget and live updates

**W11. Edit API** (Go)
- **Depends on:** #98 part B merged (`UpdateTodo`), and W8 merged.
- **Owns:** `internal/web/items_edit.go`, `internal/web/assign_dm.go`, `internal/web/messages_edit.go` (`web.edit.*`, `web.tg.assigned`), and their tests.
- **Acceptance:**
  - [ ] Endpoints 24 and 25. A field that is not in the request does not change.
  - [ ] `when: null` removes the date. A new `when` replaces the due and snooze reminders and keeps custom reminders (#98).
  - [ ] The DM to a new assignee follows #98 §4. A 403 from Telegram does not fail the save.
  - [ ] A done or deleted item gives 404.

**W12. Frontend: edit and live updates** (Node)
- **Depends on:** W9 merged. It can run at the same time as W11.
- **Owns:** `web/src/edit/**`, `web/src/live.ts`, `web/src/app.tsx` (the poll start and stop), `web/src/actions/item.tsx` (the Edit entry).
- **Acceptance:**
  - [ ] The edit form fills from endpoint 24 and sends only the changed fields.
  - [ ] The poll with `If-None-Match` runs every 30 s only while the page is visible and online.

**W13. Login Widget** (Go and Node)
- **Depends on:** W7 merged. The owner must run `/setdomain` (§16) before the manual check.
- **Owns:** `internal/web/auth_widget.go`, its tests, `internal/web/headers.go` (the CSP of `/` for the login screen), `internal/web/messages_widget.go`, `web/src/login/widget.tsx`, `web/src/login/index.tsx`.
- **Acceptance:**
  - [ ] Endpoint 26 follows §3.4, in redirect mode.
  - [ ] The widget does not show in an installed iOS app.
  - [ ] The CSP of `/` allows `telegram.org` and `oauth.telegram.org` only as §11.2 says.

**W14. "Open app" buttons and the menu button** (Go)
- **Depends on:** #98 part D merged, because both edit `internal/handler/cmd_todo.go`.
- **Owns:**
  - `internal/tg/tg.go` (`Button.URL`, `Button.WebAppURL`) and `internal/tg/tgbot/tgbot.go` (markup and `SetMenuButton`);
  - `internal/handler/cmd_todo.go` and `messages_todo.go` (the button under `/start` and `/help`);
  - `cmd/todo-bot/run.go` (`setChatMenuButton` in `lead`, only when the web is on).
- **Acceptance:**
  - [ ] In a private chat, the button is a `web_app` button to `/tg/`. Telegram allows `web_app` inline buttons only in private chats.
  - [ ] In a group, the button is a URL button to `https://t.me/<bot>?startapp=g<chatID>`.
  - [ ] The menu button opens `/tg/`.
  - [ ] With the web off, no button and no menu change.
  - [ ] The PR has a `CONTRACT CHANGE` section for `tg.Button`.

### 14.6 Order

```
Phase 1:  W1 ─┬─> W3
          W2 ─┘
Phase 2:  W3 ──> W4, W5, W6, W7            (4 at the same time)
Phase 3:  W4 + W5 ──> W8 ──> W10
          W6 ──> W9                         (W8 and W9 at the same time)
Phase 4:  #98 B + W8 ──> W11
          W9 ──> W12
          W7 ──> W13
          #98 D ──> W14                     (4 at the same time)
```

## 15. Owner decisions still open

| # | Decision | Recommendation |
|---|---|---|
| D1 | Add Node to the repository: `web/package.json`, a lockfile, a `web` CI job, and Node in `release.yml` and `deploy.sh`. | **Yes**, with Node 24 LTS. `make ci` stays Go only. |
| D2 | Frontend library. | **Preact with Vite** (§8.1). It adds about 4 KB. |
| D3 | How a browser signs in. | **The login code, confirmed in the Mini App**, in phase 2, for all devices. **The Login Widget** comes in phase 4 for desktop browsers only. |
| D4 | Session lifetimes. | Browser: 30 days idle, 90 days absolute. Mini App: 12 h. |
| D5 | Behaviour while Telegram is down. | Allow a member that Telegram confirmed less than 1 hour ago. After that, answer 503. |
| D6 | Discovery of groups. | Check every group in `chats`, at most 50, at sign-in and every 10 minutes. |
| D7 | Make the bot an administrator in each group. | **No**, unless `getChatMember` fails for a real member. |
| D8 | Settings in the web. | **Language and calendar only.** The time zone and the digest stay in the bot. |
| D9 | An `@name` at the start of the Add text. | **Do not parse it.** The form has an assignee picker. |
| D10 | A browser e2e test (Playwright) in CI. | **No, not now.** The Go e2e test and the vitest tests cover the contract. Add one later if the UI breaks often. |
| D11 | How to set the menu button. | **BotFather by hand in phase 1** (§16). W14 then sets it in code. |
| D12 | A list of done items and undo. | **Not now.** Make a later issue if the family asks for it. |

## 16. What the owner must do by hand

### 16.1 BotFather

1. Open @BotFather and select the bot.
2. Open **Bot Settings**, then **Configure Mini App**. Turn on the Main Mini App with the URL `https://todo.sadeq.uk/tg/`.
3. Open **Bot Settings**, then **Menu Button**. Set the URL `https://todo.sadeq.uk/tg/` and the title "Open".
4. Before W13, send `/setdomain` and give `todo.sadeq.uk`.
5. Do steps 1 to 4 again for the development bot, with its own host name.

The Main Mini App is necessary for the `t.me/<bot>?startapp=…` links of §3.3 and of W14.

### 16.2 Cloudflare

1. Add an `A` record `todo` for the IP of vps03. Set it to **Proxied**.
2. Set **SSL/TLS** to **Full (strict)**.
3. Turn off **Rocket Loader**, **Email Obfuscation** and **Auto Minify** for `todo.sadeq.uk`. They add scripts that the CSP blocks.
4. Add a cache rule: bypass the cache for `todo.sadeq.uk/api/*`, `/auth/*` and `/sw.js`.
5. Make sure that no Bot Fight Mode or challenge applies to `todo.sadeq.uk/api/*`. A challenge breaks the `fetch` calls of the app.

### 16.3 Caddy (in the proxy repository)

1. Add a site block for `todo.sadeq.uk` that proxies to `127.0.0.1:8088`.
2. Accept the site only from the Cloudflare IP ranges, or turn on Authenticated Origin Pulls.
3. Keep the other rules of §9.3.

### 16.4 vps03 and the bot

1. Move the bot from vps00 to vps03 as a separate operations step. Stop the bot on vps00 before you start it on vps03. Two pollers with one token get 409, and two hosts cannot share one SQLite file.
2. Run `scripts/deploy.sh` with `HOST` set to the root SSH address of vps03. The default `HOST` is vps00.
3. Add `TODO_WEB_ADDR=127.0.0.1:8088` and `TODO_WEB_ORIGIN=https://todo.sadeq.uk` to `/etc/todo-bot/env`.
4. Restart the service. Make sure that `curl -s http://127.0.0.1:8088/healthz` gives `ok`.

### 16.5 If the bot token leaks

1. Get a new token with BotFather `/revoke`, and put it in `/etc/todo-bot/env`.
2. Stop the service.
3. Delete all web sessions: `sqlite3 /var/lib/todo-bot/todo.db "DELETE FROM web_sessions;"`.
4. Start the service.

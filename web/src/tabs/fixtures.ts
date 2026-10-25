// Test data, made by hand from the shapes of docs/WEB-DESIGN.md sections 5.2
// and 5.3. They must be compared with the golden files of the read API
// (internal/web/testdata/api/*.json) when those merge. Only tests import
// this file, so the build does not include it.
import type { ChatsResponse, Item, ListResponse } from "./model";

export function item(over: Partial<Item> = {}): Item {
  return {
    id: 42,
    chat_id: 11111,
    chat_title: "",
    num: 3,
    text: "buy milk",
    due_at: "2026-10-25T17:00:00Z",
    due_text: "tomorrow 18:00",
    due_full: "Sun 25 Oct 2026 18:00",
    overdue: false,
    recur: "",
    recur_text: "",
    assignee: null,
    created_by: 11111,
    ...over,
  };
}

export function list(items: Item[], over: Partial<ListResponse> = {}): ListResponse {
  return { items, page: 0, pages: 1, rendered_at: "2026-10-24T08:30:00Z", ...over };
}

export const chats: ChatsResponse = {
  chats: [
    { id: 11111, kind: "private", title: "" },
    { id: -1001234567890, kind: "supergroup", title: "Family" },
    { id: -1009876543210, kind: "group", title: "Work" },
  ],
  partial: false,
};

export const farsiItem = item({
  id: 7,
  num: 2,
  text: "خرید شیر و نان",
  due_text: "فردا ۱۸:۰۰",
  due_full: "یکشنبه ۳ آبان ۱۴۰۵ ۱۸:۰۰",
  recur: "weekly sun 18:00",
  recur_text: "هر یکشنبه",
  assignee: { id: 22222, username: "thesadeq" },
});

// Shapes of the list endpoints (docs/WEB-DESIGN.md sections 5.2, 5.3 and 4.5).

export interface Item {
  id: number;
  chat_id: number;
  /** Always present. "" when the list is not "Assigned to me" or the chat has no title. */
  chat_title: string;
  num: number;
  text: string;
  due_at: string | null;
  due_text: string | null;
  due_full: string | null;
  overdue: boolean;
  recur: string;
  recur_text: string;
  assignee: { id: number; username: string } | null;
  created_by: number;
}

export interface ListResponse {
  items: Item[];
  page: number;
  pages: number;
  rendered_at: string;
}

export interface Chat {
  id: number;
  kind: string;
  title: string;
}

export interface ChatsResponse {
  chats: Chat[];
  partial?: boolean;
}

/** What the app keeps for offline use (one entry for each list). */
export interface SavedList {
  items: Item[];
  page: number;
  pages: number;
  rendered_at: string;
}

import { actorChat, openItem } from "../actions/store";
import { num, t } from "../i18n";
import type { Item } from "./model";

/**
 * One item. Text, names and titles come from users, so they sit in <bdi> or
 * have dir="auto". A Farsi page then does not turn "@name" into "name@".
 * Offline rows show the absolute date (due_full), because the relative form
 * ("tomorrow") is out of date. They have no buttons.
 *
 * Online, the check mark marks the item as done, and a tap on the body opens
 * the item sheet (docs/WEB-DESIGN.md section 7.3). In "Assigned to me"
 * (showChat) the actions go through the private chat of the viewer.
 */
export function Row(props: { item: Item; offline: boolean; showChat: boolean; viewerId: number }) {
  const it = props.item;
  const due = props.offline ? it.due_full : it.due_text;
  // In "Assigned to me" every item is for the viewer, so the arrow adds nothing.
  const showAssignee = it.assignee !== null && !props.showChat && it.assignee.id !== props.viewerId;
  const chat = actorChat(it, props.showChat, props.viewerId);

  const body = (
    <>
      <div class="row-text" dir="auto">
        {it.text}
      </div>
      <div class="row-meta">
        {due ? <span class={it.overdue ? "due overdue" : "due"}>{due}</span> : null}
        {it.recur_text ? (
          <span class="recur">
            <span aria-hidden="true">🔁 </span>
            <bdi>{it.recur_text}</bdi>
          </span>
        ) : null}
        {showAssignee && it.assignee ? (
          <span class="assignee">
            <span class="arrow" aria-hidden="true">
              →{" "}
            </span>
            <bdi>{it.assignee.username ? `@${it.assignee.username}` : t("web.tabs.noname")}</bdi>
          </span>
        ) : null}
        {props.showChat && it.chat_title ? (
          <span class="chat">
            <bdi>{it.chat_title}</bdi>
          </span>
        ) : null}
      </div>
    </>
  );

  if (props.offline) {
    return (
      <li class="row" data-id={it.id}>
        <span class="row-num">{num(it.num)}</span>
        <div class="row-body">{body}</div>
      </li>
    );
  }

  return (
    <li class="row" data-id={it.id}>
      <button
        type="button"
        class="row-check"
        aria-label={t("web.actions.mark_done")}
        title={t("web.actions.mark_done")}
        onClick={() => openItem(it, chat, "done")}
      />
      <span class="row-num">{num(it.num)}</span>
      <button type="button" class="row-body row-open" aria-haspopup="dialog" onClick={() => openItem(it, chat)}>
        {body}
      </button>
    </li>
  );
}

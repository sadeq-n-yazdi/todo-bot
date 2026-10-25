import type { ComponentChildren } from "preact";
import { useEffect, useRef } from "preact/hooks";
import { ApiError } from "../api";
import { t } from "../i18n";
import { useBackButton } from "./telegram";

/** The text to show for a failed request: the `message` of the server, or a local string. */
export function errText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.status === 0) return t("web.actions.offline");
    if (e.message) return e.message;
  }
  return t("web.actions.error");
}

/** A ref that is false after the component is gone, so that late answers set no state. */
export function useLive() {
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => void (live.current = false);
  }, []);
  return live;
}

/**
 * A bottom sheet. In the Mini App the Telegram BackButton closes it (or goes
 * one step back); the PWA has a Close button in the page.
 */
export function Sheet(props: {
  title: string;
  miniapp: boolean;
  onBack: () => void;
  children: ComponentChildren;
  testId?: string;
}) {
  useBackButton(props.miniapp, props.onBack);

  const back = useRef(props.onBack);
  back.current = props.onBack;
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") back.current();
    };
    document.addEventListener("keydown", key);
    return () => document.removeEventListener("keydown", key);
  }, []);

  return (
    <div class="sheet-shade">
      <section class="sheet" role="dialog" aria-modal="true" aria-label={props.title} data-testid={props.testId}>
        <header class="sheet-head">
          <h2>{props.title}</h2>
          {props.miniapp ? null : (
            <button type="button" class="sheet-close" aria-label={t("web.actions.close")} onClick={props.onBack}>
              ×
            </button>
          )}
        </header>
        {props.children}
      </section>
    </div>
  );
}

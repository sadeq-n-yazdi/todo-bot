// The MainButton, the BackButton and the haptic feedback of the Mini App
// (docs/WEB-DESIGN.md section 7.3). The PWA has none of them, and shows its
// own buttons in the page instead.

import { useEffect, useRef } from "preact/hooks";

interface TgButton {
  setText?(text: string): void;
  show(): void;
  hide(): void;
  enable?(): void;
  disable?(): void;
  showProgress?(leaveActive?: boolean): void;
  hideProgress?(): void;
  onClick(fn: () => void): void;
  offClick(fn: () => void): void;
}

export interface TgWebApp {
  MainButton?: TgButton;
  BackButton?: TgButton;
  HapticFeedback?: { notificationOccurred(kind: "success" | "error" | "warning"): void };
}

/** main.tsx declares window.Telegram with fewer fields, so read it with a cast. */
export function webApp(): TgWebApp | undefined {
  return (window as unknown as { Telegram?: { WebApp?: TgWebApp } }).Telegram?.WebApp;
}

export function haptic(kind: "success" | "error" | "warning"): void {
  try {
    webApp()?.HapticFeedback?.notificationOccurred(kind);
  } catch {
    /* an old Telegram client */
  }
}

/** Show the BackButton while `active`. A tap calls the latest `onBack`. */
export function useBackButton(active: boolean, onBack: () => void): void {
  const fn = useRef(onBack);
  fn.current = onBack;
  useEffect(() => {
    const b = webApp()?.BackButton;
    if (!active || !b) return;
    const click = () => fn.current();
    b.onClick(click);
    b.show();
    return () => {
      b.offClick(click);
      b.hide();
    };
  }, [active]);
}

/**
 * Show the MainButton with `text` while `active`. A tap calls the latest
 * `onClick`, so the handler never sees old state.
 */
export function useMainButton(active: boolean, text: string, enabled: boolean, busy: boolean, onClick: () => void): void {
  const fn = useRef(onClick);
  fn.current = onClick;
  const b = active ? webApp()?.MainButton : undefined;

  useEffect(() => {
    if (!b) return;
    const click = () => fn.current();
    b.onClick(click);
    b.show();
    return () => {
      b.offClick(click);
      b.hideProgress?.();
      b.hide();
    };
  }, [b]);

  useEffect(() => {
    if (!b) return;
    b.setText?.(text);
    if (enabled && !busy) b.enable?.();
    else b.disable?.();
    if (busy) b.showProgress?.(false);
    else b.hideProgress?.();
  }, [b, text, enabled, busy]);
}

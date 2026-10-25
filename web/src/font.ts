// The Vazirmatn font from Google Fonts (docs/WEB-DESIGN.md section 7.4). The
// app adds the links at start, so the first paint does not wait for them.
// "display=swap" shows the system font of --font until Vazirmatn arrives, and
// without the network the system font stays. The CSP allows these two hosts.

export const FONT_CSS = "https://fonts.googleapis.com/css2?family=Vazirmatn:wght@100..900&display=swap";
const FONT_FILES = "https://fonts.gstatic.com";

/** Adds the preconnect and the stylesheet link of the font, once. */
export function loadFont(doc: Document = document): void {
  if (doc.querySelector(`link[rel="stylesheet"][href="${FONT_CSS}"]`)) return;

  const pre = doc.createElement("link");
  pre.rel = "preconnect";
  pre.href = FONT_FILES;
  pre.crossOrigin = "anonymous";
  doc.head.appendChild(pre);

  const css = doc.createElement("link");
  css.rel = "stylesheet";
  css.href = FONT_CSS;
  doc.head.appendChild(css);
}

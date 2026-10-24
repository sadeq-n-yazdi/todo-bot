// Telegram theme (docs/WEB-DESIGN.md section 7.4). Telegram sets --tg-theme-*
// variables itself. This module maps themeParams to the same names, so that
// the page follows the theme also when only themeParams is known. styles.css
// builds its tokens from these variables, with the PWA colors as fallback.

const map: Record<string, string> = {
  bg_color: "--tg-theme-bg-color",
  text_color: "--tg-theme-text-color",
  hint_color: "--tg-theme-hint-color",
  link_color: "--tg-theme-link-color",
  button_color: "--tg-theme-button-color",
  button_text_color: "--tg-theme-button-text-color",
  secondary_bg_color: "--tg-theme-secondary-bg-color",
  header_bg_color: "--tg-theme-header-bg-color",
  accent_text_color: "--tg-theme-accent-text-color",
  section_bg_color: "--tg-theme-section-bg-color",
  section_header_text_color: "--tg-theme-section-header-text-color",
  subtitle_text_color: "--tg-theme-subtitle-text-color",
  destructive_text_color: "--tg-theme-destructive-text-color",
};

const color = /^#[0-9a-fA-F]{3,8}$/;

export function themeVars(params: Record<string, string> | undefined): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(params ?? {})) {
    if (map[k] && typeof v === "string" && color.test(v)) out[map[k]] = v;
  }
  return out;
}

export function applyTheme(params: Record<string, string> | undefined): void {
  const style = document.documentElement.style; // the CSSOM, so no inline style attribute in the HTML
  for (const [name, value] of Object.entries(themeVars(params))) style.setProperty(name, value);
}

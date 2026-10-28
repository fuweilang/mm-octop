export type UiLocale = "zh" | "en";

export const UI_LOCALE_STORAGE_KEY = "octop:ui-locale";

/**
 * 界面只维护中文（zh-only-ui）。浏览器语言一律按中文处理：
 * 只有用户主动切到 en（``octop:ui-locale``）或服务端下发 en 时才走英文。
 */
export function detectBrowserLocale(): UiLocale {
  return "zh";
}

export function normalizeUiLocale(raw: string | null | undefined): UiLocale {
  if (!raw) return "zh";
  return raw.toLowerCase().startsWith("zh") ? "zh" : "en";
}

export function readStoredUiLocale(): UiLocale | null {
  try {
    const raw = localStorage.getItem(UI_LOCALE_STORAGE_KEY);
    if (raw === "zh" || raw === "en") return raw;
  } catch {
    // localStorage unavailable
  }
  return null;
}

export function storeUiLocale(locale: UiLocale): void {
  try {
    localStorage.setItem(UI_LOCALE_STORAGE_KEY, locale);
  } catch {
    // quota / disabled
  }
}

/** Stored user preference wins; otherwise follow the browser. */
export function resolveInitialLocale(): UiLocale {
  return readStoredUiLocale() ?? detectBrowserLocale();
}

export function syncDocumentLang(locale: UiLocale): void {
  if (typeof document === "undefined") return;
  document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
}

/** BCP-47 tag for STT / SpeechRecognition from dashboard UI locale. */
export function speechLocaleFromUi(locale: string | null | undefined): string {
  return normalizeUiLocale(locale) === "zh" ? "zh-CN" : "en-US";
}

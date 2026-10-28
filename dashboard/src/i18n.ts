import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { getApiUrl } from "./api/config";
import { i18nApi } from "./api/modules/i18n";
import {
  resolveInitialLocale,
  syncDocumentLang,
  type UiLocale,
} from "./utils/localePrefs";

export type { UiLocale } from "./utils/localePrefs";

async function loadLocaleBundle(locale: UiLocale) {
  if (locale === "zh") {
    return (await import("./locales/zh.json")).default;
  }
  return (await import("./locales/en.json")).default;
}

export async function ensureLocaleBundle(locale: UiLocale): Promise<void> {
  if (!i18n.hasResourceBundle(locale, "translation")) {
    const bundle = await loadLocaleBundle(locale);
    i18n.addResourceBundle(locale, "translation", bundle, true, true);
  }
}

/** Raw fetch — avoids importing ``request.ts`` (circular via this module). */
async function isSetupRequired(): Promise<boolean> {
  try {
    const res = await fetch(getApiUrl("/setup/status"));
    if (!res.ok) return true;
    const body = (await res.json()) as { setup_required?: boolean };
    return body.setup_required === true;
  } catch {
    return true;
  }
}

async function hydrateToolLabels(lng: string) {
  const locale = lng.startsWith("zh") ? "zh" : "en";
  try {
    const { labels } = await i18nApi.getToolLabels();
    i18n.addResourceBundle(
      locale,
      "translation",
      { tools: labels },
      true,
      true,
    );
  } catch {
    // bundled locale JSON remains the fallback
  }
}

async function hydrateSkillLabels(lng: string) {
  const locale = lng.startsWith("zh") ? "zh" : "en";
  try {
    const { labels } = await i18nApi.getSkillLabels();
    i18n.addResourceBundle(
      locale,
      "translation",
      { skills: labels },
      true,
      true,
    );
  } catch {
    // bundled locale JSON remains the fallback
  }
}

async function hydrateServerLabels(lng: string) {
  if (await isSetupRequired()) return;
  await Promise.all([hydrateToolLabels(lng), hydrateSkillLabels(lng)]);
}

/** Pull tool/skill labels from the backend (e.g. after setup or login). */
export function refreshServerLabels(lng?: string): Promise<void> {
  return hydrateServerLabels(lng ?? i18n.language);
}

let initPromise: Promise<void> | null = null;

export function initI18n(): Promise<void> {
  if (!initPromise) {
    initPromise = (async () => {
      const initial = resolveInitialLocale();
      // zh-only-ui：只维护中文，缺 key 时回退到「另一门语言」而不是固定 en，
      // 这样英文界面缺条目会显示中文，不会露出裸 key。用户存过的语言 / 服务端
      // 下发的 locale 仍然优先（见 applyUserLocale）。
      const fallback: UiLocale = initial === "zh" ? "en" : "zh";
      const primaryBundle = await loadLocaleBundle(initial);

      await i18n.use(initReactI18next).init({
        resources: {
          [initial]: { translation: primaryBundle },
        },
        lng: initial,
        fallbackLng: fallback,
        supportedLngs: ["zh", "en"],
        nonExplicitSupportedLngs: true,
        interpolation: {
          escapeValue: false,
        },
      });

      syncDocumentLang(initial);

      const prefetchFallback = () => {
        void ensureLocaleBundle(fallback);
      };
      if (typeof requestIdleCallback !== "undefined") {
        requestIdleCallback(prefetchFallback);
      } else {
        setTimeout(prefetchFallback, 2000);
      }

      i18n.on("languageChanged", (lng) => {
        const locale: UiLocale = lng.startsWith("zh") ? "zh" : "en";
        syncDocumentLang(locale);
        void ensureLocaleBundle(locale).then(() => hydrateServerLabels(lng));
      });

      void hydrateServerLabels(i18n.language);
    })();
  }
  return initPromise;
}

export default i18n;

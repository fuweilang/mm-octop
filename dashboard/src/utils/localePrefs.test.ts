import { afterEach, describe, expect, it, vi } from "vitest";
import {
  detectBrowserLocale,
  resolveInitialLocale,
  speechLocaleFromUi,
  storeUiLocale,
  UI_LOCALE_STORAGE_KEY,
} from "./localePrefs";

describe("localePrefs", () => {
  afterEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  it("detectBrowserLocale 默认中文，不看浏览器语言", () => {
    vi.stubGlobal("navigator", {
      language: "en-US",
      languages: ["en-US", "zh-CN"],
    });
    expect(detectBrowserLocale()).toBe("zh");
  });

  it("resolveInitialLocale prefers en only when the user stored it", () => {
    vi.stubGlobal("navigator", {
      language: "zh-CN",
      languages: ["zh-CN"],
    });
    storeUiLocale("en");
    expect(resolveInitialLocale()).toBe("en");
    localStorage.removeItem(UI_LOCALE_STORAGE_KEY);
    expect(resolveInitialLocale()).toBe("zh");
  });

  it("speechLocaleFromUi maps UI locale to STT BCP-47 tags", () => {
    expect(speechLocaleFromUi("zh")).toBe("zh-CN");
    expect(speechLocaleFromUi("zh-CN")).toBe("zh-CN");
    expect(speechLocaleFromUi("en")).toBe("en-US");
    expect(speechLocaleFromUi("en-US")).toBe("en-US");
    expect(speechLocaleFromUi(null)).toBe("zh-CN");
  });
});

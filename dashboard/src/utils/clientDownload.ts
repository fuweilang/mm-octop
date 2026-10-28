export type ClientPlatform = "mac-arm64" | "mac-x64" | "win";

export const CLIENT_FILES: Record<ClientPlatform, string> = {
  "mac-arm64": "/downloads/mammoth-agent-mac-arm64.dmg",
  "mac-x64": "/downloads/mammoth-agent-mac-x64.dmg",
  win: "/downloads/mammoth-agent-win.exe",
};

export type ClientPlatformOption = {
  id: ClientPlatform;
  /** 卡片主标题的 i18n key */
  titleKey: string;
  /** 卡片副标题的 i18n key */
  subKey: string;
};

/** 顺序即弹窗里的展示顺序：Apple 芯片 Mac、Intel 芯片 Mac、Windows。 */
export const CLIENT_PLATFORM_OPTIONS: ClientPlatformOption[] = [
  {
    id: "mac-arm64",
    titleKey: "nav.downloadMacArm",
    subKey: "nav.downloadMacArmSub",
  },
  {
    id: "mac-x64",
    titleKey: "nav.downloadMacIntel",
    subKey: "nav.downloadMacIntelSub",
  },
  {
    id: "win",
    titleKey: "nav.downloadWindows",
    subKey: "nav.downloadWindowsSub",
  },
];

/** 只判断操作系统，不猜芯片（浏览器拿不到可靠的 CPU 型号）。 */
export function detectClientOs(): "mac" | "win" | "other" {
  if (typeof navigator === "undefined") return "other";
  const ua = navigator.userAgent || "";
  const platform = navigator.platform || "";
  if (/Mac|iPhone|iPad/i.test(ua) || /Mac/i.test(platform)) return "mac";
  if (/Win/i.test(ua) || /Win/i.test(platform)) return "win";
  return "other";
}

export function clientDownloadUrl(platform: ClientPlatform) {
  return `${CLIENT_FILES[platform]}?v=${Date.now()}`;
}

export type ClientDownloadInfo = {
  version: string;
  /** YYYY-MM-DD，取 `mammoth-latest.json` 的 `updated_at` */
  date: string;
  /** 安装包大小（MB），拿不到时为 null */
  sizeMb: number | null;
};

const LATEST_MANIFEST = "/downloads/mammoth-latest.json";

/**
 * 弹窗里的统一版本信息行。
 * 任何一步失败都返回 null（整行隐藏），不让弹窗卡住。
 */
export async function fetchClientDownloadInfo(): Promise<ClientDownloadInfo | null> {
  try {
    const res = await fetch(LATEST_MANIFEST, { cache: "no-store" });
    if (!res.ok) return null;
    const data = (await res.json()) as {
      updated_at?: string;
      platforms?: Record<string, { shell?: { version?: string } } | undefined>;
    };
    const version = data.platforms?.["darwin-arm64"]?.shell?.version;
    if (!version) return null;
    return {
      version,
      date: (data.updated_at || "").slice(0, 10),
      sizeMb: await probeSizeMb(CLIENT_FILES["mac-arm64"]),
    };
  } catch {
    return null;
  }
}

async function probeSizeMb(path: string): Promise<number | null> {
  try {
    const res = await fetch(path, { method: "HEAD", cache: "no-store" });
    if (!res.ok) return null;
    const bytes = Number(res.headers.get("content-length") || 0);
    if (!Number.isFinite(bytes) || bytes <= 0) return null;
    return Math.round(bytes / (1024 * 1024));
  } catch {
    return null;
  }
}

export type ClientPlatform = "mac-arm64" | "mac-x64" | "win";

export const CLIENT_FILES: Record<ClientPlatform, string> = {
  "mac-arm64": "/downloads/mammoth-agent-mac-arm64.dmg",
  "mac-x64": "/downloads/mammoth-agent-mac-x64.dmg",
  win: "/downloads/mammoth-agent-win.exe",
};

export const CLIENT_PLATFORM_IDS: ClientPlatform[] = [
  "mac-arm64",
  "mac-x64",
  "win",
];

export function detectClientPlatform(): ClientPlatform {
  if (typeof navigator === "undefined") return "win";
  const ua = navigator.userAgent || "";
  const platform = navigator.platform || "";
  const isMac = /Mac|iPhone|iPad/i.test(ua) || /Mac/i.test(platform);
  if (isMac) return "mac-arm64";
  return "win";
}

export function clientDownloadUrl(platform?: ClientPlatform) {
  const path = CLIENT_FILES[platform ?? detectClientPlatform()];
  return `${path}?v=${Date.now()}`;
}

const SKIP_OIDC_KEY = "mammoth_skip_oidc";

/** null = not loaded. "" = local gateway, use window.location.origin. */
let platformOriginCache: string | null = null;

export function resetPlatformOriginCache(): void {
  platformOriginCache = null;
}

/** 中台 origin. Local dev stays on the gateway origin; the installed client uses production. */
async function loadPlatformOrigin(): Promise<string | null> {
  if (platformOriginCache !== null) return platformOriginCache;
  try {
    const res = await fetch("/api/auth/oauth/status", { cache: "no-store" });
    if (res.ok === false) return null;
    const data = (await res.json()) as { platform_origin?: string };
    platformOriginCache = (data.platform_origin || "").replace(/\/$/, "");
    return platformOriginCache;
  } catch {
    return null;
  }
}

export async function ensurePlatformOrigin(): Promise<string> {
  const origin = await loadPlatformOrigin();
  return origin || window.location.origin;
}

/** null means the probe failed. Do not treat that as "same origin". */
export async function readPlatformOrigin(): Promise<string | null> {
  return loadPlatformOrigin();
}

export function platformUrl(path: string, origin = platformOriginCache || window.location.origin): string {
  return new URL(path, origin.endsWith("/") ? origin : `${origin}/`).toString();
}

/** In-app route the OIDC round-trip returns to. */
const OCTOP_HOME_PATH = "/chat";

/** Absolute URL the unified login sends the user back to after the OIDC round-trip. */
export function octopReturnUrl() {
  const base = import.meta.env.BASE_URL.replace(/\/$/, "");
  return `${window.location.origin}${base}${OCTOP_HOME_PATH}`;
}

/** Desktop shell is a different origin from the 中台, so it cannot share that Cookie. */
export function shouldUseLocalLogin(platformOrigin: string, pageOrigin: string): boolean {
  return Boolean(platformOrigin) && platformOrigin !== pageOrigin;
}

export function localLoginUrl(): string {
  const base = import.meta.env.BASE_URL.replace(/\/$/, "");
  return `${window.location.origin}${base}/login`;
}

export function mammothLoginUrl(next?: string, origin = platformOriginCache || window.location.origin) {
  const url = new URL("/login", origin);
  if (next) url.searchParams.set("next", next);
  return url.toString();
}

export async function goMammothLogin(next?: string) {
  const origin = await ensurePlatformOrigin();
  window.location.replace(mammothLoginUrl(next, origin));
}

function consoleLoginUrl(origin: string, ticket: string, next: string) {
  const url = new URL("/login", origin);
  url.searchParams.set("sso", ticket);
  url.searchParams.set("next", next);
  return url.toString();
}

export class MammothConsoleHandoffError extends Error {
  constructor() {
    super("console handoff missing");
    this.name = "MammothConsoleHandoffError";
  }
}

function requireTicketLoginUrl(url: string): string {
  const parsed = new URL(url);
  if (!parsed.searchParams.get("sso")) {
    throw new MammothConsoleHandoffError();
  }
  return parsed.toString();
}

/** Chrome must open as the client user, not whatever Cookie Chrome already has. */
export async function openEnterpriseConsoleFromClient(): Promise<string> {
  const origin = await ensurePlatformOrigin();
  const { getAuthToken } = await import("../api/request");
  const token = getAuthToken();
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;

  try {
    const res = await fetch("/api/auth/mammoth/console-link", {
      method: "POST",
      headers,
      credentials: "include",
    });
    if (res.ok) {
      const data = (await res.json()) as { url?: string };
      if (data.url) return requireTicketLoginUrl(data.url);
    }
  } catch {
    /* fall through */
  }

  try {
    const res = await fetch(platformUrl("/console/api/auth/browser-sso-ticket", origin), {
      method: "POST",
      credentials: "include",
    });
    if (res.ok) {
      const data = (await res.json()) as { ticket?: string; next?: string };
      if (data.ticket) {
        return consoleLoginUrl(origin, data.ticket, data.next || "/console/");
      }
    }
  } catch {
    /* fall through */
  }

  throw new MammothConsoleHandoffError();
}

/** Desktop Octop has no Vue login page. The IdP origin comes from the authorize URL. */
export class MammothLoginUnreachableError extends Error {
  origin: string;

  constructor(origin: string) {
    super(origin);
    this.name = "MammothLoginUnreachableError";
    this.origin = origin;
  }
}

const IDP_PROBE_MS = 2000;

export async function idpReachable(origin: string): Promise<boolean> {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), IDP_PROBE_MS);
  try {
    await fetch(`${origin}/login`, {
      mode: "no-cors",
      cache: "no-store",
      signal: controller.signal,
    });
    return true;
  } catch {
    return false;
  } finally {
    window.clearTimeout(timer);
  }
}

/**
 * Leave this page for the 猛犸 login.
 *
 * On the gateway, the authorize URL is same-origin and `/login` is the Vue form.
 * On the desktop shell the page is `127.0.0.1:8088`. That origin's `/login` is
 * an empty bounce, so we follow the IdP authorize URL. If that host is down,
 * navigating there paints a blank webview — stay here and report it instead.
 */
export async function startMammothSession(): Promise<void> {
  const { authApi } = await import("../api/modules/auth");
  let authorizationUrl = "";
  try {
    const started = await authApi.startOauth("oidc", OCTOP_HOME_PATH);
    authorizationUrl = started.authorization_url || "";
  } catch {
    authorizationUrl = "";
  }
  if (!authorizationUrl) {
    await goMammothLogin(octopReturnUrl());
    return;
  }

  const target = new URL(authorizationUrl, window.location.href);
  if (target.origin !== window.location.origin) {
    const up = await idpReachable(target.origin);
    if (!up) throw new MammothLoginUnreachableError(target.origin);
  }
  window.location.replace(authorizationUrl);
}

export function markSkipOidc() {
  sessionStorage.setItem(SKIP_OIDC_KEY, "1");
}

export function consumeSkipOidc() {
  const skip = sessionStorage.getItem(SKIP_OIDC_KEY) === "1";
  if (skip) sessionStorage.removeItem(SKIP_OIDC_KEY);
  return skip;
}

export async function logoutMammothIdp() {
  const origin = await ensurePlatformOrigin();
  await fetch(platformUrl("/console/api/auth/logout", origin), {
    method: "POST",
    credentials: "include",
  }).catch(() => undefined);
  localStorage.removeItem("mammothai_is_login");
  localStorage.removeItem("mammothai_current_user");
}

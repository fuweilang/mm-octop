/** Collect first-boot probe details for BootOfflinePanel (test-phase always-on). */

export type ProbeResult = {
  status: number | null;
  body: string;
  error: string;
};

export type BootPhase = "offline" | "initializing";

export type BootDiagnostics = {
  phase: BootPhase;
  appVersion: string;
  pageOrigin: string;
  platformOrigin: string;
  setup: ProbeResult;
  health: ProbeResult;
  oauth: ProbeResult;
  lastError: string;
};

const BODY_LIMIT = 240;

function clip(text: string): string {
  const t = text.replace(/\s+/g, " ").trim();
  return t.length > BODY_LIMIT ? `${t.slice(0, BODY_LIMIT)}…` : t;
}

async function probe(url: string): Promise<ProbeResult> {
  try {
    const res = await fetch(url, { cache: "no-store" });
    let body = "";
    try {
      body = clip(await res.text());
    } catch {
      body = "";
    }
    return { status: res.status, body, error: "" };
  } catch (err) {
    const message =
      err instanceof Error ? err.message : String(err ?? "fetch failed");
    return { status: null, body: "", error: message };
  }
}

function looksInitializing(oauth: ProbeResult): boolean {
  if (oauth.status === 503) return true;
  const blob = `${oauth.body} ${oauth.error}`.toLowerCase();
  return (
    /"ready"\s*:\s*false/.test(oauth.body) ||
    blob.includes("not started") ||
    blob.includes("user manager") ||
    blob.includes("user_manager") ||
    blob.includes("initializ")
  );
}

/** Best-effort version from update/status or /api/version; empty when unavailable. */
async function readAppVersion(): Promise<string> {
  for (const path of ["/api/update/status", "/api/version"]) {
    try {
      const res = await fetch(path, { cache: "no-store" });
      if (!res.ok) continue;
      const data = (await res.json()) as {
        current_version?: string;
        version?: string;
      };
      const v = data.current_version || data.version;
      if (v) return String(v);
    } catch {
      /* ignore */
    }
  }
  return "";
}

export async function collectBootDiagnostics(
  lastError = "",
): Promise<BootDiagnostics> {
  const [health, setup, oauth, appVersion] = await Promise.all([
    probe("/api/health"),
    probe("/api/setup/status"),
    probe("/api/auth/oauth/status"),
    readAppVersion(),
  ]);

  let platformOrigin = "";
  if (oauth.status && oauth.status >= 200 && oauth.status < 300 && oauth.body) {
    try {
      const data = JSON.parse(oauth.body) as { platform_origin?: string };
      platformOrigin = (data.platform_origin || "").replace(/\/$/, "");
    } catch {
      /* ignore */
    }
  }

  const healthUp =
    health.status !== null && health.status >= 200 && health.status < 500;
  const phase: BootPhase =
    healthUp && looksInitializing(oauth) ? "initializing" : "offline";

  const diagnostics: BootDiagnostics = {
    phase,
    appVersion,
    pageOrigin: typeof window !== "undefined" ? window.location.origin : "",
    platformOrigin,
    setup,
    health,
    oauth,
    lastError: lastError || oauth.error || setup.error || health.error || "",
  };
  void reportBootDiagnostics(diagnostics);
  return diagnostics;
}

/** Same signature twice in a row is only reported once per half minute. */
let lastReportKey = "";
let lastReportAt = 0;

function shouldReport(key: string): boolean {
  const now = Date.now();
  if (key === lastReportKey && now - lastReportAt < 30_000) return false;
  lastReportKey = key;
  lastReportAt = now;
  return true;
}

/**
 * Persist the probes to ~/.octop/logs/client-boot.jsonl through the local
 * service. Best effort: the shell already logged its own boot phases, and a
 * failed upload is retried by the shell on the next start.
 */
export async function reportBootDiagnostics(d: BootDiagnostics): Promise<void> {
  if (!shouldReport(`${d.phase}|${d.lastError}`)) return;
  try {
    const { getAuthToken } = await import("../api/request");
    const token = getAuthToken();
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };
    if (token) headers.Authorization = `Bearer ${token}`;
    await fetch("/api/client-log", {
      method: "POST",
      headers,
      cache: "no-store",
      body: JSON.stringify({
        shell_version: d.appVersion,
        runtime_version: d.appVersion,
        // navigator.platform is deprecated but still the cheapest read here.
        platform: typeof navigator !== "undefined" ? navigator.platform : "",
        phase: d.phase,
        last_error: d.lastError,
        probes: { health: d.health, setup: d.setup, oauth: d.oauth },
        ts: new Date().toISOString(),
      }),
    });
  } catch {
    /* offline is exactly when the shell-side jsonl matters */
  }
}

export function formatBootDiagnosticsText(d: BootDiagnostics): string {
  const line = (label: string, p: ProbeResult) => {
    if (p.status === null) return `${label}: (network) ${p.error || "—"}`;
    return `${label}: HTTP ${p.status}${p.body ? ` ${p.body}` : ""}${
      p.error ? ` err=${p.error}` : ""
    }`;
  };
  return [
    `phase: ${d.phase}`,
    `appVersion: ${d.appVersion || "—"}`,
    `pageOrigin: ${d.pageOrigin || "—"}`,
    `platformOrigin: ${d.platformOrigin || "—"}`,
    line("setup/status", d.setup),
    line("health", d.health),
    line("oauth/status", d.oauth),
    `lastError: ${d.lastError || "—"}`,
  ].join("\n");
}

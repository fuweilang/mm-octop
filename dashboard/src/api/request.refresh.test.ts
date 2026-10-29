import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./config", () => ({
  getApiUrl: (path: string) => `/api${path}`,
}));

vi.mock("../i18n", () => ({
  default: { language: "zh" },
}));

type Call = { url: string; init: RequestInit };

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

describe("session refresh on 401", () => {
  let mod: typeof import("./request");
  let calls: Call[];

  beforeEach(async () => {
    vi.resetModules();
    localStorage.clear();
    sessionStorage.clear();
    calls = [];
    mod = await import("./request");
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renews an expired token and replays the request once", async () => {
    mod.setAuthToken("old");
    const responses = [
      jsonResponse({ error: { code: "TOKEN_EXPIRED" } }, 401),
      jsonResponse({ access_token: "new" }),
      jsonResponse({ ok: true }),
    ];
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, init: RequestInit) => {
        calls.push({ url, init });
        return Promise.resolve(responses.shift() as Response);
      }),
    );

    const result = await mod.request<{ ok: boolean }>("/agents");

    expect(result).toEqual({ ok: true });
    expect(calls.map((c) => c.url)).toEqual([
      "/api/agents",
      "/api/auth/refresh",
      "/api/agents",
    ]);
    expect(mod.getAuthToken()).toBe("new");
    const retried = calls[2].init.headers as Record<string, string>;
    expect(retried.Authorization).toBe("Bearer new");
  });

  it("shares one refresh call across parallel 401s", async () => {
    mod.setAuthToken("old");
    let refreshCalls = 0;
    let refreshDone = false;
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, init: RequestInit) => {
        calls.push({ url, init });
        if (url === "/api/auth/refresh") {
          refreshCalls += 1;
          refreshDone = true;
          return Promise.resolve(jsonResponse({ access_token: "new" }));
        }
        const auth = (init.headers as Record<string, string>)?.Authorization;
        if (auth === "Bearer new") {
          return Promise.resolve(jsonResponse({ ok: true }));
        }
        // Hold the first 401 until the refresh has completed, so both requests
        // observe the expired token at the same time.
        return Promise.resolve().then(() =>
          refreshDone
            ? jsonResponse({ error: { code: "TOKEN_EXPIRED" } }, 401)
            : jsonResponse({ error: { code: "TOKEN_EXPIRED" } }, 401),
        );
      }),
    );

    const [a, b] = await Promise.all([
      mod.request("/one"),
      mod.request("/two"),
    ]);
    expect(a).toEqual({ ok: true });
    expect(b).toEqual({ ok: true });
    expect(refreshCalls).toBe(1);
  });

  it("does not retry business 401s", async () => {
    mod.setAuthToken("old");
    window.addEventListener(mod.UNAUTHORIZED_EVENT, (e) => e.preventDefault(), {
      once: true,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, init: RequestInit) => {
        calls.push({ url, init });
        return Promise.resolve(
          jsonResponse({ error: { code: "PEER_LOGIN_REQUIRED" } }, 401),
        );
      }),
    );

    await expect(mod.request("/channels")).rejects.toThrow();
    expect(calls.map((c) => c.url)).toEqual(["/api/channels"]);
  });

  it("gives up when the refresh itself fails", async () => {
    mod.setAuthToken("old");
    window.addEventListener(mod.UNAUTHORIZED_EVENT, (e) => e.preventDefault(), {
      once: true,
    });
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string, init: RequestInit) => {
        calls.push({ url, init });
        if (url === "/api/auth/refresh") {
          return Promise.resolve(jsonResponse({ error: {} }, 401));
        }
        return Promise.resolve(
          jsonResponse({ error: { code: "TOKEN_EXPIRED" } }, 401),
        );
      }),
    );

    await expect(mod.request("/agents")).rejects.toThrow();
    expect(calls.map((c) => c.url)).toEqual([
      "/api/agents",
      "/api/auth/refresh",
    ]);
    expect(mod.getAuthToken()).toBe("");
  });

  it("refreshSession is a no-op without a token", async () => {
    vi.stubGlobal("fetch", vi.fn());
    expect(await mod.refreshSession()).toBe(false);
    expect(fetch).not.toHaveBeenCalled();
  });
});

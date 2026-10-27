import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../api/modules/auth", () => ({
  authApi: {
    startOauth: vi.fn(),
  },
}));

vi.mock("../api/request", () => ({
  getAuthToken: () => "octop-token",
}));

import { authApi } from "../api/modules/auth";
import {
  ensurePlatformOrigin,
  goMammothLogin,
  mammothLoginUrl,
  octopReturnUrl,
  openEnterpriseConsoleFromClient,
  resetPlatformOriginCache,
  shouldUseLocalLogin,
  startMammothSession,
} from "./mammothSso";

function stubLocation(origin: string, href = `${origin}/`) {
  vi.stubGlobal("location", {
    origin,
    href,
    replace: vi.fn(),
  });
}

describe("shouldUseLocalLogin", () => {
  it("stays local when the desktop origin differs from 中台", () => {
    expect(shouldUseLocalLogin("https://www.mmai.vip", "http://127.0.0.1:8088")).toBe(true);
  });

  it("keeps the Vue login when the page is already on 中台", () => {
    expect(shouldUseLocalLogin("https://www.mmai.vip", "https://www.mmai.vip")).toBe(false);
    expect(shouldUseLocalLogin("", "http://127.0.0.1:5201")).toBe(false);
  });
});

describe("mammothSso", () => {
  const startOauth = vi.mocked(authApi.startOauth);

  beforeEach(() => {
    startOauth.mockReset();
    resetPlatformOriginCache();
    stubLocation("http://127.0.0.1:8088");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        json: async () => ({ platform_origin: "" }),
      }),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("builds the local login URL on the current origin", () => {
    expect(mammothLoginUrl("http://127.0.0.1:8088/chat")).toBe(
      "http://127.0.0.1:8088/login?next=http%3A%2F%2F127.0.0.1%3A8088%2Fchat",
    );
    expect(octopReturnUrl()).toBe("http://127.0.0.1:8088/chat");
  });

  it("follows the 猛犸中台 authorize URL even from the desktop origin", async () => {
    const authorizationUrl =
      "http://127.0.0.1:5201/middle/api/oidc/authorize?client_id=mammoth-octop";
    startOauth.mockResolvedValue({ authorization_url: authorizationUrl });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 200 })),
    );

    await startMammothSession();

    expect(window.location.replace).toHaveBeenCalledWith(authorizationUrl);
  });

  it("does not navigate to a dead 中台 from the desktop origin", async () => {
    startOauth.mockResolvedValue({
      authorization_url:
        "http://127.0.0.1:5201/middle/api/oidc/authorize?client_id=mammoth-octop",
    });
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));

    await expect(startMammothSession()).rejects.toMatchObject({
      name: "MammothLoginUnreachableError",
      origin: "http://127.0.0.1:5201",
    });
    expect(window.location.replace).not.toHaveBeenCalled();
  });

  it("follows same-origin OIDC on the gateway", async () => {
    stubLocation("http://127.0.0.1:5201", "http://127.0.0.1:5201/agent/chat");
    startOauth.mockResolvedValue({
      authorization_url:
        "http://127.0.0.1:5201/middle/api/oidc/authorize?client_id=mammoth-octop",
    });
    vi.stubGlobal("fetch", vi.fn());

    await startMammothSession();
    expect(fetch).not.toHaveBeenCalled();

    expect(window.location.replace).toHaveBeenCalledWith(
      "http://127.0.0.1:5201/middle/api/oidc/authorize?client_id=mammoth-octop",
    );
  });

  it("falls back to local login when OIDC start fails", async () => {
    startOauth.mockRejectedValue(new Error("offline"));
    await startMammothSession();
    expect(window.location.replace).toHaveBeenCalledWith(
      mammothLoginUrl(octopReturnUrl()),
    );
  });
});

describe("goMammothLogin", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("replaces the current document", async () => {
    resetPlatformOriginCache();
    stubLocation("http://127.0.0.1:8088");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        json: async () => ({ platform_origin: "" }),
      }),
    );
    await goMammothLogin("http://127.0.0.1:8088/chat");
    expect(window.location.replace).toHaveBeenCalledWith(
      "http://127.0.0.1:8088/login?next=http%3A%2F%2F127.0.0.1%3A8088%2Fchat",
    );
  });

  it("uses the production origin when the desktop server reports one", async () => {
    resetPlatformOriginCache();
    stubLocation("http://127.0.0.1:8088");
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        json: async () => ({ platform_origin: "https://www.mmai.vip" }),
      }),
    );
    expect(await ensurePlatformOrigin()).toBe("https://www.mmai.vip");
    await goMammothLogin("/console/");
    expect(window.location.replace).toHaveBeenCalledWith(
      "https://www.mmai.vip/login?next=%2Fconsole%2F",
    );
  });
});

describe("openEnterpriseConsoleFromClient", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("opens production login with the desktop user's SSO ticket", async () => {
    resetPlatformOriginCache();
    stubLocation("http://127.0.0.1:8088");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string) => {
        const url = String(input);
        if (url.includes("/api/auth/oauth/status")) {
          return {
            ok: true,
            json: async () => ({ platform_origin: "https://www.mmai.vip" }),
          };
        }
        if (url.includes("/api/auth/mammoth/console-link")) {
          return {
            ok: true,
            json: async () => ({
              url: "https://www.mmai.vip/login?sso=ticket-1&next=%2Fconsole%2F",
            }),
          };
        }
        throw new Error(url);
      }),
    );

    await expect(openEnterpriseConsoleFromClient()).resolves.toBe(
      "https://www.mmai.vip/login?sso=ticket-1&next=%2Fconsole%2F",
    );
  });

  it("falls back to a same-origin ticket when desktop handoff is missing", async () => {
    resetPlatformOriginCache();
    stubLocation("http://127.0.0.1:5201");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string) => {
        const url = String(input);
        if (url.includes("/api/auth/oauth/status")) {
          return { ok: true, json: async () => ({ platform_origin: "" }) };
        }
        if (url.includes("/api/auth/mammoth/console-link")) {
          return { ok: false, json: async () => ({}) };
        }
        if (url.includes("/console/api/auth/browser-sso-ticket")) {
          return {
            ok: true,
            json: async () => ({ ticket: "ticket-2", next: "/admin" }),
          };
        }
        throw new Error(url);
      }),
    );

    await expect(openEnterpriseConsoleFromClient()).resolves.toBe(
      "http://127.0.0.1:5201/login?sso=ticket-2&next=%2Fadmin",
    );
  });

  it("does not open Chrome as whatever user is already logged in", async () => {
    resetPlatformOriginCache();
    stubLocation("http://127.0.0.1:8088");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: string) => {
        const url = String(input);
        if (url.includes("/api/auth/oauth/status")) {
          return {
            ok: true,
            json: async () => ({ platform_origin: "https://www.mmai.vip" }),
          };
        }
        return { ok: false, json: async () => ({}) };
      }),
    );

    await expect(openEnterpriseConsoleFromClient()).rejects.toMatchObject({
      name: "MammothConsoleHandoffError",
    });
  });
});

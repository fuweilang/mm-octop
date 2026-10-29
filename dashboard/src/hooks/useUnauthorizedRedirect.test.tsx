import { afterEach, describe, expect, it, vi } from "vitest";
import { act, render, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { UNAUTHORIZED_EVENT } from "../api/request";
import { useUnauthorizedRedirect } from "./useUnauthorizedRedirect";

const { startOauth } = vi.hoisted(() => ({ startOauth: vi.fn() }));

vi.mock("../api/modules/auth", () => ({ authApi: { startOauth } }));

function Harness() {
  useUnauthorizedRedirect();
  return <div>chat screen</div>;
}

function dispatchUnauthorized(): Event {
  const event = new CustomEvent(UNAUTHORIZED_EVENT, { cancelable: true });
  act(() => {
    window.dispatchEvent(event);
  });
  return event;
}

/** jsdom's ``location.replace`` cannot be spied on directly. */
function stubLocationReplace() {
  const original = window.location;
  const replace = vi.fn();
  Object.defineProperty(window, "location", {
    configurable: true,
    value: { ...original, replace },
  });
  return {
    replace,
    restore: () => {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: original,
      });
    },
  };
}

function renderHarness() {
  return render(
    <MemoryRouter initialEntries={["/chat"]}>
      <Harness />
    </MemoryRouter>,
  );
}

describe("useUnauthorizedRedirect", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    startOauth.mockReset();
  });

  it("cancels the default handling and starts the SSO round-trip in place", async () => {
    startOauth.mockResolvedValue({
      authorization_url: "https://idp.test/authorize?x=1",
    });
    const stub = stubLocationReplace();
    // The IdP lives on the 中台 origin, so the session starter probes it first
    // (a dead IdP would paint a blank webview). Answer the probe.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("", { status: 200 })),
    );
    try {
      renderHarness();

      const event = dispatchUnauthorized();

      expect(event.defaultPrevented).toBe(true);
      await waitFor(() =>
        expect(stub.replace).toHaveBeenCalledWith(
          "https://idp.test/authorize?x=1",
        ),
      );
      expect(startOauth).toHaveBeenCalledWith("oidc", "/chat");
    } finally {
      stub.restore();
      vi.unstubAllGlobals();
    }
  });

  it("falls back to the unified login when the SSO round-trip cannot start", async () => {
    startOauth.mockRejectedValue(new Error("offline"));
    const stub = stubLocationReplace();
    const origin = window.location.origin;
    try {
      renderHarness();

      dispatchUnauthorized();

      await waitFor(() => expect(stub.replace).toHaveBeenCalledTimes(1));
      const target = new URL(stub.replace.mock.calls[0][0] as string);
      expect(target.pathname).toBe("/login");
      // The return URL keeps the user inside the agent after the round-trip.
      // It follows the deployed base path: "/" in dev, "/agent/" in prod.
      const base = import.meta.env.BASE_URL.replace(/\/$/, "");
      expect(target.searchParams.get("next")).toBe(`${origin}${base}/chat`);
    } finally {
      stub.restore();
    }
  });

  it("stops handling the event after unmount", () => {
    const { unmount } = renderHarness();
    unmount();

    expect(dispatchUnauthorized().defaultPrevented).toBe(false);
  });
});

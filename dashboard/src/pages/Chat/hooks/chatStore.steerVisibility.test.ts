import { afterEach, describe, expect, it, vi } from "vitest";
import {
  attachThread,
  getSnapshot,
  removeSession,
  sendSteerWhileBusy,
} from "./chatStore";

vi.mock("../../../api/config", () => ({
  getApiUrl: (path: string) => `/api${path}`,
}));

const SESSION = "test-steer-visibility";

describe("steer user bubble visibility", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    removeSession(SESSION);
  });

  function installSocket() {
    const sockets: Array<{
      readyState: number;
      onopen: (() => void) | null;
      onmessage: ((event: { data: string }) => void) | null;
      onclose: (() => void) | null;
      push: (frame: Record<string, unknown>) => void;
    }> = [];
    class FakeSocket {
      static readonly CONNECTING = 0;
      static readonly OPEN = 1;
      static readonly CLOSING = 2;
      static readonly CLOSED = 3;
      readyState = FakeSocket.CONNECTING;
      onopen: (() => void) | null = null;
      onmessage: ((event: { data: string }) => void) | null = null;
      onerror: (() => void) | null = null;
      onclose: (() => void) | null = null;
      constructor(_url: string) {
        sockets.push(this);
        queueMicrotask(() => {
          this.readyState = FakeSocket.OPEN;
          this.onopen?.();
        });
      }
      send(_data: string) {}
      close() {
        this.readyState = FakeSocket.CLOSED;
        this.onclose?.();
      }
      push(frame: Record<string, unknown>) {
        this.onmessage?.({ data: JSON.stringify(frame) });
      }
    }
    vi.stubGlobal("WebSocket", FakeSocket);
    return sockets;
  }

  async function startSteer() {
    const sockets = installSocket();
    await attachThread(SESSION, "agent-1", "thr-one");
    const socket = sockets[0];
    socket?.push({
      type: "turn.started",
      turn_id: "turn-1",
      thread_id: "thr-one",
    });
    expect(
      sendSteerWhileBusy(SESSION, "按你的建议干吧", {
        expectedTurnId: "turn-1",
        clientPendingId: "c1",
      }),
    ).toBe("ok");
    socket?.push({
      type: "steer.queued",
      client_pending_id: "c1",
      pending_id: "p1",
      thread_id: "thr-one",
      preview: "按你的建议干吧",
    });
    return socket;
  }

  it("keeps the optimistic user bubble when drain arrives without a canonical frame", async () => {
    const socket = await startSteer();
    expect(
      getSnapshot(SESSION).messages.some(
        (message) => message.id === "steer:c1",
      ),
    ).toBe(true);

    socket?.push({
      type: "steer.drained",
      client_pending_ids: ["c1"],
      pending_ids: ["p1"],
      turn_id: "turn-1",
      thread_id: "thr-one",
    });

    const snap = getSnapshot(SESSION);
    const row = snap.messages.find((message) => message.role === "user");
    expect(row?.content).toBe("按你的建议干吧");
    expect(row?.steerPending).toBe(false);
  });

  it("keeps the user bubble after turn.ended if the canonical frame never arrived", async () => {
    const socket = await startSteer();
    socket?.push({
      type: "turn.ended",
      turn_id: "turn-1",
      thread_id: "thr-one",
    });

    expect(
      getSnapshot(SESSION).messages.some(
        (message) =>
          message.role === "user" && message.content === "按你的建议干吧",
      ),
    ).toBe(true);
  });

  it("replaces the optimistic row with the canonical user frame", async () => {
    const socket = await startSteer();
    socket?.push({
      type: "user",
      content: "按你的建议干吧",
      message_id: "msg-canonical",
      meta: { steer: true },
      thread_id: "thr-one",
    });

    const users = getSnapshot(SESSION).messages.filter(
      (message) => message.role === "user",
    );
    expect(users).toHaveLength(1);
    expect(users[0]?.id).toBe("msg-canonical");
    expect(users[0]?.steerPending).toBeFalsy();
  });

  it("drops the optimistic row when the steer is discarded", async () => {
    const socket = await startSteer();
    socket?.push({
      type: "steer.discarded",
      client_pending_ids: ["c1"],
      pending_ids: ["p1"],
      turn_id: "turn-1",
      thread_id: "thr-one",
      reason: "turn_failed",
    });

    expect(
      getSnapshot(SESSION).messages.some(
        (message) => message.content === "按你的建议干吧",
      ),
    ).toBe(false);
  });
});

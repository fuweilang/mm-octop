import { afterEach, describe, expect, it } from "vitest";
import {
  appendUserMessage,
  getSnapshot,
  ingestHarnessChunk,
  removeSession,
} from "./chatStore";

const SESSION = "test-remote-user-echo";

function userMessages() {
  return getSnapshot(SESSION).messages.filter((m) => m.role === "user");
}

describe("thread-wide user echo (question sent from another tab)", () => {
  afterEach(() => {
    removeSession(SESSION);
  });

  it("adds the question the other tab sent", () => {
    ingestHarnessChunk(SESSION, {
      type: "user",
      content: "按照你的建议先干了再说",
      thread_id: SESSION,
      meta: { echo: true, client_pending_id: "cp-tab-b" },
    } as never);

    const rows = userMessages();
    expect(rows).toHaveLength(1);
    expect(rows[0].content).toBe("按照你的建议先干了再说");
    expect(rows[0].clientPendingId).toBe("cp-tab-b");
  });

  it("drops the echo of a turn this tab sent", () => {
    appendUserMessage(SESSION, {
      id: "local-1",
      role: "user",
      content: "你好",
      status: "done",
      timestamp: Date.now(),
      clientPendingId: "cp-tab-a",
    });

    ingestHarnessChunk(SESSION, {
      type: "user",
      content: "你好",
      thread_id: SESSION,
      meta: { echo: true, client_pending_id: "cp-tab-a" },
    } as never);

    expect(userMessages()).toHaveLength(1);
    expect(userMessages()[0].id).toBe("local-1");
  });

  it("ignores a repeated echo of the same remote turn", () => {
    const frame = {
      type: "user",
      content: "同一个问题",
      thread_id: SESSION,
      meta: { echo: true, client_pending_id: "cp-tab-b" },
    };
    ingestHarnessChunk(SESSION, frame as never);
    ingestHarnessChunk(SESSION, frame as never);

    expect(userMessages()).toHaveLength(1);
  });

  it("puts the question above an answer that is already streaming", () => {
    ingestHarnessChunk(SESSION, {
      type: "token",
      content: "先答后到的问题",
      thread_id: SESSION,
    });
    ingestHarnessChunk(SESSION, {
      type: "user",
      content: "问题",
      thread_id: SESSION,
      meta: { echo: true, client_pending_id: "cp-tab-b" },
    } as never);

    const messages = getSnapshot(SESSION).messages;
    expect(messages[0].role).toBe("user");
    expect(messages[0].content).toBe("问题");
  });

  it("leaves steer user frames to the steer protocol", () => {
    ingestHarnessChunk(SESSION, {
      type: "user",
      content: "补一句",
      thread_id: SESSION,
      meta: { steer: true },
    } as never);

    expect(userMessages()).toHaveLength(0);
  });

  it("does not glue a later host answer onto a finished bubble", () => {
    ingestHarnessChunk(SESSION, { type: "token", content: "能干活。" });
    ingestHarnessChunk(SESSION, { type: "done" });
    ingestHarnessChunk(SESSION, {
      type: "user",
      content: "再测试一下，可以帮我写代码吗",
      thread_id: SESSION,
      meta: { echo: true, client_pending_id: "cp-tab-b" },
    } as never);
    ingestHarnessChunk(SESSION, { type: "token", content: "可以。" });

    const messages = getSnapshot(SESSION).messages;
    expect(messages.map((m) => m.role)).toEqual(["assistant", "user", "assistant"]);
    expect(messages[0].content).toBe("能干活。");
    expect(messages[1].content).toBe("再测试一下，可以帮我写代码吗");
    expect(messages[2].content).toBe("可以。");
  });

  it("opens a new host bubble even when the other tab missed the question", () => {
    ingestHarnessChunk(SESSION, { type: "token", content: "能干活。" });
    ingestHarnessChunk(SESSION, { type: "done" });
    ingestHarnessChunk(SESSION, { type: "token", content: "可以。" });

    const assistants = getSnapshot(SESSION).messages.filter(
      (m) => m.role === "assistant",
    );
    expect(assistants).toHaveLength(2);
    expect(assistants[0].content).toBe("能干活。");
    expect(assistants[1].content).toBe("可以。");
  });
});

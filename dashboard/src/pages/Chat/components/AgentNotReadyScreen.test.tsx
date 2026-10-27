import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import AgentNotReadyScreen from "./AgentNotReadyScreen";

const requestMock = vi.fn();
const refreshMock = vi.fn();

vi.mock("../../../api/request", () => ({
  request: (...args: unknown[]) => requestMock(...args),
}));

vi.mock("../../../context/AgentContext", () => ({
  useAgent: () => ({ refresh: refreshMock }),
}));

function renderScreen() {
  return render(
    <MemoryRouter initialEntries={["/chat"]}>
      <Routes>
        <Route
          path="/chat"
          element={<AgentNotReadyScreen agent={null} noAgents />}
        />
        <Route path="/chat/:agentId" element={<div>chat-ready</div>} />
        <Route path="/experts" element={<div>experts-page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("AgentNotReadyScreen zero-expert entry", () => {
  beforeEach(() => {
    requestMock.mockReset();
    refreshMock.mockReset();
    refreshMock.mockResolvedValue(undefined);
  });

  it("creates the default expert and enters chat without visiting the expert list", async () => {
    requestMock.mockImplementation((url: string) => {
      if (url === "/experts/default") {
        return Promise.resolve({ id: "general-assistant" });
      }
      if (url === "/agents/from-expert/general-assistant") {
        return Promise.resolve({ agent_id: "main" });
      }
      return Promise.reject(new Error(`unexpected ${url}`));
    });

    renderScreen();

    await waitFor(() => {
      expect(screen.getByText("chat-ready")).toBeInTheDocument();
    });
    expect(requestMock).toHaveBeenCalledWith(
      "/agents/from-expert/general-assistant",
      { method: "POST", body: JSON.stringify({}) },
    );
    expect(refreshMock).toHaveBeenCalledWith({ force: true });
  });

  it("keeps the expert list entry available when bootstrap fails", async () => {
    requestMock.mockRejectedValue(new Error("boom"));

    const user = userEvent.setup();
    renderScreen();
    await waitFor(() => {
      expect(requestMock).toHaveBeenCalled();
    });

    await user.click(screen.getByRole("button", { name: "chat.createExpert" }));
    expect(screen.getByText("experts-page")).toBeInTheDocument();
  });
});

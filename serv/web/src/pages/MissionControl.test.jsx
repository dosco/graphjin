import React from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConsoleProvider } from "@/components/Layout/console-context";
import MissionControl from "./MissionControl";

vi.mock("../services/mission", async (importOriginal) => ({
  ...(await importOriginal()),
  fetchMissionTasks: vi.fn(async () => []),
  fetchMissionWatches: vi.fn(async () => ({ watches: [], events: [] })),
  fetchMissionAnnotations: vi.fn(async () => []),
}));

afterEach(() => {
  cleanup();
  localStorage.clear();
});

function renderTasks(bootstrap) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ConsoleProvider value={{ bootstrap, isLoading: false, error: null }}>
        <MemoryRouter initialEntries={["/user/tasks"]}>
          <MissionControl section="tasks" />
        </MemoryRouter>
      </ConsoleProvider>
    </QueryClientProvider>
  );
}

describe("mission control identity", () => {
  it("treats a signed-in console session as the owner, even with no tasks yet", async () => {
    renderTasks({ identity: { role: "admin", authenticated: true, session: true, display_name: "Ana Ruiz" } });
    expect(await screen.findByText("No tasks")).toBeInTheDocument();
    expect(screen.queryByText(/Set an identity/)).not.toBeInTheDocument();
  });

  it("asks for an identity when there is neither a session nor a dev identity", async () => {
    renderTasks({ identity: { role: "anon", authenticated: false } });
    expect(await screen.findByText(/Set an identity/)).toBeInTheDocument();
  });
});

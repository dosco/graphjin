import React from "react";
import axe from "axe-core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter } from "react-router-dom";

import { ThemeProvider } from "@/components/theme-provider";
import Header from "./Header";
import { ConsoleProvider } from "./console-context";

afterEach(() => {
  cleanup();
  localStorage.clear();
});

function renderHeader(bootstrap, route = "/user/agent") {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="light">
        <ConsoleProvider value={{ bootstrap, isLoading: false, error: null }}>
          <MemoryRouter initialEntries={[route]}>
            <Header />
          </MemoryRouter>
        </ConsoleProvider>
      </ThemeProvider>
    </QueryClientProvider>
  );
}

describe("console header", () => {
  it("renders only capability-advertised workspaces", () => {
    renderHeader({ scope: { environment: "agentic" }, identity: { role: "user" }, workspaces: [{ id: "user" }] });
    expect(screen.getByRole("button", { name: "User" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Admin" })).not.toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "User workspace" })).toBeInTheDocument();
  });

  it("has no detectable accessibility violations in the default shell", async () => {
    const { container } = renderHeader({
      scope: { environment: "dev" },
      identity: { role: "admin" },
      workspaces: [{ id: "user" }, { id: "admin" }],
    });
    const result = await axe.run(container);
    expect(result.violations).toEqual([]);
  });
});

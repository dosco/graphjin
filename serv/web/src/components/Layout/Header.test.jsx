import React from "react";
import axe from "axe-core";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter } from "react-router-dom";

import { ThemeProvider } from "@/components/theme-provider";
import Header from "./Header";
import { ConsoleProvider } from "./console-context";

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
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

  it("asks a JWT-only server's visitor to sign in instead of offering dev headers", async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ user_id: "demo-operator" }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    renderHeader({
      scope: { environment: "agentic" },
      identity: { role: "anon", authenticated: false, sign_in: { methods: ["demo", "token"] } },
      workspaces: [{ id: "user" }],
    });

    expect(await screen.findByRole("heading", { name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByText("User ID")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Access token")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /demo operator/ }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe("/api/v1/auth/session");
    expect(options.method).toBe("POST");
    expect(JSON.parse(options.body)).toEqual({ method: "demo" });
  });

  it("shows the signed-in session and offers sign out", () => {
    renderHeader({
      scope: { environment: "agentic" },
      identity: { role: "admin", authenticated: true, session: true, display_name: "Ana Ruiz" },
      workspaces: [{ id: "user" }],
    });
    fireEvent.click(screen.getByRole("button", { name: "Signed in as Ana Ruiz" }));
    expect(screen.getByRole("button", { name: /Sign out/ })).toBeInTheDocument();
    expect(screen.getByText("admin")).toBeInTheDocument();
  });
});

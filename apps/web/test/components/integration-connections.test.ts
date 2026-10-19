import { createElement, type ComponentProps } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";
import IntegrationsList from "@/app/integrations/IntegrationsList";
import { pluginConnectionsFixture } from "@/app/integrations/visual-fixture";

const state = vi.hoisted(() => ({ confirm: vi.fn(), success: vi.fn(), error: vi.fn(), buttons: [] as Array<{ children?: unknown; disabled?: boolean; onClick?: () => void }> }));
vi.mock("next/navigation", () => ({ useSearchParams: () => new URLSearchParams() }));
vi.mock("@/components/AppHeader", () => ({ default: () => null }));
vi.mock("@/components/ConfirmModal", () => ({ confirmDialog: state.confirm }));
vi.mock("sonner", () => ({ toast: { success: state.success, error: state.error } }));
vi.mock("@/components/ui/button", async importOriginal => {
  const actual = await importOriginal<typeof import("@/components/ui/button")>();
  return { ...actual, Button: (props: ComponentProps<typeof actual.Button>) => {
    state.buttons.push(props as (typeof state.buttons)[number]);
    return createElement(actual.Button, props);
  } };
});

beforeEach(() => { vi.clearAllMocks(); state.buttons.length = 0; });
afterEach(() => vi.unstubAllGlobals());

function render(props: Partial<ComponentProps<typeof IntegrationsList>> = {}) {
  return renderToStaticMarkup(createElement(IntegrationsList, { initial: pluginConnectionsFixture, ...props }));
}
const disconnect = () => state.buttons.find(button => button.children === "Disconnect")!;

describe("integration connection actions", () => {
  it("does not disconnect when confirmation is cancelled", async () => {
    const fetch = vi.fn(); vi.stubGlobal("fetch", fetch);
    state.confirm.mockResolvedValue(false);
    render(); disconnect().onClick!();
    await vi.waitFor(() => expect(state.confirm).toHaveBeenCalledWith(expect.objectContaining({ title: "Disconnect Scalekit workspace?", destructive: true })));
    expect(fetch).not.toHaveBeenCalled();
  });
  it("uses the provider label on success and keeps failure recoverable", async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true }); vi.stubGlobal("fetch", fetch);
    state.confirm.mockResolvedValue(true);
    render(); disconnect().onClick!();
    await vi.waitFor(() => expect(state.success).toHaveBeenCalledWith("Disconnected Scalekit workspace"));
    expect(fetch).toHaveBeenCalledWith("/api/integrations/disconnect/scalekit", { method: "POST" });
    fetch.mockResolvedValue({ ok: false, status: 502, json: async () => ({ error: "Service unavailable" }) });
    disconnect().onClick!();
    await vi.waitFor(() => expect(state.error).toHaveBeenCalledWith("Could not disconnect Scalekit workspace. Try again.", { description: "Service unavailable" }));
  });
  it("blocks preview and non-admin workspace actions", async () => {
    const fetch = vi.fn(); vi.stubGlobal("fetch", fetch);
    for (const props of [{ preview: true }, { isAdmin: false }]) {
      state.buttons.length = 0;
      render(props);
      expect(disconnect().disabled).toBe(true);
      disconnect().onClick!();
    }
    expect(state.confirm).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
  });
});

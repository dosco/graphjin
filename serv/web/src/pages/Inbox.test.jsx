import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { visibleItems, workspaceDefinitions } from "../app/workspaces";
import { NotificationItem, relativeTime } from "./Inbox";

afterEach(cleanup);

describe("inbox", () => {
  it("shows the Inbox item only when the server grants notifications", () => {
    const user = workspaceDefinitions.user;
    expect(visibleItems(user, null).map((item) => item.label)).not.toContain("Inbox");
    const granted = { workspaces: [{ id: "user", capabilities: ["gj_notification"] }] };
    expect(visibleItems(user, granted).map((item) => item.label)).toContain("Inbox");
  });

  it("emphasizes unseen notifications and opens their link", async () => {
    const user = userEvent.setup();
    const onOpen = vi.fn();
    const onToggleSeen = vi.fn();
    render(
      <NotificationItem
        item={{ id: "n1", kind: "routine_answer", title: "Failed invoices fired", body: "Two invoices failed twice.", link: "/user/agent?thread=t1", seen: false, created_at: new Date().toISOString() }}
        onOpen={onOpen}
        onToggleSeen={onToggleSeen}
      />,
    );
    expect(screen.getByRole("heading", { name: "Failed invoices fired" }).className).toContain("font-semibold");
    await user.click(screen.getByRole("button", { name: "Open" }));
    expect(onOpen).toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Mark seen" }));
    expect(onToggleSeen).toHaveBeenCalled();
  });

  it("reads times relative to now", () => {
    const now = Date.parse("2026-11-09T12:00:00Z");
    expect(relativeTime("2026-11-09T11:59:40Z", now)).toBe("just now");
    expect(relativeTime("2026-11-09T09:00:00Z", now)).toMatch(/3 hours ago/);
  });
});

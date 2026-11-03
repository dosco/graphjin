import React from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { groupThreadsByAgent, ThreadList } from "./AgentChat";

afterEach(cleanup);

const profiles = [
  { id: "agent_profile:fin", name: "Finance", mine: true },
  { id: "agent_profile:ops", name: "Ops", mine: false },
];

const threads = [
  { id: "th_1", title: "Revenue question", profile: "" },
  { id: "th_home_fin", title: "Finance", profile: "agent_profile:fin", unread: true },
  { id: "th_2", title: "Hidden agent chat", profile: "agent_profile:gone" },
];

describe("agent thread sidebar", () => {
  it("groups threads under their agent and keeps unknown profiles with plain chats", () => {
    const groups = groupThreadsByAgent(threads, profiles);
    expect(groups.map((group) => group.profile?.name || "chats")).toEqual(["chats", "Finance", "Ops"]);
    expect(groups[0].threads.map((thread) => thread.id)).toEqual(["th_1", "th_2"]);
    expect(groups[1].threads.map((thread) => thread.id)).toEqual(["th_home_fin"]);
    expect(groups[1].unread).toBe(true);
    expect(groups[2]).toMatchObject({ threads: [], unread: false });
  });

  it("shows an unread dot on the agent and opens its home thread", async () => {
    const user = userEvent.setup();
    const onSelectProfile = vi.fn();
    const onNewAgent = vi.fn();
    render(
      <ThreadList threads={threads} profiles={profiles} activeID="" onSelect={vi.fn()} onSelectProfile={onSelectProfile} onNewAgent={onNewAgent} />,
    );
    const agents = screen.getByRole("region", { name: "Agents" });
    const finance = within(agents).getByRole("button", { name: /Finance/ });
    expect(within(finance).getByLabelText("Unread")).toBeInTheDocument();
    expect(within(within(agents).getByRole("button", { name: /Ops/ })).queryByLabelText("Unread")).toBeNull();

    await user.click(finance);
    expect(onSelectProfile).toHaveBeenCalledWith(profiles[0]);
    await user.click(within(agents).getByRole("button", { name: "New agent" }));
    expect(onNewAgent).toHaveBeenCalled();

    const chats = screen.getByRole("region", { name: "Chats" });
    expect(within(chats).getByText("Revenue question")).toBeInTheDocument();
    expect(within(chats).queryByLabelText("Unread")).toBeNull();
  });
});

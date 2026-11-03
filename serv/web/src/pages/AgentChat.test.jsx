import React from "react";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AgentInspector, groupActionsByDelegate, groupThreadsByAgent, ResponseSummaryBadges, ThinkingMessage, ThreadList } from "./AgentChat";

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

// Three questions run at once, so their events arrive interleaved: the lead's
// ask_graphjin record (running, then done/error) and each worker's own actions.
const questions = ["Revenue last month?", "Top customer by orders?", "Open support tickets?"];
const lead = (delegate, status, extra = {}) => ({ index: delegate, source: "lead", tool: "ask_graphjin", status, delegate, question: questions[delegate - 1], ...extra });
const worker = (delegate, index, tool, args = {}) => ({ index, source: "worker", tool, status: "done", delegate, args });
const interleaved = [
  { index: 1, tool: "query_catalog", status: "done", args: { search: "plan" } },
  lead(2, "running"),
  lead(1, "running"),
  worker(2, 1, "query_catalog", { search: "customers" }),
  lead(3, "running"),
  worker(1, 1, "query_catalog", { search: "orders" }),
  worker(3, 1, "query_catalog", { search: "tickets" }),
  worker(1, 2, "execute_graphql"),
  lead(1, "done"),
  worker(2, 2, "execute_graphql"),
  lead(3, "error", { error: "worker timed out" }),
];

describe("delegated questions", () => {
  it("groups interleaved actions under their question, ordered by delegate", () => {
    const entries = groupActionsByDelegate(interleaved);
    expect(entries.map((entry) => entry.kind)).toEqual(["action", "question", "question", "question"]);
    const [, first, second, third] = entries;
    expect([first, second, third].map((entry) => [entry.delegate, entry.question, entry.status])).toEqual([
      [1, questions[0], "done"],
      [2, questions[1], "running"],
      [3, questions[2], "error"],
    ]);
    expect(first.actions.map((action) => action.tool)).toEqual(["query_catalog", "execute_graphql"]);
    expect(second.actions.map((action) => action.args.search ?? action.tool)).toEqual(["customers", "execute_graphql"]);
    expect(third.actions).toHaveLength(1);
    expect(third.error).toBe("worker timed out");
  });

  it("leaves a plain run's actions as one entry each", () => {
    const actions = [
      { index: 1, tool: "query_catalog", status: "done" },
      { index: 2, tool: "execute_graphql", status: "error" },
    ];
    expect(groupActionsByDelegate(actions)).toEqual(actions.map((action) => ({ kind: "action", action })));
  });

  it("renders one live row per question with its worker actions folded underneath", () => {
    render(<ThinkingMessage actions={interleaved} />);
    const rows = questions.map((question) => screen.getByText(question).closest("li"));
    expect(rows[0].compareDocumentPosition(rows[1]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(rows[1].compareDocumentPosition(rows[2]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    expect(within(rows[0]).getByRole("img", { name: "Done" })).toBeInTheDocument();
    expect(within(rows[0]).getByText("query_catalog(search=orders)")).toBeInTheDocument();
    expect(within(rows[0]).getByText("execute_graphql")).toBeInTheDocument();
    expect(within(rows[1]).getByRole("img", { name: "Running" })).toBeInTheDocument();
    expect(within(rows[1]).getByText("query_catalog(search=customers)")).toBeInTheDocument();
    expect(within(rows[1]).queryByText("query_catalog(search=orders)")).toBeNull();
    expect(within(rows[2]).getByRole("img", { name: "Failed" })).toBeInTheDocument();
    expect(within(rows[2]).getByText("worker timed out")).toBeInTheDocument();
    // The lead's own ask_graphjin records become the question rows, not trail lines.
    expect(screen.queryByText(/ask_graphjin/)).toBeNull();
    expect(screen.getByText("query_catalog(search=plan)")).toBeInTheDocument();
  });

  it("renders a plain run's trail unchanged", () => {
    render(<ThinkingMessage actions={[{ index: 1, tool: "query_catalog", status: "done", args: { id: "table:db.orders" } }, { index: 2, tool: "execute_graphql", status: "done" }]} />);
    const items = screen.getAllByRole("listitem");
    expect(items.map((item) => item.textContent)).toEqual(["query_catalog(id=table:db.orders)", "execute_graphql"]);
    expect(screen.queryByRole("img", { name: /Running|Done|Failed/ })).toBeNull();
  });

  it("summarizes a lead response as questions and actions and opens the Questions tab", async () => {
    const user = userEvent.setup();
    const onInspect = vi.fn();
    const response = {
      answer: "Revenue grew.",
      actions: interleaved.filter((action) => action.status !== "running"),
      data: {
        delegations: questions.map((question, index) => ({ question, status: index === 2 ? "error" : "answered", answer: `Answer ${index + 1}`, data: { n: index } })),
      },
    };
    render(<ResponseSummaryBadges response={response} onInspect={onInspect} />);
    await user.click(screen.getByRole("button", { name: "3 questions · 6 actions" }));
    expect(onInspect).toHaveBeenCalledWith(expect.objectContaining({ tab: "questions" }));
    cleanup();

    render(<AgentInspector inspection={{ response, tab: "questions" }} onClose={vi.fn()} />);
    const tabs = screen.getAllByRole("tab").map((tab) => tab.textContent);
    expect(tabs[1]).toMatch(/^Questions/);
    expect(screen.getByText("Answer 2")).toBeInTheDocument();
    expect(screen.getByText(/"n": 2/)).toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /Actions/ }));
    const groups = questions.map((question) => screen.getByText(question).closest("li"));
    expect(within(groups[0]).getAllByText(/query_catalog|execute_graphql/)).toHaveLength(2);
    expect(within(groups[2]).getByText("error")).toBeInTheDocument();
  });

  it("keeps a response without delegations unchanged", async () => {
    const user = userEvent.setup();
    const onInspect = vi.fn();
    const response = { answer: "ok", data: { rows: 1 }, actions: [{ index: 1, tool: "query_catalog", status: "done" }, { index: 2, tool: "execute_graphql", status: "done" }] };
    render(<ResponseSummaryBadges response={response} onInspect={onInspect} />);
    expect(screen.queryByText(/question/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "2 actions" }));
    expect(onInspect).toHaveBeenCalledWith(expect.objectContaining({ tab: "actions" }));
    cleanup();

    render(<AgentInspector inspection={{ response, tab: "actions" }} onClose={vi.fn()} />);
    expect(screen.getAllByRole("tab").map((tab) => tab.textContent)).toEqual(["Data1 key", "Actions2 items"]);
    expect(screen.getAllByRole("listitem").map((item) => item.querySelector(".font-mono")?.textContent)).toEqual(["query_catalog", "execute_graphql"]);
  });
});

---
title: "Agentic"
description: "MCP, catalog discovery, security posture, durable verified tasks, source mode, workflows, watches, and OAuth."
nav_group: "agentic"
weight: 4
---

Agentic GraphJin gives AI clients a discoverable, policy-aware surface for data access and controlled operations. Use the [built-in agent](/agentic/server-agent/) or connect an AI client over [MCP](/agentic/mcp/), then choose the smallest governed path that matches the request.

<div class="agentic-path-grid">
  <article>
    <span>01 · Ask</span>
    <h2>Get a governed answer now.</h2>
    <p>GraphJin discovers the relevant graph, checks policy, validates the request, runs it, and returns the evidence behind the answer.</p>
    <a href="/agentic/server-agent/">Use the server-side agent</a>
  </article>
  <article>
    <span>02 · Continue</span>
    <h2>Carry declared intent across runs.</h2>
    <p>Owner-scoped tasks warm-start later agent runs, journal their provenance, stay visible through notices, and can require a saved query to prove the declared outcome before close.</p>
    <a href="/agentic/tasks/">Use durable verified tasks</a>
  </article>
  <article>
    <span>03 · Watch</span>
    <h2>Keep a standing question running.</h2>
    <p>A cursor-backed watch records changes. An optional tool-free AI flow can summarize, score, or suppress noise before the right conversation wakes.</p>
    <a href="/agentic/watch-automation/">Choose a watch and flow</a>
  </article>
  <article>
    <span>04 · Act</span>
    <h2>Run only the action that was approved.</h2>
    <p>Workflows can use governed GraphJin tools, but autonomous watch delivery remains paused until the exact current action hash is confirmed.</p>
    <a href="/agentic/workflows/">Understand workflows</a>
  </article>
</div>

<aside class="agentic-watch-feature">
  <div>
    <span>Featured capability</span>
    <h2>From every change to only what matters.</h2>
    <p>Watch roast telemetry without flooding the agent. Turn raw readings into <code>discard</code>, <code>digest</code>, or <code>notify</code>. Wake only the conversation that owns the watch, and keep actions behind a separate approval gate.</p>
  </div>
  <a class="button-primary" href="/agentic/watch-automation/">Explore Watch Automation</a>
</aside>

The intended loop is catalog first, security second, validation or preview third, and then a governed answer or action. That sequence keeps agents useful without handing them raw database credentials or arbitrary shell access. The fastest way to see the full surface is the [coffee-roastery demo](/start/demos/#coffee-roastery).

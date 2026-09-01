---
title: "The GraphJin Agent Environment"
description: "Train and measure AI agents against a governed graph, with rewards that come from the database rather than from plausibility."
nav_group: "environment"
weight: 5
---

Most ways of scoring an agent ask a model whether an answer looks right. This
one asks the database. Every task in a GraphJin environment carries a hidden
oracle — a read-only query resolved against the same world the agent works in —
so a fluent, confident, wrong answer earns nothing.

The environment ships as a container that boots ready with no files mounted, and
as a CLI you can point at your own schema. Same engine, same reward contract,
two starting points.

<div class="agentic-path-grid">
  <article>
    <span>Train</span>
    <h2>You want an environment to train against.</h2>
    <p>Pull the image, read <code>/health</code>, and drive graded episodes — hosted, one completion at a time, or with your own agent over MCP. Held-out splits, trajectory export, and a reward that a policy cannot talk its way past.</p>
    <a href="/environment/quickstart/">Run the environment</a>
  </article>
  <article>
    <span>Measure</span>
    <h2>You want to know how an agent does on your data.</h2>
    <p>Clone the shape of a running GraphJin server into a local synthetic world — catalog structure and published value sets, never your rows — generate a verified suite from it, and grade against that.</p>
    <a href="/environment/your-own-graph/">Use your own graph</a>
  </article>
</div>

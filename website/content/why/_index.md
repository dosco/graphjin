---
title: "Why DeepORG"
headline: "Nine reasons, one page each."
description: "What DeepORG is for and why it is built the way it is: a complete agent, no hand-built tools, answers with evidence, your access rules, a public benchmark, a training environment, standing questions, every system you run, and an open engine."
lede: "DeepORG is one AI agent for your whole organization. It is graded on its own public benchmark, and the same machinery turns your organization into a training environment. Each line below is one reason to choose it, with a page that says how it works and where its limits are."
scene: "sources"
scene_label: "Animated diagram: databases, warehouses, API specs, file stores and source code flow into one map that one agent reads."
scene_caption: "many systems / one map / one agent"
actions:
  - label: "Try it in 2 minutes"
    href: "/#quickstart"
  - label: "See the public benchmark"
    href: "/benchmark/"
landing: true
toc: false
---

{{< band label="Or skip the reading" title="Run it and ask it something." >}}

Install DeepORG, add a model key and start the demo company. The CLI is still named `graphjin`.

```bash
curl -fsSL https://deeporg.io/install.sh | bash
echo 'OPENAI_API_KEY=your-key' >> .env
graphjin serve --demo
```

Then open `localhost:8083/agent` and ask which account is most at risk of churning. The [quick start](/start/quick-start/) goes on from there.

{{< endband >}}

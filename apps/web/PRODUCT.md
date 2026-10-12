# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

The primary user is the operator or admin who runs OpenNeko. This person installs
the product, connects data sources, configures packs, rules, and policies, and
keeps it healthy. The admin, settings, integrations, and onboarding surfaces are
theirs.

The first-class consumer is the CXO. A CEO, CFO, or COO reads the per-role
briefing and asks follow-up questions. Briefing and Ask are theirs. They rarely
touch admin.

The web app opens on chat at `/chat`. Every person talks to their own bots there.
A bot keeps one running chat, reports in that chat, and can use everything its
owner can access. Admins open the full console from the profile menu. People
who are not admins see only chat.

The durable center of gravity is the operator who keeps it running. The bot
chat is the first-class consumer surface.

Many customers run in regulated or industrial operations. Some operate under ITAR
and CMMC obligations.

## Product Purpose

OpenNeko reads the systems a business already runs and reports the operating risk
before it reaches the customer or the P&L. It turns scattered operational data
into a per-role briefing and answers follow-up questions in place.

Success means the operator sees the risk named, with dates and magnitudes, in
time to act.

## Positioning

Two claims form one position. Future work must preserve both.

The safety mechanism: OpenNeko reads the systems you already run with SELECT-only
reads. It never writes the customer database. It governs any write to fail
closed, and it routes governed writes only to OpenAPI sources under policy.

The operating stance: control before autonomy. OpenNeko acts only within policy.
The customer controls the deployment. The product exists for the gap between
knowing and acting.

## Operating Context

Chat (`/chat`) is the default surface: the bot list, each bot's chat, New bot,
and Connectors. The console is one product across many surfaces: Briefing, Ask, Workflows,
Actions, Runs, Library, Memory, Skills, Integrations, Admin, Settings, Apps,
Records, Sign-in, and Onboarding. `apps/web/AGENTS.md` is the authority on this
scope.

OpenNeko reads a customer database through GraphJin. It routes governed writes to
OpenAPI sources, never to the customer database.

A solution pack integrates one application. The Magento pack installs with one
command, ships a daily commerce briefing, runs low-stock and MSI reservation
checks, and diagnoses cron, indexer, and data-freshness faults.

Deployment is customer-controlled. OpenNeko is in production with defense
contractors under ITAR and CMMC obligations. The demo host (neko-vm) is
production; validate changes locally first.

## Capabilities and Constraints

Capabilities:

- Each person creates bots from the New bot screen. A bot has a neko avatar,
  instructions it keeps current, and routines that post to its chat.
- A per-role briefing states what matters now for each CXO role.
- Ask answers follow-up questions through an agent that renders A2UI cards.
- The operator approves actions through commits, not free-form model writes.
- Packs, workflows, rules and policies, runs and replay, library, memory,
  skills, and integrations complete the operator loop.
- Admin covers users, plugins, rules, and settings for agent, data, GraphJin,
  packs, research, rules, security, SSO, and sign-in.

Constraints:

- This is a fork of Next.js with breaking changes. Read the guide in
  `node_modules/next/dist/docs/` before writing Next.js code.
- The theme is light only. Do not add dark mode.
- CI enforces the design system through `scripts/check-web-design-system.mjs`.
  New and changed TSX must use the shared primitives in `src/components/ui`, the
  semantic tokens in `src/app/styles/_tokens.css`, and no page-local control
  styling or literal colors. A native control needs a `data-ui-bespoke-reason`.
- Archivo carries display type and Manrope carries body type. Both ship as
  self-hosted variable fonts, so production never depends on Google Fonts.
- Governed writes fail closed and stay gated. Do not present them as available.

Terminology:

- Write "OpenNeko" as one word, with a capital O and N.
- Call the per-role view a "briefing", never a "dashboard" or a "report".
- Call a per-application integration a "solution pack" or "pack".
- Write "Magento Open Source & Adobe Commerce", never "Magento Commerce".
- Avoid "autonomous", "insights" as an end state, "copilot", and "chat with
  your data".

## Brand Commitments

- Name: OpenNeko, one word. Domain: openneko.app. The old getneko.app domain is
  deprecated.
- Voice: an operator reporting what the data shows. Evidence-first, concrete with
  dates and figures and table names, plain, and restrained. No exclamation marks,
  no superlatives, no emoji in body copy, and no em-dashes.
- Case: sentence case for headlines and controls.
- The OpenNeko brand voice guidelines and the `stop-slop` skill govern all copy.

## Evidence on Hand

- Production use by defense contractors under ITAR and CMMC. This is an unnamed
  category claim. Do not name the customers.
- Magento pack capabilities, verified from code: one-command install, daily
  commerce briefing, low-stock and MSI checks, cron and indexer and
  data-freshness diagnosis, and SELECT-only reads.
- No named logos, testimonials, or metrics are on hand. Do not fabricate them.

## Product Principles

1. Trust through restraint. Read what the systems hold. Never write the customer
   database. Govern every write to fail closed.
2. Control before autonomy. Name the risk and act only within policy. The
   customer controls the deployment.
3. Evidence over atmosphere. Every claim rides a date, a figure, a table name, or
   a mechanism.
4. One product language across every surface. Operator and CXO surfaces share
   tokens, primitives, states, and copy. Admin, Apps, Records, and Settings stay
   precise and quiet, and no surface gets a distinctive restyle.
5. Chat as the default interface. Prefer the agent and its cards. Reserve forms
   for credential entry.

## Accessibility & Inclusion

- WCAG AA is the working bar for text contrast. The token file targets AA, and
  status colors read on both the light surfaces and the dark hero surfaces.
- Touch targets meet 44px on phone widths.
- The console honors `prefers-reduced-motion`, ships a skip-to-main link, uses
  semantic landmarks, and keeps keyboard focus visible.

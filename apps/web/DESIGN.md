---
name: OpenNeko Console
description: A proactive operator console. An agent works your systems in the background and brings you the decisions only you can make.
colors:
  paper: "#FAFAF7"
  surface: "#FFFFFF"
  ink: "#2D2A24"
  ink-muted: "#6F6A60"
  ink-soft: "#756F65"
  border: "#EEEBE4"
  neutral: "#F2EFE8"
  neutral-soft: "#F7F4ED"
  iris: "#6B5CE7"
  iris-soft: "#EDE9FE"
  iris-deep: "#5A4CD1"
  mint: "#6CFF7F"
  mint-ink: "#113719"
  mint-soft: "#E2FBE6"
  pine: "#357A57"
  amber: "#E9A23B"
  amber-soft: "#FFF1D8"
  amber-ink: "#7E6200"
  gold: "#F0D97A"
  gold-soft: "#FFF4CC"
  brick: "#A53535"
  brick-soft: "#FEECEC"
  brick-deep: "#852626"
  coral: "#F58A8A"
  on-accent: "#FFFFFF"
typography:
  display:
    fontFamily: "Archivo Variable, Archivo, sans-serif"
    fontSize: "clamp(32px, 4vw, 48px)"
    fontWeight: 800
    lineHeight: 1.02
    letterSpacing: "-0.025em"
  headline:
    fontFamily: "Archivo Variable, Archivo, sans-serif"
    fontSize: "clamp(22px, 1.8vw, 24px)"
    fontWeight: 800
    lineHeight: 1.15
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Archivo Variable, Archivo, sans-serif"
    fontSize: "18px"
    fontWeight: 800
    lineHeight: 1.25
    letterSpacing: "-0.02em"
  body:
    fontFamily: "Manrope Variable, Manrope, sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "normal"
  subtitle:
    fontFamily: "Archivo Variable, Archivo, sans-serif"
    fontSize: "16px"
    fontWeight: 700
    lineHeight: 1.25
    letterSpacing: "-0.01em"
  body-large:
    fontFamily: "Manrope Variable, Manrope, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.55
  body-small:
    fontFamily: "Manrope Variable, Manrope, sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  caption:
    fontFamily: "Manrope Variable, Manrope, sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.4
  label:
    fontFamily: "Manrope Variable, Manrope, sans-serif"
    fontSize: "11px"
    fontWeight: 800
    lineHeight: 1
    letterSpacing: "0.08em"
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace"
    fontSize: "13px"
    fontWeight: 500
    lineHeight: 1.4
rounded:
  card: "20px"
  inner: "12px"
  control: "10px"
  pill: "999px"
components:
  button-primary:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.control}"
    padding: "8px 16px"
  button-primary-hover:
    backgroundColor: "{colors.iris}"
    textColor: "{colors.on-accent}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink-muted}"
    rounded: "{rounded.control}"
    padding: "8px 16px"
  button-danger:
    backgroundColor: "{colors.brick-soft}"
    textColor: "{colors.brick}"
    rounded: "{rounded.control}"
    padding: "8px 16px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.card}"
    padding: "14px 16px"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: "10px 14px"
  pill-watch:
    backgroundColor: "{colors.amber-soft}"
    textColor: "{colors.amber-ink}"
    rounded: "{rounded.pill}"
    padding: "4px 10px"
---

# Design System: OpenNeko Console

## Overview

**Creative North Star: "The Command Deck"**

OpenNeko works your systems in the background like a tireless deputy. It watches
the data, finds the operating risk, and drafts the next move. The console is the
deck where you stand above that work. It surfaces what the agent found and what
needs a ruling, and you decide. The agent proposes; you dispose. You are the
operator, and nothing acts outside the policy you set.

The interface is built to feel proactive. Motion shows the agent at work: a soft
breathing pulse on a running task, a live signal on a briefing that just landed.
The screen opens on what changed while you were away, not an empty prompt waiting
for input. Every finding carries its evidence and the one action that resolves
it, so a decision is a single confident move, not a research project.

It stays calm while it does this. Warm paper and near-black ink, one iris accent
for the action that matters, and a fixed status vocabulary of mint, amber, and
brick. Dark hero heads frame what is live; bright accents mark what needs you
now. Depth is soft and ambient. Admin, Settings, Apps, and Records inherit the
same tokens and stay quiet. Control before autonomy: the agent is always working,
and you are always in command.

**Key Characteristics:**
- Proactive by default: the agent works in the background and surfaces decisions.
- The operator commands. Nothing acts outside the policy you set.
- Motion signals live agent work; it is never decoration.
- The screen opens on what changed, with the resolving action one move away.
- Calm surface: warm paper, near-black ink, one iris accent, fixed status colors.
- One product language across every surface, operator to CXO.

## Colors

The palette is warm and restrained: aged paper, near-black ink, one saturated
iris accent, and a narrow status set.

### Primary
- **Iris** (`#6B5CE7`): the single accent. It marks the one primary action in a
  region, the active nav item, focus rings, and interactive hover. Used on a
  small share of any screen so it stays meaningful.
- **Iris Deep** (`#5A4CD1`): the pressed and hover-deepened accent.
- **Iris Soft** (`#EDE9FE`): the accent wash behind a hovered secondary control.

### Neutral
- **Paper** (`#FAFAF7`): the page ground. Warm, not white.
- **Surface** (`#FFFFFF`): cards and controls sit one step brighter than paper.
- **Ink** (`#2D2A24`): body text, headings, and the primary button fill.
- **Ink Muted** (`#6F6A60`): secondary text and control labels. Meets AA on paper.
- **Ink Soft** (`#756F65`): tertiary text, placeholders, and captions. Meets AA.
- **Border** (`#EEEBE4`): hairline separators and control strokes.
- **Neutral / Neutral Soft** (`#F2EFE8` / `#F7F4ED`): quiet fills for chips,
  segmented controls, and disabled surfaces.

### Status
- **Mint** (`#6CFF7F`) and **Amber** (`#E9A23B`): bright status accents. They are
  legible on the dark hero heads and on dark chips. Do not set them as small text
  on a light surface.
- **Pine** (`#357A57`): the success text color for light surfaces (the readable
  weight of mint).
- **Amber Ink** (`#7E6200`): the amber text color for light surfaces and for
  text on amber-soft. Meets AA on paper, surface, amber-soft, and gold-soft.
- **Brick** (`#A53535`): danger text and destructive controls on light surfaces.
- **Coral** (`#F58A8A`): the danger text color for the dark hero heads, where
  brick red would drop below AA.
- **Soft tints** (`mint-soft` `#E2FBE6`, `amber-soft` `#FFF1D8`, `gold-soft`
  `#FFF4CC`, `brick-soft` `#FEECEC`): pill and callout backgrounds, always paired
  with the matching ink-weight text.

### Named Rules
**The Bright-on-Dark Rule.** Mint and amber at full brightness belong on the dark
hero heads and dark chips. On any light surface the same status reads through
Pine (success) and Amber Ink (watch). Brick red is the danger text on light;
Coral is the danger text on dark.

**The Mood Rule.** A card's severity drives one variable, `--mood-color`. That
one value paints the card's 4px left stripe, its faint corner tint, and its hover
border together, so the whole card agrees on one state.

**The One Accent Rule.** Iris marks the single primary action and the active
state. If two things on a screen are iris, one of them is wrong.

## Typography

**Display Font:** Archivo Variable (with Archivo, sans-serif)
**Body Font:** Manrope Variable (with Manrope, sans-serif)
**Numerals:** tabular figures for any number that compares across rows.

**Character:** Archivo is a tight, confident grotesque that carries every heading
and large number. Manrope is an even, humanist sans that carries everything a
person reads or operates. Both ship as self-hosted variable fonts, so the console
never waits on a web-font network call.

### Hierarchy
- **Display** (Archivo 800, `clamp(32px, 4vw, 48px)`, line-height 1.02): entry
  statements and empty-state headlines only.
- **Headline** (Archivo 800, `clamp(22px, 1.8vw, 24px)`, line-height 1.15): the
  page title.
- **Title** (Archivo 800, `18px`, line-height 1.25): a section heading.
- **Subtitle** (Archivo 700, `16px`, line-height 1.25): a subsection heading.
- **Body** (Manrope 400, `14px`, line-height 1.55): default reading and control
  text. Body copy holds a comfortable measure, not full-bleed lines.
- **Caption** (Manrope, `12px`): field labels, hints, and metadata.
- **Label** (Manrope 800, `11px`, letter-spacing 0.08–0.13em, uppercase): eyebrow
  and status labels. Used sparingly.

### Named Rules
**The Two-Voice Rule.** Archivo carries hierarchy. Manrope carries copy and
controls. There is no third face. Monospace appears only for real data, indices,
and measurements, never as a costume for "technical".

## Layout

The console is a single centered column, capped by a per-surface `--page-width`
(740px for reading surfaces, 1000–1200px for work and triage surfaces) with
`margin: 0 auto` and side padding. Width is always a maximum, never a fixed size.

A density layer on `<html data-density>` switches between Compact (the default,
denser grids, two-up briefing cards) and Comfortable (a single narrow column,
larger rhythm). Compact adds side rails on wide viewports (a context rail on Ask,
a reading pane on Actions) that collapse below 768px.

Chat (`/chat`) uses one column of up to 760px with a 16px side gutter on phone.
Above 1024px a 280px bot sidebar sits beside the open chat. The sidebar stays in
place and only the chat scrolls, so `body` clips horizontal overflow with `clip`,
which keeps sticky headers and the composer working. Wide tables, charts and code
scroll inside their own card. On phone the composer sits at the bottom edge.

Responsive behavior adapts rather than shrinks. The page clips horizontal
overflow, forces `min-width: 0` on flex and grid children so long identifiers
wrap instead of pushing the layout, caps inputs at full width, and honors
safe-area insets. Section navigation becomes a horizontal tab strip on phone.
Rhythm is quiet: 12–16px gaps inside groups, larger gaps between sections, and
more space above a heading than below it.

## Elevation & Depth

The system is soft-lift, calm at rest. Surfaces carry a faint ambient shadow that
combines a small offset with a soft blur. There is no hard offset block shadow
and no zero-blur halo. Depth reads as light, not as a border.

### Shadow Vocabulary
- **Rest** (`box-shadow: 0 1px 3px rgba(20,18,12,0.04), 0 4px 16px rgba(20,18,12,0.03)`):
  the default card and control shadow.
- **Hover** (`box-shadow: 0 2px 8px rgba(20,18,12,0.06), 0 12px 36px -8px rgba(20,18,12,0.07)`):
  the raised state when a card lifts.
- **Lift** (`box-shadow: 0 1px 2px rgba(20,18,12,0.04), 0 18px 48px -16px rgba(20,18,12,0.16)`):
  menus, popovers, and floating panels.

### Named Rules
**The Soft-Lift Rule.** A card rests on the Rest shadow and, on hover, moves up
one pixel to the Hover shadow. Elevation is a response to state, never a
decoration applied at rest.

## Shapes

Corners are generously rounded and consistent: 20px on cards and hero panels,
12px on nested elements inside a card, 10px on controls, and full-round (999px)
on pills and segmented controls. Strokes are 1px hairlines in Border for
separators, and 1.5px on interactive controls (buttons, inputs) so the control
edge reads before its fill. The one intentional colored edge is the 4px severity
stripe on a briefing or finding card, driven by `--mood-color`; no other card
carries a colored side border above 1px.

## Components

### Buttons
- **Shape:** 10px radius (`--radius-control`), 1.5px border, Manrope semibold,
  compact on desktop and 44px minimum on phone and coarse pointers.
- **Primary:** near-black fill (Ink) with paper text and a soft drop shadow. One
  per region. On hover it flips to the Iris accent with white text and lifts 1px.
- **Secondary:** surface fill at 85%, Ink Muted text, hairline border. On hover
  the border and text turn iris over an Iris Soft wash.
- **Ghost:** transparent, Ink Muted text; hover fills Neutral Soft.
- **Danger:** Brick text on Brick Soft with a tinted border; hover fills Brick
  with white text. Never disguise a destructive action as secondary.
- **Focus:** a 2px Iris ring with a 2px paper offset. Disabled drops to 50%
  opacity and removes the lift.

### Cards / Containers
- **Corner Style:** 20px (`--radius-card`).
- **Background:** Surface on Paper, hairline Border.
- **Shadow Strategy:** Rest shadow at rest, Hover shadow on interactive lift.
- **Internal Padding:** 14–16px, denser in Compact, looser in Comfortable.

### Inputs / Fields
- **Style:** Surface fill, 1.5px Border, 10px radius, Ink text, Ink Soft
  placeholder.
- **Focus:** border turns Iris with a 3px `--focus-ring` glow (no `outline:
  none` without this replacement).
- **Hover:** border deepens to Ink Soft.
- **Disabled:** Neutral Soft fill at 60% opacity.
- **Label:** 12px Manrope bold in Ink Muted, above the control. **Error:** 12px
  Brick text below it.

### Pills
- **Style:** full-round, 11px uppercase Manrope extrabold, soft tint background
  with matching ink-weight text and a tinted border.
- **Variants:** live (mint), watch (amber-soft + Amber Ink), success (mint-soft +
  Mint Ink), danger (brick-soft + Brick), muted (neutral + Ink Muted). Use a pill
  for compact row status; use plain sentence-case text for quiet card metadata.

### Insight / Briefing card (signature)
The defining component. It is what the agent found for you, waiting for your
decision. A card on Surface with a 4px left stripe and a faint corner tint, both
driven by `--mood-color` from the finding's severity (good / watch / act). The
whole card, including the hover border, agrees on that one color. It leads with
the finding in plain language, keeps tabular numbers and the evidence one
disclosure away, and puts the resolving action in reach. It is a decision to
make, not a metric to admire.

### Dark hero head (signature)
The top band of Library, Skills, and Workflows surfaces, and the workflow
inspector. Near-black Ink ground with Paper text, where bright Mint and Amber
accents and Coral danger carry status against the dark.

## Do's and Don'ts

### Do:
- **Do** make the console feel proactive. Show when the agent is working (a
  breathing pulse, a live signal), and open a surface on what changed, not on an
  empty prompt. Put the resolving action within reach of the finding.
- **Do** build every control from `src/components/ui` (`Button`, `Field` and its
  inputs, `Checkbox`, `Tabs`, `Pill`, `Disclosure`, `Card`, `OverflowMenu`).
- **Do** use semantic tokens from `_tokens.css`. Never write a literal color in
  TSX.
- **Do** set display type in Archivo (`font-display`) and copy and controls in
  Manrope (`font-body`).
- **Do** code status through the mood and status tokens, and keep bright Mint and
  Amber for dark surfaces while using Pine and Amber Ink for the same status on
  light.
- **Do** keep 44px tap targets on phone, honor `prefers-reduced-motion`, and
  keep keyboard focus visible.
- **Do** write sentence case, and make an error name the next step.

### Don't:
- **Don't** add dark mode. The theme is light only.
- **Don't** create page-local button, field, or status-pill styling, and don't
  add a raw native control without a `data-ui-bespoke-reason`.
- **Don't** restyle Admin, Apps, Records, or Settings to look "designed". They
  stay precise and quiet.
- **Don't** put a bright accent (Mint, Amber) on small text over a light surface.
  It fails contrast; use Pine or Amber Ink.
- **Don't** animate width, height, padding, or margin for hot-path motion. Use
  transform and opacity on controls, 100–200ms.
- **Don't** use em-dashes, Title Case, hype words, or internal implementation
  terms in default copy.

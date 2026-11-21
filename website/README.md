# DeepORG Website

This directory contains the Hugo-based DeepORG website (DeepORG was formerly
GraphJin). Content is written as Markdown under `content/`, layouts live under
`layouts/`, and static source assets live under `static/`.

## Commands

```bash
npm run dev
npm run build
npm run check
```

`npm run build` syncs the root `install.sh` into `static/install.sh`, renders
the ASCII still frames, builds the Hugo site into `public/`, and creates the
Pagefind search index. `npm run check` asserts routes, anchors, pinned copy and
links against the built site, so run it after every build.

## The homepage and the /why/ pages

The homepage is written for someone who has never heard of the product. It
gives a full section to the three reasons that define DeepORG (the agent, the
benchmark, the training environment), to standing questions and to how it
works; four more reasons share one row of cards. Each links to a deeper page.
The list lives in `data/values.yaml`; the `/why/` index and the pager on every
`/why/` page are generated from it, and `npm run check` fails if a value has no
homepage link.

The homepage uses one product name. "GraphJin" appears in the hero's "formerly"
line and in the footer, and nowhere else (`npm run check` enforces it); the
engine's own name is explained on `/why/open-engine/`.

The top nav has four destinations (Why DeepORG, Benchmark, Environment, Docs);
the docs sections are in the sidebar of every docs page.

The provider marks beside the benchmark board are listed in
`data/providers.yaml`, with where each came from. The page inlines them, so each
file in `static/logos/provider-*.svg` is a single-colour `<svg>` of `<path>`
elements and nothing else; `npm run check` refuses anything more.

The systems DeepORG connects to are listed in `data/databases.yaml` and drawn by
`{{</* database-logos */>}}`. The homepage passes `compact="true"`, which draws
a short strip of small marks with the name beside each; the deeper pages keep
the full grid. A logo drawn in a dark colour sets `on_dark: lighten` so it stays
visible on the dark theme.

The link preview card is `static/og/deeporg-site-og.png`, rendered from
`scripts/og-card.html` (the command is in that file). The tab icons
`static/favicon.ico` and `static/apple-touch-icon.png` are rendered from
`static/favicon.svg`.

A `/why/` page is Markdown between `{{</* band label="…" title="…" */>}}` and
`{{</* endband */>}}`. Inside a band, `{{</* grid */>}}…{{</* endgrid */>}}`
turns a list into an open grid of facts and `{{</* note */>}}…{{</* endnote */>}}`
marks a limit worth stating.

## ASCII figures

The animated figures are drawn by `static/js/ascii.js`: each scene is a pure
function of time, in 7-bit ASCII. Place one with
`{{</* ascii scene="map" label="…" caption="…" */>}}`.

A figure too wide to shrink onto a phone names a second scene with `narrow=`,
which replaces it at 900px and below. The homepage hero does this: `orgwide`
reads left to right across the full width, and `org` tells the same story
stacked.

```bash
npm run ascii              # re-render the still frames after editing a scene
node scripts/render-ascii-stills.mjs org 4.2   # print one frame at t=4.2s
```

The still frames in `data/ascii.json` are what a page shows before JavaScript
runs. A reader who prefers reduced motion gets the same frame, drawn once.

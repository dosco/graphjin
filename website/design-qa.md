# DeepORG benchmark design QA

- Source visual truth: `/Users/vr/.codex/generated_images/019ff70c-5f30-7de2-8e74-1bda446487da/exec-c71ea9f3-3722-4a83-88f9-a472ab2e8eee.png`
- Implementation route: `http://127.0.0.1:1313/benchmark/`
- Intended viewport: 864 CSS px wide, light theme, desktop state
- Source pixels: 864 × 1821
- Implementation screenshot: unavailable
- CSS size and density normalization: unavailable because the implementation could not be opened in a browser
- State: desktop, light theme, benchmark overview

## Full-view comparison evidence

The source visual was opened and inspected before implementation. The production Hugo render, deterministic DeepORG social-card render, Pagefind index, and generated-site assertions all complete. The managed environment still blocks both a local preview server and browser navigation to a local-file preview. Without a browser-rendered implementation screenshot, the required same-viewport comparison cannot be performed.

## Focused region comparison evidence

Blocked for the same reason. The intended focused checks are the hero score split, model comparison matrix, four-part pass contract, six task-family groups and icons, illustrative verified-answer flow, and final technical CTAs.

## Findings

- [P1] Browser-rendered visual evidence is unavailable.
  - Location: `/benchmark/` full page and responsive states.
  - Evidence: the Hugo server cannot bind to `127.0.0.1:1313`, elevated Chrome execution is denied, and the in-app browser rejects local-file navigation.
  - Impact: typography, spacing, color, icon rendering, horizontal overflow, and responsive behavior cannot be honestly accepted from source and static HTML alone.
  - Fix: explicitly allow a local Hugo preview server, capture the route at desktop and mobile widths in the in-app browser, then compare it with the selected source visual.

## Required fidelity surfaces

- Fonts and typography: blocked pending browser capture.
- Spacing and layout rhythm: blocked pending browser capture.
- Colors and visual tokens: blocked pending browser capture.
- Image and asset fidelity: the local Tabler webfont asset is present; rendered icon fidelity is blocked pending browser capture.
- Copy and content: statically verified against the selected design direction and benchmark YAML.
- Responsiveness and interactions: blocked pending browser capture.

## Comparison history

1. The selected 864 × 1821 source was inspected before implementation and remains the visual source of truth.
2. The original headless-Chrome social-card step aborted under the sandbox, and elevated execution was rejected by the managed runtime.
3. The card generator was replaced with a deterministic in-process renderer sourced from the same Hugo-ranked data. A clean `npm ci`, `npm run build`, and `npm run check` now pass, and `public/og/deeporg-og.png` was visually inspected at 1200 × 630.
4. Local preview startup still fails with `listen tcp 127.0.0.1:1313: bind: operation not permitted`.
5. A relative-file preview was rendered as a safer fallback, but the in-app browser blocks `file://` navigation by policy.
6. The full-page comparison therefore remains blocked rather than being accepted from static HTML or the social-card render alone.
7. The first 1200 × 630 card pass was rejected for heavy typography, crowded copy, and excessive metric framing. The generator was simplified to a sparse two-metric composition, switched to the freely licensed Inter font loaded from a pinned local package, and reduced to the active model, score, safety result, scope, and generation.
8. The selected page source and revised 1200 × 630 card were opened together for visual comparison. The card now follows the source's white editorial hierarchy, thin rules, restrained lime accent, and open spacing; this asset-level comparison does not replace the blocked browser QA for the full page.
9. Restored the source's full framing question—“Can an AI agent handle the questions an organization actually asks?”—and compared the regenerated card with the source in the same visual input. The full line fits without wrapping, clipping, or crowding at 1200 × 630.

## Final result

final result: blocked

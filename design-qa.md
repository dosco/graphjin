# GraphJin Console redesign QA

## Visual sources and comparison method

- Product baseline: `/private/tmp/gj-webui-audit/02-agent-live.jpg` (1280x906) and `/private/tmp/gj-webui-audit/01-runtime-live.jpg` (1280x1520). These capture the reported issues: the agent constrained to a nested card/right pane, a persistent sidebar, and overly boxed admin content.
- Implementation evidence: `/private/tmp/gj-webui-redesign/09-agent-production.png` and `/private/tmp/gj-webui-redesign/03-runtime-revised.png` at 1440x900; `/private/tmp/gj-webui-redesign/12-agent-mobile-final.png` at a 390x844 CSS viewport (DPR 2).
- Combined comparison inputs: `/private/tmp/gj-webui-redesign/agent-comparison-final.jpg` and `/private/tmp/gj-webui-redesign/runtime-comparison.jpg`. Each desktop side was normalized to 720x450. The runtime baseline uses its top crop because the original page is taller.
- State note: the baseline Agent screen had a missing-key state while the implementation used a live ready development server. The comparison therefore evaluates structure, hierarchy, density, and containment rather than identical status copy.
- Focused crops were unnecessary: the full desktop comparisons keep the shell, primary content, and composer/runtime summary legible. The mobile capture is the focused responsive proof.

## Fidelity and product-system review

- Layout and spacing: removed the persistent left sidebar and nested agent container; the workspace ribbon, route navigation, main canvas, and agent composer now use the full viewport. Dense admin pages use open summaries, tables, and dividers instead of repeated cards.
- Typography: retained a system UI stack with a compact, consistent hierarchy for workspace, page, section, metadata, and body text.
- Color and theming: semantic light/dark tokens drive the shell, shadcn controls, agent surfaces, and GraphiQL theme.
- Components and assets: uses real Lucide icons and shadcn primitives, including chat/message-scroller, command, dropdown, button-group, resizable panels, and sheets. No raster artwork, CSS drawings, text-symbol icons, or placeholder imagery is required for this console UI.
- Content: User routes are task-oriented; Admin routes expose runtime, workbench, catalog, workflows, security, code, config, and API surfaces. Trainer remains registered as a future workspace but is not advertised without a real server capability.

## Interaction and accessibility verification

- Verified User/Admin switching, last-route restoration per workspace, Cmd/Ctrl+K navigation, light/dark/system themes, operator identity sheet, Catalog detail inspector, GraphQL Workbench execution, API docs, and legacy route/query redirects.
- Verified production direct-entry routing at `/user/agent` after changing Vite's asset base to `/`.
- Verified responsive Agent layout at 390x844: document width 390px; composer right edge 377px; send button right edge 367px; no horizontal overflow.
- Automated shell accessibility check reports no axe-core violations. The mobile identity control has an explicit accessible label.
- Production browser console was clean across Agent, Runtime, Catalog, API, and Workbench flows.

## Defect and correction history

1. P2: runtime hero and source rows remained too card-heavy. Replaced with an open border summary, metric matrix, and divider rows; verified in `03-runtime-revised.png` and `runtime-comparison.jpg`.
2. P2: the compact mobile identity icon was unnamed. Added an explicit accessible label and rechecked the shell.
3. P1: production deep links loaded blank because asset URLs were relative. Set the Vite base to `/` and verified direct `/user/agent` loading.
4. P2: Catalog fallback keys could collide. Added stable fallback-plus-index keys and confirmed clean production console output.
5. P2: the mobile composer grid exceeded the viewport. Changed the implicit grid track to `minmax(0, 1fr)` and added minimum-width constraints; verified by exact bounds and `12-agent-mobile-final.png`.

## Residual notes

- GraphiQL, Monaco, and Swagger remain intentionally lazy-loaded but produce large build-chunk warnings. This is a P3 performance follow-up, not a correctness or visual blocker.
- The existing GraphiQL schema introspection can report an unknown `JSONListExpression` type before a query is run; an actual Workbench query completes successfully. This behavior predates and is outside the shell redesign.

## Homepage Console showcase QA

### Visual sources and rendered evidence

- Existing homepage source: `/private/tmp/gj-homepage-captures/homepage-source-before.png` at 1440x900, captured from the live site at `#why-it-works` before the new band.
- Approved product sources: `/private/tmp/gj-homepage-captures/webui-runtime-overview.png` and `/private/tmp/gj-homepage-captures/webui-workbench-query.png`, both real 1440x900 light-theme Console captures.
- Rendered implementation: `/private/tmp/gj-homepage-captures/homepage-console-desktop-pass1.png` at 1440x900, `/private/tmp/gj-homepage-captures/homepage-console-mobile-pass1.png` and `/private/tmp/gj-homepage-captures/homepage-console-mobile-gallery-pass1.png` at 390x844, plus `/private/tmp/gj-homepage-captures/homepage-console-showcase-1440x1200.png` for the entire section.
- Combined comparisons: `/private/tmp/gj-homepage-captures/homepage-source-implementation-comparison.png` compares the existing homepage with the local implementation at the same desktop state; `/private/tmp/gj-homepage-captures/gallery-source-implementation-comparison.png` compares the source Console captures with their homepage rendering.

### Fidelity and responsive review

- Placement and hierarchy: the new `#console` section begins directly after “Why this still works at 5,000 tables,” preserving the existing hero and opening proof sequence.
- Product-system fit: the section uses the existing display and body type, semantic color tokens, 76rem content width, restrained borders, and open separators. The full-width muted band creates distinction without introducing a separate dashboard aesthetic.
- Copy and information architecture: the approved label, heading, description, three benefits, screenshot captions, and two CTA destinations are present exactly as specified.
- Screenshot treatment: the Runtime and Workbench sources retain their original crop and content, render sharply at desktop and mobile sizes, and use minimal framing. Both committed WebP files are 1440x900 and share their filenames with Quick Start.
- Desktop layout: at 1440x900 the benefit row and two-column gallery remain aligned inside the 76rem content width with no horizontal overflow.
- Mobile layout: at 390x844 the benefits, screenshots, captions, and actions stack to one column; body and document widths remain exactly 390 CSS pixels with no horizontal overflow.
- Density note: fine Console microcopy is necessarily small in the two-column desktop gallery. This is acceptable P3 detail because the captions establish each screenshot's purpose and the same assets appear larger in Quick Start.

### Content, privacy, and accessibility review

- Runtime state is neutral and light themed; the screenshot exposes source health, discovery coverage, catalog readiness, and security posture without API keys, user IDs, machine hostnames, or generated local artifact names.
- Workbench shows a real read-only `SourceHealth` GraphQL query and JSON result. It contains no credentials or environment-specific host values.
- Images have descriptive alt text, explicit intrinsic dimensions, native lazy loading, and asynchronous decoding. Figures use semantic captions and the section is labelled by its heading.
- Verified `Open it locally` resolves to `/start/quick-start/#open-the-web-ui`, `Run the 2-minute demo` resolves to `#quickstart`, and the stable `#console` deep link opens the section.
- Browser diagnostics were clean; no console errors appeared during desktop, mobile, deep-link, or CTA checks.

### Comparison and correction history

1. Pass 1: the live-source/implementation comparison showed the existing “Why this still works” section unchanged and the new band matching the site's typography, spacing, and restrained visual language. No P0, P1, or P2 mismatch was found.
2. Pass 1: the source/gallery comparison confirmed both screenshots remained sharp, uncropped, and faithful in the responsive gallery. No corrective visual loop was required.

### Focused double-check — 2026-08-01

- Fresh accepted evidence from this run is saved in `/private/tmp/gj-homepage-double-check-20260801/`: `01-console-desktop.png`, `02-console-actions-desktop.png`, `03-console-mobile.png`, `04-console-mobile-actions.png`, and `05-quick-start-mobile.png`.
- At 1440x900, the section, benefit row, screenshots, and actions remain aligned and the document width is exactly 1440 CSS pixels. Both lazy-loaded images completed at their native 1440x900 size.
- At 390x844, the section reflows to one column; the body and document widths remain exactly 390 CSS pixels, each benefit and screenshot is 361.2 CSS pixels wide, and no horizontal overflow is present.
- `Open it locally` was clicked and resolved to `/start/quick-start/#open-the-web-ui` with the “Open the Web UI” heading present. The section-scoped `Run the 2-minute demo` link was clicked and resolved to `#quickstart` with the “Run it in two minutes.” heading present.
- OCR and original-resolution inspection of both WebP assets found no API keys, user IDs, hostnames, UUIDs, or generated local artifact names. The Runtime capture intentionally shows that the console can surface security findings; source health remains ready.
- One P3 documentation mismatch was corrected: Quick Start described the current Workbench capture as a products query. Its alt text and caption now describe the read-only `SourceHealth` query, and both documentation images declare their intrinsic dimensions and asynchronous decoding.
- Fresh website build/check, Web UI unit/build checks, focused Console server tests, and whitespace validation pass. Browser diagnostics remain clean.

final result: passed

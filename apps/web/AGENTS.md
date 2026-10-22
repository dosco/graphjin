<!-- BEGIN:nextjs-agent-rules -->
# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->

# Mandatory UI design-system workflow

Every change under `apps/web` must preserve OpenNeko's existing product language. This is a delivery requirement, not an optional polish pass. Scope is every surface: Briefing, Ask, workflows, actions, runs, library, memory, skills, integrations, Admin, Settings, Sign-in, and Onboarding.

Load `.agents/skills/openneko-web-ui/SKILL.md` before building UI. Load `.agents/skills/openneko-web-review/SKILL.md` before calling a UI change done.

Before editing UI:

1. Read `src/components/ui/README.md`, `src/app/styles/_tokens.css`, and the adjacent rendered surface.
2. Record a reuse map in the PR: each interaction or visual role, the shared component/token used, and the verification evidence.
3. Prefer the shared primitives in `src/components/ui`: `Button`, `ActionGroup`, `Field` and its controls, `Checkbox`, `Tabs`, `Pill`, `Disclosure`, `Card`, `EmptyState`, and `OverflowMenu`.

While editing:

- Do not add raw `button`, `input`, `select`, `textarea`, `details`, or `summary` elements outside `src/components/ui`.
- Do not create page-local field, label, input, button, checkbox, status-pill, focus, or disabled-state styling.
- Do not add literal color values in TSX. Use semantic tokens from `_tokens.css`.
- Use Archivo through `font-display` for hierarchy and Manrope through `font-body` or inherited body typography for controls and copy.
- Keep internal implementation terms out of default user-facing copy. Technical evidence belongs behind `Disclosure`.
- A truly unique interaction may use a native control only with a non-empty `data-ui-bespoke-reason`, an explanation in the PR reuse map, and interaction coverage.

Before handoff:

1. Run `pnpm ui:check`, web lint, typecheck, unit tests, and the relevant visual contract.
2. Inspect the live rendered UI at desktop and phone widths, including loading, empty, success, error, focus, and disabled states that the change touches.
3. Include screenshots or exact computed-style/geometry evidence in the PR. Do not call a UI change complete from source inspection alone.

CI enforces new and modified TSX through `scripts/check-web-design-system.mjs`.

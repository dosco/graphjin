# OpenNeko UI system

OpenNeko uses one calm interaction language across product surfaces. Page CSS may compose layouts, but common controls belong in this directory.

These are custom shadcn components, not untouched registry copies. `components.json` uses the Radix Nova registry; registry components are adapted to OpenNeko tokens, typography, motion, and stable `data-ui-*` contracts. Add new primitives with the shadcn CLI, then review and customize the generated source. Do not overwrite an existing component from the registry.

## Action hierarchy

- `primary`: the single main action in a working region.
- `secondary`: ordinary visible actions.
- `ghost`: low-emphasis or icon-only actions.
- `danger`: destructive actions; never disguise these as secondary.
- Put related controls in `ActionGroup`. Move destructive or infrequent row actions into `OverflowMenu` when space is tight.

Use `Button`, `IconButton`, `ButtonLink`, or `buttonClassName` instead of creating page-local button chrome. Desktop sizes are intentionally compact; all shared controls expand to a 44px minimum target on phone widths and coarse pointers.

Use the default 40px button size beside inputs and selects. Reserve 32px actions for compact rows, not form toolbars. Put hints below the whole form row when they would otherwise misalign adjacent controls.

## Surface and spacing

- Use `PageHeading` without a tinted background, shadow, or sticky panel. Titles and content share `--page-gutter` (32px desktop, 16px phone).
- Use `Card` or the existing family panel with the shared radius, white surface, and `--panel-padding` (24px desktop, 18px phone). Do not add a second decorated background inside a panel without a semantic reason.
- Search groups and composers own their outer focus treatment; do not draw a second outline on their inner field. Standalone fields and buttons retain visible keyboard focus.
- Keep metadata in sentence case. Counts must remain fully readable rather than shrink or truncate.

## Selection and input

Portalled sheets and confirmations own viewport positioning; shared overlay/menu stacking lives in `_ui.css`, above the rail, header, and workflow inspector. Consumer classes must not override their `position` or `z-index`. Never position or stack every `body > *`: Radix appends portals there. Scroll locking must preserve the responsive rail offset. The design-system browser gate checks these contracts at desktop, tablet, and phone widths, including nested confirmation and navigation sheets.

- Use `Tabs` for switching content views.
- Use `SegmentedControl` for filters or a choice within one view.
- Use `Checkbox` for boolean settings so control size, accent, focus, label typography, and disabled states stay aligned.
- Use `Field` with `Input`, `Textarea`, or `NativeSelect` for labelled form controls.
- Use `Disclosure` for secondary detail that would otherwise make a page difficult to scan.
- Use `Popover` for a small form that floats over the page, such as a list filter. It portals like menus and keeps the page layout still.
- Use `LocalDateTime` for browser-local timestamps so server rendering and hydration stay consistent.
- Use `SearchInput` for instant filtering on row-heavy indexes so search geometry and labelling stay consistent.
- Use `SkeletonList` while a list loads. Match the variant to the final layout so content does not shift when it arrives.

## Status and density

Use `Badge` for compact row status. Use plain sentence-case state text when a larger card needs quieter metadata. Do not pair a decorative dot with narrow, letter-spaced text or force status into a fixed-width gutter.

Preserve this hierarchy at every breakpoint:

1. title and orientation;
2. working content;
3. one clear primary action;
4. secondary detail on demand.

Bespoke controls are reserved for interactions whose behavior is genuinely unique, such as the Work composer, chat composer, navigation rail, and timeline playback—not for ordinary buttons, fields, tabs, or menus.

## Change contract

For every UI PR, add a reuse map before implementation:

| Surface need             | Canonical component or token | Verification                                                            |
| ------------------------ | ---------------------------- | ----------------------------------------------------------------------- |
| Example: boolean setting | `Checkbox`                   | 16px control, accent, focus, disabled and phone tap target checked live |

Run `pnpm ui:check` from the repository root. Modified page components may not introduce raw native controls, page-local control styles, literal colors, or handcrafted status pills. If an interaction is genuinely unique, add a specific `data-ui-bespoke-reason` and document why none of the shared primitives can represent it.

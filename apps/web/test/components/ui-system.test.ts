import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { Button, IconButton } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Disclosure } from "@/components/ui/disclosure";
import { EmptyState } from "@/components/ui/empty";
import { FieldInput } from "@/components/ui/field";
import { LocalDateTime } from "@/components/ui/local-date-time";
import { Segment, SegmentedControl, Tab, Tabs } from "@/components/ui/tabs";

describe("shared UI contracts", () => {
  it("keeps button hierarchy and icon actions machine-readable", () => {
    const primary = renderToStaticMarkup(
      createElement(Button, { variant: "primary", size: "lg" }, "Continue"),
    );
    expect(primary).toContain('data-ui-button=""');
    expect(primary).toContain('data-variant="primary"');
    expect(primary).toContain('data-size="lg"');
    expect(primary).toContain('type="button"');

    const icon = renderToStaticMarkup(
      createElement(
        IconButton,
        { label: "More actions" } as Parameters<typeof IconButton>[0],
        "…",
      ),
    );
    expect(icon).toContain('aria-label="More actions"');
    expect(icon).toContain('title="More actions"');
  });

  it("exposes selection state consistently for tabs and segments", () => {
    const tabs = renderToStaticMarkup(
      createElement(
        Tabs,
        null,
        createElement(Tab, { selected: true }, "Overview"),
      ),
    );
    expect(tabs).toContain('role="radiogroup"');
    expect(tabs).toContain('aria-checked="true"');
    expect(tabs).toContain('data-state="on"');
    expect(tabs).toContain('data-ui-tab=""');

    const segments = renderToStaticMarkup(
      createElement(
        SegmentedControl,
        null,
        createElement(Segment, { selected: true }, "Active"),
      ),
    );
    expect(segments).toContain('aria-checked="true"');
    expect(segments).toContain('data-ui-segment=""');
  });

  it("keeps checkbox controls and labels on the shared visual contract", () => {
    const checkbox = renderToStaticMarkup(
      createElement(Checkbox, {
        checked: true,
        label: "Allow routine changes to run automatically",
      }),
    );

    expect(checkbox).toContain('data-ui-checkbox=""');
    expect(checkbox).toContain('data-ui-checkbox-control=""');
    expect(checkbox).toContain('type="checkbox"');
    expect(checkbox).toContain("font-body");
    expect(checkbox).toContain("data-[state=checked]:bg-accent");
  });

  it("preserves labelled fields and native disclosure semantics", () => {
    const field = renderToStaticMarkup(
      createElement(FieldInput, {
        id: "workspace-name",
        label: "Workspace name",
      }),
    );
    expect(field).toContain('for="workspace-name"');
    expect(field).toContain('id="workspace-name"');
    expect(field).toContain('data-ui-field-control=""');

    const disclosure = renderToStaticMarkup(
      createElement(Disclosure, { title: "Advanced" }, "Details"),
    );
    expect(disclosure).toContain("<details");
    expect(disclosure).toContain("<summary");
    expect(disclosure).toContain('data-ui-disclosure=""');
  });

  it("exposes stable empty-state slots for surface-specific layouts", () => {
    const empty = renderToStaticMarkup(
      createElement(EmptyState, {
        title: "No concepts",
        body: "Imported knowledge appears here.",
      }),
    );

    expect(empty).toContain('data-ui-empty-state=""');
    expect(empty).toContain('data-ui-empty-state-copy=""');
    expect(empty).toContain('data-ui-empty-state-title=""');
    expect(empty).toContain('data-ui-empty-state-body=""');
  });

  it("renders deterministic timestamp fallbacks before client localization", () => {
    const timestamp = renderToStaticMarkup(
      createElement(LocalDateTime, {
        value: "2026-08-29T08:00:00.000Z",
        fallback: "recently",
      }),
    );
    expect(timestamp).toContain('dateTime="2026-08-29T08:00:00.000Z"');
    expect(timestamp).toContain(">recently</time>");
  });
});

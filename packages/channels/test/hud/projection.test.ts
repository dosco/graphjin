import { describe, expect, it } from "vitest";
import type { InteractionEvent } from "@neko/interaction";
import { inferAnchor, projectEvent } from "../../src/hud/projection";

describe("HUD projection", () => {
  it("prefers explicit spatial enrichment", () => {
    const event = {
      kind: "inform",
      id: "f1",
      mood: "act",
      title: "Alert",
      body: "Inspect it",
      enrichment: {
        spatial: { kind: "zone", id: "z-4", longitude: 56.3, latitude: 25.2 },
      },
    } as unknown as InteractionEvent;
    expect(inferAnchor(event, { kind: "hud" })).toEqual({
      kind: "zone",
      id: "z-4",
      longitude: 56.3,
      latitude: 25.2,
    });
  });

  it("falls back to generic recipient workspace", () => {
    const event: InteractionEvent = {
      kind: "progress",
      id: "p1",
      label: "Working",
      phase: "start",
    };
    expect(projectEvent(event, { kind: "hud", workspace: "factory" })).toMatchObject({
      type: "progress",
      anchor: { kind: "workspace", id: "factory" },
    });
  });
});

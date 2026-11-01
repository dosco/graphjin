import { afterEach, describe, expect, it, vi } from "vitest";
import { mapStyleSetting } from "@/lib/map-style";

describe("mapStyleSetting", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("uses OpenFreeMap when unset, turns maps off, or points at a self-hosted style", () => {
    vi.stubEnv("OPENNEKO_MAP_STYLE", "");
    expect(mapStyleSetting()).toBeUndefined();
    vi.stubEnv("OPENNEKO_MAP_STYLE", "off");
    expect(mapStyleSetting()).toBe("off");
    vi.stubEnv("OPENNEKO_MAP_STYLE", "https://tiles.example.internal/styles/light.json");
    expect(mapStyleSetting()).toBe("https://tiles.example.internal/styles/light.json");
  });

  it("ignores a value that is not an http(s) URL", () => {
    vi.stubEnv("OPENNEKO_MAP_STYLE", "javascript:alert(1)");
    expect(mapStyleSetting()).toBeUndefined();
    vi.stubEnv("OPENNEKO_MAP_STYLE", "not a url");
    expect(mapStyleSetting()).toBeUndefined();
  });
});

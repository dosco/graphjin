import { expect, it } from "vitest";
import { parseManifest } from "../src/manifest";

const manifest = {
  apiVersion: "openneko.app/v1", kind: "SolutionPack",
  metadata: { id: "skills-only", name: "Skills only", version: "1.0.0", publisher: "fixture", category: "operations" },
  compatibility: { openneko: ">=2.40.0", applications: [], databases: [] },
  inputs: [], secrets: [], artifacts: { skills: ["skills/review"] },
  health: { requiredPreflight: [], readiness: {}, postInstall: [], postWriteCanary: [] },
};
it("accepts a skills-only pack but requires compatibility for GraphJin artifacts", () => {
  expect(parseManifest(manifest).artifacts.graphjin).toBeUndefined();
  expect(() => parseManifest({ ...manifest, artifacts: { ...manifest.artifacts, graphjin: {
    sources: "sources.yaml", relationships: "relationships.yaml", specs: [], savedQueries: "queries",
  } } })).toThrow("GraphJin artifacts require GraphJin compatibility");
});

it("turns on built-in channels and rejects unknown or duplicate ones", () => {
  expect(parseManifest(manifest).channels).toBeUndefined();
  expect(parseManifest({ ...manifest, channels: ["slack", "telegram"] }).channels).toEqual(["slack", "telegram"]);
  expect(() => parseManifest({ ...manifest, channels: ["discord"] })).toThrow();
  expect(() => parseManifest({ ...manifest, channels: ["slack", "slack"] })).toThrow("duplicate channel");
});

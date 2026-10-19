import { describe, expect, it } from "vitest";
import {
  extractFrontmatterBlock,
  parseSkillFrontmatter,
} from "../src/work/skill-frontmatter";

describe("extractFrontmatterBlock", () => {
  it("returns the block between --- markers", () => {
    const content = `---
name: x
description: y
---

body content
`;
    expect(extractFrontmatterBlock(content)).toBe(`name: x\ndescription: y`);
  });

  it("returns null when no opening --- is present", () => {
    expect(extractFrontmatterBlock("body without frontmatter")).toBeNull();
  });

  it("returns null when --- never closes", () => {
    expect(extractFrontmatterBlock("---\nname: x")).toBeNull();
  });

  it("tolerates trailing whitespace on the delimiters", () => {
    const content = `---  \nname: x\n---  \nbody`;
    expect(extractFrontmatterBlock(content)).toBe("name: x");
  });
});

describe("parseSkillFrontmatter — agentskills.io spec fields", () => {
  it("parses name + description", () => {
    const out = parseSkillFrontmatter(`---
name: pdf-processing
description: Handle PDFs.
---
body`);
    expect(out.name).toBe("pdf-processing");
    expect(out.description).toBe("Handle PDFs.");
  });

  it("strips quotes around string values", () => {
    const out = parseSkillFrontmatter(`---
name: "pdf-processing"
description: 'Quoted description.'
---
body`);
    expect(out.name).toBe("pdf-processing");
    expect(out.description).toBe("Quoted description.");
  });

  it("parses license + compatibility + allowed-tools", () => {
    const out = parseSkillFrontmatter(`---
name: x
description: y
license: Apache-2.0
compatibility: Requires Python 3.14+ and uv
allowed-tools: Bash(git:*) Read
---
body`);
    expect(out.license).toBe("Apache-2.0");
    expect(out.compatibility).toBe("Requires Python 3.14+ and uv");
    expect(out.allowedTools).toBe("Bash(git:*) Read");
  });

  it("records empty name + description when frontmatter is missing entirely", () => {
    const out = parseSkillFrontmatter("body only");
    expect(out.name).toBe("");
    expect(out.description).toBe("");
  });
});

describe("parseSkillFrontmatter — prerequisites (community extension)", () => {
  it("parses inline-list commands", () => {
    const out = parseSkillFrontmatter(`---
name: airtable
description: x
prerequisites:
  commands: [curl, jq]
  env_vars: [AIRTABLE_API_KEY]
---
body`);
    expect(out.prerequisites?.commands).toEqual(["curl", "jq"]);
    expect(out.prerequisites?.envVars).toEqual(["AIRTABLE_API_KEY"]);
  });

  it("parses block-style commands", () => {
    const out = parseSkillFrontmatter(`---
name: x
description: y
prerequisites:
  commands:
    - foo
    - bar
  env_vars:
    - KEY_ONE
    - KEY_TWO
---
body`);
    expect(out.prerequisites?.commands).toEqual(["foo", "bar"]);
    expect(out.prerequisites?.envVars).toEqual(["KEY_ONE", "KEY_TWO"]);
  });

  it("tolerates empty prerequisites block", () => {
    const out = parseSkillFrontmatter(`---
name: x
description: y
prerequisites:
---
body`);
    expect(out.prerequisites).toBeUndefined();
  });

  it("returns undefined when prerequisites is absent", () => {
    const out = parseSkillFrontmatter(`---
name: x
description: y
---
body`);
    expect(out.prerequisites).toBeUndefined();
  });
});

describe("parseSkillFrontmatter — agent skill compatibility", () => {
  it("tolerates community top-level fields (version, author, platforms)", () => {
    const out = parseSkillFrontmatter(`---
name: watchers
description: Poll RSS feeds
version: 1.0.0
author: Example Author
license: MIT
platforms: [linux, macos]
---
body`);
    expect(out.name).toBe("watchers");
    expect(out.description).toBe("Poll RSS feeds");
    expect(out.license).toBe("MIT");
    expect(out.unparsed).toContain("version");
    expect(out.unparsed).toContain("author");
    expect(out.unparsed).toContain("platforms");
  });

  it("captures nested metadata fields under metadata", () => {
    const out = parseSkillFrontmatter(`---
name: airtable
description: x
metadata:
  openneko:
    tags: [Airtable, API]
    homepage: https://airtable.com/developers
---
body`);
    expect(out.metadata).toBeTruthy();
    // Nested keys flatten with dot notation in this minimal parser.
    expect(Object.keys(out.metadata ?? {})).toEqual(
      expect.arrayContaining([
        expect.stringContaining("openneko"),
      ]),
    );
  });

  it("ignores unsupported disable-model-invocation metadata", () => {
    const out = parseSkillFrontmatter(`---
name: bundled
description: Anthropic-bundled skill
disable-model-invocation: true
---
body`);
    expect(out.name).toBe("bundled");
    expect(out.unparsed).toContain("disable-model-invocation");
  });
});

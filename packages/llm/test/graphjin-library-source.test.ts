import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import { patchGraphjinLibrary } from "../src/graphjin/sources-config";

describe("patchGraphjinLibrary", () => {
  it("adds the private per-user Library source and file content once", () => {
    const first = patchGraphjinLibrary("app_name: x\nsources: []\n");
    expect(first.changed).toBe(true);
    const config = parse(first.content);
    expect(config.file_content).toEqual({ enabled: true });
    expect(config.sources).toEqual([
      {
        name: "neko_library",
        kind: "file",
        backend: "local",
        root: "files/library",
        read_only: true,
        user_prefix: "users/",
        access: { read: "authenticated" },
      },
    ]);
    expect(patchGraphjinLibrary(first.content)).toEqual({ content: first.content, changed: false });
  });

  it("keeps other sources and repairs a weakened Library source", () => {
    const raw = [
      "sources:",
      "  - name: adventureworks",
      "    kind: database",
      "  - name: neko_library",
      "    kind: file",
      "    backend: local",
      "    root: files/library",
      "    read_only: true",
      "    access:",
      "      read: public",
      "",
    ].join("\n");
    const config = parse(patchGraphjinLibrary(raw).content);
    expect(config.sources.map((s: { name: string }) => s.name)).toEqual(["adventureworks", "neko_library"]);
    expect(config.sources[1].user_prefix).toBe("users/");
    expect(config.sources[1].access.read).toBe("authenticated");
  });
});

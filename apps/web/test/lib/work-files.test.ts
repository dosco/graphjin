import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import {
  ALLOWED_UPLOAD_EXTENSIONS,
  MAX_UPLOAD_SIZE,
  joinMessageWithAttachments,
  readRunArtifact,
  readWorkFile,
  safeFileName,
  saveLibraryUpload,
  saveWorkUpload,
  writeWorkSkillFile,
} from "@/lib/work-files";
import { ensureOrgWorkspace } from "@neko/llm/work";

const cleanupPaths: string[] = [];

afterEach(async () => {
  while (cleanupPaths.length > 0) {
    const path = cleanupPaths.pop();
    if (path) await rm(path, { recursive: true, force: true });
  }
  delete process.env.HOME;
});

describe("safeFileName", () => {
  it("replaces unsafe characters with underscores", () => {
    expect(safeFileName("hello world.csv")).toBe("hello_world.csv");
    // Directory components are stripped (no traversal), then the basename
    // has unsafe characters replaced.
    expect(safeFileName("rep@rt 2025.pdf")).toBe("rep_rt_2025.pdf");
  });

  it("collapses leading dots so dotfiles can't be smuggled in", () => {
    expect(safeFileName(".env")).toBe("_env");
    expect(safeFileName("..hidden.json")).toBe("_hidden.json");
  });

  it("strips directory traversal segments", () => {
    expect(safeFileName("../../etc/passwd")).not.toContain("/");
    expect(safeFileName("foo\\bar.txt")).toBe("bar.txt");
  });

  it("falls back when nothing survives sanitisation", () => {
    expect(safeFileName("")).toBe("upload.bin");
  });
});

describe("joinMessageWithAttachments", () => {
  it("returns the trimmed text unchanged when there are no attachments", () => {
    expect(joinMessageWithAttachments("  what's revenue?  ", [])).toBe("what's revenue?");
    expect(joinMessageWithAttachments("", [])).toBe("");
  });

  it("emits a clean one-line-per-file block with size in KB", () => {
    const message = joinMessageWithAttachments("Summarise this.", [
      { relativePath: "uploads/t1/q1.csv", name: "q1.csv", size: 4096 },
      { relativePath: "uploads/t1/notes.md", name: "notes.md", size: 1500 },
    ]);
    expect(message).toBe(
      [
        "Summarise this.",
        "",
        "I've attached files:",
        "- uploads/t1/q1.csv  (q1.csv, 4 KB)",
        "- uploads/t1/notes.md  (notes.md, 1 KB)",
      ].join("\n"),
    );
  });

  it("uses singular phrasing for a single attachment", () => {
    const message = joinMessageWithAttachments("", [
      { relativePath: "uploads/t1/q1.csv", name: "q1.csv", size: 2048 },
    ]);
    expect(message.startsWith("I've attached a file:")).toBe(true);
    // No leading blank line when the user typed no message.
    expect(message.startsWith("\n")).toBe(false);
  });

  it("omits the KB suffix when size is unknown", () => {
    const message = joinMessageWithAttachments("hi", [
      { relativePath: "uploads/t1/x.csv", name: "x.csv" },
    ]);
    expect(message).toContain("- uploads/t1/x.csv  (x.csv)");
  });

  it("never leaks absolute paths", () => {
    const message = joinMessageWithAttachments("hi", [
      { relativePath: "uploads/t1/q1.csv", name: "q1.csv", size: 1024 },
    ]);
    expect(message).not.toContain("/Users/");
    expect(message).not.toContain("/home/");
  });
});

describe("upload constants", () => {
  it("caps uploads at 10 MB", () => {
    expect(MAX_UPLOAD_SIZE).toBe(10 * 1024 * 1024);
  });

  it("allows the standard document extensions", () => {
    for (const ext of [
      ".csv",
      ".pdf",
      ".md",
      ".markdown",
      ".txt",
      ".docx",
      ".xlsx",
      ".pptx",
    ]) {
      expect(ALLOWED_UPLOAD_EXTENSIONS.has(ext)).toBe(true);
    }
  });

  it("rejects images and other unsupported types", () => {
    for (const ext of [
      ".png",
      ".jpg",
      ".gif",
      ".exe",
      ".zip",
      ".json",
      ".tsv",
      ".html",
    ]) {
      expect(ALLOWED_UPLOAD_EXTENSIONS.has(ext)).toBe(false);
    }
  });
});

describe("saveWorkUpload + readWorkFile round-trip", () => {
  it("writes the file under uploads/<threadId>/ and reads it back", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-files-"));
    cleanupPaths.push(home);
    process.env.HOME = home;

    const payload = Buffer.from("col1,col2\n1,2\n3,4\n");
    const file = new File([payload], "report.csv", { type: "text/csv" });

    const saved = await saveWorkUpload("org-test", "thread-7", file);
    expect(saved.name).toBe("report.csv");
    expect(saved.relativePath).toBe(join("uploads", "thread-7", "report.csv"));
    expect(saved.size).toBe(payload.byteLength);
    expect(saved.absolutePath).toContain("thread-7");

    const onDisk = await readFile(saved.absolutePath);
    expect(onDisk.toString()).toBe(payload.toString());

    const served = await readWorkFile("org-test", "uploads/thread-7/report.csv");
    expect(served.filename).toBe("report.csv");
    expect(served.mimeType.startsWith("text/csv")).toBe(true);
    expect(served.data.toString()).toBe(payload.toString());
  }, 30_000);

  it("sanitises filenames and refuses path escapes when reading", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-files-"));
    cleanupPaths.push(home);
    process.env.HOME = home;

    const file = new File(["x"], "../../etc/passwd", { type: "text/plain" });
    const saved = await saveWorkUpload("org-test", "thread-x", file);
    expect(saved.name).not.toContain("/");
    expect(saved.name).not.toContain("..");

    await expect(readWorkFile("org-test", "secrets/foo.txt")).rejects.toThrow();
  }, 30_000);
});

describe("saveLibraryUpload", () => {
  it("writes once and returns the hash of the persisted bytes", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-library-upload-"));
    cleanupPaths.push(home);
    process.env.HOME = home;

    const payload = Buffer.from("durable library knowledge");
    const saved = await saveLibraryUpload(
      "org-test",
      "user-1",
      new File([payload], "knowledge.pdf", { type: "application/pdf" }),
    );

    expect(saved.size).toBe(payload.byteLength);
    expect(saved.contentHash).toBe(
      "88b05ad9db9ac17bb7271644404856607de0883623c626fab14fcc61d59f5eb7",
    );
    expect(saved.relativePath).toBe(
      `library/uploads/user-1/${saved.contentHash}/knowledge.pdf`,
    );
    await expect(readFile(saved.absolutePath)).resolves.toEqual(payload);
  }, 30_000);

  it("does not overwrite an earlier source with the same filename", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-library-upload-"));
    cleanupPaths.push(home);
    process.env.HOME = home;

    const first = await saveLibraryUpload(
      "org-test",
      "user-1",
      new File(["first source"], "knowledge.pdf", {
        type: "application/pdf",
      }),
    );
    const second = await saveLibraryUpload(
      "org-test",
      "user-1",
      new File(["second source"], "knowledge.pdf", {
        type: "application/pdf",
      }),
    );

    expect(first.absolutePath).not.toBe(second.absolutePath);
    await expect(readFile(first.absolutePath, "utf8")).resolves.toBe(
      "first source",
    );
    await expect(readFile(second.absolutePath, "utf8")).resolves.toBe(
      "second source",
    );
  }, 30_000);
});

describe("readRunArtifact", () => {
  it("reads a contained artifact and rejects traversal or symlink escapes", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-work-artifact-"));
    cleanupPaths.push(home);
    process.env.HOME = home;
    const upload = await saveWorkUpload(
      "org-test",
      "thread-1",
      new File(["private"], "private.txt", { type: "text/plain" }),
    );
    const orgRoot = upload.absolutePath.split(`${join("uploads", "thread-1")}`)[0]!;
    const runId = "11111111-1111-4111-8111-111111111111";
    const artifactRoot = join(orgRoot, "runs", runId, "artifacts");
    await mkdir(artifactRoot, { recursive: true });
    await writeFile(join(artifactRoot, "report.csv"), "a,b\n1,2\n");
    await symlink(upload.absolutePath, join(artifactRoot, "private-link.txt"));

    const artifact = await readRunArtifact("org-test", runId, "report.csv");
    expect(artifact.data.toString()).toContain("a,b");
    await expect(
      readRunArtifact("org-test", runId, "../private.txt"),
    ).rejects.toThrow(/Invalid artifact path/);
    await expect(
      readRunArtifact("org-test", runId, "private-link.txt"),
    ).rejects.toThrow(/outside the run/);
  }, 30_000);
});

describe("writeWorkSkillFile", () => {
  it("records an edit in the context history so the next publish sends it", async () => {
    const home = await mkdtemp(join(tmpdir(), "neko-skill-edit-"));
    cleanupPaths.push(home);
    process.env.HOME = home;
    const { skillsRoot, orgRoot } = await ensureOrgWorkspace("org-skill-edit");
    await mkdir(join(skillsRoot, "quote-check"), { recursive: true });
    await writeFile(join(skillsRoot, "quote-check", "SKILL.md"), "---\nname: quote-check\n---\nOld\n");

    expect(await writeWorkSkillFile("org-skill-edit", "quote-check", "SKILL.md", "---\nname: quote-check\n---\nNew\n"))
      .toMatchObject({ ok: true });

    const git = (...args: string[]) => execFileSync("git", args, { cwd: orgRoot, encoding: "utf8" }).trim();
    expect(git("log", "-1", "--format=%s")).toBe("Updated skill: quote-check");
    expect(git("show", "HEAD:skills/quote-check/SKILL.md")).toContain("New");
  });
});

import { describe, expect, it, vi } from "vitest";
import { moveLibraryDocumentsToSource } from "../src/jobs/library-recovery";

describe("moveLibraryDocumentsToSource", () => {
  it("moves workspace uploads into the user's Library folder and drops the original", async () => {
    const list = vi.fn(async () => [
      { orgId: "org-1", documentId: "doc-lib", userId: "user-1", filename: "policy.pdf", relativePath: "library/uploads/user-1/h/policy.pdf" },
      { orgId: "org-1", documentId: "doc-thread", userId: "user-1", filename: "notes.md", relativePath: "uploads/thread-1/notes.md" },
    ]);
    const read = vi.fn(async () => Buffer.from("bytes"));
    const save = vi.fn(async (input: { userId: string | null; filename: string }) => ({
      relativePath: `neko_library/users/${input.userId}/hash/${input.filename}`,
      contentHash: "hash",
      size: 5,
    }));
    const moved = vi.fn(async () => undefined);
    const failed = vi.fn(async () => undefined);
    const removeOriginal = vi.fn(async () => undefined);

    await expect(moveLibraryDocumentsToSource({ list, read, save, moved, failed, removeOriginal }))
      .resolves.toEqual({ moved: 2, missing: 0 });
    expect(moved).toHaveBeenCalledWith({ orgId: "org-1", id: "doc-lib", relativePath: "neko_library/users/user-1/hash/policy.pdf" });
    // Library uploads leave the workspace; thread attachments stay with their thread.
    expect(removeOriginal).toHaveBeenCalledOnce();
    expect(removeOriginal).toHaveBeenCalledWith("org-1", "library/uploads/user-1/h/policy.pdf");
  });

  it("marks a document whose original file is gone as failed", async () => {
    const list = vi.fn(async () => [
      { orgId: "org-1", documentId: "doc-gone", userId: null, filename: "a.pdf", relativePath: "library/uploads/solo/h/a.pdf" },
    ]);
    const read = vi.fn(async () => { throw new Error("ENOENT"); });
    const save = vi.fn();
    const failed = vi.fn(async () => undefined);

    await expect(moveLibraryDocumentsToSource({ list, read, save, failed, moved: vi.fn(), removeOriginal: vi.fn() }))
      .resolves.toEqual({ moved: 0, missing: 1 });
    expect(save).not.toHaveBeenCalled();
    expect(failed).toHaveBeenCalledWith(expect.objectContaining({ id: "doc-gone", error: expect.stringContaining("missing") }));
  });
});

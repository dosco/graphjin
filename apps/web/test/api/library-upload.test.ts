import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MAX_LIBRARY_UPLOAD_BYTES,
  MAX_LIBRARY_UPLOAD_REQUEST_BYTES,
} from "@/lib/library-upload-contract";

const mocks = vi.hoisted(() => ({
  actor: vi.fn(),
  createDocument: vi.fn(),
  formData: vi.fn(),
  orgId: vi.fn(),
  save: vi.fn(),
}));

vi.mock("@/lib/actor", () => ({ getCurrentActor: mocks.actor }));
vi.mock("@/lib/db", () => ({ getOrgId: mocks.orgId }));
vi.mock("@neko/llm/library/source-files", () => ({
  saveLibrarySourceFile: mocks.save,
}));
vi.mock("@neko/llm/work", () => ({
  createLibraryDocument: mocks.createDocument,
}));

function sizedFile(name: string, size: number): File {
  const value = new File(["x"], name, { type: "application/pdf" });
  Object.defineProperty(value, "size", { configurable: true, value: size });
  return value;
}

function request(
  files: File[],
  options: { field?: "file" | "files"; contentLength?: number } = {},
): Request {
  const form = new FormData();
  for (const value of files) form.append(options.field ?? "files", value);
  mocks.formData.mockResolvedValue(form);
  const headers = new Headers();
  if (options.contentLength !== undefined) {
    headers.set("content-length", String(options.contentLength));
  }
  return { headers, formData: mocks.formData } as unknown as Request;
}

describe("/api/library/upload", () => {
  let POST: typeof import("@/app/api/library/upload/route").POST;

  beforeAll(async () => {
    ({ POST } = await import("@/app/api/library/upload/route"));
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.actor.mockResolvedValue({ userId: "user-1", role: "member" });
    mocks.orgId.mockResolvedValue("org-1");
    mocks.save.mockImplementation(
      async (input: { userId: string; filename: string; data: Buffer }) => ({
        size: input.data.byteLength,
        relativePath: `neko_library/users/${input.userId}/${"a".repeat(64)}/${input.filename}`,
        contentHash: "a".repeat(64),
      }),
    );
    let id = 0;
    mocks.createDocument.mockImplementation(async () => {
      id += 1;
      return {
        document: { id: `document-${id}`, status: "ready" },
        created: true,
      };
    });
  });

  it("imports one file at exactly 100 MB", async () => {
    const response = await POST(
      request([sizedFile("annual-report.pdf", MAX_LIBRARY_UPLOAD_BYTES)], {
        contentLength: MAX_LIBRARY_UPLOAD_REQUEST_BYTES,
      }),
    );

    expect(response.status).toBe(200);
    expect(mocks.save).toHaveBeenCalledOnce();
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({ userId: "user-1", filename: "annual-report.pdf" }),
    );
    expect(mocks.createDocument).toHaveBeenCalledWith(
      expect.objectContaining({
        orgId: "org-1",
        userId: "user-1",
        relativePath: `neko_library/users/user-1/${"a".repeat(64)}/annual-report.pdf`,
      }),
    );
    await expect(response.json()).resolves.toMatchObject({
      ok: true,
      documents: [{ document: { id: "document-1" }, created: true }],
    });
  });

  it("imports multiple files in one request when their total is 100 MB", async () => {
    const response = await POST(
      request([
        sizedFile("sales.pdf", 60 * 1024 * 1024),
        sizedFile("operations.pdf", 40 * 1024 * 1024),
      ]),
    );

    expect(response.status).toBe(200);
    expect(mocks.formData).toHaveBeenCalledOnce();
    expect(mocks.save).toHaveBeenCalledTimes(2);
    expect(mocks.createDocument).toHaveBeenCalledTimes(2);
    await expect(response.json()).resolves.toMatchObject({
      ok: true,
      documents: [
        { document: { id: "document-1" }, created: true },
        { document: { id: "document-2" }, created: true },
      ],
    });
  });

  it("rejects an aggregate payload one byte over 100 MB before writing", async () => {
    const response = await POST(
      request([
        sizedFile("sales.pdf", 60 * 1024 * 1024),
        sizedFile("operations.pdf", 40 * 1024 * 1024 + 1),
      ]),
    );

    expect(response.status).toBe(413);
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.createDocument).not.toHaveBeenCalled();
    await expect(response.json()).resolves.toMatchObject({
      error: expect.stringContaining("100 MB"),
    });
  });

  it("validates the complete batch before writing any file", async () => {
    const response = await POST(
      request([
        sizedFile("valid.pdf", 10),
        sizedFile("malware.exe", 10),
      ]),
    );

    expect(response.status).toBe(415);
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.createDocument).not.toHaveBeenCalled();
  });

  it("rejects filenames that collide after cleanup before writing", async () => {
    const response = await POST(
      request([
        sizedFile("sales report.pdf", 10),
        sizedFile("sales_report.pdf", 10),
      ]),
    );

    expect(response.status).toBe(400);
    expect(mocks.save).not.toHaveBeenCalled();
    expect(mocks.createDocument).not.toHaveBeenCalled();
    await expect(response.json()).resolves.toMatchObject({
      error: expect.stringContaining("sales_report.pdf"),
    });
  });

  it("keeps the legacy singular file field compatible", async () => {
    const response = await POST(
      request([sizedFile("legacy.pdf", 10)], { field: "file" }),
    );

    expect(response.status).toBe(200);
    expect(mocks.save).toHaveBeenCalledOnce();
  });

  it("rejects a declared multipart request beyond the bounded allowance", async () => {
    const response = await POST(
      request([sizedFile("oversized.pdf", 10)], {
        contentLength: MAX_LIBRARY_UPLOAD_REQUEST_BYTES + 1,
      }),
    );

    expect(response.status).toBe(413);
    expect(mocks.formData).not.toHaveBeenCalled();
    expect(mocks.save).not.toHaveBeenCalled();
  });
});

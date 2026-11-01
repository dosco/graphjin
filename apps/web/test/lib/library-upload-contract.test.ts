import { describe, expect, it } from "vitest";
import {
  MAX_LIBRARY_UPLOAD_BYTES,
  validateLibraryUploadBatch,
} from "@/lib/library-upload-contract";

const file = (name: string, size: number) => ({ name, size });

describe("library upload contract", () => {
  it("accepts one file that uses the full 100 MB allowance", () => {
    expect(
      validateLibraryUploadBatch([
        file("annual-report.pdf", MAX_LIBRARY_UPLOAD_BYTES),
      ]),
    ).toEqual({ ok: true, totalBytes: MAX_LIBRARY_UPLOAD_BYTES });
  });

  it("accepts multiple files that together use the full allowance", () => {
    expect(
      validateLibraryUploadBatch([
        file("sales.pdf", 60 * 1024 * 1024),
        file("operations.pdf", 40 * 1024 * 1024),
      ]),
    ).toEqual({ ok: true, totalBytes: MAX_LIBRARY_UPLOAD_BYTES });
  });

  it("rejects a batch one byte over the aggregate allowance", () => {
    expect(
      validateLibraryUploadBatch([
        file("sales.pdf", 60 * 1024 * 1024),
        file("operations.pdf", 40 * 1024 * 1024 + 1),
      ]),
    ).toEqual({
      ok: false,
      status: 413,
      error:
        "The selected files exceed the 100 MB import limit. Choose fewer or smaller files.",
    });
  });

  it("rejects empty files before an import starts", () => {
    expect(validateLibraryUploadBatch([file("empty.pdf", 0)])).toEqual({
      ok: false,
      status: 400,
      error: '"empty.pdf" is empty. Choose a non-empty file.',
    });
  });
});

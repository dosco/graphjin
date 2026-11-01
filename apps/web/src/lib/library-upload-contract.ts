export const MAX_LIBRARY_UPLOAD_BYTES = 100 * 1024 * 1024;

// Multipart boundaries and per-file headers are transport overhead, not part
// of the user's 100 MB allowance. Keep the request itself bounded as an early
// rejection while leaving room for a large multi-file selection.
export const MAX_LIBRARY_UPLOAD_REQUEST_BYTES =
  MAX_LIBRARY_UPLOAD_BYTES + 4 * 1024 * 1024;

type LibraryUploadFile = {
  name: string;
  size: number;
};

export type LibraryUploadValidation =
  | { ok: true; totalBytes: number }
  | { ok: false; status: 400 | 413; error: string };

/** Shared browser/API contract: 100 MB total, inclusive, across the batch. */
export function validateLibraryUploadBatch(
  files: readonly LibraryUploadFile[],
): LibraryUploadValidation {
  if (files.length === 0) {
    return {
      ok: false,
      status: 400,
      error: "Choose at least one file to import.",
    };
  }

  let totalBytes = 0;
  for (const file of files) {
    if (!Number.isSafeInteger(file.size) || file.size <= 0) {
      return {
        ok: false,
        status: 400,
        error:
          file.size === 0
            ? `"${file.name}" is empty. Choose a non-empty file.`
            : `"${file.name}" has an invalid size. Choose the file again.`,
      };
    }
    totalBytes += file.size;
    if (totalBytes > MAX_LIBRARY_UPLOAD_BYTES) {
      return {
        ok: false,
        status: 413,
        error:
          "The selected files exceed the 100 MB import limit. Choose fewer or smaller files.",
      };
    }
  }

  return { ok: true, totalBytes };
}

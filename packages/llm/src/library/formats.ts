export const LIBRARY_SUPPORTED_EXTENSIONS = [
  ".pdf",
  ".docx",
  ".pptx",
  ".xlsx",
  ".csv",
  ".md",
  ".markdown",
  ".txt",
] as const;

export type LibrarySupportedExtension = (typeof LIBRARY_SUPPORTED_EXTENSIONS)[number];

const SUPPORTED = new Set<string>(LIBRARY_SUPPORTED_EXTENSIONS);
export const LIBRARY_UPLOAD_ACCEPT = LIBRARY_SUPPORTED_EXTENSIONS.join(",");

export function libraryFileExtension(filename: string): string {
  const idx = filename.lastIndexOf(".");
  return idx > 0 ? filename.slice(idx).toLowerCase() : "";
}

export function isLibrarySupportedExtension(
  extension: string,
): extension is LibrarySupportedExtension {
  return SUPPORTED.has(extension.toLowerCase());
}

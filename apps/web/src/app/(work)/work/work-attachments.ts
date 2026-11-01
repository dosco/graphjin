import type { UploadedWorkFile } from "./work-types";

export const MAX_ATTACHMENTS = 5;
export const MAX_ATTACHMENT_SIZE = 10 * 1024 * 1024;
export const ACCEPTED_ATTACHMENT_EXTENSIONS = [
  ".csv",
  ".docx",
  ".html",
  ".json",
  ".md",
  ".pdf",
  ".pptx",
  ".tsv",
  ".txt",
  ".xlsx",
];
export const ACCEPTED_ATTACHMENT_SUFFIXES = new Set(ACCEPTED_ATTACHMENT_EXTENSIONS);

export function fileExtension(name: string): string {
  const idx = name.lastIndexOf(".");
  return idx > 0 ? name.slice(idx).toLowerCase() : "";
}

export async function uploadFiles(
  threadId: string,
  picked: File[],
): Promise<{ uploaded: UploadedWorkFile[]; errors: string[] }> {
  const uploaded: UploadedWorkFile[] = [];
  const errors: string[] = [];
  for (const file of picked) {
    const body = new FormData();
    body.append("threadId", threadId);
    body.append("file", file);
    let res: Response;
    try {
      res = await fetch("/api/work/upload", { method: "POST", body });
    } catch (err) {
      errors.push(`"${file.name}": ${err instanceof Error ? err.message : "network error"}`);
      continue;
    }
    if (!res.ok) {
      const payload = (await res.json().catch(() => null)) as { error?: string } | null;
      errors.push(`"${file.name}": ${payload?.error ?? `upload failed (HTTP ${res.status})`}`);
      continue;
    }
    const data = (await res.json()) as { file: UploadedWorkFile };
    uploaded.push(data.file);
  }
  return { uploaded, errors };
}

export function joinMessageWithAttachments(
  text: string,
  files: UploadedWorkFile[],
): string {
  const prefix = text.trim();
  if (files.length === 0) return prefix;
  const lines = files.map((file) => {
    const kb = Math.max(1, Math.round(file.size / 1024));
    return `- ${file.relativePath}  (${file.name}, ${kb} KB)`;
  });
  const header = `I've attached ${files.length === 1 ? "a file" : "files"}:`;
  return prefix ? `${prefix}\n\n${header}\n${lines.join("\n")}` : `${header}\n${lines.join("\n")}`;
}

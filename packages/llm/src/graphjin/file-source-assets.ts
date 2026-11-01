import { createHash, randomUUID } from "node:crypto";
import {
  lstat,
  mkdir,
  open,
  readFile,
  readdir,
  realpath,
  rm,
  stat,
} from "node:fs/promises";
import { basename, dirname, join, sep } from "node:path";
import {
  assertManagedFileSourceName,
  managedFileSourceDirectoryName,
} from "./file-source";

export const MAX_MANAGED_FILE_SIZE = 10 * 1024 * 1024;
export const MAX_MANAGED_FILE_UPLOAD_SIZE = 25 * 1024 * 1024;
export const MAX_MANAGED_FILE_SOURCE_SIZE = 100 * 1024 * 1024;
export const MAX_MANAGED_FILE_UPLOAD_COUNT = 10;
export const MAX_MANAGED_FILE_SOURCE_COUNT = 500;

export type ManagedFileSourceEntry = {
  name: string;
  size: number;
  contentType: string;
  checksumSha256: string;
};

export type ManagedFileSourceUpload = {
  name: string;
  content: Uint8Array;
  contentType?: string;
};

export type ManagedFileSourceManifest = {
  sourceName: string;
  ready: true;
  files: ManagedFileSourceEntry[];
  totalSize: number;
};

const MANAGED_FILE_LOCK_STALE_MS = 10 * 60 * 1_000;

function managedStorageBase(input: {
  configFile: string;
  localBase?: string;
}): string {
  return (
    input.localBase ??
    join(/* turbopackIgnore: true */ dirname(input.configFile), "files")
  );
}

function managedFileControlPath(
  input: {
    configFile: string;
    orgId: string;
    sourceName: string;
    localBase?: string;
  },
  suffix: "lock" | "approved",
): string {
  return join(
    /* turbopackIgnore: true */ managedStorageBase(input),
    `.${managedFileSourceDirectoryName(input.orgId, input.sourceName)}.${suffix}`,
  );
}

export async function acquireManagedFileSourceLock(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  localBase?: string;
}): Promise<() => Promise<void>> {
  const base = managedStorageBase(input);
  await mkdir(/* turbopackIgnore: true */ base, {
    recursive: true,
    mode: 0o755,
  });
  const lockPath = managedFileControlPath(input, "lock");
  const token = randomUUID();
  const createLock = async () => {
    const handle = await open(
      /* turbopackIgnore: true */ lockPath,
      "wx",
      0o600,
    );
    try {
      await handle.writeFile(token);
      await handle.sync();
    } finally {
      await handle.close();
    }
  };
  try {
    await createLock();
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
    const metadata = await stat(
      /* turbopackIgnore: true */ lockPath,
    ).catch(() => null);
    if (!metadata || Date.now() - metadata.mtimeMs <= MANAGED_FILE_LOCK_STALE_MS) {
      throw new Error("managed file source has another operation in progress");
    }
    await rm(/* turbopackIgnore: true */ lockPath, { force: true });
    await createLock().catch((retryError) => {
      if ((retryError as NodeJS.ErrnoException).code === "EEXIST") {
        throw new Error("managed file source has another operation in progress");
      }
      throw retryError;
    });
  }
  return async () => {
    const currentToken = await readFile(
      /* turbopackIgnore: true */ lockPath,
      "utf8",
    ).catch(() => "");
    if (currentToken === token) {
      await rm(/* turbopackIgnore: true */ lockPath, { force: true });
    }
  };
}

export async function assertManagedFileSourceMutable(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  localBase?: string;
}): Promise<void> {
  const marker = managedFileControlPath(input, "approved");
  try {
    const metadata = await lstat(/* turbopackIgnore: true */ marker);
    if (!metadata.isFile() || metadata.isSymbolicLink()) {
      throw new Error("managed file source approval marker is invalid");
    }
    throw new Error(
      "managed file source is active; use a new source name for new content",
    );
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return;
    throw error;
  }
}

export async function markManagedFileSourceApproved(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  localBase?: string;
}): Promise<void> {
  const marker = managedFileControlPath(input, "approved");
  const handle = await open(
    /* turbopackIgnore: true */ marker,
    "wx",
    0o600,
  ).catch((error) => {
    if ((error as NodeJS.ErrnoException).code === "EEXIST") {
      throw new Error("managed file source is already active");
    }
    throw error;
  });
  try {
    await handle.writeFile(new Date().toISOString());
    await handle.sync();
  } finally {
    await handle.close();
  }
}

function safeFileName(value: string): string {
  const name = value.normalize("NFKC");
  if (
    name !== basename(name) ||
    name.includes("/") ||
    name.includes("\\") ||
    name.includes("\0") ||
    !/^[A-Za-z0-9][A-Za-z0-9._ ()-]{0,127}$/.test(name) ||
    /[ .]$/.test(name)
  ) {
    throw new Error(
      "managed file names must be 1-128 safe filename characters and may not contain a path",
    );
  }
  return name;
}

function safeContentType(value: string | undefined): string {
  const contentType = (value || "application/octet-stream").trim().slice(0, 200);
  if (!contentType || /[\r\n\0]/.test(contentType)) {
    return "application/octet-stream";
  }
  return contentType;
}

export function parseManagedFileSourceManifest(
  value: unknown,
  expectedSourceName: string,
): ManagedFileSourceManifest {
  assertManagedFileSourceName(expectedSourceName);
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("register_source local file needs a managed file manifest");
  }
  const input = value as Record<string, unknown>;
  if (input.sourceName !== expectedSourceName || input.ready !== true) {
    throw new Error(
      "register_source local file manifest must match the selected source name",
    );
  }
  if (
    !Array.isArray(input.files) ||
    input.files.length < 1 ||
    input.files.length > MAX_MANAGED_FILE_SOURCE_COUNT
  ) {
    throw new Error("managed file source manifest has an invalid file count");
  }

  const names = new Set<string>();
  let totalSize = 0;
  const files = input.files.map((value) => {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      throw new Error("managed file source manifest has an invalid file entry");
    }
    const entry = value as Record<string, unknown>;
    const name = safeFileName(String(entry.name ?? ""));
    if (names.has(name)) {
      throw new Error("managed file source manifest has duplicate names");
    }
    names.add(name);
    const size = entry.size;
    if (
      typeof size !== "number" ||
      !Number.isSafeInteger(size) ||
      size < 1 ||
      size > MAX_MANAGED_FILE_SIZE
    ) {
      throw new Error(`managed file "${name}" has an invalid size`);
    }
    const checksumSha256 = String(entry.checksumSha256 ?? "");
    if (!/^[a-f0-9]{64}$/.test(checksumSha256)) {
      throw new Error(`managed file "${name}" has an invalid checksum`);
    }
    const suppliedContentType = String(entry.contentType ?? "");
    const contentType = safeContentType(suppliedContentType);
    if (contentType !== suppliedContentType) {
      throw new Error(`managed file "${name}" has an invalid content type`);
    }
    totalSize += size;
    return { name, size, contentType, checksumSha256 };
  });
  if (
    totalSize > MAX_MANAGED_FILE_SOURCE_SIZE ||
    input.totalSize !== totalSize
  ) {
    throw new Error("managed file source manifest total does not match its files");
  }
  return { sourceName: expectedSourceName, ready: true, files, totalSize };
}

export function managedFileSourceLocalDirectory(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  localBase?: string;
}): string {
  const localBase = managedStorageBase(input);
  return join(
    /* turbopackIgnore: true */ localBase,
    managedFileSourceDirectoryName(input.orgId, input.sourceName),
  );
}

async function ensureManagedDirectory(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  localBase?: string;
}): Promise<string> {
  const localBase = managedStorageBase(input);
  await mkdir(/* turbopackIgnore: true */ localBase, {
    recursive: true,
    mode: 0o755,
  });
  const baseReal = await realpath(/* turbopackIgnore: true */ localBase);
  const directory = managedFileSourceLocalDirectory(input);
  try {
    const existing = await lstat(/* turbopackIgnore: true */ directory);
    if (!existing.isDirectory() || existing.isSymbolicLink()) {
      throw new Error("managed file source path is not a regular directory");
    }
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    await mkdir(/* turbopackIgnore: true */ directory, { mode: 0o755 });
  }
  const directoryReal = await realpath(
    /* turbopackIgnore: true */ directory,
  );
  if (!directoryReal.startsWith(`${baseReal}${sep}`)) {
    throw new Error("managed file source directory escaped its storage root");
  }
  return directoryReal;
}

export async function listManagedFileSourceFiles(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  localBase?: string;
}): Promise<ManagedFileSourceEntry[]> {
  const directory = await ensureManagedDirectory(input);
  const entries = await readdir(/* turbopackIgnore: true */ directory, {
    withFileTypes: true,
  });
  const files: ManagedFileSourceEntry[] = [];
  for (const entry of entries) {
    if (!entry.isFile() || entry.isSymbolicLink()) {
      throw new Error(
        `managed file source contains an unsupported entry "${entry.name}"`,
      );
    }
    const name = safeFileName(entry.name);
    const target = join(/* turbopackIgnore: true */ directory, name);
    const metadata = await stat(/* turbopackIgnore: true */ target);
    const content = await readFile(/* turbopackIgnore: true */ target);
    files.push({
      name,
      size: metadata.size,
      contentType: "application/octet-stream",
      checksumSha256: createHash("sha256").update(content).digest("hex"),
    });
  }
  return files.sort((left, right) => left.name.localeCompare(right.name));
}

export async function stageManagedFileSourceFiles(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  files: ManagedFileSourceUpload[];
  localBase?: string;
}): Promise<ManagedFileSourceEntry[]> {
  if (input.files.length < 1 || input.files.length > MAX_MANAGED_FILE_UPLOAD_COUNT) {
    throw new Error(
      `upload between 1 and ${MAX_MANAGED_FILE_UPLOAD_COUNT} files at a time`,
    );
  }
  const names = input.files.map((file) => safeFileName(file.name));
  if (new Set(names).size !== names.length) {
    throw new Error("the upload contains duplicate file names");
  }
  let uploadSize = 0;
  for (const file of input.files) {
    if (file.content.byteLength > MAX_MANAGED_FILE_SIZE) {
      throw new Error(
        `"${file.name}" exceeds the ${MAX_MANAGED_FILE_SIZE / (1024 * 1024)} MB file limit`,
      );
    }
    uploadSize += file.content.byteLength;
  }
  if (uploadSize > MAX_MANAGED_FILE_UPLOAD_SIZE) {
    throw new Error(
      `upload exceeds the ${MAX_MANAGED_FILE_UPLOAD_SIZE / (1024 * 1024)} MB request limit`,
    );
  }

  const existing = await listManagedFileSourceFiles(input);
  if (existing.length + input.files.length > MAX_MANAGED_FILE_SOURCE_COUNT) {
    throw new Error("managed file source has reached its file-count limit");
  }
  if (
    existing.reduce((sum, file) => sum + file.size, 0) + uploadSize >
    MAX_MANAGED_FILE_SOURCE_SIZE
  ) {
    throw new Error("managed file source has reached its storage limit");
  }

  const directory = await ensureManagedDirectory(input);
  const created: string[] = [];
  const uploaded: ManagedFileSourceEntry[] = [];
  try {
    for (let index = 0; index < input.files.length; index += 1) {
      const file = input.files[index]!;
      const name = names[index]!;
      const target = join(/* turbopackIgnore: true */ directory, name);
      const handle = await open(
        /* turbopackIgnore: true */ target,
        "wx",
        0o644,
      ).catch((error) => {
        if ((error as NodeJS.ErrnoException).code === "EEXIST") {
          throw new Error(`managed file "${name}" already exists`);
        }
        throw error;
      });
      try {
        await handle.writeFile(file.content);
        await handle.sync();
      } finally {
        await handle.close();
      }
      created.push(target);
      uploaded.push({
        name,
        size: file.content.byteLength,
        contentType: safeContentType(file.contentType),
        checksumSha256: createHash("sha256").update(file.content).digest("hex"),
      });
    }
  } catch (error) {
    await Promise.all(
      created.map((target) =>
        rm(/* turbopackIgnore: true */ target, { force: true }),
      ),
    );
    throw error;
  }
  return uploaded;
}

export async function verifyManagedFileSourceManifest(input: {
  configFile: string;
  orgId: string;
  sourceName: string;
  files: Array<{ name: string; size: number; checksumSha256: string }>;
  totalSize?: number;
  localBase?: string;
}): Promise<void> {
  if (input.files.length < 1 || input.files.length > MAX_MANAGED_FILE_SOURCE_COUNT) {
    throw new Error("managed file source manifest has an invalid file count");
  }
  const directory = await ensureManagedDirectory(input);
  const entries = await readdir(/* turbopackIgnore: true */ directory, {
    withFileTypes: true,
  });
  const names = new Set<string>();
  let totalSize = 0;
  for (const expected of input.files) {
    const name = safeFileName(expected.name);
    if (names.has(name)) throw new Error("managed file source manifest has duplicate names");
    names.add(name);
    const target = join(/* turbopackIgnore: true */ directory, name);
    const metadata = await lstat(/* turbopackIgnore: true */ target);
    if (!metadata.isFile() || metadata.isSymbolicLink()) {
      throw new Error(`managed file "${name}" is not a regular file`);
    }
    if (metadata.size !== expected.size || metadata.size > MAX_MANAGED_FILE_SIZE) {
      throw new Error(`managed file "${name}" failed its size check`);
    }
    totalSize += metadata.size;
    const checksum = createHash("sha256")
      .update(await readFile(/* turbopackIgnore: true */ target))
      .digest("hex");
    if (checksum !== expected.checksumSha256) {
      throw new Error(`managed file "${name}" failed its integrity check`);
    }
  }
  if (
    entries.length !== names.size ||
    entries.some(
      (entry) =>
        !entry.isFile() ||
        entry.isSymbolicLink() ||
        !names.has(entry.name),
    )
  ) {
    throw new Error("managed file source contents do not match its approved manifest");
  }
  if (
    totalSize > MAX_MANAGED_FILE_SOURCE_SIZE ||
    (input.totalSize !== undefined && input.totalSize !== totalSize)
  ) {
    throw new Error("managed file source manifest exceeds its storage limit");
  }
}

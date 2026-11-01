import { createHash } from "node:crypto";

const FILE_SOURCE_NAME = /^[A-Za-z][A-Za-z0-9_]{0,63}$/;
const DURATION = /^([1-9][0-9]*)(s|m|h)$/;

export function assertManagedFileSourceName(name: string): void {
  if (!FILE_SOURCE_NAME.test(name)) {
    throw new Error(
      "register_source file name must start with a letter and contain only letters, digits, and underscores (64 characters maximum)",
    );
  }
}

export function managedFileSourceDirectoryName(
  orgId: string,
  sourceName: string,
): string {
  assertManagedFileSourceName(sourceName);
  const orgHash = createHash("sha256").update(orgId).digest("hex").slice(0, 12);
  return `org_${orgHash}_${sourceName}`;
}

export function managedFileSourceRuntimeRoot(input: {
  orgId: string;
  sourceName: string;
  runtimeBase?: string;
}): string {
  const runtimeBase = (input.runtimeBase ?? "/config/files").replace(/\/+$/, "");
  if (!runtimeBase.startsWith("/")) {
    throw new Error("managed GraphJin file runtime directory must be absolute");
  }
  return `${runtimeBase}/${managedFileSourceDirectoryName(input.orgId, input.sourceName)}`;
}

export function validateObjectStoreBucket(backend: "s3" | "gcs", bucket: string): void {
  const valid =
    backend === "s3"
      ? /^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$/.test(bucket) &&
        !bucket.includes("..") &&
        !/^\d+\.\d+\.\d+\.\d+$/.test(bucket)
      : /^[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]$/.test(bucket) &&
        !bucket.includes("..") &&
        !bucket.startsWith("goog");
  if (!valid) {
    throw new Error(`register_source ${backend} file has an invalid bucket name`);
  }
}

export function validateObjectStoreEndpoint(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("register_source file endpoint must be a valid HTTP(S) URL");
  }
  if (!["http:", "https:"].includes(url.protocol)) {
    throw new Error("register_source file endpoint must use HTTP or HTTPS");
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new Error(
      "register_source file endpoint must not contain credentials, a query string, or a fragment",
    );
  }
  return url.toString().replace(/\/$/, "");
}

export function validatePublicFileBaseUrl(value: string): string {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    throw new Error("register_source file public base URL must be valid");
  }
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) {
    throw new Error(
      "register_source file public base URL must use HTTPS without credentials, a query string, or a fragment",
    );
  }
  return url.toString().replace(/\/$/, "");
}

export function validateFilePresignTtl(value: string): string {
  const match = DURATION.exec(value);
  if (!match) {
    throw new Error("register_source file presign TTL must be a duration such as 15m");
  }
  const amount = Number(match[1]);
  const seconds = amount * ({ s: 1, m: 60, h: 3600 }[match[2]!] ?? 0);
  if (!Number.isSafeInteger(seconds) || seconds < 60 || seconds > 3600) {
    throw new Error("register_source file presign TTL must be between 1m and 1h");
  }
  return value;
}

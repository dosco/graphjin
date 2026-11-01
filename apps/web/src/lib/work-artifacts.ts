import { posix } from "node:path";

type ArtifactEvent = {
  type: string;
  artifact?: { path?: unknown };
};

/** Canonicalize only paths under this exact run's artifacts directory. */
export function canonicalRunArtifactPath(
  value: unknown,
  runId: string,
): string | null {
  if (typeof value !== "string" || !value.trim()) return null;
  const raw = value.replace(/\\/g, "/");
  const prefix = `runs/${runId}/artifacts/`;
  const absoluteMarker = `/${prefix}`;
  const markerIndex = raw.lastIndexOf(absoluteMarker);
  const candidate = raw.startsWith(prefix)
    ? raw
    : markerIndex >= 0
      ? raw.slice(markerIndex + 1)
      : null;
  if (!candidate || !candidate.startsWith(prefix)) return null;
  const suffix = candidate.slice(prefix.length);
  if (
    !suffix ||
    suffix.includes("\0") ||
    suffix.startsWith("/") ||
    posix.normalize(suffix) !== suffix ||
    suffix.split("/").some((part) => !part || part === "." || part === "..")
  ) {
    return null;
  }
  return `${prefix}${suffix}`;
}

export function isEmittedRunArtifact(
  events: readonly ArtifactEvent[],
  runId: string,
  requestedPath: string,
): boolean {
  const requested = canonicalRunArtifactPath(requestedPath, runId);
  if (!requested) return false;
  return events.some(
    (event) =>
      event.type === "artifact" &&
      canonicalRunArtifactPath(event.artifact?.path, runId) === requested,
  );
}

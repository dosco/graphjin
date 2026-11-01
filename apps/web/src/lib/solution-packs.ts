const PACK_ID = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export const packReadActions = ["inspect", "plan", "status", "doctor"] as const;
export const packWriteActions = ["review", "install", "configure", "upgrade", "uninstall"] as const;

export type PackReadAction = (typeof packReadActions)[number];
export type PackWriteAction = (typeof packWriteActions)[number];

export function validPackId(packId: string): boolean {
  return PACK_ID.test(packId);
}

export function isPackReadAction(value: string): value is PackReadAction {
  return (packReadActions as readonly string[]).includes(value);
}

export function isPackWriteAction(value: string): value is PackWriteAction {
  return (packWriteActions as readonly string[]).includes(value);
}

function workerAdminBase(): string {
  return (process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100").replace(/\/+$/, "");
}

export async function requestPackWorker(
  path: string,
  init: RequestInit = {},
): Promise<{ status: number; body: unknown }> {
  const response = await fetch(`${workerAdminBase()}${path}`, {
    ...init,
    cache: "no-store",
    signal: AbortSignal.any([AbortSignal.timeout(120_000), ...(init.signal ? [init.signal] : [])]),
  });
  const body = await response.json().catch(() => ({ error: `Worker returned HTTP ${response.status}` }));
  return { status: response.status, body };
}

/** Bound bytes while reading, including requests without Content-Length. */
export async function readPackRequest(request: Request, limit: number): Promise<Buffer> {
  if (Number(request.headers.get("content-length")) > limit) throw new Error("request is too large");
  const reader = request.body?.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (reader) {
      const next = await reader.read();
      if (next.done) break;
      size += next.value.byteLength;
      if (size > limit) {
        await reader.cancel().catch(() => {});
        throw new Error("request is too large");
      }
      chunks.push(next.value);
    }
  } finally { reader?.releaseLock(); }
  return Buffer.concat(chunks, size);
}

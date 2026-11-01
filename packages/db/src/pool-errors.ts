import type pg from "pg";

/**
 * Keep a dropped connection from killing the process. pg-pool listens for
 * client errors only while a client is idle. A client checked out with
 * pool.connect() has no listener, so a Postgres restart during a held
 * transaction raised an unhandled 'error' event and stopped the worker.
 * The caller's pending query still rejects, so logging is enough here.
 */
export function guardPoolErrors(pool: pg.Pool, label: string): pg.Pool {
  const onClientError = (error: Error) => {
    console.error(`[${label}] checked-out connection error:`, error.message);
  };
  pool.on("error", (error) => {
    console.error(`[${label}] idle connection error:`, error instanceof Error ? error.message : error);
  });
  pool.on("acquire", (client) => {
    if (!client.listeners("error").includes(onClientError)) client.on("error", onClientError);
  });
  pool.on("release", (_error, client) => {
    client.removeListener("error", onClientError);
  });
  return pool;
}

const UNAVAILABLE_CODES = new Set(["57P01", "57P02", "57P03", "08000", "08001", "08003", "08004", "08006"]);
const UNAVAILABLE_MESSAGE =
  /Connection terminated unexpectedly|the database system is (starting up|in recovery mode|not yet accepting connections|shutting down)|ECONNREFUSED|ECONNRESET/;

/** True when Postgres is restarting or unreachable, as opposed to a query fault. */
export function isDatabaseUnavailable(error: unknown): boolean {
  const { code, message } = (error ?? {}) as { code?: unknown; message?: unknown };
  if (typeof code === "string" && UNAVAILABLE_CODES.has(code)) return true;
  return typeof message === "string" && UNAVAILABLE_MESSAGE.test(message);
}

/** Retry an operation through a short Postgres restart; other errors fail at once. */
export async function retryWhileDatabaseUnavailable<T>(
  operation: () => Promise<T>,
  opts: { attempts?: number; baseMs?: number; maxMs?: number } = {},
): Promise<T> {
  const attempts = opts.attempts ?? 10;
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await operation();
    } catch (error) {
      if (attempt >= attempts || !isDatabaseUnavailable(error)) throw error;
      const delay = Math.min(opts.maxMs ?? 5_000, (opts.baseMs ?? 500) * 2 ** (attempt - 1));
      await new Promise((resolve) => setTimeout(resolve, delay));
    }
  }
}

/**
 * Keep a process alive when a library rejects during a Postgres restart
 * without a handler (pg-boss does while it records a job failure). Any other
 * unhandled rejection still stops the process.
 */
export function keepProcessThroughDatabaseRestarts(label: string): void {
  process.on("unhandledRejection", (reason) => {
    if (isDatabaseUnavailable(reason)) {
      console.error(`[${label}] database unavailable; continuing:`, reason instanceof Error ? reason.message : reason);
      return;
    }
    throw reason;
  });
}

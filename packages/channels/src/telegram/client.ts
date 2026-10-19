// Thin Telegram Bot API client. The bot token is baked into the URL path
// (/bot<token>/<method>); every Bot API call is a POST with a JSON body that
// returns a { ok, result } envelope. One generic `call` covers both outbound
// (sendMessage) and inbound-side (answerCallbackQuery, getUpdates) methods, so
// the bundled runner stays small and stays bidirectional without dead wrappers.

export const TELEGRAM_API_BASE = "https://api.telegram.org";
const DEFAULT_TIMEOUT_MS = 20_000;

export interface TelegramEnvelope<T = unknown> {
  ok: boolean;
  result?: T;
  description?: string;
  error_code?: number;
}

export class TelegramApiError extends Error {
  constructor(
    message: string,
    public readonly errorCode: number | null,
    public override readonly cause?: unknown,
  ) {
    super(message);
    this.name = "TelegramApiError";
  }
}

export interface TelegramClientOptions {
  token: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
  /** Override for tests; default https://api.telegram.org */
  base?: string;
}

export interface TelegramClient {
  call<T = unknown>(method: string, body: Record<string, unknown>): Promise<T>;
}

// undici reports transport failures as an opaque "fetch failed"; the real OS
// code (ENOTFOUND, ECONNREFUSED, ENETUNREACH, …) hangs off `.cause`/`.errors`.
function fetchErrorDetail(err: unknown): string {
  const base = err instanceof Error ? err.message : String(err);
  const codes = new Set<string>();
  const visit = (e: unknown): void => {
    if (!e || typeof e !== "object") return;
    const o = e as { code?: unknown; cause?: unknown; errors?: unknown };
    if (typeof o.code === "string") codes.add(o.code);
    if (Array.isArray(o.errors)) for (const sub of o.errors) visit(sub);
    if (o.cause) visit(o.cause);
  };
  visit(err);
  return codes.size > 0 ? `${base} (${[...codes].join(", ")})` : base;
}

export function createTelegramClient(options: TelegramClientOptions): TelegramClient {
  const fetchImpl = options.fetchImpl ?? fetch;
  const base = options.base ?? TELEGRAM_API_BASE;
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const root = `${base}/bot${options.token}`;

  return {
    async call<T>(method: string, body: Record<string, unknown>): Promise<T> {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs);
      let response: Response;
      try {
        response = await fetchImpl(`${root}/${method}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
          signal: controller.signal,
        });
      } catch (err) {
        if ((err as Error)?.name === "AbortError") {
          throw new TelegramApiError(
            `Telegram ${method} timed out after ${timeoutMs}ms`,
            null,
          );
        }
        throw new TelegramApiError(
          `Telegram ${method} network error: ${fetchErrorDetail(err)}`,
          null,
          err,
        );
      } finally {
        clearTimeout(timer);
      }
      const text = await response.text().catch(() => "");
      let envelope: TelegramEnvelope<T>;
      try {
        envelope = text ? (JSON.parse(text) as TelegramEnvelope<T>) : { ok: false };
      } catch (err) {
        throw new TelegramApiError(
          `Telegram ${method} returned non-JSON (HTTP ${response.status}): ${text.slice(0, 200)}`,
          null,
          err,
        );
      }
      if (!envelope.ok) {
        throw new TelegramApiError(
          `Telegram ${method} returned ok=false (${envelope.description ?? "no description"})`,
          envelope.error_code ?? null,
        );
      }
      return envelope.result as T;
    },
  };
}

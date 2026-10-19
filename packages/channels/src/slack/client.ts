// Thin Slack Web API client. Bearer-token auth. No SDK dep so the
// bundled runner stays small. Every call returns the parsed JSON
// envelope; throws SlackApiError on `ok: false` with the Slack-
// reported error code attached.

export const SLACK_API_BASE = "https://slack.com/api";
const DEFAULT_TIMEOUT_MS = 20_000;

export interface SlackEnvelope {
  ok: boolean;
  error?: string;
  warning?: string;
  [k: string]: unknown;
}

export class SlackApiError extends Error {
  constructor(
    message: string,
    public readonly slackError: string | null,
    public override readonly cause?: unknown,
  ) {
    super(message);
    this.name = "SlackApiError";
  }
}

export interface SlackClientOptions {
  token: string;
  fetchImpl?: typeof fetch;
  timeoutMs?: number;
  /** Override for tests; default https://slack.com/api */
  base?: string;
}

export interface SlackClient {
  postForm(method: string, body: Record<string, unknown>): Promise<SlackEnvelope>;
  postJson(method: string, body: Record<string, unknown>): Promise<SlackEnvelope>;
  get(method: string, params: Record<string, string>): Promise<SlackEnvelope>;
}

export function createSlackClient(options: SlackClientOptions): SlackClient {
  const fetchImpl = options.fetchImpl ?? fetch;
  const base = options.base ?? SLACK_API_BASE;
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const auth = `Bearer ${options.token}`;

  async function callOnce(
    url: string,
    init: RequestInit,
    method: string,
  ): Promise<SlackEnvelope> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), timeoutMs);
    let response: Response;
    try {
      response = await fetchImpl(url, { ...init, signal: controller.signal });
    } catch (err) {
      if ((err as Error)?.name === "AbortError") {
        throw new SlackApiError(
          `Slack ${method} timed out after ${timeoutMs}ms`,
          "timeout",
        );
      }
      throw new SlackApiError(
        `Slack ${method} network error: ${err instanceof Error ? err.message : String(err)}`,
        null,
        err,
      );
    } finally {
      clearTimeout(timer);
    }
    const text = await response.text().catch(() => "");
    let envelope: SlackEnvelope;
    try {
      envelope = text ? (JSON.parse(text) as SlackEnvelope) : { ok: false };
    } catch (err) {
      throw new SlackApiError(
        `Slack ${method} returned non-JSON (HTTP ${response.status}): ${text.slice(0, 200)}`,
        null,
        err,
      );
    }
    if (!envelope.ok) {
      throw new SlackApiError(
        `Slack ${method} returned ok=false (${envelope.error ?? "no error code"})`,
        envelope.error ?? null,
      );
    }
    return envelope;
  }

  return {
    async postForm(method, body) {
      const url = `${base}/${encodeURIComponent(method)}`;
      const form = new URLSearchParams();
      for (const [k, v] of Object.entries(body)) {
        if (v === undefined || v === null) continue;
        form.set(k, typeof v === "string" ? v : JSON.stringify(v));
      }
      return callOnce(
        url,
        {
          method: "POST",
          headers: {
            Authorization: auth,
            "Content-Type": "application/x-www-form-urlencoded",
          },
          body: form.toString(),
        },
        method,
      );
    },
    async postJson(method, body) {
      const url = `${base}/${encodeURIComponent(method)}`;
      return callOnce(
        url,
        {
          method: "POST",
          headers: {
            Authorization: auth,
            "Content-Type": "application/json; charset=utf-8",
          },
          body: JSON.stringify(body),
        },
        method,
      );
    },
    async get(method, params) {
      const url = new URL(`${base}/${encodeURIComponent(method)}`);
      for (const [k, v] of Object.entries(params)) {
        url.searchParams.set(k, v);
      }
      return callOnce(
        url.toString(),
        { method: "GET", headers: { Authorization: auth } },
        method,
      );
    },
  };
}

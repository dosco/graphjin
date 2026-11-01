"use client";

export type AdminApiResult<T> = { ok: true; body: T } | { ok: false; error: string };

export async function adminApi<T = unknown>(path: string, method = "GET", body?: unknown): Promise<AdminApiResult<T>> {
  try {
    const response = await fetch(path, {
      method,
      cache: "no-store",
      headers: body === undefined ? undefined : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const parsed = (await response.json().catch(() => null)) as (T & { error?: string }) | null;
    if (!response.ok) return { ok: false, error: parsed?.error ?? `Request failed (${response.status})` };
    return { ok: true, body: parsed as T };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : String(error) };
  }
}

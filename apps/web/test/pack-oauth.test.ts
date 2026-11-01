import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("server-only", () => ({}));

import { packOAuthCallbackUri } from "../src/lib/pack-oauth";

describe("packOAuthCallbackUri", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("uses the browser-facing host when the server request URL is internal", () => {
    const request = new Request("http://0.0.0.0:8080/api/pack-accounts/google-workspace/workspace", {
      headers: { host: "localhost:3102" },
    });

    expect(packOAuthCallbackUri(request, "google-workspace", "workspace")).toBe(
      "http://localhost:3102/api/pack-accounts/google-workspace/workspace/callback",
    );
  });

  it("uses forwarded host and protocol behind a reverse proxy", () => {
    const request = new Request("http://web:8080/api/pack-accounts/google-workspace/workspace", {
      headers: {
        host: "web:8080",
        "x-forwarded-host": "neko.example.com",
        "x-forwarded-proto": "https",
      },
    });

    expect(packOAuthCallbackUri(request, "google-workspace", "workspace")).toBe(
      "https://neko.example.com/api/pack-accounts/google-workspace/workspace/callback",
    );
  });

  it("uses the configured public URL when present", () => {
    vi.stubEnv("OPENNEKO_PUBLIC_URL", "https://public.example.com/");
    const request = new Request("http://web:8080/api/pack-accounts/google-workspace/workspace");

    expect(packOAuthCallbackUri(request, "google-workspace", "workspace")).toBe(
      "https://public.example.com/api/pack-accounts/google-workspace/workspace/callback",
    );
  });
});

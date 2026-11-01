import { afterEach, describe, expect, it, vi } from "vitest";
import { appRedirect, publicBaseUrl } from "@/lib/public-url";

describe("public URL helpers", () => {
  afterEach(() => vi.unstubAllEnvs());

  it("uses OPENNEKO_PUBLIC_URL for links that leave the app", () => {
    vi.stubEnv("OPENNEKO_PUBLIC_URL", "http://localhost:3000/");
    expect(publicBaseUrl("http://0.0.0.0:8080/api/auth/begin")).toBe("http://localhost:3000");
    vi.stubEnv("OPENNEKO_PUBLIC_URL", "");
    expect(publicBaseUrl("http://0.0.0.0:8080/api/auth/begin")).toBe("http://0.0.0.0:8080");
  });

  it("redirects within the app with a relative location", () => {
    expect(appRedirect("/signin?notice=link-sent").headers.get("location")).toBe("/signin?notice=link-sent");
    expect(appRedirect("/a/x", 303).status).toBe(303);
    expect(appRedirect("//evil.example/path").headers.get("location")).toBe("/");
    expect(appRedirect("https://evil.example").headers.get("location")).toBe("/");
  });
});

import path from "node:path";
import type { NextConfig } from "next";
import pkg from "./package.json";
import { MAX_LIBRARY_UPLOAD_REQUEST_BYTES } from "./src/lib/library-upload-contract";

const hostWebDev = process.env.OPENNEKO_HOST_WEB_DEV === "1";
const demoMode =
  process.env.OPENNEKO_STACK_MODE === "demo" ||
  process.env.NEXT_PUBLIC_DEMO === "true" ||
  process.env.DEMO === "true";

if (hostWebDev && (process.env.NODE_ENV !== "development" || demoMode)) {
  throw new Error(
    "OPENNEKO_HOST_WEB_DEV is development-only; production and demo modes must run web inside the stack.",
  );
}

const nextConfig: NextConfig = {
  // Visual fixtures must not contend with the running workspace's dev server.
  distDir: process.env.NODE_ENV !== "production" && process.env.OPENNEKO_RECORDS_VISUAL_TEST === "true"
    ? ".next-visual"
    : ".next",
  env: {
    NEXT_PUBLIC_APP_VERSION: pkg.version,
  },
  transpilePackages: [
    "@neko/avatars",
    "@neko/db",
    "@neko/llm",
    "@neko/telemetry",
    "@neko/secret-crypt",
    "@open-neko/plugin-install",
  ],
  // Emit a self-contained build at .next/standalone for container deploys
  // (Cloud Run). Tracing root is the monorepo root so workspace deps
  // (@neko/db, @neko/llm) get included.
  output: "standalone",
  outputFileTracingRoot: path.join(__dirname, "../.."),
  // Answer tools run model-written HTML in a sandboxed frame. A frame may only
  // load from this origin, so a tool cannot navigate itself to another site.
  async headers() {
    return [{ source: "/:path*", headers: [{ key: "Content-Security-Policy", value: "frame-src 'self'" }] }];
  },
  experimental: {
    // Turbopack's FS cache (default-on since Next 16.1) was serving stale
    // globals.css after edits in dev. Disabling for dev only.
    turbopackFileSystemCacheForDev: false,
    // Proxy clones request bodies before API routes read them. Next defaults
    // that clone to 10 MB, which would truncate valid Library imports before
    // the route can enforce its own 100 MB aggregate file allowance.
    proxyClientMaxBodySize: MAX_LIBRARY_UPLOAD_REQUEST_BYTES,
  },
};

export default nextConfig;

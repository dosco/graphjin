import { describe, expect, it } from "vitest";
import nextConfig from "../next.config";
import { MAX_LIBRARY_UPLOAD_REQUEST_BYTES } from "@/lib/library-upload-contract";

describe("Next request body contract", () => {
  it("preserves the complete Library multipart request through Proxy", () => {
    expect(nextConfig.experimental?.proxyClientMaxBodySize).toBe(
      MAX_LIBRARY_UPLOAD_REQUEST_BYTES,
    );
  });
});

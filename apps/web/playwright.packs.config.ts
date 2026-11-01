import { defineConfig } from "@playwright/test";
import design from "./playwright.design-system.config";

export default defineConfig({
  ...design,
  testMatch: ["custom-packs.spec.ts", "skills-only-packs.spec.ts", "builtin-packs.spec.ts", "personal-connections.spec.ts"],
  timeout: 120_000,
  use: { ...design.use, baseURL: "http://127.0.0.1:3323" },
  webServer: {
    command: "OPENNEKO_MAGENTO_VISUAL_TEST=true NEXT_PUBLIC_VISUAL_TEST=true OPENNEKO_VISUAL_TEST=true pnpm dev --hostname 127.0.0.1 --port 3323",
    url: "http://127.0.0.1:3323/admin/settings/packs",
    timeout: 120_000,
  },
});

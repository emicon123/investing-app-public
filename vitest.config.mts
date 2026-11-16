import { defineConfig } from "vitest/config";

// One runner for the whole npm workspace: the shared packages and each app's zod schemas /
// components. jsdom is on for every file (cheap; the schema tests simply don't touch the DOM).
export default defineConfig({
  // Resolve each app's `@/*` tsconfig path alias, as its vite.config.ts does for the build.
  resolve: { tsconfigPaths: true },
  test: {
    environment: "jsdom",
    include: [
      "packages/common/*/src/**/*.test.{ts,tsx}",
      "apps/*/frontend-web/src/**/*.test.{ts,tsx}",
    ],
    setupFiles: ["./vitest.setup.ts"],
  },
});

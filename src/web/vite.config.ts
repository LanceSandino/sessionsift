/** @module ViteConfig @description Builds and tests the embedded React UI. */
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  root: "src/web",
  plugins: [react()],
  build: {
    outDir: "../webui/dist",
    emptyOutDir: true,
	rollupOptions:{input:{app:"src/web/index.html",hosted:"src/web/hosted.tsx"}},
  },
  test: {
    environment: "jsdom",
    // Whole-app tests render every pane; a loaded machine must not turn slowness into failures.
    testTimeout: 20000,
    include: ["**/*.test.{ts,tsx}"],
    coverage: {
      provider: "v8",
      reporter: ["text", "lcov"],
      thresholds: { statements: 85, branches: 70, functions: 85, lines: 85 },
    },
  },
});

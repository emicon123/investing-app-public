import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { tsconfigPaths: true },
  server: { forwardConsole: true, proxy: { "/api": { target: "http://localhost:80", changeOrigin: true } } },
  build: { minify: "oxc" },
});

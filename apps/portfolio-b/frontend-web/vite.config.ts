import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  base: "/portfolio-b/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": "/src" },
  },
  server: {
    proxy: {
      // Nginx (port 80) strips the /portfolio-b prefix itself (`rewrite ^/portfolio-b/api/(.*)$ /api/$1`),
      // so the full /portfolio-b/api/... path is forwarded unchanged — no rewrite here.
      "/portfolio-b/api": {
        target: "http://localhost:80",
        changeOrigin: true,
      },
    },
  },
});

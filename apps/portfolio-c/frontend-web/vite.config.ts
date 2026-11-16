import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  base: "/portfolio-c/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": "/src" },
  },
  server: {
    proxy: {
      // Nginx (port 80) strips the /portfolio-c prefix itself (`rewrite ^/portfolio-c/api/(.*)$ /api/$1`),
      // so the full /portfolio-c/api/... path is forwarded unchanged — no rewrite here.
      "/portfolio-c/api": {
        target: "http://localhost:80",
        changeOrigin: true,
      },
    },
  },
});

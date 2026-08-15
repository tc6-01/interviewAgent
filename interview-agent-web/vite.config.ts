import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "./",
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:9090",
        changeOrigin: true,
      },
      "/healthz": {
        target: "http://localhost:9090",
        changeOrigin: true,
      },
      "/readyz": {
        target: "http://localhost:9090",
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "node",
  },
});

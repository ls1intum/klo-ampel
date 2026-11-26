import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

// In dev, the Go server runs on :8080 and handles auth and the API.
const server = "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    proxy: { "/api": server, "/auth": server },
  },
  test: { environment: "jsdom" },
});

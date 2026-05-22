import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The production build is emitted into the operator's Go package so it can be embedded into the
// binary (go:embed, built with -tags ui) and served by the operator itself — same origin as /api,
// so no reverse proxy is needed. During local development, `vite dev` proxies /api to a
// locally-running operator (ksail operator --api-bind-address=:8080); this dev proxy is not shipped.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../../pkg/operator/ui/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});

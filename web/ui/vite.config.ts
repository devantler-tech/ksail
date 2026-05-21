import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// During local development, proxy API calls to a locally-running operator
// (ksail operator --api-bind-address=:8080). In production the UI is served
// behind an ingress/proxy that routes /api to the operator service.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});

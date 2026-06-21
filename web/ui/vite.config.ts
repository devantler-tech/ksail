import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The production build is emitted into web/ui/dist and staged into pkg/webui/dist (see
// pkg/webui/embed.go) at build time, so it is embedded into the binary (go:embed) and served by the
// operator and `ksail open web`/`ksail open desktop` themselves — same origin as /api, no reverse proxy needed.
// During local development, `vite dev` proxies /api to a locally-running operator
// (ksail operator --api-bind-address=:8080); this dev proxy is not shipped.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});

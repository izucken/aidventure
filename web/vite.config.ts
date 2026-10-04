import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  // The Go binary embeds this directory.
  build: { outDir: "../internal/ui/static/dist", emptyOutDir: true },
  server: { host: true, port: 5173, strictPort: true },
});

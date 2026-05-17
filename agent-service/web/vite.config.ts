import { defineConfig } from "vite";
import tailwind from "@tailwindcss/vite";
// Static SPA built to dist/, served by the agent-service (one port,
// no creds client-side — D1/D7).
export default defineConfig({ plugins: [tailwind()], build: { outDir: "dist" } });

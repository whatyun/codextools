import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";
import { defineConfig } from "vite";

function dependencyChunk(id: string) {
  if (!id.includes("node_modules")) return undefined;
  if (id.includes("/react/") || id.includes("/react-dom/") || id.includes("/scheduler/")) return "react-vendor";
  if (id.includes("/@dnd-kit/")) return "dnd-vendor";
  if (id.includes("/@tauri-apps/")) return "tauri-vendor";
  if (
    id.includes("/lucide-react/")
    || id.includes("/tailwind-merge/")
    || id.includes("/class-variance-authority/")
    || id.includes("/clsx/")
    || id.includes("/@radix-ui/")
  ) {
    return "ui-vendor";
  }
  return "vendor";
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    target: "safari15",
    rollupOptions: {
      output: {
        manualChunks: dependencyChunk,
      },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    host: "127.0.0.1",
    port: 1420,
    strictPort: true,
  },
});

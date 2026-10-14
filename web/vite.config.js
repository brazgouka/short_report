import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev loop: `go run .` (backend on :8787) + `npm run dev` in web/ (frontend
// on :5173, hot reload, proxies /api to the Go backend below).
// Production: `npm run build` writes web/dist, which Go embeds directly.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8787',
    },
  },
});

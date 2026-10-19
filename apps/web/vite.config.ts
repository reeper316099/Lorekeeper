import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';

/** Strict CSP for production builds only (the dev server needs inline scripts for HMR). */
const csp: Plugin = {
  name: 'lorekeeper-csp',
  apply: 'build',
  transformIndexHtml: () => [
    {
      tag: 'meta',
      attrs: {
        'http-equiv': 'Content-Security-Policy',
        content:
          "default-src 'self'; script-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; connect-src 'self' http://127.0.0.1:*; font-src 'self' data:; object-src 'none'; base-uri 'none'",
      },
      injectTo: 'head-prepend',
    },
  ],
};

export default defineConfig({
  plugins: [react(), csp],
  server: { port: 5173, strictPort: true },
  build: { target: 'es2022', sourcemap: true },
});

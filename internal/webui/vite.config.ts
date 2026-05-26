/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'

const apiPaths = [
  '/auth',
  '/me',
  '/quota',
  '/sessions',
  '/tools',
  '/agent',
  '/admin',
  '/v1',
  '/memories',
  '/sandbox',
  '/healthz',
  '/readyz',
]

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  server: {
    port: 5173,
    proxy: Object.fromEntries(
      apiPaths.map((p) => [
        p,
        {
          target: 'http://localhost:8080',
          changeOrigin: false,
          ws: p === '/sessions',
        },
      ]),
    ),
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 600,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    css: true,
    server: {
      deps: {
        inline: ['@xyflow/react'],
      },
    },
  },
})

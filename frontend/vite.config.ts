import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
export default defineConfig({ plugins: [react()], server: { port: 5173, strictPort: true, proxy: { '/api': 'http://127.0.0.1:8080', '/ws': { target: 'ws://127.0.0.1:8080', ws: true } } }, test: { environment: 'jsdom', setupFiles: ['./src/tests/setup.ts'], include: ['src/**/*.test.{ts,tsx}'], globals: true } })

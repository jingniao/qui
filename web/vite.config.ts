import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import path from 'path'
import { mkdirSync, writeFileSync } from 'node:fs'

// https://vitejs.dev/config/
let embedOutputDirectory = path.resolve(__dirname, 'dist')
export default defineConfig({
  plugins: [vue(), {
    name: 'keep-embed-directory',
    configResolved(config) {
      embedOutputDirectory = path.resolve(config.root, config.build.outDir)
    },
    closeBundle() {
      mkdirSync(embedOutputDirectory, { recursive: true })
      writeFileSync(path.join(embedOutputDirectory, '.gitkeep'), '')
    }
  }],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src')
    }
  },
  server: {
    host: '0.0.0.0',
    port: 8080,
    strictPort: true,
    open: false,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8081', changeOrigin: false },
      '/healthz': { target: 'http://127.0.0.1:8081', changeOrigin: false },
      '/sub': { target: 'http://127.0.0.1:8081', changeOrigin: false }
    }
  }
})

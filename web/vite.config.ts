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
    port: 3000,
    open: false,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/healthz': { target: 'http://127.0.0.1:8080', changeOrigin: false },
      '/sub': { target: 'http://127.0.0.1:8080', changeOrigin: false }
    }
  }
})

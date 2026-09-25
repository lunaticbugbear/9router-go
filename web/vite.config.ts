import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:20131',
      '/v1': 'http://localhost:20131',
      '/usage': 'http://localhost:20131',
      '/translator': 'http://localhost:20131',
      '/debug': 'http://localhost:20131',
    },
  },
})

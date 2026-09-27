import { existsSync, readdirSync } from 'node:fs'
import path from 'node:path'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig, type Plugin } from 'vite'

const VIRTUAL_MODULE_ID = 'virtual:provider-icons'
const RESOLVED_VIRTUAL_MODULE_ID = '\0' + VIRTUAL_MODULE_ID
const PNG_EXT = '.png'

/**
 * Serves the basenames of `public/providers/*.png` as a virtual module, so
 * `lib/providerIcons.ts` can tell which provider icons were actually shipped.
 *
 * The directory is read here rather than with `import.meta.glob`: globbing the
 * assets pulls every PNG into the module graph, and the bundler then emits a
 * duplicate base64-inlined chunk per icon (~1.6 MB across 216 files), even
 * though the same PNGs are already copied verbatim to `dist/providers`.
 */
function providerIcons(): Plugin {
  let iconsDir = ''
  const readIconIds = () =>
    !existsSync(iconsDir)
      ? []
      : readdirSync(iconsDir)
          .filter((file) => file.endsWith(PNG_EXT))
          .map((file) => file.slice(0, -PNG_EXT.length))
          .sort()

  return {
    name: 'provider-icons',
    configResolved(config) {
      iconsDir = path.join(config.publicDir, 'providers')
    },
    resolveId(id) {
      if (id === VIRTUAL_MODULE_ID) return RESOLVED_VIRTUAL_MODULE_ID
    },
    load(id) {
      if (id !== RESOLVED_VIRTUAL_MODULE_ID) return
      return `export default ${JSON.stringify(readIconIds())}`
    },
    configureServer(server) {
      server.watcher.add(iconsDir)
      const invalidate = (file: string) => {
        if (!file.endsWith(PNG_EXT) || !file.startsWith(iconsDir)) return
        const mod = server.moduleGraph.getModuleById(RESOLVED_VIRTUAL_MODULE_ID)
        if (mod) server.moduleGraph.invalidateModule(mod)
        server.ws.send({ type: 'full-reload' })
      }
      server.watcher.on('add', invalidate)
      server.watcher.on('unlink', invalidate)
    },
  }
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte(), tailwindcss(), providerIcons()],
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

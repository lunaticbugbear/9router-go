/**
 * Guard against provider-icon 404s.
 *
 * Loads the real catalog and `getIconPath` through Vite's SSR module loader, so
 * `import.meta.glob` resolves exactly as it does in the app, then asserts that
 * every value a caller can pass in (catalog id, or catalog alias) maps to a PNG
 * that actually exists in `web/public/providers`. Exits non-zero otherwise.
 *
 * Usage: node scripts/check-provider-icons.mjs   (from web/)
 */
import { readdirSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const providersDir = path.join(webRoot, 'public', 'providers')

const onDisk = new Set(
  readdirSync(providersDir)
    .filter((f) => f.endsWith('.png'))
    .map((f) => f.slice(0, -'.png'.length))
)

const server = await createServer({
  root: webRoot,
  server: { middlewareMode: true },
  logLevel: 'silent',
})

let missing = 0
const fallbackIds = []
const fallbackAliases = []
const shadowed = []

try {
  const { PROVIDER_CATALOG } = await server.ssrLoadModule('/src/lib/providers.ts')
  const { getIconPath } = await server.ssrLoadModule('/src/components/connections/types.ts')

  const probes = []
  for (const p of PROVIDER_CATALOG) {
    probes.push({ value: p.id, kind: 'id', ownId: p.id })
    if (p.alias && p.alias !== p.id) probes.push({ value: p.alias, kind: 'alias', ownId: p.id })
  }

  for (const { value, kind, ownId } of probes) {
    const src = getIconPath(value)
    const file = src.startsWith('/providers/') && src.endsWith('.png') ? src.slice('/providers/'.length, -'.png'.length) : null

    if (file === null || !onDisk.has(file)) {
      missing++
      console.error(`FAIL ${kind} ${value} -> ${src} (no such file in public/providers)`)
      continue
    }
    if (file === ownId) continue

    if (onDisk.has(ownId)) {
      // Its own artwork exists but an earlier branch won (anthropic*, openai-compatible*).
      shadowed.push([value, ownId, src])
    } else if (kind === 'id') {
      fallbackIds.push([value, src])
    } else {
      fallbackAliases.push([value, ownId, src])
    }
  }

  console.log(`catalog entries:                ${PROVIDER_CATALOG.length}`)
  console.log(`pngs in public/providers:       ${onDisk.size}`)
  console.log(`ids + aliases checked:          ${probes.length}`)
  console.log(`resolutions with no png (FAIL): ${missing}`)
  console.log('')
  console.log(`catalog ids with no artwork:    ${fallbackIds.length}`)
  for (const [id, src] of [...fallbackIds].sort()) console.log(`  ${id.padEnd(38)} -> ${src}`)
  console.log('')
  console.log(`aliases of those ids:           ${fallbackAliases.length}`)
  for (const [alias, ownId, src] of [...fallbackAliases].sort()) {
    console.log(`  ${alias.padEnd(38)} -> ${src}  (${ownId})`)
  }
  console.log('')
  console.log(`ids shadowed by an earlier branch (pre-existing, not a 404): ${shadowed.length}`)
  for (const [value, ownId, src] of [...shadowed].sort()) {
    console.log(`  ${value.padEnd(38)} -> ${src}  (own artwork ${ownId}.png exists)`)
  }
} finally {
  await server.close()
}

process.exit(missing === 0 ? 0 : 1)

import iconIds from 'virtual:provider-icons'

/**
 * Provider icon availability, derived at build time from `web/public/providers`.
 *
 * The provider catalog (`lib/providers.ts`) is generated from the upstream
 * registry and contains entries that deliberately have no PNG — notably the
 * header-name placeholder providers (`user-agent`, `x-requested-with`, ...)
 * which declare a Material Symbols `icon` such as `dns` instead of artwork.
 * Building `/providers/<id>.png` for those produces a 404 on every render, so
 * callers must check availability first.
 *
 * The list comes from the real directory listing (the `provider-icons` plugin
 * in `vite.config.ts`) rather than a hand-maintained allowlist, so adding or
 * removing a PNG in `web/public/providers/` is picked up automatically and the
 * set can never drift out of sync with the catalog.
 */
export const AVAILABLE_PROVIDER_ICONS: ReadonlySet<string> = new Set(iconIds)

/** Fallback used for unknown ids and for catalog entries without artwork. */
export const DEFAULT_PROVIDER_ICON = '/providers/oai-cc.png'

/** Fallback for OpenAI-compatible providers speaking the Responses API. */
export const RESPONSES_PROVIDER_ICON = '/providers/oai-r.png'

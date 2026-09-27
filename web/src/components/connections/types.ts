import { api, getAuthHeaders, type ProviderConnection } from '../../api/client'
import { getModelCaps, getModelKind } from '../../lib/models'
import { AVAILABLE_PROVIDER_ICONS, DEFAULT_PROVIDER_ICON, RESPONSES_PROVIDER_ICON } from '../../lib/providerIcons'
import { PROVIDER_CATALOG, PROVIDER_CATALOG_MAP } from '../../lib/providers'

export const MEDIA_KINDS: Record<string, true> = {
  image: true,
  tts: true,
  stt: true,
  embedding: true,
  video: true,
}

export function isChatModel(m: unknown): boolean {
  const kind = getModelKind(m)
  if (!kind || kind === 'llm' || kind === 'chat') {
    const obj = typeof m === 'object' && m !== null ? (m as { kind?: string; type?: string }) : null
    return !(obj?.kind && MEDIA_KINDS[obj.kind]) && !(obj?.type && MEDIA_KINDS[obj.type])
  }
  return false
}

export interface ProviderStats {
  total: number
  connected: number
  errorCount: number
  allDisabled: boolean
  latestError?: string | null
  connections: ProviderConnection[]
}

export interface ModelItem {
  id: string
  name?: string
  isCustom?: boolean
  caps: { vision: boolean; reasoning: boolean }
  kind?: string
}

export interface CustomModelData {
  id: string
  name?: string
  providerAlias?: string
  type?: string
  /** Capability flags saved from the Add Custom Model modal (upstream parity). */
  caps?: { vision?: boolean; reasoning?: boolean }
}

/**
 * Resolve the dashboard icon URL for a provider id (or catalog alias).
 *
 * Only returns `/providers/<id>.png` when that PNG is actually shipped in
 * `web/public/providers` — several catalog entries are header-name
 * placeholders with no artwork, and requesting their PNG logs a 404.
 * Unknown ids fall back to the OpenAI-compatible / Responses artwork.
 */
export function getIconPath(id?: string | null, apiType?: string): string {
  const fallback = apiType === 'responses' ? RESPONSES_PROVIDER_ICON : DEFAULT_PROVIDER_ICON
  if (!id) return fallback
  const clean = id.trim()
  if (clean.startsWith('openai-compatible')) {
    return apiType === 'responses' ? RESPONSES_PROVIDER_ICON : DEFAULT_PROVIDER_ICON
  }
  if (clean.startsWith('anthropic-compatible') || clean.includes('anthropic')) {
    return '/providers/anthropic-m.png'
  }
  if (PROVIDER_CATALOG_MAP.has(clean) && AVAILABLE_PROVIDER_ICONS.has(clean)) {
    return `/providers/${clean}.png`
  }
  const byAlias = PROVIDER_CATALOG.find((p) => p.alias === clean)
  if (byAlias && AVAILABLE_PROVIDER_ICONS.has(byAlias.id)) {
    return `/providers/${byAlias.id}.png`
  }
  return fallback
}

export function getProviderStats(
  connections: ProviderConnection[],
  providerId: string,
  authTypes?: string[]
): ProviderStats {
  const list = connections.filter((c) => {
    if (c.provider !== providerId) return false
    if (authTypes && authTypes.length > 0) {
      return authTypes.includes(c.authType)
    }
    return true
  })

  const total = list.length
  const connected = list.filter((c) => c.isActive === 1 && c.testStatus !== 'error' && !c.lastError).length
  const errorCount = list.filter((c) => c.testStatus === 'error' || !!c.lastError).length
  const allDisabled = total > 0 && list.every((c) => c.isActive === 0)
  const latestError = list.find((c) => !!c.lastError)?.lastError

  return { total, connected, errorCount, allDisabled, latestError, connections: list }
}

export function matchesFilter(stats: ProviderStats, statusFilter: string, noAuth?: boolean): boolean {
  if (statusFilter === 'all') return true
  if (statusFilter === 'connected') return stats.connected > 0 || (!!noAuth && stats.total === 0)
  if (statusFilter === 'error') return stats.errorCount > 0
  if (statusFilter === 'disabled') return stats.allDisabled
  if (statusFilter === 'not_connected') return stats.total === 0 && !noAuth
  return true
}

export function matchesSearch(name: string, searchQuery: string): boolean {
  if (!searchQuery.trim()) return true
  return name.toLowerCase().includes(searchQuery.trim().toLowerCase())
}

export async function fetchProviderModelsData(
  providerId: string,
  storageAlias: string
): Promise<{ customModels: CustomModelData[]; disabledModelIds: string[] }> {
  try {
    const [customs, disabled] = await Promise.all([
      api.getCustomModels().catch(() => ({})),
      api.getDisabledModels().catch(() => ({})),
    ])
    let customModels: CustomModelData[] = []
    if (Array.isArray(customs)) {
      customModels = customs
    } else if (customs && typeof customs === 'object' && 'models' in customs && Array.isArray(customs.models)) {
      customModels = customs.models as CustomModelData[]
    } else if (customs && typeof customs === 'object') {
      const rec = customs as Record<string, unknown>
      customModels = Object.entries(rec).map(([k, v]) => {
        if (v && typeof v === 'object') {
          const item = v as Record<string, unknown>
          return { id: k, ...item }
        }
        return { id: k, name: String(v) }
      })
    }
    const disRec = disabled && typeof disabled === 'object' ? (disabled as Record<string, unknown>) : null
    const disArr = (disRec && disRec[storageAlias]) || (disRec && disRec[providerId]) || []
    return {
      customModels,
      disabledModelIds: Array.isArray(disArr) ? disArr : [],
    }
  } catch {
    return { customModels: [], disabledModelIds: [] }
  }
}

export interface SuggestedModel {
  id: string
  name: string
  contextLength?: number
}

// In-memory cache for suggested-model catalogs (upstream parity:
// providerModelsFetcher.js CACHE_TTL_MS).
const suggestedCache = new Map<string, { data: SuggestedModel[]; expiresAt: number }>()
const SUGGESTED_CACHE_TTL_MS = 5 * 60 * 1000

/** Port of fetchSuggestedModels (upstream providerModelsFetcher.js). */
export async function fetchSuggestedModels(fetcher: {
  url: string
  type: string
}): Promise<SuggestedModel[]> {
  if (!fetcher?.url || !fetcher?.type) return []
  const cached = suggestedCache.get(fetcher.url)
  if (cached && Date.now() < cached.expiresAt) return cached.data
  try {
    const params = new URLSearchParams({ url: fetcher.url, type: fetcher.type })
    const res = await fetch(`/api/providers/suggested-models?${params}`, {
      headers: getAuthHeaders(),
    })
    if (!res.ok) return []
    const json = await res.json()
    const data = Array.isArray(json?.data) ? json.data : []
    suggestedCache.set(fetcher.url, { data, expiresAt: Date.now() + SUGGESTED_CACHE_TTL_MS })
    return data
  } catch {
    return []
  }
}

export function buildAvailableModels(
  builtInModels: Array<{ id: string; name?: string; kind?: string; type?: string }>,
  providerCustomModels: CustomModelData[]
): ModelItem[] {
  const list: ModelItem[] = []
  const seen = new Set<string>()

  for (const cm of providerCustomModels) {
    if (!cm.id || seen.has(cm.id)) continue
    seen.add(cm.id)
    list.push({
      id: cm.id,
      name: cm.name || cm.id,
      isCustom: true,
      caps: getModelCaps(cm.id, cm),
      kind: getModelKind(cm),
    })
  }
  for (const bm of builtInModels) {
    if (!bm.id || seen.has(bm.id)) continue
    seen.add(bm.id)
    list.push({
      id: bm.id,
      name: bm.name,
      isCustom: false,
      caps: getModelCaps(bm.id, bm),
      kind: getModelKind(bm),
    })
  }
  return list
}

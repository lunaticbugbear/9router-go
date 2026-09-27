<script lang="ts">
  import { api, getAuthHeaders, type ProviderConnection, type ProviderNode } from '../../api/client'
  import { PROVIDER_CATALOG } from '../../lib/providers'
  import {
    fmt,
    timeAgo,
    PERIODS,
    type MainTab,
    type Period,
    type StatsData,
    type RequestDetailItem,
    type ActiveRequestItem,
    type RecentRequestItem
  } from './types'
  import Button from '../../lib/ui/Button.svelte'
  import SummaryKpiCards from './SummaryKpiCards.svelte'
  import UsageBreakdownTable from './UsageBreakdownTable.svelte'
  import RequestDetailsTab from './RequestDetailsTab.svelte'
  import ProviderTopologyCard from './ProviderTopologyCard.svelte'
  interface Props {
    connections?: ProviderConnection[]
    providerNodes?: ProviderNode[]
  }

  let { connections = [], providerNodes = [] }: Props = $props()

  let activeTab = $state<MainTab>('overview')
  let period = $state<Period>('today')
  let isFetching = $state(false)
  let stats = $state<StatsData>({})
  // Fetch failures were console.error-only, so a 401/500 rendered as a screen of
  // zeros (overview) or as "No request logs found" (details) — i.e. an outage
  // looked like an empty database. Both are now surfaced with a retry.
  let statsError = $state('')
  let detailsError = $state('')
  let activeRequests = $state<ActiveRequestItem[]>([])
  let pulseProvider = $state<string>('')
  let lastProvider = $state<string>('')
  let errorProvider = $state<string>('')
  let pulseTimer: ReturnType<typeof setTimeout> | null = null

  function triggerPulse(provider: string) {
    if (!provider) return
    pulseProvider = provider
    if (pulseTimer) clearTimeout(pulseTimer)
    pulseTimer = setTimeout(() => {
      pulseProvider = ''
    }, 3000)
  }
  // mergeRecent unions an incoming SSE list with what is on screen. The SSE
  // stream carries only this process's in-memory ring, so a plain replace
  // collapses the DB-backed list (20 rows after a REST load) down to the few
  // rows seen since the last restart — the list visibly blinks and rows below
  // vanish. Union + dedupe + newest-first keeps rows the stream has not seen.
  function mergeRecent(
    prev: RecentRequestItem[] | undefined,
    next: RecentRequestItem[] | undefined,
  ): RecentRequestItem[] {
    const byKey = new Map<string, RecentRequestItem>()
    const keyOf = (r: RecentRequestItem) =>
      `${r.model}|${r.provider}|${r.promptTokens}|${r.completionTokens}|${(r.timestamp || '').slice(0, 16)}`
    for (const r of prev || []) byKey.set(keyOf(r), r)
    for (const r of next || []) byKey.set(keyOf(r), r)
    return [...byKey.values()]
      .sort((a, b) => (b.timestamp || '').localeCompare(a.timestamp || ''))
      .slice(0, 20)
  }
  // Request details tab state
  let details = $state<RequestDetailItem[]>([])
  let detailsTotal = $state(0)
  let detailsPage = $state(1)
  let detailsLoading = $state(false)
  async function loadStats(targetPeriod: Period) {
    isFetching = true
    try {
      const res = await api.getUsageStats(targetPeriod)
      if (!res) throw new Error('empty usage response')
      stats = res
      statsError = ''
      if (!lastProvider && Array.isArray(res.recentRequests) && res.recentRequests.length > 0) {
        lastProvider = res.recentRequests[0].provider || ''
      }
    } catch (err) {
      statsError = err instanceof Error ? err.message : String(err)
    } finally {
      isFetching = false
    }
  }

  async function loadDetails(page = 1) {
    detailsLoading = true
    try {
      const limit = 20
      const offset = (page - 1) * limit
      const res = await api.getRequestDetails(limit, offset)
      // A wrong shape used to be swallowed the same way a failure was.
      if (!res || !Array.isArray(res.details)) throw new Error('malformed request-details response')
      details = res.details
      detailsTotal = res.total || 0
      detailsPage = page
      detailsError = ''
    } catch (err) {
      detailsError = err instanceof Error ? err.message : String(err)
    } finally {
      detailsLoading = false
    }
  }

  $effect(() => {
    loadStats(period)
  })

  $effect(() => {
    if (activeTab === 'details') {
      loadDetails(detailsPage)
    }
  })

  // SSE real-time updates for activeRequests, recentRequests and error notifications
  $effect(() => {
    let isCancelled = false
    let controller: AbortController | null = null
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null

    const token = typeof localStorage !== 'undefined' ? localStorage.getItem('9router_key') || '' : ''
    let streamInitialized = false

    const connectStream = async () => {
      if (isCancelled) return
      controller = new AbortController()

      try {
        const streamUrl = token ? `/api/usage/stream?key=${encodeURIComponent(token)}` : '/api/usage/stream'
        const res = await fetch(streamUrl, {
          headers: getAuthHeaders(),
          signal: controller.signal,
        })

        if (!res.ok) {
          throw new Error(`usage stream failed: ${res.status}`)
        }

        const reader = res.body?.getReader()
        const decoder = new TextDecoder()
        if (!reader) return

        let buffer = ''
        while (!isCancelled) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() || ''

          for (const line of lines) {
            const trimmed = line.trim()
            if (!trimmed || trimmed.startsWith(':')) continue
            if (!trimmed.startsWith('data: ')) continue

            try {
              const data = JSON.parse(trimmed.slice(6))
              if (Array.isArray(data.recentRequests) && data.recentRequests.length > 0) {
                const prevTop = stats.recentRequests?.[0]
                const newTop = data.recentRequests[0]
                // Only pulse animation when a GENUINE new model request arrives AFTER stream initialization
                if (streamInitialized && prevTop) {
                  const isNewRequest =
                    newTop.timestamp !== prevTop.timestamp ||
                    newTop.model !== prevTop.model ||
                    newTop.tokens !== prevTop.tokens
                  if (isNewRequest && newTop.provider) {
                    lastProvider = newTop.provider
                    triggerPulse(newTop.provider)
                  }
                } else if (!lastProvider && newTop.provider) {
                  lastProvider = newTop.provider
                }
                streamInitialized = true
                stats = { ...stats, recentRequests: mergeRecent(stats.recentRequests, data.recentRequests) }
              }
              if (Array.isArray(data.activeRequests)) {
                activeRequests = data.activeRequests
                stats = { ...stats, activeRequests: data.activeRequests }
                if (data.activeRequests.length > 0 && data.activeRequests[0].provider) {
                  lastProvider = data.activeRequests[0].provider
                }
              }
              if (data.errorProvider) {
                errorProvider = data.errorProvider
              }
            } catch (err) {
              console.error('Failed to parse SSE usage stream:', err)
            }
          }
        }
      } catch (err) {
        if (!isCancelled) {
          reconnectTimeout = setTimeout(connectStream, 3000)
        }
      }
    }

    connectStream()

    // Auto-poll stats every 5s so KPI counters smoothly increment in real time
    const pollTimer = setInterval(() => {
      if (activeTab === 'overview' && (typeof document === 'undefined' || !document.hidden)) {
        loadStats(period)
      }
    }, 5000)

    return () => {
      isCancelled = true
      if (controller) controller.abort()
      if (reconnectTimeout) clearTimeout(reconnectTimeout)
      if (pulseTimer) clearTimeout(pulseTimer)
      clearInterval(pollTimer)
    }
  })
  let nodeNameById = $derived.by(() => {
    const m = new Map<string, string>()
    for (const n of providerNodes || []) {
      if (n?.id && n?.name) m.set(n.id, n.name)
    }
    return m
  })

  function topologyName(providerId: string, fallbackName?: string): string {
    const nodeName = nodeNameById.get(providerId)
    if (nodeName) return nodeName
    const cat = PROVIDER_CATALOG.find((p) => p.id === providerId || p.alias === providerId)
    if (cat?.name) return cat.name
    if (fallbackName && fallbackName !== providerId) {
      // Numeric key names (e.g. "12") are connection labels, not provider names —
      // fall back to the raw provider id so custom nodes never render as "12".
      if (!/^\d+$/.test(fallbackName.trim())) return fallbackName
      return providerId
    }
    return providerId
  }

  let topologyProviders = $derived.by(() => {
    const seen = new Set<string>()
    const list: { id: string; name: string; color?: string; type: string }[] = []

    for (const c of connections) {
      if (c.isActive !== 0 && c.provider && !seen.has(c.provider)) {
        seen.add(c.provider)
        const cat = PROVIDER_CATALOG.find((p) => p.id === c.provider || p.alias === c.provider)
        list.push({
          id: c.provider,
          name: topologyName(c.provider, c.name || undefined),
          color: cat?.color || '#3B82F6',
          type: 'connection'
        })
      }
    }

    if (stats.byProvider) {
      for (const prov of Object.keys(stats.byProvider)) {
        if (!seen.has(prov)) {
          seen.add(prov)
          const cat = PROVIDER_CATALOG.find((p) => p.id === prov || p.alias === prov)
          list.push({
            id: prov,
            name: topologyName(prov),
            color: cat?.color || '#10B981',
            type: 'active'
          })
        }
      }
    }
    // Always include key free/noAuth providers if not yet listed
    const FREE_DEFAULTS = [
      { id: 'antigravity', name: 'Antigravity', color: '#F59E0B' },
      { id: 'opencode', name: 'OpenCode Free', color: '#3B82F6' },
      { id: 'nvidia', name: 'NVIDIA NIM', color: '#76B900' },
      { id: 'openrouter', name: 'OpenRouter', color: '#6366F1' },
      { id: 'clinepass', name: 'ClinePass', color: '#8B5CF6' }
    ]
    for (const f of FREE_DEFAULTS) {
      if (!seen.has(f.id)) {
        seen.add(f.id)
        list.push({
          id: f.id,
          name: f.name,
          color: f.color,
          type: 'default'
        })
      }
    }

    return list.slice(0, 14)
  })
</script>

<div class="flex min-w-0 flex-col gap-6 px-1 sm:px-0">
  <!-- Page identity: the shell TopBar only carries a one-line breadcrumb. -->
  <div class="flex flex-col gap-1">
    <span class="ui-kicker">II · Observe</span>
    <h1 class="ui-heading text-text-main">Usage &amp; Analytics</h1>
    <p class="max-w-3xl text-sm leading-relaxed text-text-muted">
      Token consumption, per-provider traffic and the request log. Figures come from the gateway's own
      usage store, refreshed live while this tab is open.
    </p>
  </div>

  <!-- Tabs + Period Selector Row -->
  <div class="flex flex-col gap-3 border-b border-border-subtle pb-3 sm:flex-row sm:items-center sm:justify-between">
    <div class="inline-flex rounded-brand border border-border bg-surface p-1" role="tablist" aria-label="Analytics view">
      <button
        type="button"
        role="tab"
        aria-selected={activeTab === 'overview'}
        onclick={() => (activeTab = 'overview')}
        class="cursor-pointer rounded-brand px-4 py-1.5 text-xs font-medium transition-colors sm:text-sm {activeTab === 'overview'
          ? 'bg-primary/12 font-semibold text-primary'
          : 'text-text-muted hover:text-text-main'}"
      >
        Overview
      </button>
      <button
        type="button"
        role="tab"
        aria-selected={activeTab === 'details'}
        onclick={() => (activeTab = 'details')}
        class="cursor-pointer rounded-brand px-4 py-1.5 text-xs font-medium transition-colors sm:text-sm {activeTab === 'details'
          ? 'bg-primary/12 font-semibold text-primary'
          : 'text-text-muted hover:text-text-main'}"
      >
        Details
      </button>
    </div>

    {#if activeTab === 'overview'}
      <div class="flex items-center gap-1.5 self-start sm:self-auto">
        <div class="inline-flex rounded-brand border border-border bg-surface p-1">
          {#each PERIODS as p}
            <button
              type="button"
              onclick={() => (period = p.value)}
              disabled={isFetching}
              class="cursor-pointer rounded-brand px-3 py-1 text-xs font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60 sm:text-sm {period === p.value
                ? 'bg-primary/12 font-semibold text-primary'
                : 'text-text-muted hover:text-text-main'}"
            >
              {p.label}
            </button>
          {/each}
        </div>
        {#if isFetching}
          <span class="size-2 rounded-full bg-primary" aria-hidden="true"></span>
          <span class="sr-only">Refreshing statistics</span>
        {/if}
      </div>
    {/if}
  </div>

  {#if activeTab === 'overview'}
    {#if statsError}
      <div
        class="flex flex-wrap items-center justify-between gap-2 rounded-brand border border-danger/40 bg-danger/10 px-3 py-2 text-xs text-danger"
        role="alert"
      >
        <span class="min-w-0 truncate">Usage stats unavailable — {statsError}</span>
        <Button variant="secondary" size="sm" onclick={() => loadStats(period)}>Retry</Button>
      </div>
    {/if}
    <!-- KPI row -->
    <section class="flex min-w-0 flex-col gap-3">
      <div class="flex flex-wrap items-baseline justify-between gap-2">
        <span class="ui-kicker">Key figures</span>
        <span class="font-code text-[11px] text-text-subtle">period · {PERIODS.find((p) => p.value === period)?.label ?? period}</span>
      </div>
      <SummaryKpiCards {stats} />
    </section>

    <!-- Topology + Recent Requests -->
    <section class="flex min-w-0 flex-col gap-3">
      <span class="ui-kicker">Routing topology &amp; live traffic</span>
      <div class="grid min-w-0 grid-cols-1 items-stretch gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
        <ProviderTopologyCard
          providers={topologyProviders}
          {activeRequests}
          {pulseProvider}
          {lastProvider}
          {errorProvider}
          onRefresh={() => loadStats(period)}
        />
        <!-- Recent Requests Card -->
        <div class="ui-panel flex min-w-0 flex-col overflow-hidden p-4 h-[320px] sm:h-[380px] lg:h-[480px]">
          <div class="flex shrink-0 items-center justify-between gap-2 border-b border-border-subtle px-1 pb-2">
            <span class="ui-kicker">Recent requests</span>
            {#if stats.recentRequests?.length}
              <span class="font-code text-[11px] tabular-nums text-text-subtle">{stats.recentRequests.length} rows</span>
            {/if}
          </div>

          {#if !stats.recentRequests || stats.recentRequests.length === 0}
            <div class="ui-empty m-1 flex flex-1 flex-col items-center justify-center gap-1 text-center">
              <p class="text-xs font-medium text-text-main">No requests recorded yet</p>
              <p class="text-[11px] leading-relaxed">
                Rows appear here as soon as a client sends its first request in this period.
              </p>
            </div>
          {:else}
            <div class="min-h-0 flex-1 overflow-y-auto overflow-x-auto">
              <table class="w-full min-w-[260px] table-fixed border-collapse text-xs">
                <colgroup>
                  <col class="w-[18px]" />
                  <col />
                  <col class="w-[104px]" />
                  <col class="w-[58px]" />
                </colgroup>
                <thead class="sticky top-0 z-10 bg-surface">
                  <tr class="border-b border-border-subtle">
                    <th class="py-2 pl-1" scope="col"><span class="sr-only">Status</span></th>
                    <th class="py-2 text-left font-semibold text-text-muted" scope="col">Model</th>
                    <th class="py-2 text-right font-semibold text-text-muted whitespace-nowrap" scope="col">In / Out</th>
                    <th class="py-2 pr-1 text-right font-semibold text-text-muted whitespace-nowrap" scope="col">When</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-border-subtle font-code text-[11px]">
                  {#each stats.recentRequests as req}
                    <tr class="transition-colors hover:bg-surface-2/60">
                      <td class="py-2 pl-1 align-middle">
                        <span
                          class="block size-1.5 rounded-full {req.status === 'ok' || req.status === 'success' ? 'bg-success' : 'bg-danger'}"
                          title={req.status === 'ok' || req.status === 'success' ? 'Succeeded' : 'Failed'}
                        ></span>
                      </td>
                      <td class="min-w-0 py-2 pr-2">
                        <span class="block truncate" title={req.model}>{req.model}</span>
                      </td>
                      <td class="py-2 pr-3 text-right whitespace-nowrap tabular-nums">
                        <span class="text-primary">{fmt(req.promptTokens)}↑</span>
                        <span class="text-success">{fmt(req.completionTokens)}↓</span>
                      </td>
                      <td class="py-2 pr-1 text-right text-[10px] whitespace-nowrap text-text-subtle">
                        {timeAgo(req.timestamp)}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </div>
      </div>
    </section>

    <!-- Breakdown Table -->
    <section class="flex min-w-0 flex-col gap-3">
      <span class="ui-kicker">Breakdown</span>
      <UsageBreakdownTable {stats} />
    </section>
  {:else}
    <RequestDetailsTab
      {details}
      {detailsTotal}
      {detailsPage}
      {detailsLoading}
      error={detailsError}
      onPageChange={loadDetails}
      onRefresh={() => loadDetails(detailsPage)}
    />
  {/if}
</div>

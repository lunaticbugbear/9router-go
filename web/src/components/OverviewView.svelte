<script lang="ts">
  import { api, type APIKey, type Combo, type ProviderConnection, type ProviderNode, type Settings } from '../api/client'
  import { TAB_ROUTES, type ActiveTab } from '../lib/router'
  import { PROVIDER_CATALOG } from '../lib/providers'
  import { notifications } from '../lib/notifications'
  import Badge from '../lib/ui/Badge.svelte'
  import Button from '../lib/ui/Button.svelte'
  import EmptyState from '../lib/ui/EmptyState.svelte'
  import PageHeader from '../lib/ui/PageHeader.svelte'
  import Section from '../lib/ui/Section.svelte'
  import StatTile from '../lib/ui/StatTile.svelte'
  import StatusDot from '../lib/ui/StatusDot.svelte'
  import { fmt, fmtCost, timeAgo, type StatsData } from './analytics/types'
  import { getIconPath, getProviderStats } from './connections/types'

  let {
    connections = [],
    providerNodes = [],
    combos = [],
    apiKeys = [],
    settings = {},
    navigate,
    onSelectProvider,
    onCreateCombo,
  }: {
    connections?: ProviderConnection[]
    providerNodes?: ProviderNode[]
    combos?: Combo[]
    apiKeys?: APIKey[]
    settings?: Settings
    navigate: (tab: ActiveTab) => void
    onSelectProvider: (id: string) => void
    onCreateCombo: () => void
  } = $props()

  let stats = $state<StatsData | null>(null)
  let statsFailed = $state(false)

  const endpointUrl = typeof window !== 'undefined' ? `${window.location.origin}/v1` : '/v1'
  const compact = new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })

  async function loadStats() {
    try {
      stats = await api.getUsageStats('today')
      statsFailed = false
    } catch {
      statsFailed = true
    }
  }

  $effect(() => {
    loadStats()
    const timer = setInterval(() => {
      if (!document.hidden) loadStats()
    }, 30000)
    return () => clearInterval(timer)
  })

  async function copyEndpoint() {
    try {
      await navigator.clipboard.writeText(endpointUrl)
      notifications.success(endpointUrl, 'Endpoint copied')
    } catch {
      notifications.error('Clipboard access was denied by the browser.', 'Copy failed')
    }
  }

  function addProvider() {
    navigate('connections')
    window.history.replaceState(window.history.state, '', `${TAB_ROUTES.connections}?view=catalog`)
  }

  let providerRows = $derived.by(() => {
    const ids = [...new Set(connections.map((c) => c.provider))]
    return ids
      .map((id) => {
        const node = providerNodes.find((n) => n.id === id)
        const name = node?.name || PROVIDER_CATALOG.find((p) => p.id === id)?.name || id
        const s = getProviderStats(connections, id)
        const tone: 'error' | 'ok' | 'idle' | 'warn' =
          s.errorCount > 0 ? 'error' : s.connected > 0 ? 'ok' : s.allDisabled ? 'idle' : 'warn'
        return { id, name, icon: getIconPath(id, node?.apiType), ...s, tone }
      })
      .sort((a, b) => b.errorCount - a.errorCount || b.connected - a.connected || a.name.localeCompare(b.name))
  })

  let activeCount = $derived(connections.filter((c) => c.isActive === 1).length)
  let failingCount = $derived(providerRows.filter((row) => row.tone === 'error').length)
  let gatewayTone = $derived<'live' | 'warn' | 'idle'>(
    activeCount > 0 ? (failingCount > 0 ? 'warn' : 'live') : 'idle'
  )
  let gatewayLabel = $derived(
    connections.length === 0
      ? 'No providers connected'
      : activeCount === 0
        ? 'All providers disabled'
        : failingCount > 0
          ? `${failingCount} ${failingCount === 1 ? 'provider needs' : 'providers need'} attention`
          : 'Online'
  )

  let totalTokens = $derived((stats?.totalPromptTokens ?? 0) + (stats?.totalCompletionTokens ?? 0))
  let inFlight = $derived(stats?.activeRequests?.reduce((sum, r) => sum + (r.count ?? 1), 0) ?? 0)
  let recent = $derived((stats?.recentRequests ?? []).slice(0, 8))

  function providerLabel(id?: string): string {
    if (!id) return 'Unknown provider'
    return providerNodes.find((node) => node.id === id)?.name || PROVIDER_CATALOG.find((provider) => provider.id === id)?.name || id
  }

  function statusTone(value?: string): 'success' | 'danger' | 'neutral' {
    const v = (value ?? '').toLowerCase()
    if (v === 'ok' || v === 'success' || v.startsWith('2')) return 'success'
    if (v === 'error' || v === 'failed' || v.startsWith('4') || v.startsWith('5')) return 'danger'
    return 'neutral'
  }

  const shortcuts: { label: string; hint: string; icon: string; run: () => void }[] = [
    { label: 'Create combo', hint: 'Chain models with fallback', icon: 'layers', run: () => onCreateCombo() },
    { label: 'Set up a CLI', hint: 'Claude Code, Codex, Cursor…', icon: 'terminal', run: () => navigate('cli-tools') },
    { label: 'Quota', hint: 'Remaining limits per account', icon: 'speed', run: () => navigate('quota') },
    { label: 'Console log', hint: 'Live server output', icon: 'receipt_long', run: () => navigate('console-log') },
  ]
</script>

<PageHeader tab="overview">
  {#snippet actions()}
    <Button variant="secondary" icon="bar_chart" onclick={() => navigate('analytics')}>Usage</Button>
    <Button icon="add" onclick={addProvider}>Add provider</Button>
  {/snippet}
</PageHeader>

<div class="flex flex-col gap-8">
  <!-- Monolith: the one thing every operator needs first -->
  <section class="ui-panel ui-frame grid gap-6 p-6 lg:grid-cols-[minmax(0,1fr)_15rem] lg:items-center lg:gap-8 lg:p-7" aria-label="Gateway">
    <div class="flex min-w-0 flex-col gap-4">
      <div class="flex items-center gap-2.5">
        <StatusDot tone={gatewayTone} />
        <span class="font-code text-[11px] uppercase tracking-[0.16em] text-text-muted">{gatewayLabel}</span>
      </div>

      <div class="min-w-0">
        <p class="ui-kicker text-text-subtle">Base URL</p>
        <div class="mt-2 flex min-w-0 flex-wrap items-center gap-3">
          <code class="min-w-0 break-all font-code text-xl text-text-main lg:text-2xl">{endpointUrl}</code>
          <Button variant="secondary" size="sm" icon="content_copy" onclick={copyEndpoint}>Copy</Button>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        {#if settings.requireApiKey}
          <Badge tone="success" dot>API key required</Badge>
        {:else}
          <Badge tone="warning" dot>Open — no key required</Badge>
        {/if}
        <Button variant="ghost" size="sm" iconRight="arrow_forward" onclick={() => navigate('endpoint')}>
          Endpoint & keys
        </Button>
      </div>
    </div>

    <dl class="grid grid-cols-3 gap-4 border-t border-border-subtle pt-5 lg:grid-cols-1 lg:gap-0 lg:divide-y lg:divide-border-subtle lg:border-l lg:border-t-0 lg:pl-8 lg:pt-0">
      <div class="lg:flex lg:items-baseline lg:justify-between lg:py-2.5">
        <dt class="ui-kicker text-text-subtle">Providers</dt>
        <dd class="ui-stat mt-1 text-2xl text-text-main lg:mt-0 lg:text-xl">
          {activeCount}<span class="text-base text-text-subtle">/{connections.length}</span>
        </dd>
      </div>
      <div class="lg:flex lg:items-baseline lg:justify-between lg:py-2.5">
        <dt class="ui-kicker text-text-subtle">Combos</dt>
        <dd class="ui-stat mt-1 text-2xl text-text-main lg:mt-0 lg:text-xl">{combos.length}</dd>
      </div>
      <div class="lg:flex lg:items-baseline lg:justify-between lg:py-2.5">
        <dt class="ui-kicker text-text-subtle">API keys</dt>
        <dd class="ui-stat mt-1 text-2xl text-text-main lg:mt-0 lg:text-xl">{apiKeys.length}</dd>
      </div>
    </dl>
  </section>

  <!-- Today -->
  <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
    <StatTile label="Requests today" value={stats ? compact.format(stats.totalRequests ?? 0) : '—'} icon="swap_vert" />
    <StatTile
      label="Tokens today"
      value={stats ? compact.format(totalTokens) : '—'}
      hint={stats?.totalCachedTokens ? `${compact.format(stats.totalCachedTokens)} cached` : ''}
      icon="toll"
    />
    <StatTile label="Cost today" value={stats ? fmtCost(stats.totalCost) : '—'} icon="payments" />
    <StatTile
      label="In flight"
      value={stats ? inFlight : '—'}
      tone={inFlight > 0 ? 'signal' : 'default'}
      hint={statsFailed ? 'Usage data unavailable' : ''}
      icon="bolt"
    />
  </div>

  <div class="grid gap-8 lg:grid-cols-3">
    <Section index="I" title="Recent requests" description="Latest calls through the gateway." class="lg:col-span-2">
      {#snippet actions()}
        <Button variant="ghost" size="sm" iconRight="arrow_forward" onclick={() => navigate('analytics')}>
          All usage
        </Button>
      {/snippet}

      {#if recent.length === 0}
        <EmptyState
          compact
          icon="monitoring"
          title="No traffic yet"
          description="Requests appear here as soon as a client calls the endpoint."
        />
      {:else}
        <div class="ui-panel min-w-0 overflow-hidden">
          <table class="w-full table-fixed text-left text-[13px]">
            <thead class="border-b border-brass/40 bg-bg-alt">
              <tr class="font-code text-[10.5px] uppercase tracking-[0.14em] text-text-subtle">
                <th class="w-[5.5rem] px-2 py-2.5 font-semibold sm:px-3">Time</th>
                <th class="px-2 py-2.5 font-semibold sm:px-3">Model</th>
                <th class="w-[4.5rem] px-1 py-2.5 text-right font-semibold sm:w-[6rem] sm:px-3">Tokens</th>
                <th class="w-[4rem] px-2 py-2.5 text-right font-semibold sm:w-[5rem] sm:px-3">Status</th>
              </tr>
            </thead>
            <tbody>
              {#each recent as req, i (i)}
                <tr class="border-b border-border-subtle last:border-0 even:bg-bg-alt/60 hover:shadow-[inset_2px_0_0_var(--app-focus)]">
                  <td class="whitespace-nowrap px-2 py-2.5 font-code text-[11px] text-text-subtle sm:px-3">{timeAgo(req.timestamp)}</td>
                  <td class="min-w-0 px-2 py-2.5 sm:px-3">
                    <span class="block truncate font-code text-[12px] text-text-main" title={req.model || ''}>{req.model || '—'}</span>
                    <span class="block truncate text-[11px] text-text-muted" title={req.provider || ''}>{providerLabel(req.provider)}</span>
                  </td>
                  <td class="ui-stat px-1 py-2.5 text-right text-[11px] text-text-muted sm:px-3 sm:text-[12px]">
                    {fmt((req.promptTokens ?? 0) + (req.completionTokens ?? 0))}
                  </td>
                  <td class="px-2 py-2.5 text-right sm:px-3">
                    <Badge size="sm" tone={statusTone(req.status)}>{req.status || '—'}</Badge>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </Section>

    <Section index="II" title="Provider health" description="Connections per provider.">
      {#if providerRows.length === 0}
        <EmptyState compact icon="dns" title="No providers" description="Add one to start routing." />
      {:else}
        <ul class="ui-panel divide-y divide-border-subtle">
          {#each providerRows.slice(0, 8) as row (row.id)}
            <li>
              <button
                type="button"
                onclick={() => onSelectProvider(row.id)}
                class="flex w-full cursor-pointer items-center gap-3 px-4 py-3 text-left transition-colors duration-150 ease-imperial hover:bg-surface-2"
              >
                <span class="flex size-8 flex-shrink-0 items-center justify-center rounded-brand border border-border-subtle bg-surface-2">
                  <img src={row.icon} alt="" class="size-5 object-contain" />
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-[13px] font-medium text-text-main">{row.name}</span>
                  {#if row.latestError}
                    <span class="block truncate text-[11px] text-danger">{row.latestError}</span>
                  {:else}
                    <span class="block text-[11px] text-text-subtle">
                      {row.allDisabled ? 'Disabled' : `${row.connected} of ${row.total} active`}
                    </span>
                  {/if}
                </span>
                <StatusDot tone={row.tone} label={row.tone} />
              </button>
            </li>
          {/each}
        </ul>
        {#if providerRows.length > 8}
          <Button variant="ghost" size="sm" iconRight="arrow_forward" onclick={() => navigate('connections')}>
            {providerRows.length - 8} more
          </Button>
        {/if}
      {/if}
    </Section>
  </div>

  <Section index="III" title="Shortcuts">
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
      {#each shortcuts as item (item.label)}
        <button
          type="button"
          onclick={item.run}
          class="ui-panel flex cursor-pointer items-center gap-3 p-4 text-left transition-colors duration-150 ease-imperial hover:border-brass/50 hover:bg-surface-2"
        >
          <span class="flex size-10 flex-shrink-0 items-center justify-center rounded-brand border border-border-subtle bg-surface-2 text-primary">
            <span class="material-symbols-outlined text-[20px]" aria-hidden="true">{item.icon}</span>
          </span>
          <span class="min-w-0">
            <span class="block text-[13px] font-semibold text-text-main">{item.label}</span>
            <span class="block text-xs leading-snug text-text-muted">{item.hint}</span>
          </span>
        </button>
      {/each}
    </div>
  </Section>
</div>

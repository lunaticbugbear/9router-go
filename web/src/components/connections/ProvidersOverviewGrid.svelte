<script lang="ts">
  import type { ProviderConnection, ProviderNode } from '../../api/client'
  import Button from '../../lib/ui/Button.svelte'
  import Icon from '../../lib/ui/Icon.svelte'
  import PageHeader from '../../lib/ui/PageHeader.svelte'
  import EmptyState from '../../lib/ui/EmptyState.svelte'
  import StatusDot from '../../lib/ui/StatusDot.svelte'
  import SegmentedControl from '../../lib/ui/SegmentedControl.svelte'
  import { PROVIDER_CATALOG, isChatProvider } from '../../lib/providers'
  import { getIconPath, getProviderStats, matchesFilter, matchesSearch } from './types'

  interface Props {
    connections: ProviderConnection[]
    providerNodes: ProviderNode[]
    onSelectProvider: (id: string) => void
    onToggleAll: (id: string, active: boolean) => void
    onAddAnthropic: () => void
    onAddOpenAI: () => void
  }

  let {
    connections = [],
    providerNodes = [],
    onSelectProvider,
    onToggleAll,
    onAddAnthropic,
    onAddOpenAI,
  }: Props = $props()

  let searchQuery = $state('')
  let statusFilter = $state<'all' | 'connected' | 'error' | 'disabled' | 'not_connected'>('all')
  let catalogCategory = $state<'all' | 'oauth' | 'free' | 'apikey' | 'custom'>('all')
  let visibleCount = $state(30)
  let mode = $state<'connected' | 'catalog'>(
    typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('view') === 'catalog'
      ? 'catalog' : 'connected'
  )

  const categoryOptions = [
    { value: 'all', label: 'All' },
    { value: 'oauth', label: 'Sign-in' },
    { value: 'free', label: 'Free' },
    { value: 'apikey', label: 'API key' },
    { value: 'custom', label: 'Custom' },
  ] as const

  let connectedProviders = $derived.by(() => {
    const ids = new Set(connections.map((connection) => connection.provider))
    return [...ids].map((id) => {
      const node = providerNodes.find((item) => item.id === id)
      const catalog = PROVIDER_CATALOG.find((item) => item.id === id)
      return { id, name: node?.name || catalog?.name || id, stats: getProviderStats(connections, id) }
    }).sort((first, second) => second.stats.errorCount - first.stats.errorCount || first.name.localeCompare(second.name))
  })

  // 1. Custom Providers
  let customNodes = $derived(
    providerNodes
      .filter((n) => matchesSearch(n.name, searchQuery) && matchesFilter(getProviderStats(connections, n.id), statusFilter))
      .map((n) => ({ ...n, stats: getProviderStats(connections, n.id) }))
  )

  // 2. OAuth Providers
  let oauthProviders = $derived(
    PROVIDER_CATALOG
      .filter((p) => isChatProvider(p) && !p.hidden && p.category === 'oauth' && matchesSearch(p.name, searchQuery) && matchesFilter(getProviderStats(connections, p.id, ['oauth']), statusFilter))
      .map((p) => ({ ...p, stats: getProviderStats(connections, p.id, ['oauth']) }))
      .sort((a, b) => (b.stats.connected > 0 ? 1 : 0) - (a.stats.connected > 0 ? 1 : 0) || a.name.localeCompare(b.name))
  )

  // 3. Free Tier Providers
  let freeTierProviders = $derived(
    PROVIDER_CATALOG
      .filter((p) => isChatProvider(p) && !p.hidden && (p.category === 'free' || p.category === 'freeTier') && matchesSearch(p.name, searchQuery) && matchesFilter(getProviderStats(connections, p.id), statusFilter, p.noAuth))
      .map((p) => ({ ...p, stats: getProviderStats(connections, p.id) }))
      .sort((a, b) => (b.stats.connected > 0 || b.noAuth ? 1 : 0) - (a.stats.connected > 0 || a.noAuth ? 1 : 0) || a.name.localeCompare(b.name))
  )

  // 4. API Key Providers
  let apikeyProviders = $derived(
    PROVIDER_CATALOG
      .filter((p) => isChatProvider(p) && !p.hidden && (p.category === 'apikey' || p.category === 'webCookie') && matchesSearch(p.name, searchQuery) && matchesFilter(getProviderStats(connections, p.id, ['apikey', 'api_key']), statusFilter))
      .map((p) => ({ ...p, stats: getProviderStats(connections, p.id, ['apikey', 'api_key']) }))
      .sort((a, b) => (b.stats.connected > 0 ? 1 : 0) - (a.stats.connected > 0 ? 1 : 0) || a.name.localeCompare(b.name))
  )

  let directoryRows = $derived([
    ...customNodes.map((node) => ({ id: node.id, name: node.name, stats: node.stats, category: 'custom' as const, apiType: node.apiType })),
    ...oauthProviders.map((provider) => ({ id: provider.id, name: provider.name, stats: provider.stats, category: 'oauth' as const, apiType: undefined })),
    ...freeTierProviders.map((provider) => ({ id: provider.id, name: provider.name, stats: provider.stats, category: 'free' as const, apiType: undefined })),
    ...apikeyProviders.map((provider) => ({ id: provider.id, name: provider.name, stats: provider.stats, category: 'apikey' as const, apiType: undefined })),
  ].filter((provider) => catalogCategory === 'all' || provider.category === catalogCategory))

  let visibleRows = $derived(
    searchQuery.trim() || statusFilter !== 'all' ? directoryRows : directoryRows.slice(0, visibleCount)
  )

  // The catalog sections disappear when they have no match, which used to leave
  // a blank page. Track the filter state so the view can explain itself instead.
  let matchCount = $derived(directoryRows.length)
  let catalogCount = $derived(
    PROVIDER_CATALOG.filter((p) => isChatProvider(p) && !p.hidden).length
  )
  let filterSummary = $derived(
    [
      searchQuery.trim() ? `“${searchQuery.trim()}”` : '',
      statusFilter !== 'all' ? statusFilter.replace('_', ' ') : '',
    ]
      .filter(Boolean)
      .join(' · ')
  )

  function clearFilters() {
    searchQuery = ''
    statusFilter = 'all'
    catalogCategory = 'all'
    visibleCount = 30
  }

  function switchMode(next: 'connected' | 'catalog') {
    mode = next
    const url = new URL(window.location.href)
    if (next === 'catalog') url.searchParams.set('view', 'catalog')
    else url.searchParams.delete('view')
    window.history.replaceState(window.history.state, '', url)
  }
</script>

<div class="flex flex-col gap-8">
  <PageHeader tab="connections">
    {#snippet actions()}
      {#if mode === 'catalog'}
        <Button variant="secondary" icon="arrow_back" onclick={() => switchMode('connected')}>Connected</Button>
      {:else}
        <Button icon="add" onclick={() => switchMode('catalog')}>Add provider</Button>
      {/if}
    {/snippet}
  </PageHeader>

  {#if mode === 'connected'}
    <section class="flex flex-col gap-4" aria-label="Connected providers">
      <div class="flex flex-wrap items-end justify-between gap-2 border-b border-border-subtle pb-3">
        <div>
          <p class="ui-kicker">I · Active routes</p>
          <h2 class="mt-1 text-[16px] font-semibold text-text-main">Connected providers</h2>
        </div>
        <span class="font-code text-xs text-text-muted">{connectedProviders.length} configured</span>
      </div>
      {#if connectedProviders.length === 0}
        <EmptyState icon="dns" title="No providers yet" description="Connect an account or compatible endpoint to start routing requests.">
          {#snippet action()}
            <Button icon="add" onclick={() => switchMode('catalog')}>Browse providers</Button>
          {/snippet}
        </EmptyState>
      {:else}
        <ul class="divide-y divide-border-subtle border-y border-border-subtle">
          {#each connectedProviders as provider (provider.id)}
            <li>
              <button type="button" onclick={() => onSelectProvider(provider.id)}
                class="flex min-h-16 w-full cursor-pointer items-center gap-4 px-3 py-3 text-left transition-colors hover:bg-surface-2 focus-visible:bg-surface-2">
                <img src={getIconPath(provider.id)} alt="" class="size-8 shrink-0 rounded-brand bg-surface-2 object-contain p-1" />
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm font-semibold text-text-main">{provider.name}</span>
                  <span class="block truncate font-code text-[11px] text-text-muted">
                    {provider.stats.latestError || `${provider.stats.connected} of ${provider.stats.total} connections active`}
                  </span>
                </span>
                <StatusDot tone={provider.stats.errorCount > 0 ? 'error' : provider.stats.allDisabled ? 'idle' : provider.stats.connected > 0 ? 'ok' : 'warn'}
                  label={provider.stats.errorCount > 0 ? 'Error' : provider.stats.allDisabled ? 'Disabled' : provider.stats.connected > 0 ? 'Connected' : 'Not connected'} />
                <span class="hidden font-code text-xs text-text-subtle sm:block">{provider.stats.total} {provider.stats.total === 1 ? 'account' : 'accounts'}</span>
                <Icon name="arrow-right" class="text-text-subtle" />
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else}
  <div class="flex items-center gap-2 border-b border-border-subtle pb-3">
    <Icon name="search" class="text-brass" />
    <div>
      <h2 class="text-[16px] font-semibold text-text-main">Provider catalog</h2>
      <p class="text-xs text-text-muted">Choose an account or connect your own endpoint.</p>
    </div>
  </div>
  <!-- Search / Filter Bar -->
  <div class="flex flex-col gap-3 border-b border-border-subtle pb-4 sm:flex-row sm:items-end sm:justify-between">
    <div class="flex min-w-0 flex-1 flex-col gap-1.5">
      <label for="provider-search" class="ui-kicker">Find a provider</label>
      <div class="relative w-full max-w-md">
        <Icon name="search" size={16} class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-text-subtle" />
        <input
          id="provider-search"
          type="search"
          bind:value={searchQuery}
          placeholder="Search by provider name…"
          class="ui-input w-full pl-9 pr-3 text-xs"
        />
      </div>
    </div>

    <div class="flex shrink-0 flex-col gap-1.5">
      <label for="provider-status" class="ui-kicker">Status</label>
      <select id="provider-status" bind:value={statusFilter} class="ui-input cursor-pointer text-xs">
        <option value="all">All statuses</option>
        <option value="connected">Connected</option>
        <option value="error">Error</option>
        <option value="disabled">Disabled</option>
        <option value="not_connected">Not connected</option>
      </select>
    </div>
  </div>

  <div class="flex flex-wrap items-center justify-between gap-3">
    <SegmentedControl label="Connection type" options={categoryOptions} bind:value={catalogCategory} />
    <span class="font-code text-xs text-text-subtle" role="status">{matchCount} of {catalogCount + providerNodes.length} providers</span>
  </div>

  <div class="flex flex-wrap items-center gap-2 border-b border-border-subtle pb-4">
    <span class="mr-auto text-xs text-text-muted">Your own endpoint?</span>
    <Button size="sm" variant="secondary" icon="add" onclick={onAddOpenAI}>OpenAI compatible</Button>
    <Button size="sm" variant="secondary" icon="add" onclick={onAddAnthropic}>Anthropic compatible</Button>
  </div>

  {#if matchCount === 0}
    <EmptyState compact icon="search" title="No matching providers" description={`Nothing matches ${filterSummary || 'this category'}.`}>
      {#snippet action()}
        <Button variant="secondary" size="sm" onclick={clearFilters}>Clear filters</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <ul class="divide-y divide-border-subtle border-y border-border-subtle">
      {#each visibleRows as provider (provider.id)}
        <li class="flex min-h-14 items-center gap-2 transition-colors hover:bg-surface-2">
          <button type="button" onclick={() => onSelectProvider(provider.id)}
            class="flex min-w-0 flex-1 cursor-pointer items-center gap-3 px-3 py-2.5 text-left">
            <img src={getIconPath(provider.id, provider.apiType)} alt="" class="size-8 shrink-0 rounded-brand bg-surface-2 object-contain p-1" />
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-medium text-text-main">{provider.name}</span>
              <span class="block font-code text-[11px] text-text-subtle">
                {provider.category === 'oauth' ? 'Sign in' : provider.category === 'free' ? 'Free tier' : provider.category === 'custom' ? 'Custom endpoint' : 'API key'}
              </span>
            </span>
            {#if provider.stats.total > 0}
              <span class="hidden items-center gap-2 font-code text-xs text-text-muted sm:flex">
                <StatusDot tone={provider.stats.errorCount > 0 ? 'error' : provider.stats.connected > 0 ? 'ok' : 'idle'} />
                {provider.stats.connected}/{provider.stats.total}
              </span>
            {/if}
            <Icon name="arrow-right" class="text-text-subtle" />
          </button>
          {#if provider.stats.total > 0}
            <button type="button" onclick={() => onToggleAll(provider.id, provider.stats.allDisabled)}
              title={provider.stats.allDisabled ? `Enable ${provider.name}` : `Disable ${provider.name}`}
              aria-label={provider.stats.allDisabled ? `Enable ${provider.name}` : `Disable ${provider.name}`}
              class="mr-2 flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-brand text-text-muted hover:bg-surface-3 hover:text-text-main">
              <Icon name={provider.stats.allDisabled ? 'power' : 'power-off'} size={16} />
            </button>
          {/if}
        </li>
      {/each}
    </ul>
    {#if visibleRows.length < matchCount}
      <Button variant="secondary" onclick={() => (visibleCount += 30)}>Show more ({matchCount - visibleRows.length})</Button>
    {/if}
  {/if}
  {/if}
</div>

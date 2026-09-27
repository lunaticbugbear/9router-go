<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type APIKey, type Combo, type ProviderConnection, type ProviderNode, type Settings } from '../../api/client'
  import { getProvidersByKind, type ProviderCatalogItem } from '../../lib/providers'
  import { notifications } from '../../lib/notifications'
  import Badge from '../../lib/ui/Badge.svelte'
  import Card from '../../lib/ui/Card.svelte'
  import AddCompatibleNodeModal from '../connections/AddCompatibleNodeModal.svelte'
  import MediaProviderCard from './MediaProviderCard.svelte'
  import MediaProviderDetail from './MediaProviderDetail.svelte'
  import { MEDIA_KIND_INFO, type MediaKind } from './mediaTypes'

  interface Props {
    kind: MediaKind
    connections?: ProviderConnection[]
    apiKeys?: APIKey[]
    settings?: Settings
    combos?: Combo[]
    onRefresh: () => void
    onSelectProvider?: (kind: string, id: string) => void
    initialProviderId?: string | null
  }

  let {
    kind,
    connections = [],
    apiKeys = [],
    settings = {},
    combos = [],
    onRefresh,
    onSelectProvider,
    initialProviderId = null,
  }: Props = $props()

  let selectedProvider = $state<ProviderCatalogItem | null>(null)
  let customNodes = $state<ProviderNode[]>([])
  let showCustomModal = $state(false)
  // Provider ids with an in-flight multi-connection toggle.
  let togglingProvider = $state<string | null>(null)
  // Outcome of the last multi-connection provider toggle. Kept on screen so a
  // partial failure is still visible after the toast auto-dismisses.
  let toggleOutcome = $state<{
    providerId: string
    providerName: string
    action: 'enabled' | 'disabled'
    updated: string[]
    failed: string[]
    errors: string[]
  } | null>(null)

  const COMBO_KINDS = new Set<string>([])
  const COMBO_BASE_NAMES: Record<string, string> = { image: 'image-combo', tts: 'tts-combo' }

  let kindConfig = $derived(MEDIA_KIND_INFO[kind])
  let isEmbedding = $derived(kind === 'embedding')
  let supportsCombo = $derived(COMBO_KINDS.has(kind))
  let kindCombos = $derived(combos.filter((c) => (c as any).kind === kind))

  let providers = $derived(getProvidersByKind(kind))

  $effect(() => {
    if (initialProviderId && providers.length > 0) {
      const match = providers.find((p) => p.id === initialProviderId)
      if (match) selectedProvider = match
    }
  })

  onMount(() => {
    if (isEmbedding) {
      api.getProviderNodes().then((nodes) => {
        customNodes = (nodes || []).filter((n) => n.type === 'custom-embedding')
      }).catch(() => {})
    }
  })

  let customProviders = $derived(
    customNodes.map((n) => ({
      id: n.id,
      name: n.name || 'Custom Embedding',
      category: 'custom' as const,
      alias: n.prefix || n.id,
      color: '#6366F1',
      icon: 'data_array',
      serviceKinds: ['embedding'],
      noAuth: true,
    } as ProviderCatalogItem))
  )

  let allProviders = $derived([...providers, ...customProviders])

  function connectionLabel(c: ProviderConnection): string {
    return c.name?.trim() || c.email?.trim() || c.displayName?.trim() || c.id
  }

  async function handleToggleProvider(providerId: string, newActive: boolean) {
    if (togglingProvider) return
    const list = connections.filter((c) => c.provider === providerId)
    if (list.length === 0) return

    const providerName = providers.find((p) => p.id === providerId)?.name || providerId
    togglingProvider = providerId
    toggleOutcome = null

    try {
      const results = await Promise.allSettled(
        list.map((c) => api.updateConnection(c.id, { isActive: newActive ? 1 : 0 }))
      )

      const updated: string[] = []
      const failed: string[] = []
      const errors: string[] = []
      results.forEach((result, index) => {
        const conn = list[index]
        if (result.status === 'fulfilled') {
          updated.push(connectionLabel(conn))
        } else {
          failed.push(connectionLabel(conn))
          errors.push(
            result.reason instanceof Error ? result.reason.message : String(result.reason)
          )
        }
      })

      toggleOutcome = {
        providerId,
        providerName,
        action: newActive ? 'enabled' : 'disabled',
        updated,
        failed,
        errors: [...new Set(errors)],
      }

      const verb = newActive ? 'enabled' : 'disabled'
      if (failed.length === 0) {
        notifications.success(
          `All ${updated.length} ${providerName} ${updated.length === 1 ? 'connection' : 'connections'} ${verb}.`
        )
      } else if (updated.length === 0) {
        notifications.error(
          `Could not ${newActive ? 'enable' : 'disable'} any of ${list.length} ${providerName} connections.`,
          'Provider update failed'
        )
      } else {
        notifications.warning(
          `${updated.length} of ${list.length} ${providerName} connections ${verb}; ${failed.length} failed.`,
          'Partial provider update'
        )
      }
    } catch (error) {
      // Defensive: Promise.allSettled never rejects, but a throw here must not
      // be reported as success.
      const message = error instanceof Error ? error.message : String(error)
      toggleOutcome = {
        providerId,
        providerName,
        action: newActive ? 'enabled' : 'disabled',
        updated: [],
        failed: list.map((c) => connectionLabel(c)),
        errors: [message],
      }
      notifications.error(message, 'Provider update failed')
    } finally {
      // Always reconcile against the server: after a partial write local state
      // is not trustworthy.
      onRefresh()
      togglingProvider = null
    }
  }

  function dismissToggleOutcome() {
    toggleOutcome = null
  }

  async function handleCreateCombo() {
    const base = COMBO_BASE_NAMES[kind] || `${kind}-combo`
    let name = base
    let i = 1
    const existing = new Set(combos.map((c) => c.name))
    while (existing.has(name)) {
      name = `${base}-${i++}`
    }
    try {
      await api.createCombo({ name, models: [], kind })
      onRefresh()
    } catch (err: any) {
      alert(err.message || 'Failed to create combo')
    }
  }

  async function handleCreateCustomNode(data: { name: string; prefix: string; baseUrl: string; type: string }) {
    const node = await api.createProviderNode(data)
    customNodes = [...customNodes, node]
    showCustomModal = false
    onRefresh()
  }
</script>

{#if selectedProvider}
  <MediaProviderDetail
    provider={selectedProvider}
    {kind}
    {connections}
    {apiKeys}
    {settings}
    onBack={() => (selectedProvider = null)}
    {onRefresh}
  />
{:else}
  <div class="flex flex-col gap-6 animate-fade-in">
    <!-- Multi-connection toggle outcome: survives the toast so a partial
         failure is never mistaken for a clean success. -->
    {#if toggleOutcome}
      {@const hasFailures = toggleOutcome.failed.length > 0}
      <div
        role={hasFailures ? 'alert' : 'status'}
        class="flex items-start gap-2 rounded-xl border px-3 py-2 text-xs {hasFailures
          ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
          : 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'}"
      >
        <span class="material-symbols-outlined shrink-0 text-[16px]">
          {hasFailures ? 'warning' : 'check_circle'}
        </span>
        <div class="min-w-0 flex-1">
          <p>
            <strong>{toggleOutcome.providerName}</strong>:
            {toggleOutcome.updated.length} of
            {toggleOutcome.updated.length + toggleOutcome.failed.length} connections
            {toggleOutcome.action}.
            {#if hasFailures}
              <strong>{toggleOutcome.failed.length}</strong> failed.
            {/if}
          </p>
          {#if toggleOutcome.updated.length > 0}
            <p class="mt-1 opacity-90">Updated: {toggleOutcome.updated.join(', ')}</p>
          {/if}
          {#if hasFailures}
            <p class="mt-1 opacity-90">Failed: {toggleOutcome.failed.join(', ')}</p>
            {#if toggleOutcome.errors.length > 0}
              <p class="mt-1 opacity-90">Reason: {toggleOutcome.errors.join('; ')}</p>
            {/if}
          {/if}
        </div>
        <button
          type="button"
          onclick={dismissToggleOutcome}
          class="shrink-0 text-current opacity-70 transition-opacity hover:opacity-100"
          title="Dismiss"
          aria-label="Dismiss provider update result"
        >
          <span class="material-symbols-outlined text-[16px]">close</span>
        </button>
      </div>
    {/if}

    {#if isEmbedding || supportsCombo}
      <div class="flex items-center justify-end gap-2">
        {#if supportsCombo}
          <button
            type="button"
            onclick={handleCreateCombo}
            class="inline-flex items-center gap-1.5 h-8 px-3 rounded-lg bg-surface border border-border hover:bg-surface-2 text-text-main text-xs font-medium transition-colors cursor-pointer shadow-sm"
          >
            <span class="material-symbols-outlined text-sm">add</span>
            Create Combo
          </button>
        {/if}
        {#if isEmbedding}
          <button
            type="button"
            onclick={() => (showCustomModal = true)}
            class="inline-flex items-center gap-1.5 h-8 px-3 rounded-lg bg-primary hover:bg-primary-hover text-white text-xs font-semibold transition-colors cursor-pointer shadow-sm"
          >
            <span class="material-symbols-outlined text-sm">add</span>
            Add Custom Embedding
          </button>
        {/if}
      </div>
    {/if}

    {#if supportsCombo && kindCombos.length > 0}
      <div class="flex flex-col gap-2">
        <h2 class="text-xs font-semibold text-text-muted uppercase tracking-wider">Combos</h2>
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {#each kindCombos as combo (combo.id)}
            <a href={`/dashboard/media-providers/combo/${combo.id}`}>
              <Card padding="xs" class="h-full hover:bg-black/[0.01] dark:hover:bg-white/[0.01] transition-colors cursor-pointer">
                <div class="flex items-center gap-3">
                  <div class="size-8 rounded-lg flex items-center justify-center shrink-0 bg-primary/10 text-primary">
                    <span class="material-symbols-outlined text-lg">alt_route</span>
                  </div>
                  <div class="min-w-0 flex-1">
                    <h3 class="font-semibold text-sm truncate">{combo.name}</h3>
                    <div class="flex items-center gap-2 mt-0.5">
                      <Badge variant="default" size="sm">{combo.models?.length ?? 0} models</Badge>
                    </div>
                  </div>
                </div>
              </Card>
            </a>
          {/each}
        </div>
      </div>
    {/if}

    {#if allProviders.length === 0}
      <div class="text-center py-12 border border-dashed border-border rounded-xl text-text-muted text-sm">
        No providers support <strong>{kindConfig?.title || kind}</strong> yet.
      </div>
    {:else}
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        {#each providers as provider (provider.id)}
          <MediaProviderCard
            {provider}
            {kind}
            {connections}
            busy={togglingProvider === provider.id}
            onToggle={handleToggleProvider}
            onSelect={() => {
              if (onSelectProvider) {
                onSelectProvider(kind, provider.id)
              } else {
                selectedProvider = provider
              }
            }}
          />
        {/each}
        {#each customProviders as provider (provider.id)}
          <MediaProviderCard
            {provider}
            {kind}
            {connections}
            isCustom
            busy={togglingProvider === provider.id}
            onToggle={handleToggleProvider}
            onSelect={() => {
              if (onSelectProvider) {
                onSelectProvider(kind, provider.id)
              } else {
                selectedProvider = provider
              }
            }}
          />
        {/each}
      </div>
    {/if}

    {#if isEmbedding && showCustomModal}
      <AddCompatibleNodeModal
        isOpen={showCustomModal}
        nodeType="custom-embedding"
        onClose={() => (showCustomModal = false)}
        onCreated={handleCreateCustomNode}
      />
    {/if}
  </div>
{/if}

<script lang="ts">
  // Feature Flags: one settings surface for every optional capability the
  // gateway can expose. The registry lives server-side and is append-only, so
  // this view renders whatever GET /api/settings/features returns instead of
  // hardcoding a list that could drift from the gateway's actual switches.
  import { onMount } from 'svelte'
  import { CircleAlert, Loader2, RotateCcw } from 'lucide-svelte'
  import { api, type FeatureFlag } from '../api/client'
  import Badge from '../lib/ui/Badge.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import ConfirmModal from '../lib/ui/ConfirmModal.svelte'

  let flags = $state<FeatureFlag[]>([])
  let categories = $state<string[]>([])
  let stableCount = $state(0)
  let plannedCount = $state(0)
  let loading = $state(true)
  let error = $state('')
  let notice = $state('')
  // Per-row in-flight marker, keyed by flag id. A row is only marked while its
  // own PUT is outstanding, so a slow write cannot make an unrelated row look
  // busy.
  let saving = $state<Record<string, boolean>>({})
  let resetting = $state(false)
  let confirmResetOpen = $state(false)

  // A flag is "planned" when the registry says the capability is not built yet;
  // its toggle is rendered but inert. Anything else is treated as wired, and the
  // raw stage string is shown as the badge so an unrecognized future stage is
  // reported verbatim rather than mislabeled as stable.
  const isPlanned = (f: FeatureFlag) => f.stage === 'planned'

  const chosenCount = $derived(flags.filter((f) => f.chosen).length)

  // Sections follow the API's category order; a flag whose category is missing
  // from that list still gets rendered, in a trailing section, because dropping
  // a switch the gateway honors would be worse than an untidy heading.
  const sections = $derived.by(() => {
    const grouped = categories.map((name) => ({
      name,
      flags: flags.filter((f) => f.category === name),
    }))
    const known = new Set(categories)
    const orphans = flags.filter((f) => !known.has(f.category))
    if (orphans.length > 0) grouped.push({ name: 'Other', flags: orphans })
    return grouped.filter((s) => s.flags.length > 0)
  })

  onMount(() => {
    void reload()
  })

  async function reload() {
    loading = true
    error = ''
    try {
      const result = await api.getFeatureFlags()
      flags = result.flags ?? []
      categories = result.categories ?? []
      stableCount = result.stableCount ?? 0
      plannedCount = result.plannedCount ?? 0
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  async function toggleFlag(flag: FeatureFlag, next: boolean) {
    // Planned flags have no behavior behind them yet, so a PUT would store a
    // choice that changes nothing and would then be reported as "chosen".
    // Refuse here rather than writing a misleading preference.
    if (isPlanned(flag) || saving[flag.id]) return
    const previous = flag.on
    // Optimistic: the switch follows the click immediately, and is rolled back
    // below if the gateway rejects the write.
    flag.on = next
    saving[flag.id] = true
    error = ''
    notice = ''
    try {
      const result = await api.setFeatureFlag(flag.id, next)
      // Trust the response over the optimistic guess: the server owns the
      // effective state.
      flag.on = result.on
      flag.chosen = true
    } catch (e) {
      flag.on = previous
      error = e instanceof Error ? e.message : String(e)
    } finally {
      saving[flag.id] = false
    }
  }

  async function resetAll() {
    if (resetting) return
    const cleared = chosenCount
    resetting = true
    error = ''
    notice = ''
    try {
      await api.resetFeatureFlags()
      // Re-fetch rather than applying the DELETE response's flag map: the map
      // carries values only, so it cannot refresh `chosen` and the header
      // counts, and the menu would keep claiming choices that no longer exist.
      await reload()
      notice =
        cleared === 0
          ? 'No stored choices to clear; every flag is already at its default.'
          : `Cleared ${cleared} stored ${cleared === 1 ? 'choice' : 'choices'}; every flag is back to its default.`
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      resetting = false
      confirmResetOpen = false
    }
  }
</script>

<div class="mx-auto max-w-5xl space-y-4 p-4 md:p-6">
  <header>
    <h1 class="text-xl font-semibold">Feature Flags</h1>
    <p class="mt-1 max-w-3xl text-sm text-text-muted">
      Every optional capability the gateway can expose, in one place. A flag marked
      <span class="font-medium text-text-main">stable</span> controls behavior that is wired up today, so the switch takes effect on the next request.
      A flag marked <span class="font-medium text-text-main">planned</span> is registered but not built: it is listed so you can see what is coming, and its switch does nothing until that work lands.
      Turning a flag off does not delete anything and does not change how the request plane behaves for features you never enabled.
    </p>
  </header>

  {#if error}
    <div role="alert" aria-live="assertive" class="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{error}</div>
  {/if}
  {#if notice}
    <div role="status" aria-live="polite" class="rounded border border-emerald-700/40 bg-emerald-950/30 px-3 py-2 text-sm text-emerald-300">{notice}</div>
  {/if}

  {#if loading}
    <div class="space-y-4">
      <div class="h-16 rounded-[14px] bg-surface border border-border-subtle animate-pulse"></div>
      <div class="h-64 rounded-[14px] bg-surface border border-border-subtle animate-pulse"></div>
      <div class="h-40 rounded-[14px] bg-surface border border-border-subtle animate-pulse"></div>
    </div>
  {:else}
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div class="flex flex-wrap items-center gap-2">
        <span class="inline-flex items-center gap-1.5 rounded-full border border-success/25 bg-success/10 px-2.5 py-1 text-xs font-medium text-success">
          {stableCount} live
        </span>
        <span class="inline-flex items-center gap-1.5 rounded-full border border-warning/25 bg-warning/10 px-2.5 py-1 text-xs font-medium text-warning">
          {plannedCount} planned
        </span>
        <span class="text-xs text-text-muted">
          {#if chosenCount === 0}
            No flag has been overridden yet.
          {:else}
            {chosenCount} {chosenCount === 1 ? 'flag' : 'flags'} overridden.
          {/if}
        </span>
      </div>
      <Button variant="outline" onclick={() => (confirmResetOpen = true)} disabled={resetting}>
        {#if resetting}
          <Loader2 class="w-3.5 h-3.5 animate-spin" />
        {:else}
          <RotateCcw class="w-3.5 h-3.5" />
        {/if}
        Reset all to defaults
      </Button>
    </div>

    {#if flags.length === 0}
      {#if error}
        <div class="rounded border border-border bg-surface-2/40 p-3 text-sm text-text-muted">
          The flag list could not be loaded, so this view cannot say which capabilities exist. The error above is the gateway's own message.
        </div>
      {:else}
        <div class="rounded border border-border bg-surface-2/40 p-3 text-sm text-text-muted">
          The gateway reported no feature flags. That means this build has no optional capabilities registered — not that every capability is off.
        </div>
      {/if}
    {:else}
      {#each sections as section (section.name)}
        <Card>
          <h2 class="font-semibold">{section.name}</h2>
          <ul class="mt-3 divide-y divide-border-subtle">
            {#each section.flags as flag (flag.id)}
              {@const planned = isPlanned(flag)}
              {@const busy = saving[flag.id] === true}
              <li class="flex items-start justify-between gap-4 py-3">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span id="flag-title-{flag.id}" class="text-sm font-medium text-text-main">{flag.title}</span>
                    <Badge tone={planned ? 'warning' : 'success'} size="sm">{flag.stage}</Badge>
                    {#if busy}
                      <span class="inline-flex items-center gap-1 text-[11px] text-text-muted" role="status" aria-live="polite">
                        <Loader2 class="w-3 h-3 animate-spin" />
                        Saving…
                      </span>
                    {/if}
                  </div>
                  <p id="flag-desc-{flag.id}" class="mt-1 max-w-2xl text-xs leading-relaxed text-text-muted">{flag.description}</p>
                  {#if planned}
                    <p class="mt-1 text-[11px] text-warning">Not built yet — toggle has no effect.</p>
                  {/if}
                </div>
                <div class="flex shrink-0 flex-col items-end gap-1 pt-0.5">
                  <!-- A plain toggle button (rather than lib/ui/Toggle.svelte)
                       because each switch needs its own id and aria-pressed so a
                       screen reader can name the exact capability it controls. -->
                  <button
                    type="button"
                    id={flag.id}
                    aria-pressed={flag.on}
                    aria-labelledby="flag-title-{flag.id}"
                    aria-describedby="flag-desc-{flag.id}"
                    title={planned ? `${flag.title} is planned; this toggle has no effect yet` : `Turn ${flag.title} ${flag.on ? 'off' : 'on'}`}
                    disabled={planned || busy}
                    onclick={() => toggleFlag(flag, !flag.on)}
                    class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed {flag.on
                      ? 'bg-brand-500'
                      : 'bg-surface-3'}"
                  >
                    <span
                      class="inline-block h-3.5 w-3.5 transform rounded-full bg-white shadow transition-transform {flag.on
                        ? 'translate-x-[18px]'
                        : 'translate-x-[2px]'}"
                    ></span>
                  </button>
                  <span class="text-[11px] text-text-muted">
                    {flag.on ? 'on' : 'off'}
                    {#if !flag.chosen}· default{/if}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        </Card>
      {/each}
    {/if}
  {/if}

  <ConfirmModal
    isOpen={confirmResetOpen}
    title="Reset all feature flags"
    message="This clears every stored choice; every flag returns to its default. Capabilities you turned on and off deliberately will follow their fresh-install default again, so review the switches after resetting."
    confirmText="Reset all"
    loading={resetting}
    onClose={() => {
      if (!resetting) confirmResetOpen = false
    }}
    onConfirm={resetAll}
  />

  {#if error && !loading}
    <div class="flex items-center gap-2 text-xs text-text-muted">
      <CircleAlert class="w-3.5 h-3.5" />
      <span>The list above may be stale. Reload to re-read the gateway's current state.</span>
      <Button variant="ghost" size="sm" onclick={() => reload()}>Reload</Button>
    </div>
  {/if}
</div>

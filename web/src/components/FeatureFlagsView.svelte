<script lang="ts">
  // Feature Flags: one settings surface for every optional capability the
  // gateway can expose. The registry lives server-side and is append-only, so
  // this view renders whatever GET /api/settings/features returns instead of
  // hardcoding a list that could drift from the gateway's actual switches.
  import { onMount } from 'svelte'
  import { Loader2 } from 'lucide-svelte'
  import { api, type FeatureFlag } from '../api/client'
  import Badge from '../lib/ui/Badge.svelte'
  import Button from '../lib/ui/Button.svelte'
  import EmptyState from '../lib/ui/EmptyState.svelte'
  import PageHeader from '../lib/ui/PageHeader.svelte'
  import Section from '../lib/ui/Section.svelte'
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

  // Section eyebrows follow the shell's numbered-section convention (the sidebar
  // uses I / II / III). The registry can grow past twelve categories, so an
  // out-of-range index falls back to the plain number instead of rendering a
  // wrong or empty numeral.
  const ROMAN = ['I', 'II', 'III', 'IV', 'V', 'VI', 'VII', 'VIII', 'IX', 'X', 'XI', 'XII']
  const sectionNumeral = (n: number) => ROMAN[n - 1] ?? String(n)

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

<div class="mx-auto flex w-full min-w-0 max-w-5xl flex-col gap-8">
  <PageHeader tab="feature-flags">
    {#snippet actions()}
      {#if chosenCount > 0}
        <Button variant="secondary" icon="reset" onclick={() => (confirmResetOpen = true)} disabled={resetting} loading={resetting}>
          Reset {chosenCount} {chosenCount === 1 ? 'override' : 'overrides'}
        </Button>
      {/if}
    {/snippet}
  </PageHeader>

  {#if error && flags.length > 0}
    <div role="alert" aria-live="assertive" class="rounded-[4px] border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{error}</div>
  {/if}
  {#if notice}
    <div role="status" aria-live="polite" class="rounded-[4px] border border-success/40 bg-success/10 px-3 py-2 text-sm text-success">{notice}</div>
  {/if}

  {#if loading}
    <div class="flex flex-col gap-6" role="status" aria-live="polite">
      <span class="sr-only">Loading feature flags…</span>
      <div class="ui-panel h-16 animate-pulse" aria-hidden="true"></div>
      <div class="ui-panel h-64 animate-pulse" aria-hidden="true"></div>
      <div class="ui-panel h-40 animate-pulse" aria-hidden="true"></div>
    </div>
  {:else if error && flags.length === 0}
    <div role="alert">
      <EmptyState icon="error" title="Flags unavailable" description={`The gateway returned: ${error}`}>
        {#snippet action()}
          <Button variant="secondary" icon="refresh" onclick={() => reload()}>Retry</Button>
        {/snippet}
      </EmptyState>
    </div>
  {:else if flags.length === 0}
    <EmptyState icon="flags" title="No feature flags" description="This gateway build has no optional capabilities." />
  {:else}
    <div class="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-border-subtle pb-4 font-code text-xs text-text-muted">
      <span><span class="text-success">{stableCount}</span> live</span>
      <span><span class="text-warning">{plannedCount}</span> planned</span>
      <span><span class="text-primary">{chosenCount}</span> overridden</span>
    </div>
    <div class="flex flex-col gap-10">
      {#each sections as section, index (section.name)}
        {@const liveInSection = section.flags.filter((flag) => !isPlanned(flag)).length}
        {@const plannedInSection = section.flags.length - liveInSection}
        <Section title={section.name} index={sectionNumeral(index + 1)}>
          {#snippet actions()}
            <span class="font-code text-[11px] text-text-muted">
              {section.flags.length} flags{plannedInSection > 0 ? ` · ${plannedInSection} planned` : ''}
            </span>
          {/snippet}
          <ul class="flex flex-col divide-y divide-border-subtle">
            {#each section.flags as flag (flag.id)}
              {@const planned = isPlanned(flag)}
              {@const busy = saving[flag.id] === true}
              <li class="flex items-start justify-between gap-4 py-4 first:pt-0">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <span id="flag-title-{flag.id}" class="text-sm font-medium text-text-main">{flag.title}</span>
                    <Badge tone={planned ? 'warning' : 'success'} size="sm">{flag.stage}</Badge>
                    {#if flag.chosen}
                      <Badge tone="primary" size="sm">overridden</Badge>
                    {/if}
                    {#if busy}
                      <span class="inline-flex items-center gap-1 text-[11px] text-text-muted" role="status" aria-live="polite">
                        <Loader2 class="w-3 h-3 animate-spin" />
                        Saving…
                      </span>
                    {/if}
                  </div>
                  <p id="flag-desc-{flag.id}" class="mt-1 max-w-2xl text-xs leading-relaxed text-text-muted">{flag.description}</p>
                  {#if planned}
                    <p class="mt-1 text-[11px] leading-relaxed text-warning">Coming later</p>
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
                    class="relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors duration-150 cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed {flag.on
                      ? 'bg-primary/85'
                      : 'bg-surface-3'}"
                  >
                    <span
                      class="inline-block h-3.5 w-3.5 transform rounded-full bg-bg shadow transition-transform duration-150 {flag.on
                        ? 'translate-x-[18px]'
                        : 'translate-x-[2px]'}"
                    ></span>
                  </button>
                  <span class="font-code text-[11px] text-text-muted">
                    {flag.on ? 'on' : 'off'} · {flag.chosen ? 'overridden' : 'default'}
                  </span>
                </div>
              </li>
            {/each}
          </ul>
        </Section>
      {/each}
    </div>
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

</div>

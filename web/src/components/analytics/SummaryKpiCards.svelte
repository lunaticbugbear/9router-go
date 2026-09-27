<script lang="ts">
  import { fmt, fmtCost, type StatsData } from './types'

  let { stats = {} }: { stats?: StatsData } = $props()

  // Values use one restrained accent each: brass for the primary spend metric,
  // verdigris for output, ochre for the estimated cost. Everything else is bone
  // or muted so the row reads as a hierarchy instead of a colour chart.
  const cards = [
    { label: 'Total requests', value: () => fmt(stats.totalRequests), tone: 'text-text-main' },
    { label: 'Input tokens', value: () => fmt(stats.totalPromptTokens), tone: 'text-primary' },
    { label: 'Cached tokens', value: () => fmt(stats.totalCachedTokens), tone: 'text-text-muted' },
    { label: 'Output tokens', value: () => fmt(stats.totalCompletionTokens), tone: 'text-success' },
    { label: 'Est. cost', value: () => `~${fmtCost(stats.totalCost)}`, tone: 'text-warning' },
  ]
</script>

<div class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-5">
  {#each cards as card (card.label)}
    <div class="ui-panel flex min-w-0 flex-col gap-1 px-4 py-3">
      <span class="ui-kicker">{card.label}</span>
      <span class="ui-stat truncate text-2xl {card.tone}">{card.value()}</span>
      {#if card.label === 'Est. cost'}
        <span class="text-[10px] leading-tight text-text-subtle">Estimated, not actual billing</span>
      {/if}
    </div>
  {/each}
</div>

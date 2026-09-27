<script lang="ts">
  import Badge from '../../lib/ui/Badge.svelte'
  import Card from '../../lib/ui/Card.svelte'
  import { getIconPath } from '../connections/types'
  import {
    fmt,
    fmtCost,
    timeAgo,
    TABLE_OPTIONS,
    type StatsData,
    type TableView,
    type ViewMode,
    type UsageItem
  } from './types'

  let { stats = {} }: { stats?: StatsData } = $props()

  let tableView = $state<TableView>('model')
  let viewMode = $state<ViewMode>('costs')

  interface ProcessedUsageRow extends UsageItem {
    key: string
    totalTokens: number
  }

  let tableData = $derived((): ProcessedUsageRow[] => {
    if (!stats) return []
    let sourceMap: Record<string, UsageItem> = {}
    if (tableView === 'model') sourceMap = stats.byModel || {}
    else if (tableView === 'account') sourceMap = stats.byAccount || {}
    else if (tableView === 'apiKey') sourceMap = stats.byApiKey || {}
    else if (tableView === 'endpoint') sourceMap = stats.byEndpoint || {}

    return Object.entries(sourceMap)
      .map(([key, item]) => {
        const totalTokens = (item.promptTokens || 0) + (item.completionTokens || 0)
        return {
          key,
          ...item,
          totalTokens,
        }
      })
      .sort((a, b) => (b.requests || 0) - (a.requests || 0))
  })
</script>

<div class="flex flex-col gap-3">
  <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
    <!-- View selector dropdown -->
    <select
      bind:value={tableView}
      class="ui-input w-full cursor-pointer text-sm font-semibold sm:w-auto"
    >
      {#each TABLE_OPTIONS as opt}
        <option value={opt.value}>{opt.label}</option>
      {/each}
    </select>

    <!-- Toggle: Costs | Tokens -->
    <div class="inline-flex self-start rounded-brand border border-border bg-surface p-1 sm:self-auto">
      <button
        type="button"
        onclick={() => (viewMode = 'costs')}
        class="cursor-pointer rounded-brand px-3 py-1 text-xs font-semibold transition-colors {viewMode === 'costs'
          ? 'bg-primary/12 text-primary'
          : 'text-text-muted hover:text-text-main'}"
      >
        Costs
      </button>
      <button
        type="button"
        onclick={() => (viewMode = 'tokens')}
        class="cursor-pointer rounded-brand px-3 py-1 text-xs font-semibold transition-colors {viewMode === 'tokens'
          ? 'bg-primary/12 text-primary'
          : 'text-text-muted hover:text-text-main'}"
      >
        Tokens
      </button>
    </div>
  </div>

  <!-- Breakdown Table Card -->
  <Card padding="none" class="overflow-hidden">
    {#if tableData().length === 0}
      <div class="ui-empty m-3 flex flex-col items-center gap-1 text-center">
        <p class="text-sm font-medium text-text-main">No usage recorded for this period</p>
        <p class="text-xs leading-relaxed">Pick a wider period, or send a request to populate this table.</p>
      </div>
    {:else}
      <div class="custom-scrollbar overflow-x-auto">
        <table class="w-full min-w-[520px] border-collapse text-left text-xs">
          <thead class="border-b border-border bg-bg-alt text-[10px] font-semibold uppercase tracking-wider text-text-muted">
            <tr>
              <th class="px-4 py-3" scope="col">
                {tableView === 'model' ? 'Model' : tableView === 'account' ? 'Account' : tableView === 'apiKey' ? 'Key name' : 'Endpoint'}
              </th>
              <th class="px-4 py-3" scope="col">Provider</th>
              <th class="px-4 py-3 text-right" scope="col">Requests</th>
              {#if viewMode === 'costs'}
                <th class="px-4 py-3 text-right" scope="col">Total cost</th>
              {:else}
                <th class="px-4 py-3 text-right" scope="col">In tokens</th>
                <th class="px-4 py-3 text-right" scope="col">Out tokens</th>
                <th class="px-4 py-3 text-right" scope="col">Cached</th>
              {/if}
              <th class="px-4 py-3 text-right" scope="col">Last used</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-border-subtle">
            {#each tableData() as row}
              <tr class="transition-colors hover:bg-surface-2/60">
                <td class="px-4 py-3 font-code text-xs font-medium text-text-main">
                  <div class="flex items-center gap-2">
                    {#if row.provider}
                      <img
                        src={getIconPath(row.provider)}
                        alt=""
                        class="size-4 shrink-0 rounded border border-border-subtle bg-surface-2 object-contain p-0.5"
                        onerror={(e) => {
                          (e.currentTarget as HTMLElement).style.display = 'none'
                        }}
                        loading="lazy"
                      />
                    {/if}
                    <span class="truncate">{row.rawModel || row.accountName || row.keyName || row.endpoint || row.key}</span>
                  </div>
                </td>
                <td class="px-4 py-3">
                  <div class="flex items-center gap-1.5">
                    {#if row.provider}
                      <img
                        src={getIconPath(row.provider)}
                        alt=""
                        class="size-3.5 shrink-0 rounded object-contain"
                        onerror={(e) => {
                          (e.currentTarget as HTMLElement).style.display = 'none'
                        }}
                        loading="lazy"
                      />
                    {/if}
                    <Badge variant="neutral" size="sm">{row.provider || 'unknown'}</Badge>
                  </div>
                </td>
                <td class="px-4 py-3 text-right font-code font-semibold tabular-nums text-text-main">
                  {fmt(row.requests)}
                </td>
                {#if viewMode === 'costs'}
                  <td class="px-4 py-3 text-right font-code font-bold tabular-nums text-warning">
                    {fmtCost(row.cost)}
                  </td>
                {:else}
                  <td class="px-4 py-3 text-right font-code tabular-nums text-primary">
                    {fmt(row.promptTokens)}
                  </td>
                  <td class="px-4 py-3 text-right font-code tabular-nums text-success">
                    {fmt(row.completionTokens)}
                  </td>
                  <td class="px-4 py-3 text-right font-code tabular-nums text-info">
                    {fmt(row.cachedTokens)}
                  </td>
                {/if}
                <td class="whitespace-nowrap px-4 py-3 text-right text-[11px] text-text-muted">
                  {timeAgo(row.lastUsed)}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Card>
</div>

<script lang="ts">
  import type { Combo } from '../../api/client'
  import Icon from '../../lib/ui/Icon.svelte'
  import {
    getComboModels,
    hasReasoning,
    hasVision,
    type ComboStrategyInfo
  } from './types'

  interface Props {
    combo: Combo
    strategyInfo?: ComboStrategyInfo
    copiedId?: string | null
    onSetStrategy: (combo: Combo, strategy: string) => void
    onOpenJudgePicker: (combo: Combo) => void
    onClearJudge: (comboName: string) => void
    onCopy: (name: string, id: string) => void
    onEdit: (combo: Combo) => void
    onDelete: (combo: Combo) => void
  }

  let {
    combo,
    strategyInfo = {},
    copiedId = null,
    onSetStrategy,
    onOpenJudgePicker,
    onClearJudge,
    onCopy,
    onEdit,
    onDelete,
  }: Props = $props()

  let modelsList = $derived(getComboModels(combo))
  let currentStrategy = $derived(strategyInfo.fallbackStrategy || combo.strategy || 'fallback')
  let judgeModel = $derived(strategyInfo.judgeModel || '')
  let isFusion = $derived(currentStrategy === 'fusion')
  let strategySummary = $derived(
    currentStrategy === 'round-robin'
      ? 'Rotate requests across this list.'
      : isFusion
        ? 'Run all models in parallel; the judge combines responses.'
        : 'Try in order and move on when a model fails.'
  )
</script>

<article class="grid gap-4 py-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-8">
  <div class="min-w-0">
    <div class="flex items-center gap-2">
      <Icon name="route" class="text-brass" />
      <code class="truncate font-code text-sm font-medium text-text-main">{combo.name}</code>
      <button type="button" onclick={() => onCopy(combo.name, combo.id)}
        title={copiedId === combo.id ? 'Copied' : 'Copy combo name'}
        aria-label={copiedId === combo.id ? 'Copied' : `Copy ${combo.name}`}
        class="flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-brand text-text-muted hover:bg-surface-2 hover:text-text-main">
        <Icon name={copiedId === combo.id ? 'check' : 'copy'} size={16} />
      </button>
    </div>
    <div class="mt-3 flex flex-wrap items-center gap-2">
      <label for="strategy-{combo.id}" class="ui-kicker">Strategy</label>
      <select id="strategy-{combo.id}" value={currentStrategy}
        onchange={(event) => onSetStrategy(combo, event.currentTarget.value)}
        class="ui-input min-w-40 cursor-pointer font-code text-xs">
        <option value="fallback">Fallback</option>
        <option value="round-robin">Round robin</option>
        <option value="fusion">Fusion</option>
      </select>
    </div>
    <p class="mt-1 text-[11px] leading-relaxed text-text-muted">{strategySummary}</p>
    <div class="mt-3 flex items-center gap-1">
      <button type="button" onclick={() => onEdit(combo)} title={`Edit ${combo.name}`} aria-label={`Edit ${combo.name}`}
        class="flex size-9 cursor-pointer items-center justify-center rounded-brand text-text-muted hover:bg-surface-2 hover:text-text-main">
        <Icon name="edit" size={16} />
      </button>
      <button type="button" onclick={() => onDelete(combo)} title={`Delete ${combo.name}`} aria-label={`Delete ${combo.name}`}
        class="flex size-9 cursor-pointer items-center justify-center rounded-brand text-danger hover:bg-danger/10">
        <Icon name="delete" size={16} />
      </button>
    </div>
  </div>

  <div class="min-w-0 border-l border-border-subtle pl-4">
    <p class="ui-kicker mb-2 text-text-subtle">
      {isFusion ? 'Panel models' : currentStrategy === 'round-robin' ? 'Rotation order' : 'Fallback order'}
    </p>
    {#if modelsList.length === 0}
      <p class="text-xs text-warning">No models. This combo cannot route requests.</p>
    {:else}
      <ol class="divide-y divide-border-subtle">
        {#each modelsList as model, index (model)}
          <li class="flex min-w-0 items-center gap-3 py-2 first:pt-0">
            <span class="w-5 shrink-0 font-code text-[11px] text-brass">{String(index + 1).padStart(2, '0')}</span>
            <code class="min-w-0 flex-1 break-all font-code text-xs text-text-main">{model}</code>
            {#if hasVision(model)}<Icon name="eye" size={14} class="text-info" label="Vision" />{/if}
            {#if hasReasoning(model)}<Icon name="sparkles" size={14} class="text-warning" label="Reasoning" />{/if}
          </li>
        {/each}
      </ol>
    {/if}
    {#if isFusion}
      <div class="flex flex-wrap items-center gap-2 border-t border-border-subtle pt-2">
        <span class="ui-kicker">Judge</span>
        <button type="button" onclick={() => onOpenJudgePicker(combo)} title="Choose judge model"
          class="min-w-0 cursor-pointer truncate font-code text-xs text-primary hover:underline">
          {judgeModel || `Auto · ${modelsList[0] || 'first model'}`}
        </button>
        {#if judgeModel}
          <button type="button" onclick={() => onClearJudge(combo.name)} title="Reset judge to Auto" aria-label="Reset judge to Auto"
            class="flex size-8 cursor-pointer items-center justify-center rounded-brand text-text-muted hover:bg-surface-2">
            <Icon name="close" size={14} />
          </button>
        {/if}
      </div>
    {/if}
  </div>
</article>

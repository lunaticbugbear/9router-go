<script lang="ts">
  import type { Combo } from '../../api/client'
  import { getModelCaps } from '../../lib/models'
  import Button from '../../lib/ui/Button.svelte'
  import Icon from '../../lib/ui/Icon.svelte'
  import Input from '../../lib/ui/Input.svelte'
  import Modal from '../../lib/ui/Modal.svelte'
  import SegmentedControl from '../../lib/ui/SegmentedControl.svelte'

  interface Props {
    isOpen: boolean
    editingCombo: Combo | null
    models: string[]
    isSaving?: boolean
    onClose: () => void
    onSave: (name: string, models: string[], strategy: string) => Promise<void> | void
    onOpenModelPicker: () => void
    onUpdateModels: (models: string[]) => void
  }

  let {
    isOpen,
    editingCombo,
    models,
    isSaving = false,
    onClose,
    onSave,
    onOpenModelPicker,
    onUpdateModels,
  }: Props = $props()

  let modalName = $state('')
  let modalNameError = $state('')
  let modalModelsError = $state('')
  let modalStrategy = $state<'fallback' | 'round-robin' | 'fusion'>('fallback')
  let editingIdx = $state<number | null>(null)
  let editDraft = $state('')

  const strategyOptions = [
    { value: 'fallback', label: 'Fallback' },
    { value: 'round-robin', label: 'Round robin' },
    { value: 'fusion', label: 'Fusion' },
  ] as const

  $effect(() => {
    if (!isOpen) return
    modalName = editingCombo?.name || ''
    modalStrategy = editingCombo?.strategy === 'round-robin' || editingCombo?.strategy === 'fusion'
      ? editingCombo.strategy
      : 'fallback'
    modalNameError = ''
    modalModelsError = ''
  })

  const VALID_NAME_REGEX = /^[a-zA-Z0-9_.-]+$/

  function validateModalName(name: string): boolean {
    if (!name.trim()) {
      modalNameError = 'Name is required'
      return false
    }
    if (!VALID_NAME_REGEX.test(name.trim())) {
      modalNameError = 'Only letters, numbers, -, _ and . allowed'
      return false
    }
    modalNameError = ''
    return true
  }

  function handleSave() {
    if (!validateModalName(modalName)) return
    if (models.length === 0) {
      modalModelsError = 'Add at least one model to make this route usable.'
      return
    }
    modalModelsError = ''
    onSave(modalName.trim(), models, modalStrategy)
  }

  function moveModel(idx: number, delta: number) {
    const arr = [...models]
    const target = idx + delta
    if (target < 0 || target >= arr.length) return
    const temp = arr[idx]
    arr[idx] = arr[target]
    arr[target] = temp
    onUpdateModels(arr)
  }

  function removeModel(idx: number) {
    onUpdateModels(models.filter((_, i) => i !== idx))
    if (editingIdx === idx) editingIdx = null
  }

  function startEdit(idx: number, model: string) {
    editingIdx = idx
    editDraft = model
  }

  function commitEdit(idx: number) {
    const trimmed = editDraft.trim()
    if (trimmed && trimmed !== models[idx]) {
      const arr = [...models]
      arr[idx] = trimmed
      onUpdateModels(arr)
    }
    editingIdx = null
  }
</script>

<Modal
  {isOpen}
  onClose={onClose}
  title={editingCombo ? 'Edit Combo' : 'Create Combo'}
  size="lg"
>
  <div class="flex flex-col gap-6">
    <div class="grid gap-4 sm:grid-cols-2">
      <Input
        label="Route name"
        bind:value={modalName}
        placeholder="fast-reliable"
        error={modalNameError}
      />
      <div class="flex flex-col gap-2">
        <span class="ui-kicker text-text-muted">Routing strategy</span>
        <SegmentedControl label="Routing strategy" options={strategyOptions} bind:value={modalStrategy} />
        <p class="text-xs leading-relaxed text-text-muted">
          {modalStrategy === 'fallback'
            ? 'Try models in order; move on when one fails.'
            : modalStrategy === 'round-robin'
              ? 'Rotate requests across the model list.'
              : 'Query every model, then combine responses with a judge.'}
        </p>
      </div>
    </div>
    <p class="-mt-4 text-[11px] text-text-subtle">Letters, numbers, hyphens, underscores and periods.</p>

    <section class="flex flex-col gap-3" aria-label="Models in this route">
      <div class="flex items-end justify-between gap-3 border-b border-border-subtle pb-2">
        <div>
          <p class="ui-kicker text-text-subtle">{models.length} selected</p>
          <h3 class="mt-0.5 text-sm font-semibold text-text-main">
            {modalStrategy === 'fusion' ? 'Panel models' : 'Model order'}
          </h3>
        </div>
        <span class="font-code text-[10px] text-text-subtle">
          {modalStrategy === 'fallback' ? 'Next on failure' : modalStrategy === 'round-robin' ? 'Rotation order' : 'Run in parallel'}
        </span>
      </div>
      {#if models.length === 0}
        <div class="flex flex-col items-center gap-2 border-y border-dashed border-border-subtle py-8 text-center">
          <Icon name="layers" class="text-text-subtle" />
          <p class="text-sm font-medium text-text-main">Add the first model</p>
          <p class="text-xs text-text-muted">Choose an available provider model for this route.</p>
        </div>
      {:else}
        <ol class="max-h-[42vh] divide-y divide-border-subtle overflow-y-auto border-y border-border-subtle sm:max-h-[350px]">
          {#each models as model, idx}
            {@const caps = getModelCaps(model)}
            <li class="grid min-w-0 grid-cols-[2rem_minmax(0,1fr)_auto] items-center gap-2 py-2.5">
              <span class="font-code text-[11px] text-brass">{String(idx + 1).padStart(2, '0')}</span>
              {#if editingIdx === idx}
                <input
                  bind:value={editDraft}
                  onblur={() => commitEdit(idx)}
                  onkeydown={(e) => {
                    if (e.key === 'Enter') commitEdit(idx)
                    if (e.key === 'Escape') editingIdx = null
                  }}
                  aria-label={`Edit model ${model}`}
                  class="ui-input min-w-0 font-code text-xs"
                />
              {:else}
                <button
                  type="button"
                  onclick={() => startEdit(idx, model)}
                  class="min-w-0 truncate text-left font-code text-xs text-text-main hover:text-primary"
                  title={`Edit ${model}`}
                >
                  {model}
                </button>
              {/if}
              <div class="flex shrink-0 items-center gap-0.5">
                {#if caps.vision}<Icon name="eye" size={14} class="text-info" label="Vision" />{/if}
                {#if caps.reasoning}<Icon name="brain" size={14} class="text-warning" label="Reasoning" />{/if}
                <button
                  type="button"
                  onclick={() => moveModel(idx, -1)}
                  disabled={idx === 0}
                  aria-label={`Move ${model} up`}
                  class="flex size-8 items-center justify-center rounded-brand {idx === 0 ? 'text-text-muted/20 cursor-not-allowed' : 'text-text-muted hover:bg-surface-2 hover:text-text-main cursor-pointer'}"
                  title="Move up"
                >
                  <Icon name="arrow-up" size={14} />
                </button>
                <button
                  type="button"
                  onclick={() => moveModel(idx, 1)}
                  disabled={idx === models.length - 1}
                  aria-label={`Move ${model} down`}
                  class="flex size-8 items-center justify-center rounded-brand {idx === models.length - 1 ? 'text-text-muted/20 cursor-not-allowed' : 'text-text-muted hover:bg-surface-2 hover:text-text-main cursor-pointer'}"
                  title="Move down"
                >
                  <Icon name="arrow-down" size={14} />
                </button>
                <button
                  type="button"
                  onclick={() => removeModel(idx)}
                  aria-label={`Remove ${model}`}
                  class="flex size-8 items-center justify-center rounded-brand text-text-muted transition-colors hover:bg-danger/10 hover:text-danger cursor-pointer"
                  title="Remove"
                >
                  <Icon name="close" size={14} />
                </button>
              </div>
            </li>
          {/each}
        </ol>
      {/if}
      {#if modalModelsError}<p role="alert" class="text-xs text-danger">{modalModelsError}</p>{/if}
      <Button variant="secondary" icon="add" onclick={() => { modalModelsError = ''; onOpenModelPicker() }} fullWidth>
        Add models
      </Button>
    </section>
    <div class="flex justify-end gap-2 border-t border-border-subtle pt-4">
      <Button onclick={onClose} variant="ghost" disabled={isSaving}>Cancel</Button>
      <Button
        onclick={handleSave}
        disabled={!modalName.trim() || !!modalNameError || models.length === 0 || isSaving}
        loading={isSaving}
      >
        {editingCombo ? 'Save route' : 'Create route'}
      </Button>
    </div>
  </div>
</Modal>

<script module lang="ts">
  export type PaletteProvider = { id: string; name: string; icon: string }
  export type PaletteAction = { id: string; label: string; icon: string; hint?: string; run: () => void }
</script>

<script lang="ts">
  import { tick } from 'svelte'
  import type { ActiveTab } from '../lib/router'
  import { ALL_NAV_GROUPS, groupTitle } from './Sidebar.svelte'

  type Item = {
    id: string
    group: string
    label: string
    hint?: string
    icon?: string
    image?: string
    run: () => void
  }

  let {
    open = $bindable(false),
    navigate,
    providers = [],
    onSelectProvider,
    actions = [],
  }: {
    open?: boolean
    navigate?: (tab: ActiveTab) => void
    providers?: PaletteProvider[]
    onSelectProvider?: (id: string) => void
    actions?: PaletteAction[]
  } = $props()

  let query = $state('')
  let activeIndex = $state(0)
  let inputEl = $state<HTMLInputElement | null>(null)

  let items = $derived<Item[]>([
    ...ALL_NAV_GROUPS.flatMap((group) =>
      group.links.map((link) => ({
        id: `nav:${link.tab}`,
        group: groupTitle(group),
        label: link.label,
        hint: link.hint,
        icon: link.icon,
        run: () => navigate?.(link.tab),
      }))
    ),
    ...providers.map((provider) => ({
      id: `provider:${provider.id}`,
      group: 'Providers',
      label: provider.name,
      hint: provider.id,
      image: provider.icon,
      run: () => onSelectProvider?.(provider.id),
    })),
    ...actions.map((action) => ({ ...action, id: `action:${action.id}`, group: 'Actions' })),
  ])

  let results = $derived.by(() => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
    if (terms.length === 0) return items
    return items.filter((item) => {
      const haystack = `${item.label} ${item.hint ?? ''} ${item.group}`.toLowerCase()
      return terms.every((term) => haystack.includes(term))
    })
  })

  const optionId = (index: number) => `palette-option-${index}`

  $effect(() => {
    if (!open) return
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    query = ''
    activeIndex = 0
    tick().then(() => inputEl?.focus())
    return () => previousFocus?.focus()
  })

  $effect(() => {
    if (activeIndex >= results.length) activeIndex = Math.max(0, results.length - 1)
  })

  $effect(() => {
    if (!open) return
    document.getElementById(optionId(activeIndex))?.scrollIntoView({ block: 'nearest' })
  })

  function close() {
    open = false
  }

  function choose(item: Item | undefined) {
    if (!item) return
    close()
    item.run()
  }

  function move(delta: number) {
    if (results.length === 0) return
    activeIndex = (activeIndex + delta + results.length) % results.length
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      move(1)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      move(-1)
    } else if (event.key === 'Enter') {
      event.preventDefault()
      choose(results[activeIndex])
    } else if (event.key === 'Escape') {
      event.preventDefault()
      close()
    } else if (event.key === 'Tab') {
      // Single-field dialog: keep focus on the prompt instead of leaking to the page.
      event.preventDefault()
    }
  }
</script>

{#if open}
  <div class="fixed inset-0 z-[70] flex items-start justify-center p-4 pt-[12vh]" data-modal-open="true">
    <button
      type="button"
      tabindex="-1"
      aria-label="Close command palette"
      class="absolute inset-0 cursor-default bg-[rgba(5,7,9,0.78)]"
      onclick={close}
    ></button>

    <div
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      class="ui-panel ui-frame relative z-10 flex max-h-[70vh] w-full max-w-xl flex-col overflow-hidden border-brass/40 shadow-elevated"
    >
      <div class="flex items-center gap-3 border-b border-border-subtle px-4 py-3">
        <span class="font-code text-base text-focus" aria-hidden="true">›</span>
        <input
          bind:this={inputEl}
          bind:value={query}
          onkeydown={handleKeydown}
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-results"
          aria-activedescendant={results.length ? optionId(activeIndex) : undefined}
          aria-autocomplete="list"
          autocomplete="off"
          spellcheck="false"
          placeholder="Jump to a page, provider or action"
          class="ui-bare min-w-0 flex-1 bg-transparent font-code text-[13px] text-text-main placeholder:text-text-subtle"
        />
        <kbd class="rounded-brand border border-border-subtle bg-surface-2 px-1.5 py-0.5 font-code text-[10px] text-text-subtle">
          esc
        </kbd>
      </div>

      <ul id="palette-results" role="listbox" aria-label="Results" class="custom-scrollbar overflow-y-auto p-2">
        {#each results as item, index (item.id)}
          {#if index === 0 || results[index - 1].group !== item.group}
            <li role="presentation" class="ui-kicker px-2.5 pb-1 pt-3 first:pt-1">{item.group}</li>
          {/if}
          <!-- Keyboard selection is driven by the combobox input above. -->
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <li
            id={optionId(index)}
            role="option"
            aria-selected={index === activeIndex}
            onclick={() => choose(item)}
            onmousemove={() => (activeIndex = index)}
            class="flex cursor-pointer items-center gap-3 rounded-r-brand border-l-2 px-2.5 py-2 text-[13px] {index === activeIndex
              ? 'border-focus bg-surface-2 text-text-main'
              : 'border-transparent text-text-muted'}"
          >
            {#if item.image}
              <img src={item.image} alt="" class="size-[18px] flex-shrink-0 rounded-brand object-contain" />
            {:else}
              <span class="material-symbols-outlined text-[18px] text-text-subtle" aria-hidden="true">{item.icon}</span>
            {/if}
            <span class="min-w-0 flex-1 truncate">{item.label}</span>
            {#if item.hint}
              <span class="max-w-[45%] truncate font-code text-[11px] text-text-subtle">{item.hint}</span>
            {/if}
          </li>
        {:else}
          <li class="px-3 py-10 text-center">
            <p class="font-headline text-lg font-medium uppercase tracking-[0.06em] text-text-main">No signal</p>
            <p class="mt-1 text-xs text-text-muted">Nothing matches “{query}”.</p>
          </li>
        {/each}
      </ul>

      <div class="flex items-center gap-4 border-t border-border-subtle px-4 py-2 font-code text-[10px] uppercase tracking-[0.14em] text-text-subtle">
        <span>↑↓ select</span>
        <span>↵ open</span>
        <span class="ml-auto">{results.length} {results.length === 1 ? 'result' : 'results'}</span>
      </div>
    </div>
  </div>
{/if}

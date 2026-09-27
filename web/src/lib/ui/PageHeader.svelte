<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { ActiveTab } from '../router'
  import { PAGE_META } from '../pageMeta'
  import { sectionLabelFor } from '../../components/Sidebar.svelte'

  let {
    tab,
    kicker,
    title,
    description,
    actions,
    meta,
    class: klass = '',
  }: {
    /** Derives kicker, title and description from the nav model / PAGE_META when omitted. */
    tab?: ActiveTab
    kicker?: string
    title?: string
    description?: string
    actions?: Snippet
    meta?: Snippet
    class?: string
  } = $props()

  const resolvedKicker = $derived(kicker ?? (tab ? sectionLabelFor(tab) : ''))
  const resolvedTitle = $derived(title ?? (tab ? PAGE_META[tab].title : ''))
  const resolvedDescription = $derived(description ?? (tab ? PAGE_META[tab].description : ''))
</script>

<header class="mb-8 flex flex-col gap-5 {klass}">
  <div class="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
    <div class="min-w-0 max-w-3xl">
      {#if resolvedKicker}<p class="ui-kicker">{resolvedKicker}</p>{/if}
      <h1 class="ui-heading mt-1.5 text-text-main">{resolvedTitle}</h1>
      {#if resolvedDescription}
        <p class="mt-2 text-sm leading-relaxed text-text-muted">{resolvedDescription}</p>
      {/if}
    </div>
    {#if actions}
      <div class="flex flex-shrink-0 flex-wrap items-center gap-2">{@render actions()}</div>
    {/if}
  </div>
  {#if meta}
    <div class="flex flex-wrap items-center gap-x-6 gap-y-2">{@render meta()}</div>
  {/if}
  <div class="ui-divider" aria-hidden="true">◆</div>
</header>

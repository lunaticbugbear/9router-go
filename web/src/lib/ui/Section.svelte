<script lang="ts">
  import type { Snippet } from 'svelte'

  let {
    title,
    description = '',
    index = '',
    actions,
    children,
    class: klass = '',
  }: {
    title: string
    description?: string
    /** Roman numeral or step marker shown before the title. */
    index?: string
    actions?: Snippet
    children?: Snippet
    class?: string
  } = $props()
</script>

<section class="flex min-w-0 flex-col gap-4 {klass}">
  <div class="flex flex-wrap items-end justify-between gap-3 border-b border-border-subtle pb-3">
    <div class="min-w-0">
      <h2 class="flex items-baseline gap-2 text-[15px] font-semibold text-text-main">
        {#if index}<span class="font-code text-[11px] font-medium text-brass">{index}</span>{/if}
        {title}
      </h2>
      {#if description}<p class="mt-0.5 text-[13px] text-text-muted">{description}</p>{/if}
    </div>
    {#if actions}<div class="flex flex-wrap items-center gap-2">{@render actions()}</div>{/if}
  </div>
  {@render children?.()}
</section>

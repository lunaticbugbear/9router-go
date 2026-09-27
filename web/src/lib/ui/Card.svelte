<script lang="ts">
  // Shared surface: semantic tokens keep dense views coherent in both themes.
  import type { Snippet } from 'svelte'

  let {
    title,
    subtitle,
    icon,
    action,
    padding = 'md',
    hover = false,
    elev = false,
    class: klass = '',
    children
  }: {
    title?: string
    subtitle?: string
    icon?: Snippet
    action?: Snippet
    padding?: 'none' | 'xs' | 'sm' | 'md' | 'lg'
    hover?: boolean
    elev?: boolean
    class?: string
    children?: Snippet
  } = $props()

  const paddings = {
    none: '',
    xs: 'p-3',
    sm: 'p-4',
    md: 'p-4 sm:p-6',
    lg: 'p-6 sm:p-8',
  }
</script>

<div
  class="ui-panel min-w-0 {elev ? 'ui-frame shadow-elevated' : ''} {hover
    ? 'hover:border-brass/50 hover:bg-surface-2 transition-colors duration-150 ease-imperial cursor-pointer'
    : ''} {paddings[padding]} {klass}"
>
  {#if title || action}
    <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div class="flex min-w-0 items-start gap-3">
        {#if icon}
          <div class="flex size-9 shrink-0 items-center justify-center rounded-brand border border-border-subtle bg-surface-2 text-primary">
            {@render icon()}
          </div>
        {/if}
        <div class="min-w-0">
          {#if title}
            <h3 class="text-[15px] font-semibold leading-snug text-text-main">{title}</h3>
          {/if}
          {#if subtitle}
            <p class="mt-1 text-sm leading-relaxed text-text-muted">{subtitle}</p>
          {/if}
        </div>
      </div>
      {#if action}
        <div>{@render action()}</div>
      {/if}
    </div>
  {/if}
  {@render children?.()}
</div>

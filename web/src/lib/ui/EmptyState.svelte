<script lang="ts">
  import type { Snippet } from 'svelte'

  let {
    icon = 'inbox',
    title,
    description = '',
    compact = false,
    action,
    class: klass = '',
  }: {
    icon?: string
    title: string
    description?: string
    compact?: boolean
    action?: Snippet
    class?: string
  } = $props()
</script>

<div
  class="flex flex-col items-center justify-center rounded-brand-lg border border-dashed border-border px-6 text-center {compact
    ? 'py-6'
    : 'py-16'} {klass}"
>
  <span
    class="flex items-center justify-center rounded-brand border border-border-subtle bg-surface-2 text-text-subtle {compact
      ? 'mb-3 size-9'
      : 'mb-4 size-12'}"
  >
    <span class="material-symbols-outlined {compact ? 'text-[20px]' : 'text-[24px]'}" aria-hidden="true">{icon}</span>
  </span>
  {#if compact}
    <p class="text-sm font-semibold text-text-main">{title}</p>
  {:else}
    <p class="font-headline text-xl font-medium uppercase tracking-[0.06em] text-text-main">{title}</p>
  {/if}
  {#if description}
    <p class="max-w-md text-sm leading-relaxed text-text-muted {compact ? 'mt-1' : 'mt-2'}">{description}</p>
  {/if}
  {#if action}<div class="flex flex-wrap justify-center gap-2 {compact ? 'mt-4' : 'mt-6'}">{@render action()}</div>{/if}
</div>

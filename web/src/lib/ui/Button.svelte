<script lang="ts">
  // Shared action hierarchy: primary is deliberate, quiet actions stay quiet.
  import type { Snippet } from 'svelte'
  import Icon from './Icon.svelte'

  type Variant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'danger' | 'success'
  type Size = 'sm' | 'md' | 'lg'

  let {
    variant = 'primary',
    size = 'md',
    type = 'button',
    icon = '',
    iconRight = '',
    disabled = false,
    loading = false,
    fullWidth = false,
    title = '',
    class: klass = '',
    onclick,
    children
  }: {
    title?: string
    variant?: Variant
    size?: Size
    type?: 'button' | 'submit' | 'reset'
    icon?: string
    iconRight?: string
    disabled?: boolean
    loading?: boolean
    fullWidth?: boolean
    class?: string
    onclick?: (e: MouseEvent) => void
    children?: Snippet
  } = $props()
  const variants: Record<Variant, string> = {
    primary:
      'bg-primary text-bg font-semibold uppercase tracking-[0.06em] hover:bg-primary-hover active:brightness-90 disabled:bg-surface-3 disabled:text-text-muted',
    secondary:
      'border border-border bg-surface-2 text-text-main hover:bg-surface-3 hover:border-brass/50 disabled:opacity-50',
    outline:
      'border border-brass/40 bg-transparent text-text-main hover:bg-surface-2 hover:border-brass/70',
    ghost: 'text-text-muted hover:bg-surface-2 hover:text-text-main',
    danger: 'border border-danger/40 bg-danger/12 text-danger hover:bg-danger/20 disabled:bg-surface-3 disabled:text-text-muted',
    success: 'border border-success/40 bg-success/12 text-success hover:bg-success/20 disabled:bg-surface-3 disabled:text-text-muted',
  }

  const sizes: Record<Size, string> = {
    sm: 'min-h-8 px-3 text-xs',
    md: 'min-h-9 px-4 text-[13px]',
    lg: 'min-h-11 px-6 text-[13px]',
  }

  const shape = $derived(variant === 'primary' && size !== 'sm' ? 'ui-chamfer' : 'rounded-brand')
</script>

<button
  {type}
  {title}
  class="inline-flex items-center justify-center gap-2 {variant === 'primary' ? '' : 'font-medium tracking-[0.01em]'} transition-[background-color,border-color,color,filter] duration-150 ease-imperial cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed {variants[variant]} {sizes[size]} {shape} {fullWidth ? 'w-full' : ''} {klass}"
  disabled={disabled || loading}
  aria-busy={loading}
  {onclick}
>
  {#if loading}
    <Icon name="spinner" spin />
  {:else if icon}
    <Icon name={icon} />
  {/if}
  {@render children?.()}
  {#if iconRight && !loading}
    <Icon name={iconRight} />
  {/if}
</button>

<script lang="ts">
  // Port of decolua/9router src/shared/components/Badge.js
  import type { Snippet } from 'svelte'

  type Tone = 'default' | 'neutral' | 'primary' | 'success' | 'warning' | 'danger' | 'error' | 'info' | 'outline'

  let {
    tone,
    variant = 'default',
    size = 'md',
    dot = false,
    class: klass = '',
    children
  }: {
    tone?: Tone
    variant?: Tone
    size?: 'sm' | 'md' | 'lg'
    dot?: boolean
    class?: string
    children?: Snippet
  } = $props()

  const activeTone = $derived(tone || variant || 'default')

  const tones: Record<string, string> = {
    default: 'bg-surface-2 text-text-muted border border-border',
    neutral: 'bg-surface-2 text-text-muted border border-border',
    outline: 'bg-transparent text-text-muted border border-border',
    primary: 'bg-primary/10 text-primary border border-primary/30',
    success: 'bg-success/10 text-success border border-success/30',
    warning: 'bg-warning/10 text-warning border border-warning/30',
    danger: 'bg-danger/10 text-danger border border-danger/30',
    error: 'bg-danger/10 text-danger border border-danger/30',
    info: 'bg-info/10 text-info border border-info/30',
  }

  const dotColors: Record<string, string> = {
    default: 'bg-text-muted',
    neutral: 'bg-text-muted',
    outline: 'bg-text-muted',
    primary: 'bg-primary',
    success: 'bg-success',
    warning: 'bg-warning',
    danger: 'bg-danger',
    error: 'bg-danger',
    info: 'bg-info',
  }

  const sizeClasses: Record<string, string> = {
    sm: 'text-[10.5px] px-1.5 py-px',
    md: 'text-[11px] px-2 py-0.5',
    lg: 'text-xs px-2.5 py-1',
  }
</script>

<!-- No text-transform: badges often carry case-sensitive model IDs. -->
<span
  class="inline-flex items-center gap-1.5 rounded-brand font-code font-medium tracking-wide {sizeClasses[size] || sizeClasses.md} {tones[activeTone] || tones.default} {klass}"
>
  {#if dot}
    <span class="w-1.5 h-1.5 rounded-full shrink-0 {dotColors[activeTone] || 'bg-current'}"></span>
  {/if}
  {@render children?.()}
</span>

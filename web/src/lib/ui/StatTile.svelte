<script lang="ts">
  type Tone = 'default' | 'primary' | 'success' | 'warning' | 'danger' | 'signal'

  let {
    label,
    value,
    hint = '',
    icon = '',
    tone = 'default',
    class: klass = '',
  }: {
    label: string
    value: string | number
    hint?: string
    icon?: string
    tone?: Tone
    class?: string
  } = $props()

  const toneClass: Record<Tone, string> = {
    default: 'text-text-main',
    primary: 'text-primary',
    success: 'text-success',
    warning: 'text-warning',
    danger: 'text-danger',
    signal: 'text-focus',
  }
</script>

<div class="ui-panel flex min-w-0 flex-col gap-2 p-4 {klass}">
  <div class="flex items-center justify-between gap-2">
    <span class="ui-kicker truncate text-text-subtle">{label}</span>
    {#if icon}
      <span class="material-symbols-outlined text-[18px] text-text-subtle" aria-hidden="true">{icon}</span>
    {/if}
  </div>
  <span class="ui-stat truncate text-[28px] leading-8 {toneClass[tone]}">{value}</span>
  {#if hint}<span class="truncate text-xs text-text-muted">{hint}</span>{/if}
</div>

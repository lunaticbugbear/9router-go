<script lang="ts" generics="T extends string">
  let {
    options,
    value = $bindable(),
    label,
    onchange,
    class: klass = '',
  }: {
    options: readonly { value: T; label: string }[]
    value: T
    label: string
    onchange?: (value: T) => void
    class?: string
  } = $props()
</script>

<div
  role="group"
  aria-label={label}
  class="inline-flex max-w-full overflow-x-auto rounded-brand border border-border-subtle bg-bg-alt p-0.5 {klass}"
>
  {#each options as option (option.value)}
    {@const active = option.value === value}
    <button
      type="button"
      aria-pressed={active}
      onclick={() => {
        value = option.value
        onchange?.(option.value)
      }}
      class="cursor-pointer whitespace-nowrap rounded-[2px] px-3 py-1.5 font-code text-[11px] uppercase tracking-[0.12em] transition-colors duration-150 ease-imperial {active
        ? 'bg-surface-2 text-text-main shadow-[inset_0_-1px_0_var(--app-primary)]'
        : 'text-text-subtle hover:text-text-main'}"
    >
      {option.label}
    </button>
  {/each}
</div>

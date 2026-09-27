<script lang="ts">
  // Accessible hit target with a compact visual switch inside.
  let {
    checked = false,
    disabled = false,
    label = 'Toggle',
    title = '',
    size = 'md',
    onChange,
    onchange
  }: {
    checked?: boolean
    disabled?: boolean
    label?: string
    title?: string
    size?: 'sm' | 'md'
    onChange?: (value: boolean) => void
    onchange?: (value: boolean) => void
  } = $props()
</script>

<button
  type="button"
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {title}
  {disabled}
  onclick={() => {
    const fn = onChange || onchange
    fn?.(!checked)
  }}
  class="relative inline-flex shrink-0 items-center rounded-full border border-border/70 transition-colors duration-150 ease-imperial cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 {size === 'sm' ? 'h-7 w-11' : 'h-8 w-12'} {checked
    ? 'bg-primary'
    : 'bg-surface-3'}"
>
  <span
    aria-hidden="true"
    class="inline-block transform rounded-full bg-[#ece5d6] shadow transition-transform duration-150 ease-imperial {size === 'sm' ? 'size-3.5' : 'size-4'} {checked
      ? (size === 'sm' ? 'translate-x-[25px]' : 'translate-x-[26px]')
      : (size === 'sm' ? 'translate-x-[5px]' : 'translate-x-[6px]')}"
  ></span>
</button>

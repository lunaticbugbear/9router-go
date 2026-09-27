<script lang="ts">
  // Port of decolua/9router src/shared/components/Input.js
  let {
    label = '',
    type = 'text',
    placeholder = '',
    value = $bindable(''),
    error = '',
    hint = '',
    icon = '',
    disabled = false,
    required = false,
    min = '',
    max = '',
    class: klass = '',
    inputClass = ''
  }: {
    label?: string
    type?: string
    placeholder?: string
    value?: string
    error?: string
    hint?: string
    icon?: string
    disabled?: boolean
    required?: boolean
    min?: string
    max?: string
    class?: string
    inputClass?: string
  } = $props()

  const inputId = $props.id()
</script>

<div class="flex min-w-0 flex-col gap-1.5 {klass}">
  {#if label}
    <label for={inputId} class="ui-kicker text-text-muted">
      {label}
      {#if required}<span class="ml-1 text-danger">*</span>{/if}
    </label>
  {/if}

  <div class="relative">
    {#if icon}
      <div aria-hidden="true" class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-text-muted">
        <span class="material-symbols-outlined text-[20px]">{icon}</span>
      </div>
    {/if}
    <input
      id={inputId}
      {type}
      {placeholder}
      {disabled}
      {required}
      {min}
      {max}
      bind:value
      aria-invalid={error ? 'true' : undefined}
      aria-describedby={error || hint ? `${inputId}-message` : undefined}
      class="ui-input w-full text-[16px] disabled:cursor-not-allowed disabled:opacity-50 sm:text-sm {icon ? 'pl-10' : ''} {error ? 'border-danger ui-glitch' : ''} {inputClass}"
    />
  </div>

  {#if error}
    <p id="{inputId}-message" role="alert" class="flex items-center gap-1 text-xs text-danger">
      <span aria-hidden="true" class="material-symbols-outlined text-[14px]">error</span>
      {error}
    </p>
  {:else if hint}
    <p id="{inputId}-message" class="text-xs text-text-muted">{hint}</p>
  {/if}
</div>

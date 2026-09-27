<script lang="ts">
  type Tone = 'ok' | 'warn' | 'error' | 'idle' | 'live'

  let { tone = 'idle', label = '' }: { tone?: Tone; label?: string } = $props()

  const toneClass: Record<Tone, string> = {
    ok: 'bg-success',
    warn: 'bg-warning',
    error: 'bg-danger',
    idle: 'bg-text-subtle',
    live: 'bg-focus ui-pulse',
  }
</script>

<span
  class="inline-block size-2 flex-shrink-0 rounded-full {toneClass[tone]}"
  role={label ? 'img' : undefined}
  aria-label={label || undefined}
  aria-hidden={label ? undefined : 'true'}
></span>

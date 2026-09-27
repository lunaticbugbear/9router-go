<script lang="ts">
  // Port of decolua/9router src/shared/components/Modal.js
  import type { Snippet } from 'svelte'
  import { tick } from 'svelte'

  let {
    isOpen = false,
    onClose,
    title = '',
    size = 'md',
    closeOnOverlay = true,
    class: klass = '',
    children,
    footer
  }: {
    isOpen?: boolean
    onClose?: () => void
    title?: string
    size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
    closeOnOverlay?: boolean
    class?: string
    children?: Snippet
    footer?: Snippet
  } = $props()

  const sizes = {
    sm: 'max-w-sm',
    md: 'max-w-md',
    lg: 'max-w-lg',
    xl: 'max-w-xl',
    full: 'max-w-4xl'
  }

  let dialogElement = $state<HTMLElement | undefined>()
  let closeButton = $state<HTMLButtonElement | undefined>()

  // Lock background scroll while the modal is open.
  $effect(() => {
    if (!isOpen) return
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = previous
    }
  })

  $effect(() => {
    if (!isOpen) return
    let mounted = true
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    tick().then(() => {
      if (mounted) closeButton?.focus()
    })

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose?.()
        return
      }
      if (e.key !== 'Tab' || !dialogElement) return
      const focusables = [...dialogElement.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
      )].filter((node) => node.offsetParent !== null)
      if (!focusables.length) return
      const first = focusables[0]
      const last = focusables[focusables.length - 1]
      if (e.shiftKey && (document.activeElement === first || !dialogElement.contains(document.activeElement))) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && (document.activeElement === last || !dialogElement.contains(document.activeElement))) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      mounted = false
      document.removeEventListener('keydown', handleKeyDown)
      previousFocus?.focus()
    }
  })
</script>

{#if isOpen}
  <div class="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-6" data-modal-open="true">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="absolute inset-0 bg-[rgba(5,7,9,0.78)]"
      onclick={closeOnOverlay ? () => onClose?.() : undefined}
      onkeydown={(e) => e.key === 'Escape' && onClose?.()}
      role="presentation"
    ></div>

    <div
      bind:this={dialogElement}
      role="dialog"
      aria-modal="true"
      aria-label={title || 'Dialog'}
      class="ui-panel ui-frame relative z-10 flex max-h-[90vh] w-full flex-col overflow-hidden border-brass/40 shadow-elevated {sizes[size]} {klass}"
    >
      <div class="flex items-start justify-between gap-4 border-b border-border-subtle px-4 py-3 sm:px-6 sm:py-4">
        <div class="min-w-0">
          <span class="ui-kicker">Gateway / dialog</span>
          {#if title}<h2 class="mt-1 font-headline text-xl font-medium uppercase leading-tight tracking-[0.06em] text-text-main">{title}</h2>{/if}
        </div>
        <button
          bind:this={closeButton}
          type="button"
          onclick={() => onClose?.()}
          aria-label="Close dialog"
          class="flex size-9 shrink-0 items-center justify-center rounded-brand border border-border text-text-muted transition-colors hover:border-brass/60 hover:text-text-main"
        >
          <span class="material-symbols-outlined text-[20px]">close</span>
        </button>
      </div>

      <div class="custom-scrollbar min-h-0 overflow-y-auto p-4 sm:p-6">
        {@render children?.()}
      </div>

      {#if footer}
        <div class="flex flex-wrap items-center justify-end gap-3 border-t border-border-subtle bg-surface-2/50 p-4 sm:px-6">
          {@render footer()}
        </div>
      {/if}
    </div>
  </div>
{/if}

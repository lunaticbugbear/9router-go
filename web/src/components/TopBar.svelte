<script lang="ts">
  import { api } from '../api/client'
  import ChangelogModal from './ChangelogModal.svelte'
  import CommandPalette, { type PaletteAction, type PaletteProvider } from './CommandPalette.svelte'
  import { type ActiveTab } from '../lib/router'
  import { notifications } from '../lib/notifications'
  import Button from '../lib/ui/Button.svelte'
  import Icon from '../lib/ui/Icon.svelte'
  import Modal from '../lib/ui/Modal.svelte'

  let {
    activeTab = 'endpoint',
    sectionLabel = 'Dashboard',
    pageTitle,
    pageDescription,
    selectedProvider = null,
    menuOpen = false,
    providers = [],
    navigate,
    onSelectProvider,
    onBackToProviders,
    onMenuClick,
    onLogout,
  }: {
    activeTab?: ActiveTab
    sectionLabel?: string
    pageTitle?: string
    pageDescription?: string
    selectedProvider?: { id: string; name: string; icon: string } | null
    menuOpen?: boolean
    providers?: PaletteProvider[]
    navigate?: (tab: ActiveTab) => void
    onSelectProvider?: (id: string) => void
    onBackToProviders?: () => void
    onMenuClick?: () => void
    onLogout?: () => void
  } = $props()

  // Theme state
  let isDark = $state(true)
  let isDonateOpen = $state(false)
  let isAccountOpen = $state(false)
  let isChangelogOpen = $state(false)
  let isPaletteOpen = $state(false)
  let accountMenuEl = $state<HTMLDivElement | null>(null)
  let accountButtonEl = $state<HTMLButtonElement | null>(null)

  const modKey =
    typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl'

  $effect(() => {
    if (typeof window !== 'undefined') {
      const stored = localStorage.getItem('9router-theme') || localStorage.getItem('theme')
      if (stored === 'light') {
        isDark = false
      } else if (stored === 'dark') {
        isDark = true
      } else {
        isDark = document.documentElement.classList.contains('dark')
      }
      applyTheme(isDark)
    }
  })

  function applyTheme(dark: boolean) {
    document.documentElement.classList.toggle('dark', dark)
    document.documentElement.classList.toggle('light', !dark)
    localStorage.setItem('9router-theme', dark ? 'dark' : 'light')
    localStorage.setItem('theme', dark ? 'dark' : 'light')
  }

  function toggleTheme() {
    isDark = !isDark
    applyTheme(isDark)
  }

  function closeAccountMenu(restoreFocus = false) {
    if (!isAccountOpen) return
    isAccountOpen = false
    if (restoreFocus) accountButtonEl?.focus()
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if ((event.metaKey || event.ctrlKey) && !event.altKey && event.key.toLowerCase() === 'k') {
      // Don't stack the palette on top of another dialog's focus trap.
      if (!isPaletteOpen && document.querySelector('[data-modal-open="true"]')) return
      event.preventDefault()
      closeAccountMenu()
      isPaletteOpen = !isPaletteOpen
      return
    }
    if (event.key !== 'Escape' || !isAccountOpen) return
    // The shell's drawer handler also listens on `window` and cannot be
    // cancelled from here; it skips instead when it sees `data-popover-open`.
    closeAccountMenu(true)
  }

  function handleWindowPointerDown(event: MouseEvent) {
    if (!isAccountOpen) return
    const target = event.target
    if (target instanceof Node && accountMenuEl?.contains(target)) return
    closeAccountMenu()
  }

  async function handleLogout() {
    closeAccountMenu()
    try {
      await api.logout()
    } catch {}
    if (onLogout) {
      onLogout()
    } else {
      window.location.assign('/login')
    }
  }

  async function copyEndpoint() {
    const url = `${window.location.origin}/v1`
    try {
      await navigator.clipboard.writeText(url)
      notifications.success(url, 'Endpoint copied')
    } catch {
      notifications.error('Clipboard access was denied by the browser.', 'Copy failed')
    }
  }

  let paletteActions = $derived<PaletteAction[]>([
    { id: 'copy-endpoint', label: 'Copy endpoint URL', icon: 'content_copy', hint: '/v1', run: copyEndpoint },
    {
      id: 'theme',
      label: isDark ? 'Switch to Parchment theme' : 'Switch to Obsidian theme',
      icon: isDark ? 'light_mode' : 'dark_mode',
      run: toggleTheme,
    },
    { id: 'changelog', label: 'Release notes', icon: 'history', run: () => (isChangelogOpen = true) },
    { id: 'logout', label: 'Sign out', icon: 'logout', run: handleLogout },
  ])
</script>

<svelte:window onkeydown={handleWindowKeydown} onpointerdown={handleWindowPointerDown} />

<header
  class="z-20 flex h-14 flex-shrink-0 items-center justify-between gap-3 border-b border-border-subtle bg-vibrancy px-4 sm:gap-4 lg:px-10"
>
  <!-- Left: mobile drawer trigger + destination context (views own the page heading) -->
  <div class="flex min-w-0 flex-1 items-center gap-3">
    {#if onMenuClick}
      <button
        id="app-menu-trigger"
        type="button"
        onclick={onMenuClick}
        class="flex size-9 flex-shrink-0 cursor-pointer items-center justify-center rounded-brand text-text-muted transition-colors hover:bg-surface-2 hover:text-text-main lg:hidden"
        aria-label={menuOpen ? 'Close navigation' : 'Open navigation'}
        aria-expanded={menuOpen}
        aria-controls="app-sidebar"
      >
        <Icon name={menuOpen ? 'close' : 'menu'} size={22} />
      </button>
    {/if}

    <nav aria-label="Breadcrumb" class="min-w-0">
      {#if activeTab === 'connections' && selectedProvider}
        <ol class="flex min-w-0 items-center gap-1.5">
          <li class="hidden flex-shrink-0 sm:block">
            <span class="ui-kicker">{sectionLabel}</span>
          </li>
          <li aria-hidden="true" class="hidden flex-shrink-0 sm:block">
            <Icon name="chevron-right" size={16} class="text-text-subtle" />
          </li>
          <li class="flex-shrink-0">
            <button
              type="button"
              onclick={onBackToProviders}
              class="cursor-pointer rounded-brand px-1 py-0.5 text-[13px] text-text-muted transition-colors hover:text-text-main"
            >
              Providers
            </button>
          </li>
          <li aria-hidden="true" class="flex-shrink-0">
            <Icon name="chevron-right" size={16} class="text-text-subtle" />
          </li>
          <li class="flex min-w-0 items-center gap-2">
            <img
              src={selectedProvider.icon}
              alt=""
              class="size-5 flex-shrink-0 rounded-brand object-contain"
            />
            <span class="truncate text-sm font-medium text-text-main">{selectedProvider.name}</span>
          </li>
        </ol>
      {:else}
        <!-- Location breadcrumb only. The destination's own view renders the
             page heading, so repeating it here would duplicate the title. -->
        <ol class="flex min-w-0 items-center gap-1.5">
          <li class="flex-shrink-0">
            <span class="ui-kicker">{sectionLabel}</span>
          </li>
          <li aria-hidden="true" class="flex-shrink-0">
            <Icon name="chevron-right" size={16} class="text-text-subtle" />
          </li>
          <li class="min-w-0">
            <span
              class="block truncate text-[13px] text-text-muted"
              title={pageDescription || pageTitle || 'Dashboard'}
            >
              {pageTitle || 'Dashboard'}
            </span>
          </li>
        </ol>
      {/if}
    </nav>
  </div>

  <!-- Right: command palette, theme, changelog, account -->
  <div class="flex flex-shrink-0 items-center gap-1">
    <button
      type="button"
      onclick={() => (isPaletteOpen = true)}
      class="mr-2 hidden h-8 cursor-pointer items-center gap-2 rounded-brand border border-border-subtle bg-input pl-2.5 pr-1.5 font-code text-[12px] text-text-subtle transition-colors duration-150 ease-imperial hover:border-brass/50 hover:text-text-muted md:flex"
      aria-label="Open command palette"
      aria-keyshortcuts="Meta+K Control+K"
    >
      <span class="text-focus" aria-hidden="true">›</span>
      <span class="w-32 text-left lg:w-44">Jump to…</span>
      <kbd class="rounded-brand border border-border-subtle bg-surface-2 px-1.5 py-px text-[10px]">
        {modKey} K
      </kbd>
    </button>
    <button
      type="button"
      onclick={() => (isPaletteOpen = true)}
      class="flex size-9 cursor-pointer items-center justify-center rounded-brand text-text-muted transition-colors hover:bg-surface-2 hover:text-text-main md:hidden"
      aria-label="Open command palette"
    >
      <Icon name="search" size={20} />
    </button>

    <button
      type="button"
      onclick={toggleTheme}
      class="flex size-9 cursor-pointer items-center justify-center rounded-brand text-text-muted transition-colors hover:bg-surface-2 hover:text-text-main"
      title={isDark ? 'Switch to light theme' : 'Switch to dark theme'}
      aria-label={isDark ? 'Switch to light theme' : 'Switch to dark theme'}
    >
      <Icon name={isDark ? 'sun' : 'moon'} size={20} />
    </button>

    <button
      type="button"
      onclick={() => (isChangelogOpen = true)}
      class="flex h-9 cursor-pointer items-center gap-2 rounded-brand px-2 text-text-muted transition-colors hover:bg-surface-2 hover:text-text-main"
      title="Release notes"
    >
      <Icon name="history" size={20} />
      <span class="hidden text-[13px] xl:inline">Changelog</span>
      <span class="sr-only xl:hidden">Changelog</span>
    </button>

    <div class="relative" bind:this={accountMenuEl}>
      <button
        bind:this={accountButtonEl}
        type="button"
        onclick={() => (isAccountOpen = !isAccountOpen)}
        class="flex size-9 cursor-pointer items-center justify-center rounded-brand border border-transparent text-text-muted transition-colors hover:bg-surface-2 hover:text-text-main {isAccountOpen
          ? 'border-border-subtle bg-surface-2 text-text-main'
          : ''}"
        aria-label="Account and session"
        aria-haspopup="menu"
        aria-expanded={isAccountOpen}
        aria-controls="account-menu"
      >
        <Icon name="user" size={20} />
      </button>

      {#if isAccountOpen}
        <div
          id="account-menu"
          role="menu"
          aria-label="Account and session"
          data-popover-open="true"
          class="ui-panel absolute right-0 top-full z-50 mt-2 w-60 py-1.5 shadow-elevated"
        >
          <div class="border-b border-border-subtle px-4 pb-2.5 pt-1.5">
            <p class="font-code text-[10px] uppercase tracking-[0.16em] text-text-subtle">
              Session
            </p>
            <p class="mt-0.5 text-[13px] text-text-main">Local gateway operator</p>
          </div>

          <button
            type="button"
            role="menuitem"
            onclick={() => {
              closeAccountMenu()
              isChangelogOpen = true
            }}
            class="flex w-full cursor-pointer items-center gap-3 px-4 py-2 text-[13px] text-text-main transition-colors hover:bg-surface-2"
          >
            <span class="material-symbols-outlined text-[18px] text-text-subtle" aria-hidden="true">
              history
            </span>
            <span class="flex-1 text-left">Release notes</span>
          </button>

          <button
            type="button"
            role="menuitem"
            onclick={() => {
              closeAccountMenu()
              isDonateOpen = true
            }}
            class="flex w-full cursor-pointer items-center gap-3 px-4 py-2 text-[13px] text-text-main transition-colors hover:bg-surface-2"
          >
            <span class="material-symbols-outlined text-[18px] text-text-subtle" aria-hidden="true">
              volunteer_activism
            </span>
            <span class="flex-1 text-left">Support the project</span>
          </button>

          <div class="my-1 h-px bg-border-subtle"></div>

          <button
            type="button"
            role="menuitem"
            onclick={handleLogout}
            class="flex w-full cursor-pointer items-center gap-3 px-4 py-2 text-[13px] text-danger transition-colors hover:bg-danger/10"
          >
            <span class="material-symbols-outlined text-[18px]" aria-hidden="true">logout</span>
            <span class="flex-1 text-left">Sign out</span>
          </button>
        </div>
      {/if}
    </div>
  </div>
</header>

<Modal isOpen={isDonateOpen} onClose={() => (isDonateOpen = false)} title="Support 9router-go" size="lg">
  <div class="flex flex-col gap-5">
    <p class="text-sm leading-relaxed text-text-muted">
      9router-go is a fast, lightweight, open-source high-throughput AI gateway in Go. If it saves
      you time and tokens, consider supporting the project.
    </p>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <a
        href="https://github.com/luqman-v1/9router-go"
        target="_blank"
        rel="noopener noreferrer"
        class="flex items-center gap-3 rounded-brand border border-border-subtle bg-surface-2 p-3.5 transition-colors duration-150 ease-imperial hover:border-brass/50"
      >
        <span class="flex size-10 items-center justify-center rounded-brand border border-primary/30 bg-primary/10 text-primary">
          <span class="material-symbols-outlined text-[22px]" aria-hidden="true">star</span>
        </span>
        <span class="min-w-0">
          <span class="block text-sm font-semibold text-text-main">GitHub repository</span>
          <span class="block text-xs text-text-muted">Star and contribute</span>
        </span>
      </a>

      <a
        href="https://github.com/luqman-v1/9router-go/releases"
        target="_blank"
        rel="noopener noreferrer"
        class="flex items-center gap-3 rounded-brand border border-border-subtle bg-surface-2 p-3.5 transition-colors duration-150 ease-imperial hover:border-brass/50"
      >
        <span class="flex size-10 items-center justify-center rounded-brand border border-info/30 bg-info/10 text-info">
          <span class="material-symbols-outlined text-[22px]" aria-hidden="true">rocket_launch</span>
        </span>
        <span class="min-w-0">
          <span class="block text-sm font-semibold text-text-main">Releases and updates</span>
          <span class="block text-xs text-text-muted">Latest builds and notes</span>
        </span>
      </a>
    </div>
  </div>

  {#snippet footer()}
    <Button variant="secondary" onclick={() => (isDonateOpen = false)}>Close</Button>
  {/snippet}
</Modal>

<CommandPalette
  bind:open={isPaletteOpen}
  {navigate}
  {providers}
  {onSelectProvider}
  actions={paletteActions}
/>

<!-- Change Log Modal -->
<ChangelogModal isOpen={isChangelogOpen} onClose={() => (isChangelogOpen = false)} />

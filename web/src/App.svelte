<script lang="ts">
  import { onMount } from 'svelte'
  import { Loader2 } from 'lucide-svelte'
  import {
    api,
    onSessionExpired,
    type APIKey,
    type Combo,
    type ProviderConnection,
    type ProviderNode,
    type Settings
  } from './api/client'
  import AnalyticsView from './components/analytics/AnalyticsView.svelte'
  import ApiKeysView from './components/ApiKeysView.svelte'
  import CliToolsView from './components/CliToolsView.svelte'
  import CombosView from './components/combos/CombosView.svelte'
  import ConnectionsView from './components/connections/ConnectionsView.svelte'
  import OAuthCallbackView from './components/connections/OAuthCallbackView.svelte'
  import EndpointView from './components/EndpointView.svelte'
  import LoginView from './components/LoginView.svelte'
  import MediaKindView from './components/media/MediaKindView.svelte'
  import MediaProviderDetail from './components/media/MediaProviderDetail.svelte'
  import ProxyPoolsView from './components/ProxyPoolsView.svelte'
  import ProfileSettingsView from './components/ProfileSettingsView.svelte'
  import MediaWebView from './components/media/MediaWebView.svelte'
  import OverviewView from './components/OverviewView.svelte'
  import QuotaTrackerView from './components/QuotaTrackerView.svelte'
  import SkillsView from './components/SkillsView.svelte'
  import SettingsView from './components/SettingsView.svelte'
  import Sidebar, { sectionLabelFor } from './components/Sidebar.svelte'
  import Toasts from './lib/ui/Toasts.svelte'
  import TerminalView from './components/TerminalView.svelte'
  import TokenSaverView from './components/TokenSaverView.svelte'
  import TopBar from './components/TopBar.svelte'
  import BountyAssistView from './components/BountyAssistView.svelte'
  import PersonaView from './components/PersonaView.svelte'
  import FeatureFlagsView from './components/FeatureFlagsView.svelte'
  import { parseMediaProvider, parseProviderId, pathToTab, providerPath, mediaProviderPath, TAB_ROUTES, type ActiveTab, type MediaProviderRoute } from './lib/router'
  import { PROVIDER_CATALOG } from './lib/providers'
  import { PAGE_META } from './lib/pageMeta'
  import { getIconPath } from './components/connections/types'

  let activeTab = $state<ActiveTab>(
    typeof window !== 'undefined' ? pathToTab(window.location.pathname) : 'overview'
  )
  let connections = $state<ProviderConnection[]>([])
  let providerNodes = $state<ProviderNode[]>([])
  let combos = $state<Combo[]>([])
  let apiKeys = $state<APIKey[]>([])
  let settings = $state<Settings>({})
  let isLoading = $state(true)
  let isAuthChecking = $state(true)
  let isAuthenticatedState = $state(false)
  let requireLogin = $state(false)
  // True when the session ended mid-use (a dashboard call answered 401), so the
  // login page can say why it appeared instead of looking like a random logout.
  let sessionExpired = $state(false)
  let isCreateComboOpen = $state(false)
  let isMobileMenuOpen = $state(false)
  // The drawer markup is always mounted (it doubles as the desktop rail), so
  // below `lg` it must be inert while closed or its off-screen links would stay
  // in the tab order. Track the breakpoint so `inert` is never applied on the
  // desktop rail, where the same element is visible. Seeded from matchMedia so
  // the first paint already agrees with the viewport.
  let isDesktopViewport = $state(hasDesktopViewport())

  function hasDesktopViewport(): boolean {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return true
    return window.matchMedia('(min-width: 1024px)').matches
  }
  let selectedProviderId = $state<string | null>(
    typeof window !== 'undefined' ? parseProviderId(window.location.pathname) : null
  )
  let selectedMedia = $state<MediaProviderRoute | null>(
    typeof window !== 'undefined' ? parseMediaProvider(window.location.pathname) : null
  )
  // OAuth callback tab (provider redirect target): standalone page, no login
  // gate — it only hands the code over to the dashboard tab via storage.
  let isOAuthCallback = $state(
    typeof window !== 'undefined' && window.location.pathname.replace(/\/+$/, '') === '/callback'
  )


  function navigate(tab: ActiveTab, replace = false, providerId?: string | null) {
    activeTab = tab
    selectedMedia = null
    if (tab === 'connections') {
      selectedProviderId = providerId || null
      const path = providerId ? providerPath(providerId) : TAB_ROUTES.connections
      if (typeof window !== 'undefined' && window.location.pathname !== path) {
        if (replace) {
          window.history.replaceState({ tab, providerId }, '', path)
        } else {
          window.history.pushState({ tab, providerId }, '', path)
        }
      }
      return
    }
    selectedProviderId = null
    const path = TAB_ROUTES[tab]
    if (typeof window !== 'undefined' && window.location.pathname !== path) {
      if (replace) {
        window.history.replaceState({ tab }, '', path)
      } else {
        window.history.pushState({ tab }, '', path)
      }
    }
  }

  function navigateMediaProvider(kind: string, providerId: string, replace = false) {
    const path = mediaProviderPath(kind, providerId)
    activeTab = pathToTab(path)
    selectedMedia = { kind, providerId }
    selectedProviderId = null
    if (typeof window !== 'undefined' && window.location.pathname !== path) {
      // selectedMedia is a $state proxy, which history state cannot clone.
      const state = { tab: activeTab, media: { kind, providerId } }
      if (replace) {
        window.history.replaceState(state, '', path)
      } else {
        window.history.pushState(state, '', path)
      }
    }
  }

  async function loadData() {
    try {
      const [connsRes, nodesRes, combosRes, keysRes, settingsRes] = await Promise.all([
        api.getConnections().catch(() => []),
        api.getProviderNodes().catch(() => []),
        api.getCombos().catch(() => []),
        api.getApiKeys().catch(() => []),
        api.getSettings().catch(() => ({})),
      ])
      connections = connsRes
      providerNodes = nodesRes
      combos = combosRes
      apiKeys = keysRes
      settings = settingsRes
    } finally {
      isLoading = false
    }
  }

  async function checkAuth() {
    try {
      const authStatus = await api.checkRequireLogin()
      requireLogin = !!authStatus.requireLogin
      isAuthenticatedState = !!authStatus.authenticated || !requireLogin
      if (!isAuthenticatedState) {
        sessionStorage.removeItem('9router_auth')
        localStorage.removeItem('9router_auth')
      }
    } catch {
      // Fail closed, matching api.checkRequireLogin: an auth check that could
      // not be answered is not proof of a session, so show login rather than a
      // dashboard whose APIs would 401.
      requireLogin = true
      isAuthenticatedState = false
      sessionStorage.removeItem('9router_auth')
      localStorage.removeItem('9router_auth')
    } finally {
      isAuthChecking = false
    }
  }

  // A 401 from a dashboard API means the session ended mid-use (expired cookie,
  // restarted gateway, server-side invalidation). client.ts confirms it and
  // calls back once, so the shell cannot stay rendered against a dead session.
  function handleSessionExpired() {
    // A notice is only meaningful when the user was actually working; landing
    // on /login from a cold load is not a session ending.
    if (isAuthenticatedState || activeTab !== 'login') sessionExpired = true
    requireLogin = true
    isAuthenticatedState = false
    sessionStorage.removeItem('9router_auth')
    localStorage.removeItem('9router_auth')
    // The route is deliberately left alone. The auth gate below renders the
    // login view because requireLogin is true and the session is not
    // authenticated, and the URL still names the page the user was on — so
    // signing in again resumes exactly there instead of at the overview.
  }

  // --- Mobile drawer -------------------------------------------------------
  // The rail is the desktop sidebar; below `lg` it becomes a modal drawer that
  // owns Escape, background scroll and focus. Route state is untouched, so
  // browser back/forward and deep links keep working exactly as before.

  function openDrawer() {
    isMobileMenuOpen = true
  }

  function closeDrawer(restoreFocus = true) {
    if (!isMobileMenuOpen) return
    isMobileMenuOpen = false
    if (!restoreFocus || typeof document === 'undefined') return
    const trigger = document.getElementById('app-menu-trigger')
    // `offsetParent` is null when the trigger is display:none (i.e. we just
    // crossed into the desktop breakpoint), so only restore when it is visible.
    if (trigger instanceof HTMLElement && trigger.offsetParent !== null) trigger.focus()
  }

  function toggleDrawer() {
    if (isMobileMenuOpen) closeDrawer()
    else openDrawer()
  }

  // --- Desktop rail: collapsible and resizable, persisted per browser -------
  const SIDEBAR_WIDTH_KEY = '9router-sidebar-width'
  const SIDEBAR_COLLAPSED_KEY = '9router-sidebar-collapsed'
  const SIDEBAR_MIN = 208
  const SIDEBAR_MAX = 420
  const SIDEBAR_DEFAULT = 288
  const SIDEBAR_RAIL = 64

  function clampSidebar(w: number): number {
    return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, Math.round(w)))
  }

  function readStored(key: string): string | null {
    try {
      return typeof localStorage !== 'undefined' ? localStorage.getItem(key) : null
    } catch {
      return null
    }
  }

  let sidebarWidth = $state(clampSidebar(Number(readStored(SIDEBAR_WIDTH_KEY)) || SIDEBAR_DEFAULT))
  let sidebarCollapsed = $state(readStored(SIDEBAR_COLLAPSED_KEY) === '1')
  let isResizingSidebar = $state(false)
  let railCollapsed = $derived(sidebarCollapsed && isDesktopViewport)

  function persistSidebar() {
    try {
      localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth))
      localStorage.setItem(SIDEBAR_COLLAPSED_KEY, sidebarCollapsed ? '1' : '0')
    } catch {}
  }

  function toggleSidebarCollapsed() {
    sidebarCollapsed = !sidebarCollapsed
    persistSidebar()
  }

  function startSidebarResize(event: PointerEvent) {
    if (event.button !== 0) return
    event.preventDefault()
    const startX = event.clientX
    const startWidth = sidebarCollapsed ? SIDEBAR_RAIL : sidebarWidth
    isResizingSidebar = true
    const previousCursor = document.body.style.cursor
    document.body.style.cursor = 'col-resize'

    function onMove(e: PointerEvent) {
      const next = startWidth + (e.clientX - startX)
      // Dragging well past the minimum folds the rail; dragging out unfolds it.
      if (next < SIDEBAR_MIN - 64) {
        sidebarCollapsed = true
      } else {
        sidebarCollapsed = false
        sidebarWidth = clampSidebar(next)
      }
    }
    function onUp() {
      isResizingSidebar = false
      document.body.style.cursor = previousCursor
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
      persistSidebar()
    }
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
  }

  function handleResizeKeydown(event: KeyboardEvent) {
    const step = event.shiftKey ? 48 : 16
    if (event.key === 'ArrowLeft') {
      if (!sidebarCollapsed && sidebarWidth <= SIDEBAR_MIN) sidebarCollapsed = true
      else sidebarWidth = clampSidebar(sidebarWidth - step)
    } else if (event.key === 'ArrowRight') {
      if (sidebarCollapsed) sidebarCollapsed = false
      else sidebarWidth = clampSidebar(sidebarWidth + step)
    } else if (event.key === 'Enter' || event.key === ' ') {
      sidebarCollapsed = !sidebarCollapsed
    } else if (event.key === 'Home') {
      sidebarCollapsed = false
      sidebarWidth = SIDEBAR_MIN
    } else if (event.key === 'End') {
      sidebarCollapsed = false
      sidebarWidth = SIDEBAR_MAX
    } else {
      return
    }
    event.preventDefault()
    persistSidebar()
  }

  function resetSidebarWidth() {
    sidebarCollapsed = false
    sidebarWidth = SIDEBAR_DEFAULT
    persistSidebar()
  }

  // Any open overlay — our own dialogs, a `data-modal-open` host rendered by
  // lib/ui/Modal / ChangelogModal, or the TopBar's account popover — must
  // swallow Escape before the drawer does, otherwise dismissing a dialog would
  // also dismiss the navigation behind it. A DOM check is used rather than
  // event propagation because both handlers listen on `window`, where
  // stopPropagation between sibling listeners has no effect.
  function hasOpenOverlay(): boolean {
    if (typeof document === 'undefined') return false
    return (
      document.querySelector(
        '[data-modal-open="true"], [data-popover-open="true"], dialog[open]'
      ) !== null
    )
  }

  function handleGlobalKeydown(event: KeyboardEvent) {
    // Ctrl/Cmd+B folds the desktop rail (skipped while typing).
    if ((event.metaKey || event.ctrlKey) && !event.altKey && !event.shiftKey && event.key.toLowerCase() === 'b') {
      const el = event.target as HTMLElement | null
      const typing = !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
      if (isDesktopViewport && !typing && !hasOpenOverlay()) {
        event.preventDefault()
        toggleSidebarCollapsed()
      }
      return
    }
    if (event.key !== 'Escape') return
    if (!isMobileMenuOpen) return
    if (hasOpenOverlay()) return
    closeDrawer()
  }

  $effect(() => {
    if (typeof document === 'undefined' || !isMobileMenuOpen) return
    // Lock the page behind the drawer so a scroll gesture cannot move the
    // viewport underneath it.
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const frame = requestAnimationFrame(() => {
      const panel = document.getElementById('app-sidebar')
      // Only claim focus if nothing inside the rail already has it.
      if (!(panel instanceof HTMLElement)) return
      if (panel.contains(document.activeElement)) return
      panel.focus({ preventScroll: true })
    })
    return () => {
      cancelAnimationFrame(frame)
      document.body.style.overflow = previousOverflow
    }
  })

  onMount(() => {
    const unsubscribeSessionExpired = onSessionExpired(handleSessionExpired)
    checkAuth().then(() => {
      if (isAuthenticatedState && activeTab === 'login') {
        navigate('overview', true)
      }
    })
    loadData()

    const rawPath = window.location.pathname.replace(/\/+$/, '') || '/'
    if (rawPath === '/' || rawPath === '/dashboard') {
      window.history.replaceState({ tab: activeTab }, '', TAB_ROUTES[activeTab])
    }

    function handlePopState() {
      activeTab = pathToTab(window.location.pathname)
      selectedProviderId = parseProviderId(window.location.pathname)
      selectedMedia = parseMediaProvider(window.location.pathname)
      // Back/forward changes the route behind the drawer; drop it without
      // stealing focus, since the focus target may be gone.
      closeDrawer(false)
    }
    window.addEventListener('popstate', handlePopState)
    window.addEventListener('keydown', handleGlobalKeydown)

    const viewportQuery =
      typeof window.matchMedia === 'function' ? window.matchMedia('(min-width: 1024px)') : null
    if (viewportQuery) isDesktopViewport = viewportQuery.matches
    function handleViewportChange(event: MediaQueryListEvent) {
      isDesktopViewport = event.matches
      if (event.matches) closeDrawer(false)
    }
    viewportQuery?.addEventListener('change', handleViewportChange)

    const interval = setInterval(async () => {
      if (typeof document !== 'undefined' && document.hidden) return
      // Nothing to refresh once the shell has handed back to login; the login
      // view does its own probe, so polling a dead session would only add 401s.
      if (!isAuthenticatedState || activeTab === 'login') return
      try {
        const [connsRes, nodesRes] = await Promise.all([
          api.getConnections().catch(() => null),
          api.getProviderNodes().catch(() => null)
        ])
        if (connsRes) connections = connsRes
        if (nodesRes) providerNodes = nodesRes
      } catch {
        // silent refresh error
      }
    }, 10000)

    return () => {
      unsubscribeSessionExpired()
      clearInterval(interval)
      window.removeEventListener('popstate', handlePopState)
      window.removeEventListener('keydown', handleGlobalKeydown)
      viewportQuery?.removeEventListener('change', handleViewportChange)
    }
  })

  let activeConnectionsCount = $derived(connections.filter((c) => c.isActive === 1).length)
  let paletteProviders = $derived.by(() => {
    const ids = new Set([...connections.map((c) => c.provider), ...providerNodes.map((n) => n.id)])
    return [...ids]
      .map((id) => {
        const node = providerNodes.find((n) => n.id === id)
        const cat = PROVIDER_CATALOG.find((p) => p.id === id)
        return { id, name: node?.name || cat?.name || id, icon: getIconPath(id, node?.apiType) }
      })
      .sort((a, b) => a.name.localeCompare(b.name))
  })
  let selectedMediaCatalogItem = $derived.by(() => {
    if (!selectedMedia) return null
    return PROVIDER_CATALOG.find((p) => p.id === selectedMedia!.providerId) || null
  })
  let selectedProviderMeta = $derived.by(() => {
    if (selectedMedia && selectedMediaCatalogItem) {
      return {
        id: selectedMedia.providerId,
        name: selectedMediaCatalogItem.name || selectedMedia.providerId,
        icon: getIconPath(selectedMedia.providerId)
      }
    }
    if (activeTab !== 'connections' || !selectedProviderId) return null
    const node = providerNodes.find((n) => n.id === selectedProviderId)
    const cat = PROVIDER_CATALOG.find((p) => p.id === selectedProviderId)
    return {
      id: selectedProviderId,
      name: node?.name || cat?.name || selectedProviderId,
      icon: getIconPath(selectedProviderId, node?.apiType)
    }
  })
  // Every destination the shell can show, so the TopBar never falls back to a
  // generic title. `console-log` and `terminal` are the same page reached by
  // two tabs, so they share one entry.
  const pageMeta = PAGE_META

  function handleOpenNewCombo() {
    navigate('combos')
    isCreateComboOpen = true
  }
</script>

{#if isOAuthCallback}
  <OAuthCallbackView />
{:else if isAuthChecking}
  <div class="flex min-h-screen items-center justify-center bg-bg p-4">
    <div class="flex flex-col items-center gap-3 text-center">
      <div class="inline-block h-8 w-8 animate-spin rounded-full border-b-2 border-primary"></div>
      <p class="ui-kicker">Authenticating</p>
    </div>
  </div>
{:else if (requireLogin && !isAuthenticatedState) || activeTab === 'login'}
  <LoginView
    sessionExpired={sessionExpired}
    onSuccess={() => {
      sessionExpired = false
      isAuthenticatedState = true
      loadData()
      if (activeTab === 'login') {
        navigate('overview')
      }
    }}
  />
{:else}
  <div class="flex h-dvh w-full overflow-hidden bg-bg font-body text-text-main transition-colors duration-300">
    <Toasts />
    <a
      href="#main-content"
      class="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[60] focus:rounded-brand focus:border focus:border-border focus:bg-surface focus:px-3 focus:py-2 focus:text-sm focus:text-text-main focus:shadow-elevated"
    >
      Skip to content
    </a>

    <!-- Drawer scrim (mobile only). A real button keeps click semantics and
         keyboard reachability without inventing a fake role. -->
    {#if isMobileMenuOpen}
      <button
        type="button"
        tabindex="-1"
        aria-label="Close navigation"
        class="fixed inset-0 z-40 cursor-default bg-black/60 backdrop-blur-sm lg:hidden"
        onclick={() => closeDrawer()}
      ></button>
    {/if}

    <!-- Left rail: static sidebar on desktop, modal drawer below lg -->
    <div
      id="app-sidebar"
      tabindex="-1"
      inert={!isMobileMenuOpen && !isDesktopViewport}
      style="--sidebar-w: {railCollapsed ? SIDEBAR_RAIL : sidebarWidth}px"
      class="fixed inset-y-0 left-0 z-50 w-72 flex-shrink-0 outline-none transition-transform duration-300 ease-out lg:relative lg:z-auto lg:w-[var(--sidebar-w)] lg:translate-x-0 {isResizingSidebar
        ? 'lg:transition-none'
        : 'lg:transition-[width] lg:duration-200'} {isMobileMenuOpen
        ? 'translate-x-0 shadow-elevated lg:shadow-none'
        : '-translate-x-full'}"
    >
      <Sidebar
        bind:activeTab
        navigate={(tab, replace) => {
          navigate(tab, replace, null)
        }}
        activeConnections={activeConnectionsCount}
        totalConnections={connections.length}
        collapsed={railCollapsed}
        onToggleCollapse={toggleSidebarCollapsed}
        onClose={() => closeDrawer()}
      />
      {#if isDesktopViewport}
        <!-- A focusable separator is the ARIA window-splitter pattern. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize sidebar (double-click to reset)"
          aria-valuemin={SIDEBAR_MIN}
          aria-valuemax={SIDEBAR_MAX}
          aria-valuenow={railCollapsed ? SIDEBAR_RAIL : sidebarWidth}
          tabindex="0"
          title="Drag to resize · double-click to reset · Ctrl/⌘+B to collapse"
          onpointerdown={startSidebarResize}
          ondblclick={resetSidebarWidth}
          onkeydown={handleResizeKeydown}
          class="group absolute inset-y-0 right-0 z-10 w-1.5 cursor-col-resize touch-none outline-none"
        >
          <span
            class="absolute inset-y-0 left-1/2 w-px -translate-x-1/2 transition-colors {isResizingSidebar
              ? 'bg-primary'
              : 'bg-transparent group-hover:bg-brass/60 group-focus-visible:bg-primary'}"
          ></span>
        </div>
      {/if}
    </div>

    <!-- Main Viewport (TopBar + Scrollable Canvas) -->
    <div class="relative isolate flex h-full min-w-0 flex-1 flex-col">
      <!-- Faint grid background (upstream landing-grid) -->
      <div class="landing-grid pointer-events-none absolute inset-0 -z-10" aria-hidden="true"></div>

      <TopBar
        {activeTab}
        sectionLabel={sectionLabelFor(activeTab)}
        pageTitle={pageMeta[activeTab]?.title}
        pageDescription={pageMeta[activeTab]?.description}
        selectedProvider={selectedProviderMeta}
        menuOpen={isMobileMenuOpen}
        providers={paletteProviders}
        navigate={(tab) => navigate(tab)}
        onSelectProvider={(id) => navigate('connections', false, id)}
        onBackToProviders={() => navigate('connections', false, null)}
        onMenuClick={toggleDrawer}
        onLogout={() => {
          closeDrawer(false)
          isAuthenticatedState = false
          navigate('login')
        }}
      />

      <main
        id="main-content"
        class="custom-scrollbar relative flex-1 overflow-y-auto px-4 py-6 sm:px-6 sm:py-8 lg:px-10 lg:py-10"
      >
        <div class="mx-auto w-full min-w-0 max-w-[1440px]">
          {#if isLoading}
            <div class="flex h-[60vh] flex-col items-center justify-center gap-3 text-text-muted">
              <Loader2 class="h-7 w-7 animate-spin text-primary" />
              <span class="font-code text-xs">Connecting to 9router-go Localhost Gateway...</span>
            </div>
          {:else}
            {#if activeTab === 'overview'}
              <OverviewView
                {connections}
                {providerNodes}
                {combos}
                {apiKeys}
                {settings}
                navigate={(tab) => navigate(tab)}
                onSelectProvider={(id) => navigate('connections', false, id)}
                onCreateCombo={handleOpenNewCombo}
              />
            {:else if activeTab === 'endpoint'}
              <EndpointView {apiKeys} {settings} onRefresh={loadData} />
            {:else if activeTab === 'connections'}
              <ConnectionsView
                {connections}
                {providerNodes}
                onRefresh={loadData}
                bind:selectedProviderId
                onSelectProvider={(id) => navigate('connections', false, id)}
                onBackToOverview={() => navigate('connections', false, null)}
              />
            {:else if activeTab === 'combos'}
              <CombosView {combos} {connections} {providerNodes} onRefresh={loadData} bind:isCreatingOpen={isCreateComboOpen} />
            {:else if activeTab === 'analytics'}
              <AnalyticsView {connections} {providerNodes} />
            {:else if activeTab === 'quota'}
              <QuotaTrackerView {connections} />
            {:else if activeTab === 'token-saver'}
              <TokenSaverView {settings} onRefresh={loadData} />
            {:else if activeTab === 'cli-tools'}
              <CliToolsView {apiKeys} {connections} onRefresh={loadData} />
            {:else if activeTab === 'keys'}
              <ApiKeysView {apiKeys} onRefresh={loadData} />
            {:else if activeTab === 'bounty'}
              <BountyAssistView />
            {:else if activeTab === 'personas'}
              <PersonaView />
            {:else if activeTab === 'feature-flags'}
              <FeatureFlagsView />
            {:else if activeTab === 'media-embedding'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind="embedding"
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-embedding')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaKindView
                  kind="embedding"
                  {connections}
                  {apiKeys}
                  {settings}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(k, id) => navigateMediaProvider(k, id)}
                />
              {/if}
            {:else if activeTab === 'media-image'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind="image"
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-image')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaKindView
                  kind="image"
                  {connections}
                  {apiKeys}
                  {settings}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(k, id) => navigateMediaProvider(k, id)}
                />
              {/if}
            {:else if activeTab === 'media-tts'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind="tts"
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-tts')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaKindView
                  kind="tts"
                  {connections}
                  {apiKeys}
                  {settings}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(k, id) => navigateMediaProvider(k, id)}
                />
              {/if}
            {:else if activeTab === 'media-stt'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind="stt"
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-stt')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaKindView
                  kind="stt"
                  {connections}
                  {apiKeys}
                  {settings}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(k, id) => navigateMediaProvider(k, id)}
                />
              {/if}
            {:else if activeTab === 'media-video'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind="video"
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-video')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaKindView
                  kind="video"
                  {connections}
                  {apiKeys}
                  {settings}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(k, id) => navigateMediaProvider(k, id)}
                />
              {/if}
            {:else if activeTab === 'media-systemone'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind={selectedMedia.kind as any}
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-systemone')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaKindView
                  kind="systemone"
                  {connections}
                  {apiKeys}
                  {settings}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(k, id) => navigateMediaProvider(k, id)}
                />
              {/if}
            {:else if activeTab === 'media-web'}
              {#if selectedMedia && selectedMediaCatalogItem}
                <MediaProviderDetail
                  provider={selectedMediaCatalogItem}
                  kind={selectedMedia.kind as any}
                  {connections}
                  {apiKeys}
                  {settings}
                  onBack={() => {
                    selectedMedia = null
                    navigate('media-web')
                  }}
                  onRefresh={loadData}
                />
              {:else}
                <MediaWebView
                  {connections}
                  {combos}
                  onRefresh={loadData}
                  onSelectProvider={(kind, id) => navigateMediaProvider(kind, id)}
                />
              {/if}
            {:else if activeTab === 'proxy-pools'}
              <ProxyPoolsView />
            {:else if activeTab === 'skills'}
              <SkillsView />
            {:else if activeTab === 'console-log' || activeTab === 'terminal'}
              <TerminalView />
            {:else if activeTab === 'settings'}
              <ProfileSettingsView {settings} onRefresh={loadData} />
            {/if}
          {/if}
        </div>
      </main>
    </div>
  </div>
{/if}

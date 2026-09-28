<script lang="ts">
  import { onMount } from 'svelte'
  import {
    AlertCircle,
    AlertTriangle,
    Check,
    CloudUpload,
    Copy,
    ExternalLink,
    Eye,
    EyeOff,
    Key,
    Loader2,
    Power,
    Radio,
    Shield,
    Trash2
  } from 'lucide-svelte'
  import Badge from '../lib/ui/Badge.svelte'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import ConfirmModal from '../lib/ui/ConfirmModal.svelte'
  import Modal from '../lib/ui/Modal.svelte'
  import Toggle from '../lib/ui/Toggle.svelte'
  import { api, type APIKey, type Settings, type TunnelStatusResponse } from '../api/client'

  interface Props {
    apiKeys?: APIKey[]
    settings?: Settings
    onRefresh?: () => void
  }

  let {
    apiKeys = [],
    settings = {},
    onRefresh
  }: Props = $props()

  // Local state for keys & settings
  let localKeys = $state<APIKey[]>([])
  let requireApiKey = $state(false)
  let requireLogin = $state(true)
  let hasPassword = $state(true)
  let tunnelDashboardAccess = $state(false)
  let endpointSettingsError = $state('')
  let tunnelDashboardAccessError = $state('')
  let copiedId = $state<string | null>(null)
  let shownKeyIds = $state<Set<string>>(new Set())

  // Origin resolution (SSR fallback uses the Go default port 20130)
  let localOrigin = $state('http://localhost:20130')
  let localEndpoint = $derived(`${localOrigin}/v1`)
  onMount(() => {
    if (typeof window !== 'undefined') {
      localOrigin = window.location.origin
    }
  })
  // Tunnel state
  let tunnelEnabled = $state(false)
  let tunnelRunning = $state(false)
  let tunnelReachable = $state(false)
  let tunnelEverReachable = $state(false)
  let tunnelMissCount = 0
  let tunnelUrl = $state('')
  let publicUrl = $state('')
  let isTunnelLoading = $state(false)
  let tunnelStatusText = $state('')
  let tunnelError = $state<string | null>(null)
  let showEnableTunnelModal = $state(false)
  let showDisableTunnelModal = $state(false)

  // Tailscale state
  let tailscaleEnabled = $state(false)
  let tailscaleRunning = $state(false)
  let tailscaleReachable = $state(false)
  let tailscaleEverReachable = $state(false)
  let tailscaleMissCount = 0
  let tailscaleUrl = $state('')
  let isTailscaleLoading = $state(false)
  let tailscaleStatusText = $state('')
  let tailscaleAuthUrl = $state('')
  let tailscaleError = $state<string | null>(null)
  // Set when Tailscale answers funnelNotEnabled: the tailnet has Funnel switched
  // off, so the server hands back the console URL that turns it on. The notice
  // is rendered below the Tailscale row; the empty string means "no notice".
  let tailscaleFunnelEnableUrl = $state('')
  let tailscaleFunnelNotice = $state('')
  let tailscaleInstalled = $state<boolean | null>(null)
  let showTailscaleModal = $state(false)
  let showDisableTailscaleModal = $state(false)

  // Keys modal state
  let isCreateKeyOpen = $state(false)
  let newKeyName = $state('')
  let isSubmittingKey = $state(false)
  let newlyCreatedKey = $state<string | null>(null)

  // Confirmation modal
  let confirmModal = $state<{
    title: string
    message: string
    confirmLabel?: string
    isDanger?: boolean
    onConfirm: () => void
  } | null>(null)

  // Sync props to state
  $effect(() => {
    if (apiKeys && apiKeys.length > 0) {
      localKeys = [...apiKeys]
    }
  })
  $effect(() => {
    if (settings) {
      requireApiKey = !!settings.requireApiKey
      tunnelDashboardAccess = !!settings.tunnelDashboardAccess
      if (typeof settings.requireLogin === 'boolean') requireLogin = settings.requireLogin
      if (typeof settings.hasPassword === 'boolean') hasPassword = settings.hasPassword
    }
  })

  // Browser-side health probe: must reach origin (not just CF/TS edge).
  // /api/health sets Access-Control-Allow-Origin: * so CORS works through tunnel.
  async function clientPingUrl(url: string): Promise<boolean> {
    if (!url) return false
    try {
      const controller = new AbortController()
      const timeoutId = setTimeout(() => controller.abort(), 5000)
      const res = await fetch(`${url}/api/health`, {
        mode: 'cors',
        cache: 'no-store',
        signal: controller.signal,
      })
      clearTimeout(timeoutId)
      return res.ok
    } catch {
      return false
    }
  }

  function markReachable(ok: boolean, kind: 'tunnel' | 'tailscale') {
    // Debounce reachable=false: the server may briefly return false during a
    // background refresh, so only flip after 5 consecutive misses (upstream).
    if (kind === 'tunnel') {
      if (ok) {
        tunnelMissCount = 0
        tunnelReachable = true
        tunnelEverReachable = true
      } else if (++tunnelMissCount >= 5) {
        tunnelReachable = false
      }
    } else {
      if (ok) {
        tailscaleMissCount = 0
        tailscaleReachable = true
        tailscaleEverReachable = true
      } else if (++tailscaleMissCount >= 5) {
        tailscaleReachable = false
      }
    }
  }

  // Load status
  async function loadStatus() {
    try {
      const [keysRes, settingsRes, tunnelRes] = await Promise.all([
        api.getApiKeys().catch(() => null),
        api.getSettings().catch(() => null),
        api.getTunnelStatus().catch(() => null)
      ])

      if (keysRes) {
        localKeys = keysRes
      }
      if (settingsRes) {
        requireApiKey = !!settingsRes.requireApiKey
        tunnelDashboardAccess = !!settingsRes.tunnelDashboardAccess
        if (typeof settingsRes.requireLogin === 'boolean') requireLogin = settingsRes.requireLogin
        if (typeof settingsRes.hasPassword === 'boolean') hasPassword = settingsRes.hasPassword
      }
      if (tunnelRes) {
        applyTunnelStatus(tunnelRes)
      }
    } catch {
      // silent
    }
  }

  function applyTunnelStatus(status: TunnelStatusResponse) {
    if (status.tunnel) {
      tunnelEnabled = !!(status.tunnel.settingsEnabled ?? status.tunnel.enabled)
      tunnelRunning = !!status.tunnel.running
      tunnelUrl = status.tunnel.tunnelUrl || ''
      publicUrl = status.tunnel.publicUrl || ''
    }
    if (status.tailscale) {
      tailscaleEnabled = !!(status.tailscale.settingsEnabled ?? status.tailscale.enabled)
      tailscaleRunning = !!status.tailscale.running
      tailscaleUrl = status.tailscale.tunnelUrl || ''
    }
  }

  onMount(() => {
    loadStatus()
    // Status poll only while degraded; healthy tunnels rely on the browser
    // ping below (upstream STATUS_POLL_FAST_MS behaviour).
    const statusTimer = setInterval(async () => {
      if (typeof document !== 'undefined' && document.hidden) return
      const tunnelHealthy = !tunnelEnabled || tunnelReachable
      const tsHealthy = !tailscaleEnabled || tailscaleReachable
      if (tunnelHealthy && tsHealthy) return
      try {
        const res = await api.getTunnelStatus()
        if (res) applyTunnelStatus(res)
      } catch {}
    }, 5000)
    // Browser-side ping: probe tunnel/tailscale URLs directly every 10s.
    const pingTimer = setInterval(async () => {
      if (typeof document !== 'undefined' && document.hidden) return
      if (tunnelEnabled && (tunnelUrl || publicUrl)) {
        const direct = tunnelUrl ? clientPingUrl(tunnelUrl) : Promise.resolve(false)
        const pub = publicUrl ? clientPingUrl(publicUrl) : Promise.resolve(false)
        markReachable((await direct) || (await pub), 'tunnel')
      }
      if (tailscaleEnabled && tailscaleUrl) {
        markReachable(await clientPingUrl(tailscaleUrl), 'tailscale')
      }
    }, 10000)
    return () => {
      clearInterval(statusTimer)
      clearInterval(pingTimer)
    }
  })

  function copy(text: string, id: string) {
    navigator.clipboard.writeText(text)
    copiedId = id
    setTimeout(() => {
      if (copiedId === id) copiedId = null
    }, 2000)
  }

  function toggleShowKey(id: string) {
    const next = new Set(shownKeyIds)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    shownKeyIds = next
  }

  function maskKey(key: string): string {
    if (!key) return ''
    if (key.length <= 10) return key
    return key.slice(0, 6) + '••••••' + key.slice(-4)
  }

  // Toggle Require API Key
  async function toggleRequireApiKey(value: boolean) {
    requireApiKey = value
    endpointSettingsError = ''
    try {
      await api.updateSettings({ requireApiKey: value })
      onRefresh?.()
    } catch (err) {
      requireApiKey = !value
      endpointSettingsError = err instanceof Error ? err.message : String(err)
    }
  }

  // Toggle Tunnel Dashboard Access
  async function toggleTunnelDashboardAccess(value: boolean) {
    tunnelDashboardAccess = value
    tunnelDashboardAccessError = ''
    try {
      await api.updateSettings({ tunnelDashboardAccess: value })
      onRefresh?.()
    } catch (err) {
      tunnelDashboardAccess = !value
      tunnelDashboardAccessError = err instanceof Error ? err.message : String(err)
    }
  }

  // Security gate: block remote exposure while dashboard uses default
  // password or login is off (upstream isLoginUnsafe).
  let isLoginUnsafe = $derived(!requireLogin || !hasPassword)
  let unsafeReason = $derived(
    !requireLogin
      ? 'Enable "Require login" and set a custom password before activating the tunnel.'
      : 'Change the default dashboard password before activating the tunnel.'
  )

  // Start Cloudflare Tunnel
  async function startTunnel() {
    showEnableTunnelModal = false
    isTunnelLoading = true
    tunnelStatusText = 'Creating tunnel...'
    tunnelError = null
    tunnelRowNotice = ''

    try {
      const res = await api.enableTunnel()
      if (res.error) {
        tunnelError = res.error
        return
      }
      if (res.tunnelUrl) {
        tunnelUrl = res.tunnelUrl
        publicUrl = res.publicUrl || ''
        tunnelEnabled = true
        tunnelRunning = true
      }
    } catch (err) {
      tunnelError = err instanceof Error ? err.message : String(err)
    } finally {
      isTunnelLoading = false
      tunnelStatusText = ''
    }
  }

  // A failed disable leaves the row in its connected state, where the row's own
  // error branch cannot render (that branch needs the tunnel to be off). Each
  // row therefore gets a line of its own underneath, carrying the server's
  // reason; the row keeps its URL and Disable button as the retry path.
  let tunnelRowNotice = $state('')
  let tailscaleRowNotice = $state('')

  // Disable Cloudflare Tunnel
  async function stopTunnel() {
    showDisableTunnelModal = false
    isTunnelLoading = true
    tunnelError = null
    tunnelRowNotice = ''
    try {
      await api.disableTunnel()
      tunnelEnabled = false
      tunnelRunning = false
      tunnelUrl = ''
      publicUrl = ''
    } catch (err) {
      // The disable write did not land, so the tunnel is still configured.
      // Leave the state alone (the row keeps its URL and Disable button) and
      // report the failure instead of showing "Not connected".
      tunnelRowNotice = err instanceof Error ? err.message : String(err)
    } finally {
      isTunnelLoading = false
    }
  }

  // Tailscale handlers
  async function handleTailscaleClick() {
    if (isLoginUnsafe) {
      tailscaleError = `Security required: ${unsafeReason}`
      return
    }
    tailscaleError = null
    try {
      const check = await api.checkTailscale()
      tailscaleInstalled = !!check.installed
      if (check.installed) {
        startTailscale()
      } else {
        showTailscaleModal = true
      }
    } catch {
      showTailscaleModal = true
    }
  }

  async function startTailscale() {
    showTailscaleModal = false
    isTailscaleLoading = true
    tailscaleStatusText = 'Connecting to Tailscale...'
    tailscaleAuthUrl = ''
    tailscaleFunnelEnableUrl = ''
    tailscaleFunnelNotice = ''
    tailscaleRowNotice = ''
    tailscaleError = null

    try {
      const res = await api.enableTailscale()
      if (res.error) {
        tailscaleError = res.error
        return
      }
      if (res.needsLogin && res.authUrl) {
        tailscaleAuthUrl = res.authUrl
        return
      }
      // Funnel is off for the whole tailnet. Upstream returns this as a plain
      // success:false with no message, so it used to render as nothing at all —
      // the operator pressed Enable, the row silently stayed "Not connected",
      // and there was no way to learn why. Surface the reason below the row
      // (with the console URL that turns Funnel on) and keep the row's Enable
      // button as the retry path.
      if (res.funnelNotEnabled) {
        tailscaleFunnelNotice = 'Tailscale Funnel is not enabled for your tailnet. Turn it on in the Tailscale admin console, then enable Tailscale here again.'
        tailscaleFunnelEnableUrl = res.enableUrl || ''
        return
      }
      if (res.tunnelUrl) {
        tailscaleUrl = res.tunnelUrl
        tailscaleEnabled = true
        tailscaleRunning = true
        tailscaleFunnelEnableUrl = ''
        tailscaleFunnelNotice = ''
      }
    } catch (err) {
      tailscaleError = err instanceof Error ? err.message : String(err)
    } finally {
      isTailscaleLoading = false
      tailscaleStatusText = ''
    }
  }

  async function stopTailscale() {
    showDisableTailscaleModal = false
    isTailscaleLoading = true
    tailscaleError = null
    tailscaleRowNotice = ''
    try {
      await api.disableTailscale()
      tailscaleEnabled = false
      tailscaleRunning = false
      tailscaleUrl = ''
    } catch (err) {
      // The disable write failed, so the funnel is still advertised. Keep the
      // card as-is and say so instead of pretending it was stopped.
      tailscaleRowNotice = err instanceof Error ? err.message : String(err)
    } finally {
      isTailscaleLoading = false
    }
  }

  // Keys management
  function closeCreateKeyModal() {
    isCreateKeyOpen = false
    newKeyName = ''
  }

  async function handleCreateKey(e: SubmitEvent) {
    e.preventDefault()
    if (!newKeyName.trim()) return

    isSubmittingKey = true
    try {
      const res = await api.createApiKey({ name: newKeyName.trim() })
      if (res.key) {
        newlyCreatedKey = res.key
        newKeyName = ''
        isCreateKeyOpen = false
        await loadStatus()
        onRefresh?.()
      }
    } catch (err) {
      alert(`Failed to create key: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      isSubmittingKey = false
    }
  }

  async function handleToggleKey(key: APIKey, nextActive: boolean) {
    if (!nextActive && (key.isActive === 1 || key.isActive === true)) {
      confirmModal = {
        title: 'Pause API Key',
        message: `Pause API key "${key.name || 'API Key'}"? This key will stop working immediately but can be resumed later.`,
        confirmLabel: 'Pause Key',
        isDanger: false,
        onConfirm: async () => {
          confirmModal = null
          await executeToggleKey(key.id, false)
        }
      }
    } else {
      await executeToggleKey(key.id, nextActive)
    }
  }

  async function executeToggleKey(id: string, active: boolean) {
    try {
      await api.toggleApiKey(id, active)
      localKeys = localKeys.map((k) => (k.id === id ? { ...k, isActive: active ? 1 : 0 } : k))
      onRefresh?.()
    } catch (err) {
      alert(`Failed to toggle key: ${err instanceof Error ? err.message : String(err)}`)
    }
  }

  function handleDeleteKey(key: APIKey) {
    confirmModal = {
      title: 'Delete API Key',
      message: `Are you sure you want to permanently delete API key "${key.name || 'API Key'}"?`,
      confirmLabel: 'Delete Key',
      isDanger: true,
      onConfirm: async () => {
        confirmModal = null
        try {
          await api.deleteApiKey(key.id)
          localKeys = localKeys.filter((k) => k.id !== key.id)
          onRefresh?.()
        } catch (err) {
          alert(`Failed to delete key: ${err instanceof Error ? err.message : String(err)}`)
        }
      }
    }
  }

  const tunnelValueProps = [
    { icon: 'public', title: 'Access Anywhere', desc: 'Use your API from any network' },
    { icon: 'group', title: 'Share Endpoint', desc: 'Share URL with team members' },
    { icon: 'code', title: 'Use in Cursor/Cline', desc: 'Connect AI tools remotely' },
    { icon: 'lock', title: 'Encrypted', desc: 'End-to-end TLS via Cloudflare' },
  ]

  // One honest headline for the whole page: is this endpoint actually usable?
  // "Open" is reserved for the deliberate no-auth state, never for "unknown".
  type EndpointTone = 'success' | 'warning' | 'danger' | 'neutral'

  let activeKeyCount = $derived(localKeys.filter((k) => k.isActive === 1 || k.isActive === true).length)
  let keyCountLabel = $derived(
    localKeys.length === 0
      ? 'No keys yet'
      : activeKeyCount === 0
        ? `${localKeys.length} ${localKeys.length === 1 ? 'key' : 'keys'}, all paused`
        : activeKeyCount === localKeys.length
          ? `${activeKeyCount} active ${activeKeyCount === 1 ? 'key' : 'keys'}`
          : `${activeKeyCount} of ${localKeys.length} keys active`
  )
  let hasPublicRoute = $derived(tunnelRunning || tailscaleRunning)

  let endpointState = $derived.by((): { label: string; detail: string; tone: EndpointTone } => {
    if (hasPublicRoute && !requireApiKey) {
      return {
        label: 'Exposed without a key',
        detail: 'A tunnel is live and "Require API key" is off, so anyone who can reach the URL can send requests.',
        tone: 'danger',
      }
    }
    if (isLoginUnsafe) {
      return {
        label: 'Dashboard login not hardened',
        detail: 'The dashboard itself is reachable with weak credentials. Harden login before exposing it remotely.',
        tone: 'warning',
      }
    }
    if (!requireApiKey) {
      return {
        label: 'Open endpoint',
        detail: 'No key is required — any process that can reach this host may send requests.',
        tone: 'warning',
      }
    }
    if (activeKeyCount === 0) {
      return {
        label: 'No active key',
        detail: 'Key checking is on but every key is paused, so requests will be rejected.',
        tone: 'danger',
      }
    }
    return {
      label: 'Ready for requests',
      detail: `${keyCountLabel} can authenticate against this endpoint.`,
      tone: 'success',
    }
  })

  const endpointStateTone: Record<EndpointTone, string> = {
    success: 'text-success',
    warning: 'text-warning',
    danger: 'text-danger',
    neutral: 'text-text-muted',
  }

  const endpointStateDot: Record<EndpointTone, string> = {
    success: 'bg-success',
    warning: 'bg-warning',
    danger: 'bg-danger',
    neutral: 'bg-text-subtle',
  }
</script>

<div class="flex flex-col gap-6">
  <!-- TOP SECTION: API Endpoint Card -->
  <Card padding="md" class="space-y-4">
    <div class="mb-2 flex flex-wrap items-start justify-between gap-3">
      <div class="flex min-w-0 items-start gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-brand border border-border-subtle bg-surface-2 text-primary">
          <Radio class="w-5 h-5" />
        </div>
        <div class="min-w-0">
          <span class="ui-kicker">I · Gateway</span>
          <h2 class="mt-1 font-headline text-lg font-medium leading-tight text-text-main">
            API Endpoint
          </h2>
          <p class="mt-0.5 text-xs leading-relaxed text-text-muted">
            Direct, secure connection points to your local AI router
          </p>
        </div>
      </div>
      <div class="flex flex-col items-start gap-1 sm:items-end">
        <Badge tone={endpointState.tone}>{endpointState.label}</Badge>
        <span class="text-[11px] text-text-subtle">{keyCountLabel}</span>
      </div>
    </div>

    <!-- Connection state: one plain sentence explaining the badge above. -->
    <div class="flex items-start gap-2 rounded-brand border border-border-subtle bg-surface-2/60 px-3 py-2">
      <span class="mt-1.5 size-1.5 shrink-0 rounded-full {endpointStateDot[endpointState.tone]}"></span>
      <p class="min-w-0 flex-1 text-xs leading-relaxed text-text-muted">
        <span class="font-semibold {endpointStateTone[endpointState.tone]}">{endpointState.label}.</span>
        {endpointState.detail}
      </p>
    </div>

    <div class="flex flex-col gap-3">
      <!-- Local Endpoint Field -->
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
        <span class="w-full shrink-0 rounded-brand border border-border bg-surface-2 px-2 py-1 text-center font-code text-[11px] font-semibold uppercase tracking-wider text-text-muted sm:w-[104px]">
          Local
        </span>
        <div class="flex min-w-0 flex-1 items-center gap-2">
          <input
            type="text"
            value={localEndpoint}
            readonly
            class="ui-input flex-1 font-code text-xs selection:bg-primary/25"
          />
          <button
            type="button"
            onclick={() => copy(localEndpoint, 'local_url')}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-border text-text-muted transition-colors hover:border-primary/50 hover:text-primary"
            title="Copy Local URL"
          >
            {#if copiedId === 'local_url'}
              <Check class="w-4 h-4 text-success" />
            {:else}
              <Copy class="w-4 h-4" />
            {/if}
          </button>
        </div>
      </div>

      <!-- Tunnel (Cloudflare Quick Tunnel) -->
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
        <span
          class="w-full shrink-0 rounded-brand border px-2 py-1 text-center font-code text-[11px] font-semibold uppercase tracking-wider transition-colors sm:w-[104px] {tunnelRunning && tunnelUrl
            ? 'border-primary/40 bg-primary/10 text-primary'
            : 'border-border bg-surface-2 text-text-muted'}"
        >
          Tunnel
        </span>

        <div class="flex min-w-0 flex-1 flex-wrap items-center gap-2">
        {#if tunnelEnabled && !isTunnelLoading && tunnelReachable}
          <input
            type="text"
            value="{publicUrl || tunnelUrl}/v1"
            readonly
            class="ui-input min-w-[160px] flex-1 font-code text-xs selection:bg-primary/25"
          />
          <button
            type="button"
            onclick={() => copy(`${publicUrl || tunnelUrl}/v1`, 'tunnel_url')}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-border text-text-muted transition-colors hover:border-primary/50 hover:text-primary"
            title="Copy Tunnel URL"
          >
            {#if copiedId === 'tunnel_url'}
              <Check class="w-4 h-4 text-success" />
            {:else}
              <Copy class="w-4 h-4" />
            {/if}
          </button>
          <button
            type="button"
            onclick={() => (showDisableTunnelModal = true)}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-danger/30 text-danger transition-colors hover:bg-danger/10"
            title="Disable Tunnel"
          >
            <Power class="w-4 h-4" />
          </button>
        {:else if tunnelEnabled && !isTunnelLoading && !tunnelReachable}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
            <Loader2 class="w-4 h-4 animate-spin shrink-0" />
            <span>{tunnelEverReachable ? 'Tunnel reconnecting...' : 'Tunnel checking...'}</span>
          </div>
          <button
            type="button"
            onclick={() => (showDisableTunnelModal = true)}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-danger/30 text-danger transition-colors hover:bg-danger/10"
            title="Disable Tunnel"
          >
            <Power class="w-4 h-4" />
          </button>
        {:else if isTunnelLoading}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-border bg-surface-2 px-3 py-2 text-xs text-text-muted">
            <Loader2 class="w-4 h-4 animate-spin text-primary shrink-0" />
            <span>{tunnelStatusText || 'Creating tunnel...'}</span>
          </div>
          <button
            type="button"
            onclick={() => (isTunnelLoading = false)}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-border text-danger transition-colors hover:bg-danger/10"
            title="Cancel"
          >
            <Power class="w-4 h-4" />
          </button>
        {:else if tunnelError}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
            <AlertCircle class="w-4 h-4 shrink-0" />
            <span class="truncate">{tunnelError}</span>
          </div>
          <button
            type="button"
            onclick={() => (showEnableTunnelModal = true)}
            class="flex shrink-0 cursor-pointer items-center gap-1.5 rounded-brand border border-primary/40 px-3 py-2 text-xs font-semibold text-primary transition-colors hover:bg-primary/10"
          >
            <CloudUpload class="w-3.5 h-3.5" />
            <span>Enable</span>
          </button>
        {:else}
          <div class="ui-input flex min-w-[160px] flex-1 items-center font-code text-xs text-text-muted">
            <span>Not connected</span>
          </div>
          <button
            type="button"
            onclick={() => {
              if (isLoginUnsafe) {
                tunnelError = `Security required: ${unsafeReason}`
              } else if (!requireApiKey) {
                tunnelError = 'Security required: Enable "Require API key" before activating the tunnel.'
              } else {
                showEnableTunnelModal = true
              }
            }}
            class="flex shrink-0 cursor-pointer items-center gap-1.5 rounded-brand border border-primary/40 px-3 py-2 text-xs font-semibold text-primary transition-colors hover:bg-primary/10"
          >
            <CloudUpload class="w-3.5 h-3.5" />
            <span>Enable</span>
          </button>
        {/if}
        </div>
      </div>

      <!-- A failed disable leaves the tunnel connected, so the row above cannot
           show it. This line carries the server's reason and the row's own
           Disable button stays as the retry path. -->
      {#if tunnelRowNotice}
        <div class="flex flex-wrap items-center gap-2 rounded-brand border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger sm:ml-[112px]">
          <AlertCircle class="w-4 h-4 shrink-0" />
          <p class="min-w-0 flex-1 leading-relaxed">
            Tunnel is still active — it was not disconnected. {tunnelRowNotice}
          </p>
        </div>
      {/if}

      <!-- Tailscale (Serve / Funnel) -->
      <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
        <span
          class="w-full shrink-0 rounded-brand border px-2 py-1 text-center font-code text-[11px] font-semibold uppercase tracking-wider transition-colors sm:w-[104px] {tailscaleRunning && tailscaleUrl
            ? 'border-info/40 bg-info/10 text-info'
            : 'border-border bg-surface-2 text-text-muted'}"
        >
          Tailscale
        </span>

        <div class="flex min-w-0 flex-1 flex-wrap items-center gap-2">
        {#if tailscaleEnabled && !isTailscaleLoading && tailscaleReachable}
          <input
            type="text"
            value="{tailscaleUrl}/v1"
            readonly
            class="ui-input min-w-[160px] flex-1 font-code text-xs selection:bg-info/25"
          />
          <button
            type="button"
            onclick={() => copy(`${tailscaleUrl}/v1`, 'ts_url')}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-border text-text-muted transition-colors hover:border-info/50 hover:text-info"
            title="Copy Tailscale URL"
          >
            {#if copiedId === 'ts_url'}
              <Check class="w-4 h-4 text-success" />
            {:else}
              <Copy class="w-4 h-4" />
            {/if}
          </button>
          <button
            type="button"
            onclick={() => (showDisableTailscaleModal = true)}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-danger/30 text-danger transition-colors hover:bg-danger/10"
            title="Disable Tailscale"
          >
            <Power class="w-4 h-4" />
          </button>
        {:else if tailscaleEnabled && !isTailscaleLoading && !tailscaleReachable}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
            <Loader2 class="w-4 h-4 animate-spin shrink-0" />
            <span>{tailscaleEverReachable ? 'Tailscale reconnecting...' : 'Tailscale checking...'}</span>
          </div>
          <button
            type="button"
            onclick={() => (showDisableTailscaleModal = true)}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-danger/30 text-danger transition-colors hover:bg-danger/10"
            title="Disable Tailscale"
          >
            <Power class="w-4 h-4" />
          </button>
        {:else if isTailscaleLoading}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-border bg-surface-2 px-3 py-2 text-xs text-text-muted">
            <Loader2 class="w-4 h-4 animate-spin text-info shrink-0" />
            <span>{tailscaleStatusText || 'Connecting...'}</span>
          </div>
          <button
            type="button"
            onclick={() => (isTailscaleLoading = false)}
            class="flex size-10 shrink-0 cursor-pointer items-center justify-center rounded-brand border border-border text-danger transition-colors hover:bg-danger/10"
            title="Cancel"
          >
            <Power class="w-4 h-4" />
          </button>
        {:else if tailscaleAuthUrl}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-info/30 bg-info/10 px-3 py-2 text-xs text-info">
            <ExternalLink class="w-4 h-4 shrink-0" />
            <span>Login required to finish connecting Tailscale.</span>
          </div>
          <button
            type="button"
            onclick={() => window.open(tailscaleAuthUrl, '_blank')}
            class="flex shrink-0 cursor-pointer items-center gap-1.5 rounded-brand border border-info/40 px-3 py-2 text-xs font-semibold text-info transition-colors hover:bg-info/10"
          >
            <ExternalLink class="w-3.5 h-3.5" />
            <span>Login</span>
          </button>
        {:else if tailscaleError}
          <div class="flex min-w-[160px] flex-1 items-center gap-2 rounded-brand border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
            <AlertCircle class="w-4 h-4 shrink-0" />
            <span class="truncate">{tailscaleError}</span>
          </div>
          <button
            type="button"
            onclick={handleTailscaleClick}
            class="flex shrink-0 cursor-pointer items-center gap-1.5 rounded-brand border border-info/40 px-3 py-2 text-xs font-semibold text-info transition-colors hover:bg-info/10"
          >
            <Shield class="w-3.5 h-3.5" />
            <span>Enable</span>
          </button>
        {:else}
          <div class="ui-input flex min-w-[160px] flex-1 items-center font-code text-xs text-text-muted">
            <span>Not connected</span>
          </div>
          <button
            type="button"
            onclick={() => {
              if (isLoginUnsafe) {
                tailscaleError = `Security required: ${unsafeReason}`
              } else {
                handleTailscaleClick()
              }
            }}
            class="flex shrink-0 cursor-pointer items-center gap-1.5 rounded-brand border border-info/40 px-3 py-2 text-xs font-semibold text-info transition-colors hover:bg-info/10"
          >
            <Shield class="w-3.5 h-3.5" />
            <span>Enable</span>
          </button>
        {/if}
        </div>
      </div>

      <!-- Same as the tunnel row: a failed disable keeps Tailscale connected. -->
      {#if tailscaleRowNotice}
        <div class="flex flex-wrap items-center gap-2 rounded-brand border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger sm:ml-[112px]">
          <AlertCircle class="w-4 h-4 shrink-0" />
          <p class="min-w-0 flex-1 leading-relaxed">
            Tailscale is still active — it was not disconnected. {tailscaleRowNotice}
          </p>
        </div>
      {/if}

      <!-- Tailscale Funnel off for the tailnet: the enable response carried a
           reason and a console URL, and both used to be dropped on the floor. -->
      {#if tailscaleFunnelNotice}
        <div class="flex flex-wrap items-center gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning sm:ml-[112px]">
          <AlertTriangle class="w-4 h-4 shrink-0" />
          <p class="min-w-0 flex-1 leading-relaxed">{tailscaleFunnelNotice}</p>
          {#if tailscaleFunnelEnableUrl}
            <button
              type="button"
              onclick={() => window.open(tailscaleFunnelEnableUrl, '_blank')}
              class="flex shrink-0 cursor-pointer items-center gap-1.5 font-semibold underline hover:opacity-80"
            >
              <ExternalLink class="w-3.5 h-3.5" />
              <span>Open Tailscale settings</span>
            </button>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Pre-enable security gate banner (upstream isLoginUnsafe) -->
    {#if isLoginUnsafe && !tunnelEnabled && !tailscaleEnabled}
      <div class="mt-4 flex flex-wrap items-center gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
        <AlertTriangle class="w-4 h-4 shrink-0" />
        <p class="min-w-0 flex-1 leading-relaxed">{unsafeReason}</p>
        <a href="/dashboard/profile" class="shrink-0 font-semibold underline hover:opacity-80">
          Open settings
        </a>
      </div>
    {/if}

    <!-- Security warnings when tunnel or tailscale is active -->
    {#if (tunnelEnabled || tailscaleEnabled) && (!requireApiKey || isLoginUnsafe)}
      <div class="mt-4 flex flex-col gap-2">
        {#if !requireApiKey}
          <div class="flex flex-wrap items-center gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
            <AlertTriangle class="w-4 h-4 shrink-0" />
            <p class="min-w-0 flex-1 leading-relaxed">
              Require API key is disabled — your endpoint is publicly accessible without authentication.
            </p>
            <button
              type="button"
              onclick={() => toggleRequireApiKey(true)}
              class="shrink-0 cursor-pointer font-semibold underline hover:opacity-80"
            >
              Enable
            </button>
          </div>
        {/if}
        {#if isLoginUnsafe}
          <div class="flex flex-wrap items-center gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
            <AlertTriangle class="w-4 h-4 shrink-0" />
            <p class="min-w-0 flex-1 leading-relaxed">
              {!requireLogin
                ? 'Require login is disabled — anyone can access your dashboard via tunnel.'
                : 'Dashboard uses the default password — change it in Profile settings.'}
            </p>
            <a href="/dashboard/profile" class="shrink-0 font-semibold underline hover:opacity-80">
              {!requireLogin ? 'Enable' : 'Change password'}
            </a>
          </div>
        {/if}
      </div>
    {/if}

    <!-- Allow dashboard access via tunnel toggle -->
    {#if tunnelRunning || tailscaleRunning}
      <div class="mt-4 flex items-start justify-between gap-4 border-t border-border-subtle pt-4">
        <div class="min-w-0 space-y-0.5">
          <p class="text-sm font-medium text-text-main">Allow dashboard access via tunnel</p>
          <p class="text-xs leading-relaxed text-text-muted">
            When enabled, the dashboard can be accessed through your tunnel or Tailscale URL (login still required).
          </p>
        </div>
        <Toggle
          checked={tunnelDashboardAccess}
          label="Allow dashboard access via tunnel"
          onChange={toggleTunnelDashboardAccess}
        />
      </div>
      {#if tunnelDashboardAccessError}
        <p class="mt-2 text-xs text-danger" role="alert">
          Could not update tunnel dashboard access — {tunnelDashboardAccessError}
        </p>
      {/if}
    {/if}
  </Card>

  <!-- BOTTOM SECTION: API Keys Card -->
  <Card padding="md" class="space-y-4">
    <!-- Header with Create Key button -->
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <div class="flex size-9 shrink-0 items-center justify-center rounded-brand border border-border-subtle bg-surface-2 text-primary">
          <Key class="w-5 h-5" />
        </div>
        <div class="min-w-0">
          <span class="ui-kicker">II · Access</span>
          <h2 class="mt-1 font-headline text-lg font-medium leading-tight text-text-main">
            API Keys
          </h2>
          <p class="mt-0.5 text-xs leading-relaxed text-text-muted">
            Manage Bearer tokens for clients connecting to this endpoint
          </p>
        </div>
      </div>

      <Button size="sm" icon="add" onclick={() => (isCreateKeyOpen = true)} class="w-full sm:w-auto">
        Create Key
      </Button>
    </div>

    <!-- Master Switch: Require API key -->
    <div class="flex items-start justify-between gap-4 border-b border-border-subtle pb-4 pt-2">
      <div class="min-w-0 space-y-0.5">
        <p class="text-sm font-medium text-text-main">Require API key</p>
        <p class="text-xs leading-relaxed text-text-muted">Requests without a valid key will be rejected</p>
      </div>
      <Toggle
        checked={requireApiKey}
        label="Require API key"
        onChange={toggleRequireApiKey}
      />
    </div>

      {#if endpointSettingsError}
        <p class="text-xs text-danger" role="alert">
          Could not update API key requirement — {endpointSettingsError}
        </p>
      {/if}

    <!-- Warning if disabled -->
    {#if !requireApiKey}
      <div class="flex items-start gap-2 rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
        <AlertTriangle class="w-4 h-4 shrink-0" />
        <span class="leading-relaxed">Endpoint is exposed without an API key. Anyone who can reach this host can make requests.</span>
      </div>
    {/if}

    <!-- Keys List / Table -->
    {#if localKeys.length === 0}
      <div class="ui-empty flex flex-col items-center gap-3 text-center">
        <div class="flex size-12 items-center justify-center rounded-full border border-border-subtle bg-surface text-primary">
          <Key class="w-6 h-6" />
        </div>
        <div class="space-y-1">
          <p class="font-headline text-base font-medium text-text-main">No API keys yet</p>
          <p class="text-xs leading-relaxed text-text-muted">
            Create your first key so clients can authenticate against this endpoint.
          </p>
        </div>
        <Button size="sm" icon="add" onclick={() => (isCreateKeyOpen = true)}>
          Create Key
        </Button>
      </div>
    {:else}
      <ul class="flex flex-col divide-y divide-border-subtle">
        {#each localKeys as key (key.id)}
          {@const isShown = shownKeyIds.has(key.id)}
          {@const isActive = key.isActive === 1 || key.isActive === true}
          <li class="group flex flex-col gap-3 py-3.5 transition-opacity sm:flex-row sm:items-center sm:justify-between {isActive ? '' : 'opacity-60'}">
            <div class="min-w-0 flex-1 sm:pr-4">
              <p class="text-sm font-semibold text-text-main">{key.name || 'Default Key'}</p>
              <div class="mt-1 flex min-w-0 flex-wrap items-center gap-2">
                <code class="ui-code min-w-0 px-2 py-0.5 text-[11px] select-all">
                  {isShown ? key.key : maskKey(key.key)}
                </code>

                <!-- Eye Toggle -->
                <button
                  type="button"
                  onclick={() => toggleShowKey(key.id)}
                  class="cursor-pointer rounded-brand p-1.5 text-text-muted transition-colors hover:bg-surface-2 hover:text-text-main"
                  title={isShown ? 'Hide key' : 'Show key'}
                >
                  {#if isShown}
                    <EyeOff class="w-3.5 h-3.5" />
                  {:else}
                    <Eye class="w-3.5 h-3.5" />
                  {/if}
                </button>

                <!-- Copy -->
                <button
                  type="button"
                  onclick={() => copy(key.key, key.id)}
                  class="cursor-pointer rounded-brand p-1.5 text-text-muted transition-colors hover:bg-surface-2 hover:text-primary"
                  title="Copy key"
                >
                  {#if copiedId === key.id}
                    <Check class="w-3.5 h-3.5 text-success" />
                  {:else}
                    <Copy class="w-3.5 h-3.5" />
                  {/if}
                </button>
              </div>

              <div class="mt-1.5 flex flex-wrap items-center gap-2 text-[11px] text-text-subtle">
                <span>Created {key.createdAt ? new Date(key.createdAt).toLocaleDateString() : '—'}</span>
                {#if !isActive}
                  <span aria-hidden="true">•</span>
                  <span class="font-medium text-warning">Paused</span>
                {/if}
              </div>
            </div>

            <div class="flex shrink-0 items-center gap-3">
              <!-- Active Switch -->
              <Toggle
                checked={isActive}
                size="sm"
                label={isActive ? 'Pause key' : 'Resume key'}
                title={isActive ? 'Pause key' : 'Resume key'}
                onChange={(nextActive) => handleToggleKey(key, nextActive)}
              />

              <!-- Delete Button -->
              <button
                type="button"
                onclick={() => handleDeleteKey(key)}
                class="cursor-pointer rounded-brand p-2 text-text-subtle transition-all hover:bg-danger/10 hover:text-danger sm:opacity-0 sm:group-hover:opacity-100 sm:focus-visible:opacity-100"
                title="Delete key"
              >
                <Trash2 class="w-4 h-4" />
              </button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </Card>
</div>

<!-- MODAL: Create API Key -->
<Modal isOpen={isCreateKeyOpen} title="Create API Key" size="md" onClose={closeCreateKeyModal}>
  <form onsubmit={handleCreateKey} class="space-y-4">
    <div class="space-y-1.5">
      <label for="key-name-input" class="ui-kicker">Key name</label>
      <input
        id="key-name-input"
        type="text"
        bind:value={newKeyName}
        placeholder="Production Key"
        class="ui-input w-full text-sm"
      />
      <p class="text-xs leading-relaxed text-text-subtle">
        Only a label — clients authenticate with the key value, never the name.
      </p>
    </div>

    <div class="flex flex-col gap-2 pt-2 sm:flex-row">
      <Button
        type="submit"
        disabled={!newKeyName.trim()}
        loading={isSubmittingKey}
        class="flex-1"
      >
        Create
      </Button>
      <Button variant="secondary" onclick={closeCreateKeyModal} class="flex-1">
        Cancel
      </Button>
    </div>
  </form>
</Modal>

<!-- MODAL: API Key Created -->
<Modal isOpen={!!newlyCreatedKey} title="API Key Created" size="md" onClose={() => (newlyCreatedKey = null)}>
  <div class="space-y-4">
    <div class="space-y-1 rounded-brand border border-warning/30 bg-warning/10 p-4 text-warning">
      <p class="text-sm font-bold">Save this key now</p>
      <p class="text-xs leading-relaxed">
        This is the only time you will see this key. Store it securely.
      </p>
    </div>

    <div class="flex flex-col gap-2 sm:flex-row sm:items-center">
      <input
        type="text"
        value={newlyCreatedKey}
        readonly
        class="ui-input min-w-0 flex-1 font-code text-xs select-all"
      />
      <Button
        variant="secondary"
        size="sm"
        onclick={() => copy(newlyCreatedKey || '', 'created_key')}
        class="shrink-0"
      >
        {#if copiedId === 'created_key'}
          <Check class="w-3.5 h-3.5 text-success" />
          <span>Copied</span>
        {:else}
          <Copy class="w-3.5 h-3.5" />
          <span>Copy</span>
        {/if}
      </Button>
    </div>

    <Button fullWidth onclick={() => (newlyCreatedKey = null)}>Done</Button>
  </div>
</Modal>

<!-- MODAL: Confirmation (Delete / Pause) -->
<ConfirmModal
  isOpen={!!confirmModal}
  title={confirmModal?.title || 'Confirm'}
  message={confirmModal?.message || ''}
  confirmText={confirmModal?.confirmLabel || 'Confirm'}
  variant={confirmModal?.isDanger ? 'danger' : 'primary'}
  onClose={() => (confirmModal = null)}
  onConfirm={() => confirmModal?.onConfirm()}
/>

<!-- MODAL: Enable Cloudflare Tunnel -->
<Modal isOpen={showEnableTunnelModal} title="Enable Tunnel" size="lg" onClose={() => (showEnableTunnelModal = false)}>
  <div class="space-y-4">
    <!-- Cloudflare Tunnel Info Box -->
    <div class="flex items-start gap-3 rounded-brand border border-border-subtle bg-surface-2 p-4">
      <CloudUpload class="w-5 h-5 text-primary shrink-0 mt-0.5" />
      <div class="space-y-1 text-xs">
        <p class="font-bold text-text-main">Cloudflare Quick Tunnel</p>
        <p class="leading-relaxed text-text-muted">
          Expose your local 9router-go to the internet. No port forwarding, no static IP needed. Share endpoint URL with your team or use it in Cursor, Cline, and other AI tools from anywhere.
        </p>
      </div>
    </div>

    <!-- Feature Grid -->
    <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
      {#each tunnelValueProps as prop (prop.title)}
        <div class="space-y-1 rounded-brand border border-border-subtle bg-surface-2/60 p-3 text-center">
          <p class="text-xs font-bold text-text-main">{prop.title}</p>
          <p class="text-[11px] text-text-muted">{prop.desc}</p>
        </div>
      {/each}
    </div>

    <p class="text-xs text-text-subtle">
      Requires outbound port 7844 (TCP/UDP). Connection may take 10-30s.
    </p>

    <div class="flex flex-col gap-2 pt-2 sm:flex-row">
      <Button onclick={startTunnel} class="flex-1">Start Tunnel</Button>
      <Button variant="secondary" onclick={() => (showEnableTunnelModal = false)} class="flex-1">
        Cancel
      </Button>
    </div>
  </div>
</Modal>

<!-- MODAL: Disable Cloudflare Tunnel -->
<ConfirmModal
  isOpen={showDisableTunnelModal}
  title="Disable Tunnel"
  message="The Cloudflare tunnel will be disconnected. Remote access via tunnel URL will stop working."
  confirmText="Disable Tunnel"
  variant="danger"
  onClose={() => (showDisableTunnelModal = false)}
  onConfirm={stopTunnel}
/>

<!-- MODAL: Tailscale Funnel -->
<Modal isOpen={showTailscaleModal} title="Tailscale Funnel" size="md" onClose={() => (showTailscaleModal = false)}>
  <div class="space-y-4">
    {#if tailscaleInstalled === false}
      <div class="space-y-3">
        <p class="text-sm leading-relaxed text-text-muted">
          Tailscale CLI is not installed or detected on your system path. Install Tailscale to enable Funnel.
        </p>
        <div class="ui-code px-3 py-2 text-xs select-all">
          curl -fsSL https://tailscale.com/install.sh | sh
        </div>
      </div>
    {:else}
      <p class="text-sm leading-relaxed text-text-muted">
        Tailscale is installed. Click Connect to expose your 9router-go via Tailscale Funnel.
      </p>
    {/if}

    <div class="flex flex-col gap-2 pt-2 sm:flex-row">
      {#if tailscaleInstalled}
        <Button onclick={startTailscale} class="flex-1">Connect</Button>
      {/if}
      <Button variant="secondary" onclick={() => (showTailscaleModal = false)} class="flex-1">
        Cancel
      </Button>
    </div>
  </div>
</Modal>

<!-- MODAL: Disable Tailscale -->
<ConfirmModal
  isOpen={showDisableTailscaleModal}
  title="Disable Tailscale"
  message="Tailscale Funnel will be stopped. Remote access via Tailscale URL will stop working."
  confirmText="Disable Tailscale"
  variant="danger"
  onClose={() => (showDisableTailscaleModal = false)}
  onConfirm={stopTailscale}
/>

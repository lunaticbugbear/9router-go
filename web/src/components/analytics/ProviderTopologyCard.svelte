<script lang="ts">
  import { onMount } from 'svelte'
  import type { ActiveRequestItem } from './types'
  import { getIconPath } from '../connections/types'

  interface ProviderNodeItem {
    id: string
    name: string
    color?: string
    type?: string
  }

  interface Props {
    providers?: ProviderNodeItem[]
    activeRequests?: ActiveRequestItem[]
    pulseProvider?: string
    lastProvider?: string
    errorProvider?: string
    onRefresh?: () => void
  }

  let {
    providers = [],
    activeRequests = [],
    pulseProvider = '',
    lastProvider = '',
    errorProvider = '',
    onRefresh
  }: Props = $props()

  // Default fallback providers if none connected
  const FALLBACK_PROVIDERS: ProviderNodeItem[] = [
    { id: 'antigravity', name: 'Antigravity', color: '#F59E0B' },
    { id: 'opencode', name: 'OpenCode Free', color: '#3B82F6' },
    { id: 'nvidia', name: 'NVIDIA NIM', color: '#76B900' },
    { id: 'freebuff', name: 'Freebuff', color: '#10B981' },
    { id: 'openrouter', name: 'OpenRouter', color: '#6366F1' },
    { id: 'clinepass', name: 'ClinePass', color: '#8B5CF6' }
  ]

  let displayProviders = $derived(
    providers.length > 0 ? providers.slice(0, 14) : FALLBACK_PROVIDERS
  )

  let activeProviderIds = $derived(
    new Set(
      activeRequests
        .map((r) => r.provider?.toLowerCase())
        .filter((p): p is string => Boolean(p))
    )
  )

  let hasPulse = $derived(Boolean(pulseProvider))
  let activeCount = $derived(
    Math.max(activeRequests.reduce((sum, r) => sum + (r.count || 1), 0), hasPulse ? 1 : 0)
  )
  // Container sizing and fitView state
  let containerEl = $state<HTMLDivElement | null>(null)
  let containerWidth = $state(800)
  let containerHeight = $state(480)

  // Zoom & Pan transforms
  let zoom = $state(0.85)
  let panX = $state(0)
  let panY = $state(0)
  let isDragging = $state(false)
  let dragStart = { x: 0, y: 0 }

  // Track image load errors to fallback to stylish initials
  let imageErrors = $state<Record<string, boolean>>({})

  // Upstream exact node radius formula:
  // s = providers.length
  // a = Math.max(320, 204 * s / (2 * Math.PI)) // rx
  // i = Math.max(200, 0.55 * a)                 // ry
  let geometry = $derived.by(() => {
    const s = displayProviders.length
    const rx = Math.max(320, (204 * s) / (2 * Math.PI))
    const ry = Math.max(200, 0.55 * rx)

    const nodes = displayProviders.map((p, idx) => {
      const angle = -Math.PI / 2 + (2 * Math.PI * idx) / s
      const x = rx * Math.cos(angle)
      const y = ry * Math.sin(angle)

      const pid = p.id.toLowerCase()
      const pname = (p.name || '').toLowerCase()
      const isActive =
        activeProviderIds.has(pid) ||
        activeProviderIds.has(pname) ||
        (hasPulse && (pulseProvider.toLowerCase() === pid || pulseProvider.toLowerCase() === pname))
      const isLast = !isActive && Boolean(lastProvider) && (lastProvider.toLowerCase() === pid || lastProvider.toLowerCase() === pname)
      const isError = !isActive && Boolean(errorProvider) && (errorProvider.toLowerCase() === pid || errorProvider.toLowerCase() === pname)
      // Edge handle coordinates based on angle
      let sourceX = 0
      let sourceY = 0
      let targetX = x
      let targetY = y
      let isVertical = false

      if (
        Math.abs(angle + Math.PI / 2) < Math.PI / 4 ||
        Math.abs(angle - (3 * Math.PI) / 2) < Math.PI / 4
      ) {
        // Top quadrant
        sourceX = 0
        sourceY = -22
        targetX = x
        targetY = y + 26
        isVertical = true
      } else if (Math.abs(angle - Math.PI / 2) < Math.PI / 4) {
        // Bottom quadrant
        sourceX = 0
        sourceY = 22
        targetX = x
        targetY = y - 26
        isVertical = true
      } else if (angle > -Math.PI / 2 && angle < Math.PI / 2) {
        // Right quadrant
        sourceX = 65
        sourceY = 0
        targetX = x - 75
        targetY = y
        isVertical = false
      } else {
        // Left quadrant
        sourceX = -65
        sourceY = 0
        targetX = x + 75
        targetY = y
        isVertical = false
      }

      const path = isVertical
        ? `M ${sourceX} ${sourceY} C ${sourceX} ${(sourceY + targetY) / 2}, ${targetX} ${(sourceY + targetY) / 2}, ${targetX} ${targetY}`
        : `M ${sourceX} ${sourceY} C ${(sourceX + targetX) / 2} ${sourceY}, ${(sourceX + targetX) / 2} ${targetY}, ${targetX} ${targetY}`

      const returnPath = isVertical
        ? `M ${targetX} ${targetY} C ${targetX} ${(sourceY + targetY) / 2}, ${sourceX} ${(sourceY + targetY) / 2}, ${sourceX} ${sourceY}`
        : `M ${targetX} ${targetY} C ${(sourceX + targetX) / 2} ${targetY}, ${(sourceX + targetX) / 2} ${sourceY}, ${sourceX} ${sourceY}`

      const textIcon = (p.name || p.id || '?').slice(0, 2).toUpperCase()

      return {
        ...p,
        x,
        y,
        isActive,
        isLast,
        isError,
        path,
        returnPath,
        textIcon,
        color: p.color || '#6b7280'
      }
    })

    return { rx, ry, nodes }
  })

  function fitView() {
    if (!containerWidth || !containerHeight) return
    const requiredWidth = 2 * geometry.rx + 220
    const requiredHeight = 2 * geometry.ry + 120
    const scaleX = (containerWidth - 32) / requiredWidth
    const scaleY = (containerHeight - 32) / requiredHeight
    zoom = Math.min(1.05, Math.max(0.3, Math.min(scaleX, scaleY)))
    panX = 0
    panY = 0
  }

  function handlePointerDown(e: PointerEvent) {
    if ((e.target as HTMLElement).closest('button')) return
    isDragging = true
    dragStart = { x: e.clientX - panX, y: e.clientY - panY }
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  }

  function handlePointerMove(e: PointerEvent) {
    if (!isDragging) return
    panX = e.clientX - dragStart.x
    panY = e.clientY - dragStart.y
  }

  function handlePointerUp(e: PointerEvent) {
    if (isDragging) {
      isDragging = false
      try {
        ;(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId)
      } catch {}
    }
  }

  function handleWheel(e: WheelEvent) {
    e.preventDefault()
    const zoomFactor = e.deltaY < 0 ? 1.08 : 0.92
    zoom = Math.min(2.2, Math.max(0.25, zoom * zoomFactor))
  }

  onMount(() => {
    if (!containerEl) return
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        containerWidth = entry.contentRect.width || 800
        containerHeight = entry.contentRect.height || 480
        fitView()
      }
    })
    observer.observe(containerEl)
    fitView()
    return () => observer.disconnect()
  })
</script>

<div
  bind:this={containerEl}
  role="region"
  aria-label="Provider topology map"
  class="relative h-[320px] w-full min-w-0 select-none overflow-hidden rounded-brand border border-border-subtle bg-bg-alt/40 sm:h-[480px] cursor-grab active:cursor-grabbing"
  onpointerdown={handlePointerDown}
  onpointermove={handlePointerMove}
  onpointerup={handlePointerUp}
  onpointercancel={handlePointerUp}
  onwheel={handleWheel}
>
  <!-- Interactive Viewport Layer (centered at 0, 0) -->
  <div
    class="absolute inset-0 pointer-events-none"
    style="transform: translate({containerWidth / 2 + panX}px, {containerHeight / 2 + panY}px) scale({zoom}); transform-origin: 0 0;"
  >
    <!-- SVG Layer for Bezier Edges -->
    <svg class="absolute inset-0 overflow-visible pointer-events-none" style="transform: translate(0, 0);">
      <defs>
        <filter id="topo-electric" x="-30%" y="-30%" width="160%" height="160%">
          <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="1" seed="2" result="noise">
            <animate attributeName="baseFrequency" values="0.8;1.3;0.8" dur="0.25s" repeatCount="indefinite" />
          </feTurbulence>
          <feDisplacementMap in="SourceGraphic" in2="noise" scale="3" xChannelSelector="R" yChannelSelector="G" />
        </filter>
      </defs>

      <!-- Edges connecting router to provider nodes -->
      {#each geometry.nodes as node (node.id)}
        {#if node.isActive}
          <!-- Expanding Shockwave Ripples from Provider Target -->
          <g class="topology-shockwave-group">
            <circle
              cx={node.x}
              cy={node.y}
              r="35"
              fill="none"
              stroke={node.color || 'var(--app-focus)'}
              opacity="0"
              style="filter: drop-shadow(0 0 4px {node.color || 'var(--app-focus)'});"
            >
              <animate attributeName="r" values="32;105" dur="1.1s" repeatCount="indefinite" />
              <animate attributeName="opacity" values="0.55;0" dur="1.1s" repeatCount="indefinite" />
              <animate attributeName="stroke-width" values="2.5;0.5" dur="1.1s" repeatCount="indefinite" />
            </circle>
            <circle
              cx={node.x}
              cy={node.y}
              r="35"
              fill="none"
              stroke="var(--app-primary)"
              opacity="0"
              style="filter: drop-shadow(0 0 4px var(--app-primary));"
            >
              <animate attributeName="r" values="32;105" dur="1.1s" begin="0.55s" repeatCount="indefinite" />
              <animate attributeName="opacity" values="0.45;0" dur="1.1s" begin="0.55s" repeatCount="indefinite" />
              <animate attributeName="stroke-width" values="2;0.5" dur="1.1s" begin="0.55s" repeatCount="indefinite" />
            </circle>
          </g>

          <!-- Bidirectional stream (Router <-> Provider): hairline traces, no laser core -->
          <g class="topology-edge-electric">
            <!-- 1. Outgoing prompt trace: soft brass halo -->
            <path
              d={node.path}
              fill="none"
              stroke="var(--app-primary)"
              stroke-width="6"
              stroke-opacity="0.22"
              stroke-linecap="round"
              filter="url(#topo-electric)"
              class="topology-edge-halo"
            />
            <!-- 2. Mid plasma forward stream -->
            <path
              d={node.path}
              fill="none"
              stroke="var(--app-focus)"
              stroke-width="2.5"
              stroke-opacity="0.65"
              stroke-linecap="round"
              filter="url(#topo-electric)"
              class="topology-edge-plasma"
            />
            <!-- 3. Forward core trace -->
            <path
              d={node.path}
              fill="none"
              stroke="var(--app-text-main)"
              stroke-width="1"
              stroke-opacity="0.75"
              class="topology-edge-kame"
            />

            <!-- 4. Incoming token stream (Provider -> Router) -->
            <path
              d={node.returnPath}
              fill="none"
              stroke="var(--color-success)"
              stroke-width="2"
              stroke-opacity="0.7"
              class="topology-edge-return"
            />
            <path
              d={node.returnPath}
              fill="none"
              stroke="var(--app-primary)"
              stroke-width="1"
              stroke-opacity="0.6"
              class="topology-edge-return"
            />

            <!-- Forward prompt tokens: Router -> Provider -->
            {#each [0, 1, 2] as i}
              <circle
                r={i === 0 ? 3 : 2.2}
                fill={i === 0 ? 'var(--app-focus)' : 'var(--app-text-muted)'}
                opacity="0.8"
              >
                <animateMotion
                  dur="{0.42 + i * 0.09}s"
                  repeatCount="indefinite"
                  path={node.path}
                  begin="{i * 0.12}s"
                />
              </circle>
            {/each}

            <!-- Reverse output tokens: Provider -> Router -->
            {#each [0, 1, 2, 3] as i}
              <circle
                r={i % 2 === 0 ? 2.8 : 1.9}
                fill={i % 2 === 0 ? 'var(--color-success)' : 'var(--app-primary)'}
                opacity="0.8"
              >
                <animateMotion
                  dur="{0.48 + i * 0.1}s"
                  repeatCount="indefinite"
                  path={node.returnPath}
                  begin="{i * 0.11}s"
                />
              </circle>
            {/each}

            <!-- Sparse trace sparks -->
            {#each [0, 1, 2, 3] as i}
              <circle
                r="1.2"
                fill="var(--app-text-main)"
                opacity="0"
              >
                <animate
                  attributeName="opacity"
                  values="0;0.6;0;0;0.6;0"
                  dur="{0.35 + (i % 3) * 0.1}s"
                  begin="{i * 0.07}s"
                  repeatCount="indefinite"
                />
                <animateMotion
                  dur="{0.28 + i * 0.05}s"
                  repeatCount="indefinite"
                  path={node.path}
                  begin="{i * 0.11}s"
                />
              </circle>
            {/each}
          </g>
        {:else if node.isLast}
          <!-- Last provider used (ochre) -->
          <path
            d={node.path}
            fill="none"
            stroke="var(--color-warning)"
            stroke-width="1.5"
            opacity="0.6"
          />
        {:else if node.isError}
          <!-- Error edge (crimson) -->
          <path
            d={node.path}
            fill="none"
            stroke="var(--color-danger)"
            stroke-width="2"
            opacity="0.8"
          />
        {:else}
          <!-- Inactive Subtle Edge -->
          <path
            d={node.path}
            fill="none"
            stroke="var(--color-border)"
            stroke-width="1"
            opacity="0.3"
          />
        {/if}
      {/each}
      <!-- Center router absorption rings when traffic is in flight -->
      {#if activeCount > 0}
        <g class="topology-router-absorption">
          <circle
            cx="0"
            cy="0"
            r="30"
            fill="none"
            stroke="var(--app-focus)"
            stroke-dasharray="5 5"
            opacity="0"
            style="filter: drop-shadow(0 0 3px var(--app-focus));"
          >
            <animate attributeName="r" values="55;20" dur="0.9s" repeatCount="indefinite" />
            <animate attributeName="opacity" values="0;0.5;0" dur="0.9s" repeatCount="indefinite" />
            <animate attributeName="stroke-width" values="1;1.8;0.5" dur="0.9s" repeatCount="indefinite" />
          </circle>
          <circle
            cx="0"
            cy="0"
            r="30"
            fill="none"
            stroke="var(--app-primary)"
            stroke-dasharray="4 6"
            opacity="0"
            style="filter: drop-shadow(0 0 3px var(--app-primary));"
          >
            <animate attributeName="r" values="65;24" dur="1.1s" begin="0.45s" repeatCount="indefinite" />
            <animate attributeName="opacity" values="0;0.4;0" dur="1.1s" begin="0.45s" repeatCount="indefinite" />
            <animate attributeName="stroke-width" values="0.8;1.4;0.5" dur="1.1s" begin="0.45s" repeatCount="indefinite" />
          </circle>
        </g>
      {/if}
    </svg>

    <!-- HTML Router Node (Center 0, 0) -->
    <div
      class="absolute z-10 flex min-w-[130px] items-center justify-center rounded-brand border-2 px-5 py-3 {activeCount > 0 ? 'topology-router-core' : 'border-primary bg-primary/5'} pointer-events-auto"
      style="left: 0px; top: 0px; transform: translate(-50%, -50%);"
    >
      <img
        src="/favicon.svg"
        alt="9router-go"
        class="mr-2 h-6 w-6 object-contain {activeCount > 0 ? 'topology-router-icon' : ''}"
        loading="lazy"
        decoding="async"
      />
      <span class="text-sm font-bold text-primary {activeCount > 0 ? 'topology-router-label' : ''}">
        9router-go
      </span>
      {#if activeCount > 0}
        <span class="topology-router-badge ml-2 rounded-full border border-primary/40 bg-primary/15 px-1.5 py-0.5 font-code text-xs font-bold tabular-nums text-primary">
          {activeCount}
        </span>
      {/if}
    </div>
    <!-- HTML Provider Nodes -->
    {#each geometry.nodes as node (node.id)}
      <div
        class="absolute flex items-center gap-2.5 rounded-brand border-2 bg-bg px-4 py-2.5 shadow-soft transition-all duration-300 pointer-events-auto {node.isActive ? 'topology-node-active-bounce' : ''}"
        style="left: {node.x}px; top: {node.y}px; transform: translate(-50%, -50%); border-color: {node.isActive ? node.color : node.isLast ? 'var(--color-warning)' : 'var(--color-border)'}; box-shadow: {node.isActive ? `0 0 12px ${node.color}40` : node.isLast ? '0 0 6px rgba(211, 173, 115, 0.2)' : 'none'}; min-width: 150px;"
      >
        <div
          class="flex size-8 shrink-0 items-center justify-center rounded-brand border border-border-subtle bg-surface-2"
        >
          {#if !imageErrors[node.id]}
            <img
              src={getIconPath(node.id)}
              alt={node.name}
              class="h-6 w-6 rounded-sm object-contain"
              onerror={() => {
                imageErrors = { ...imageErrors, [node.id]: true }
              }}
              loading="lazy"
              decoding="async"
            />
          {:else}
            <span class="text-sm font-bold" style="color: {node.color};">
              {node.textIcon}
            </span>
          {/if}
        </div>
        <span
          class="max-w-[200px] truncate text-base font-medium"
          style="color: {node.isActive ? node.color : 'var(--app-text-main)'}"
          title={node.name}
        >
          {node.name}
        </span>

        <!-- Active indicator -->
        {#if node.isActive}
          <div class="ml-auto flex shrink-0 items-center gap-1.5">
            <span class="rounded-brand border border-success/30 bg-success/15 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-success">
              Live
            </span>
            <span class="relative flex size-2">
              <span
                class="absolute inline-flex h-full w-full rounded-full opacity-60"
                style="background-color: {node.color};"
              ></span>
              <span
                class="relative inline-flex size-2 rounded-full"
                style="background-color: {node.color};"
              ></span>
            </span>
          </div>
        {/if}
      </div>
    {/each}
  </div>

  <!-- Bottom-left React Flow style controls -->
  <div class="absolute bottom-4 left-4 z-20 flex flex-col overflow-hidden rounded-brand border border-border bg-surface/95 shadow-soft">
    <button
      type="button"
      onclick={() => (zoom = Math.min(2.5, zoom * 1.2))}
      class="p-1.5 text-text-muted hover:text-text-main hover:bg-surface-2 transition-colors border-b border-border cursor-pointer flex items-center justify-center"
      title="Zoom In"
      aria-label="Zoom In"
    >
      <span class="material-symbols-outlined text-[16px]">add</span>
    </button>
    <button
      type="button"
      onclick={() => (zoom = Math.max(0.2, zoom / 1.2))}
      class="p-1.5 text-text-muted hover:text-text-main hover:bg-surface-2 transition-colors border-b border-border cursor-pointer flex items-center justify-center"
      title="Zoom Out"
      aria-label="Zoom Out"
    >
      <span class="material-symbols-outlined text-[16px]">remove</span>
    </button>
    <button
      type="button"
      onclick={fitView}
      class="p-1.5 text-text-muted hover:text-text-main hover:bg-surface-2 transition-colors cursor-pointer flex items-center justify-center"
      title="Fit View"
      aria-label="Fit View"
    >
      <span class="material-symbols-outlined text-[16px]">crop_free</span>
    </button>
  </div>
</div>

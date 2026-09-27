<script module lang="ts">
  import type { ActiveTab } from '../lib/router'

  export type NavLink = {
    tab: ActiveTab
    label: string
    icon: string
    /** Tabs that should also light this entry up (one route, several names). */
    match?: ActiveTab[]
    /** Short qualifier shown in command palette results. */
    hint?: string
  }
  export type NavGroup = {
    id: string
    label: string
    /** Roman numeral prefix; omitted for the footer group. */
    numeral?: string
    links: NavLink[]
  }

  /**
   * Single source of truth for navigation. The TopBar breadcrumb and the command
   * palette read the same model, so no destination can drift out of sync.
   */
  export const NAV_GROUPS: NavGroup[] = [
    {
      id: 'gateway',
      numeral: 'I',
      label: 'Gateway',
      links: [
        { tab: 'overview', label: 'Overview', icon: 'space_dashboard', hint: 'Status and traffic' },
        { tab: 'endpoint', label: 'Endpoint', icon: 'api', match: ['endpoint', 'keys'], hint: 'Base URL and keys' },
        { tab: 'connections', label: 'Providers', icon: 'dns', hint: 'Upstream connections' },
        { tab: 'combos', label: 'Combos', icon: 'layers', hint: 'Model fallback chains' },
        { tab: 'proxy-pools', label: 'Proxy Pools', icon: 'lan', hint: 'Egress routing' },
      ],
    },
    {
      id: 'observe',
      numeral: 'II',
      label: 'Observe',
      links: [
        { tab: 'analytics', label: 'Usage', icon: 'bar_chart', hint: 'Traffic and tokens' },
        { tab: 'quota', label: 'Quota', icon: 'data_usage', hint: 'Limit headroom' },
        { tab: 'console-log', label: 'Console Log', icon: 'terminal', match: ['console-log', 'terminal'], hint: 'Live server output' },
      ],
    },
    {
      id: 'optimize',
      numeral: 'III',
      label: 'Optimize',
      links: [
        { tab: 'token-saver', label: 'Token Saver', icon: 'savings', hint: 'Prompt compression' },
        { tab: 'feature-flags', label: 'Feature Flags', icon: 'toggle_on', hint: 'Optional capabilities' },
      ],
    },
    {
      id: 'media',
      numeral: 'IV',
      label: 'Media',
      links: [
        { tab: 'media-embedding', label: 'Embeddings', icon: 'data_array', hint: 'Vector models' },
        { tab: 'media-image', label: 'Image', icon: 'brush', hint: 'Image generation' },
        { tab: 'media-tts', label: 'Text to Speech', icon: 'record_voice_over', hint: 'Voice synthesis' },
        { tab: 'media-stt', label: 'Speech to Text', icon: 'mic', hint: 'Transcription' },
        { tab: 'media-video', label: 'Video', icon: 'movie', hint: 'Video generation' },
        { tab: 'media-systemone', label: 'System One', icon: 'psychology', hint: 'State evaluation' },
        { tab: 'media-web', label: 'Web Fetch & Search', icon: 'travel_explore', hint: 'Search and scrape' },
      ],
    },
    {
      id: 'tools',
      numeral: 'V',
      label: 'Tools',
      links: [
        { tab: 'cli-tools', label: 'CLI Tools', icon: 'terminal', hint: 'Client setup' },
        { tab: 'skills', label: 'Skills', icon: 'extension', hint: 'Copy-and-paste links' },
        { tab: 'personas', label: 'Personas', icon: 'psychology', hint: 'System prompts' },
        { tab: 'bounty', label: 'Bug Bounty Assist', icon: 'bug_report', hint: 'Scope context' },
      ],
    },
  ]

  /** Rendered in the rail footer rather than as a numbered section. */
  export const SYSTEM_NAV_GROUP: NavGroup = {
    id: 'system',
    label: 'System',
    links: [{ tab: 'settings', label: 'Settings', icon: 'settings', hint: 'Preferences and profile' }],
  }

  export const ALL_NAV_GROUPS: NavGroup[] = [...NAV_GROUPS, SYSTEM_NAV_GROUP]

  export function linkMatches(link: NavLink, tab: ActiveTab): boolean {
    return link.match ? link.match.includes(tab) : link.tab === tab
  }

  export function groupTitle(group: NavGroup): string {
    return group.numeral ? `${group.numeral} · ${group.label}` : group.label
  }

  /** Section label for a destination, used by the TopBar breadcrumb. */
  export function sectionLabelFor(tab: ActiveTab): string {
    const group = ALL_NAV_GROUPS.find((g) => g.links.some((link) => linkMatches(link, tab)))
    return group ? groupTitle(group) : 'Dashboard'
  }
</script>

<script lang="ts">
  import { api } from '../api/client'
  import { TAB_ROUTES, type ActiveTab } from '../lib/router'
  import Icon from '../lib/ui/Icon.svelte'

  export type { ActiveTab }

  let {
    activeTab = $bindable('overview'),
    navigate = (tab: ActiveTab) => {
      activeTab = tab
    },
    activeConnections = 0,
    totalConnections = 0,
    collapsed = false,
    onToggleCollapse,
    onClose,
  }: {
    activeTab: ActiveTab
    navigate?: (tab: ActiveTab, replace?: boolean) => void
    activeConnections?: number
    totalConnections?: number
    collapsed?: boolean
    onToggleCollapse?: () => void
    onClose?: () => void
  } = $props()

  let version = $state('')
  const MEDIA_OPEN_KEY = '9router-nav-media-open'
  const GROUP_OPEN_KEY = '9router-nav-group-'

  let openGroups = $state<Record<string, boolean>>(Object.fromEntries(
    NAV_GROUPS.map((group) => {
      let saved: string | null = null
      try {
        saved = localStorage.getItem(group.id === 'media' ? MEDIA_OPEN_KEY : `${GROUP_OPEN_KEY}${group.id}-open`)
      } catch {}
      return [group.id, saved === null ? group.id !== 'media' : saved === '1']
    })
  ))

  $effect(() => {
    api
      .getSystemVersion()
      .then((v) => {
        if (v) version = v.currentVersion || ''
      })
      .catch(() => {})
  })

  function handleNav(tab: ActiveTab, e?: MouseEvent) {
    if (e) {
      if (e.ctrlKey || e.metaKey || e.shiftKey || e.altKey || e.button !== 0) return
      e.preventDefault()
    }
    navigate(tab)
    onClose?.()
  }

  function isLinkActive(link: NavLink): boolean {
    return linkMatches(link, activeTab)
  }

  function toggleGroup(id: string) {
    openGroups[id] = !openGroups[id]
    try {
      localStorage.setItem(id === 'media' ? MEDIA_OPEN_KEY : `${GROUP_OPEN_KEY}${id}-open`, openGroups[id] ? '1' : '0')
    } catch {}
  }

  $effect(() => {
    const group = NAV_GROUPS.find((item) => item.links.some((link) => linkMatches(link, activeTab)))
    if (group) openGroups[group.id] = true
  })

  const gatewayLabel = $derived(
    totalConnections === 0
      ? 'No providers'
      : activeConnections > 0
        ? 'Gateway online'
        : 'Gateway idle'
  )
  const gatewayTone = $derived(
    totalConnections === 0 ? 'muted' : activeConnections > 0 ? 'ok' : 'idle'
  )
</script>

<aside
  class="flex h-full w-full select-none flex-col overflow-hidden border-r border-border-subtle bg-sidebar"
>
  <!-- Wordmark, version, collapse toggle -->
  <div
    class="flex border-b border-border-subtle {collapsed
      ? 'flex-col items-center gap-2 px-2 py-4'
      : 'items-center justify-between gap-2 px-5 py-5'}"
  >
    <a
      href={TAB_ROUTES.overview}
      onclick={(e) => handleNav('overview', e)}
      title={collapsed ? '9router-go' : undefined}
      class="flex min-w-0 cursor-pointer items-center gap-3"
    >
      <span
        class="flex size-10 flex-shrink-0 items-center justify-center overflow-hidden rounded-brand border border-border-subtle bg-surface-2 p-2"
      >
        <img src="/favicon.svg" alt="" class="h-full w-full object-contain" />
      </span>
      {#if !collapsed}
        <span class="flex min-w-0 flex-col gap-0.5">
          <span
            class="truncate font-headline text-[15px] font-semibold uppercase leading-tight tracking-[0.12em] text-text-main"
          >
            9router-go
          </span>
          <span class="ui-stat text-[11px] text-text-subtle">
            {version ? `v${version}` : 'v1.9.0'}
          </span>
        </span>
      {/if}
    </a>
    {#if onToggleCollapse}
      <button
        type="button"
        onclick={onToggleCollapse}
        aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        title={collapsed ? 'Expand sidebar (Ctrl/⌘+B)' : 'Collapse sidebar (Ctrl/⌘+B)'}
        class="hidden size-8 flex-shrink-0 cursor-pointer items-center justify-center rounded-brand text-text-subtle transition-colors hover:bg-surface-2 hover:text-text-main lg:flex"
      >
        <Icon name={collapsed ? 'sidebar-open' : 'sidebar-close'} size={18} />
      </button>
    {/if}
  </div>

  <nav
    class="custom-scrollbar min-h-0 flex-1 overflow-y-auto py-4 {collapsed ? 'px-2' : 'px-3'}"
    aria-label="Dashboard navigation"
  >
    {#each NAV_GROUPS as group, gi (group.id)}
      <section class="mb-4 last:mb-0" aria-labelledby="nav-heading-{group.id}">
        {#if collapsed}
          <h2 id="nav-heading-{group.id}" class="sr-only">{group.label}</h2>
          {#if gi > 0}<div class="mx-2 mb-3 h-px bg-border-subtle" aria-hidden="true"></div>{/if}
          <button
            type="button"
            onclick={() => toggleGroup(group.id)}
            aria-expanded={openGroups[group.id]}
            aria-controls="nav-links-{group.id}"
            aria-label="{openGroups[group.id] ? 'Hide' : 'Show'} {group.label}"
            title="{group.label} · {openGroups[group.id] ? 'Collapse' : 'Expand'}"
            class="flex w-full cursor-pointer items-center justify-center rounded-brand py-1.5 transition-colors hover:bg-surface-2 {group.links.some((link) => isLinkActive(link))
              ? 'text-primary'
              : 'text-text-subtle hover:text-text-muted'}"
          >
            <Icon name={group.id === 'media' ? 'media' : group.links[0].icon} />
          </button>
        {:else}
          <h2 id="nav-heading-{group.id}">
            <button
              type="button"
              onclick={() => toggleGroup(group.id)}
              aria-expanded={openGroups[group.id]}
              aria-controls="nav-links-{group.id}"
              class="flex w-full cursor-pointer items-center justify-between px-3 pb-1.5 font-code text-[10px] font-semibold uppercase tracking-[0.16em] text-text-subtle transition-colors hover:text-text-muted"
            >
              <span><span class="text-brass">{group.numeral}</span> · {group.label}</span>
              <Icon name="chevron-down" size={14} class="transition-transform duration-150 ease-imperial {openGroups[group.id] ? 'rotate-180' : ''}" />
            </button>
          </h2>
        {/if}

        {#if openGroups[group.id]}
          <ul id="nav-links-{group.id}" class="space-y-0.5">
            {#each group.links as link (link.tab)}
              {@render navItem(link)}
            {/each}
          </ul>
        {/if}
      </section>
    {/each}
  </nav>

  <!-- Footer: system destinations + gateway readout -->
  <div class="flex flex-shrink-0 flex-col gap-2 border-t border-border-subtle py-3 {collapsed ? 'px-2' : 'px-3'}">
    <ul class="space-y-0.5">
      {#each SYSTEM_NAV_GROUP.links as link (link.tab)}
        {@render navItem(link)}
      {/each}
    </ul>

    <a
      href={TAB_ROUTES.connections}
      onclick={(e) => handleNav('connections', e)}
      title={collapsed ? `${activeConnections}/${totalConnections} active · ${gatewayLabel}` : gatewayLabel}
      class="flex items-center gap-2 rounded-brand border border-border-subtle bg-surface py-2 transition-colors duration-150 ease-imperial hover:border-brass/50 {collapsed
        ? 'flex-col justify-center px-1'
        : 'justify-between px-3'}"
    >
      <span class="flex items-center gap-2 font-code text-[11px] uppercase tracking-[0.12em] text-text-muted {collapsed ? 'flex-col gap-1' : ''}">
        <span
          class="size-1.5 flex-shrink-0 rounded-full {gatewayTone === 'ok'
            ? 'bg-success'
            : gatewayTone === 'idle'
              ? 'bg-warning'
              : 'bg-text-subtle'}"
          aria-hidden="true"
        ></span>
        <span class="ui-stat text-text-main {collapsed ? 'tracking-normal' : ''}">{activeConnections}/{totalConnections}</span>
        {#if !collapsed}active{/if}
      </span>
      {#if !collapsed}
        <span class="truncate text-[11px] text-text-subtle">{gatewayLabel}</span>
      {/if}
    </a>
  </div>
</aside>

{#snippet navItem(link: NavLink)}
  {@const active = isLinkActive(link)}
  <li>
    <a
      href={TAB_ROUTES[link.tab]}
      onclick={(e) => handleNav(link.tab, e)}
      aria-current={active ? 'page' : undefined}
      aria-label={collapsed ? link.label : undefined}
      title={collapsed ? link.label : undefined}
      class="group flex cursor-pointer items-center border-l-2 py-1.5 text-[13px] transition-colors duration-150 ease-imperial {collapsed
        ? 'justify-center rounded-brand px-0'
        : 'gap-2.5 rounded-r-brand pl-2.5 pr-3'} {active
        ? 'border-primary bg-surface text-text-main'
        : 'border-transparent text-text-muted hover:bg-surface-2 hover:text-text-main'}"
    >
      <Icon name={link.icon} class={active ? 'text-primary' : 'text-text-subtle transition-colors group-hover:text-text-muted'} />
      {#if !collapsed}<span class="truncate">{link.label}</span>{/if}
    </a>
  </li>
{/snippet}


<script lang="ts">
  // Port of decolua/9router src/app/(dashboard)/dashboard/console-log/ConsoleLogClient.js
  import { Trash2 } from 'lucide-svelte'
  import { getAuthHeaders } from '../api/client'
  import Button from '../lib/ui/Button.svelte'
  import Card from '../lib/ui/Card.svelte'
  import { notifications } from '../lib/notifications'

  const MAX_LINES = 200

  // Semantic tokens only (DESIGN.md §2): raw Tailwind palettes are banned in views.
  const LOG_LEVEL_COLORS: Record<string, string> = {
    LOG: 'text-success',
    INFO: 'text-info',
    INF: 'text-info',
    WARN: 'text-warning',
    WRN: 'text-warning',
    ERROR: 'text-danger',
    ERR: 'text-danger',
    DEBUG: 'text-text-subtle',
    DBG: 'text-text-subtle',
  }

  const ANSI_RE = /\u001b\[[0-9;]*m/g
  const BRACKET_TAG_RE = /\[(\w+)\]/
  const LEADING_LEVEL_RE = /^(LOG|INFO|INF|WARN|WRN|ERROR|ERR|DEBUG|DBG)\b/

  let logs = $state<string[]>([])
  let logViewportEl = $state<HTMLDivElement | null>(null)
  let logLevel = $state('info')
  let levelBusy = $state(false)
  let clearBusy = $state(false)
  let levelError = $state('')
  let autoScroll = $state(true)
  let connection = $state<'idle' | 'connecting' | 'connected' | 'reconnecting'>('idle')
  let lastStreamError = $state<string | null>(null)
  // A short, predictable retry: this stream is served by the local gateway, so a
  // fixed 5s recovers quickly after a restart or a config change. Exponential
  // backoff up to 30s only delayed recovery here; the visible status and the
  // manual "Retry now" cover the dead-gateway case instead.
  const STREAM_RETRY_MS = 5000
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let reconnectNow: (() => void) | null = null

  const LOG_LEVELS = ['debug', 'info', 'warn', 'error']

  /** Server lines can still carry terminal colors (e.g. captured stdout). */
  function stripAnsi(line: string): string {
    return line.replace(ANSI_RE, '')
  }

  /** Same rule as Next: a level tag wins, everything else is green. */
  function levelColor(line: string): string {
    const stripped = stripAnsi(line)
    const tagged = stripped.match(BRACKET_TAG_RE)?.[1] ?? ''
    const level = tagged || stripped.match(LEADING_LEVEL_RE)?.[1] || ''
    return LOG_LEVEL_COLORS[level] || 'text-success'
  }

  function trim(lines: string[]): string[] {
    return lines.length > MAX_LINES ? lines.slice(-MAX_LINES) : lines
  }

  async function fetchLogLevel() {
    try {
      const res = await fetch('/translator/console-logs/level', {
        headers: getAuthHeaders(),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.json()
      if (typeof data.level === 'string') {
        logLevel = data.level
        levelError = ''
      }
    } catch (err) {
      // Surface once, inline — no toast: the GET can 401 before login and a
      // toast on every mount would be noise. The selector still applies
      // changes, and that path reports its own failures as a toast.
      levelError = err instanceof Error ? err.message : String(err)
    }
  }

  async function handleLevelChange(e: Event) {
    const next = (e.target as HTMLSelectElement).value
    const prev = logLevel
    logLevel = next
    levelBusy = true
    try {
      const res = await fetch('/translator/console-logs/level', {
        method: 'PUT',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ level: next }),
      })
      const data = await res.json().catch(() => null)
      if (!res.ok || !data?.success) throw new Error(data?.error || `HTTP ${res.status}`)
      logLevel = data.level || next
      levelError = ''
      notifications.success(`Log level set to ${logLevel} (no restart needed)`)
    } catch (err) {
      logLevel = prev
      notifications.error(`Failed to set log level: ${err instanceof Error ? err.message : err}`)
    } finally {
      levelBusy = false
    }
  }

  async function handleClear() {
    clearBusy = true
    try {
      const res = await fetch('/translator/console-logs', {
        method: 'DELETE',
        headers: getAuthHeaders(),
      })
      const data = await res.json().catch(() => null)
      if (!res.ok || !data?.success) throw new Error(data?.error || `HTTP ${res.status}`)
      // Server clears the buffer; the SSE "clear" event empties the view.
      // If the stream is not connected (paused/reconnecting), clear locally
      // now so the view matches the server state.
      if (connection !== 'connected') logs = []
    } catch (err) {
      notifications.error(`Failed to clear console logs: ${err instanceof Error ? err.message : err}`)
    } finally {
      clearBusy = false
    }
  }

  // EventSource cannot send the Authorization header the dashboard API requires,
  // so read the SSE body off fetch — same wire format as the Next EventSource.
  fetchLogLevel()
  $effect(() => {
    let isCancelled = false
    let controller: AbortController | null = null

    const connect = async () => {
      if (isCancelled) return
      connection = 'connecting'
      try {
        controller = new AbortController()
        const res = await fetch('/translator/console-logs/stream', {
          headers: getAuthHeaders(),
          signal: controller.signal,
        })
        if (!res.ok) throw new Error(`HTTP ${res.status}`)

        const reader = res.body?.getReader()
        const decoder = new TextDecoder()
        if (!reader) throw new Error('response body is not readable')

        connection = 'connected'
        lastStreamError = null

        let buffer = ''
        while (!isCancelled) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() || ''

          for (const line of lines) {
            const trimmed = line.trim()
            if (!trimmed || trimmed.startsWith(':')) continue
            if (!trimmed.startsWith('data: ')) continue

            try {
              const msg = JSON.parse(trimmed.slice(6))
              if (msg.type === 'init') {
                logs = trim((msg.logs || []).slice(-MAX_LINES))
              } else if (msg.type === 'line') {
                logs = trim([...logs, msg.line])
              } else if (msg.type === 'lines') {
                logs = trim([...logs, ...(msg.lines || [])])
              } else if (msg.type === 'clear') {
                logs = []
              }
            } catch {
              // ignore malformed frames
            }
          }
        }
      } catch (err) {
        // Aborts (unmount/navigation) stop here; real failures retry below.
        if (!isCancelled && !(err instanceof DOMException && err.name === 'AbortError')) {
          lastStreamError = err instanceof Error ? err.message : String(err)
        }
      }

      if (!isCancelled) {
        connection = 'reconnecting'
        retryTimer = setTimeout(() => {
          retryTimer = null
          connect()
        }, STREAM_RETRY_MS)
      }
    }

    // Manual retry: only fires while a retry is actually pending, so a double
    // click cannot open two streams.
    reconnectNow = () => {
      if (!retryTimer) return
      clearTimeout(retryTimer)
      retryTimer = null
      connect()
    }

    connect()

    return () => {
      isCancelled = true
      reconnectNow = null
      controller?.abort()
      if (retryTimer) {
        clearTimeout(retryTimer)
        retryTimer = null
      }
      connection = 'idle'
    }
  })

  // Auto-scroll follows new lines only while the operator is already at the
  // bottom (or has pinned it back on). Scrolling up to read old lines must not
  // yank the viewport down; a short stick window absorbs the scrollHeight
  // change the append itself causes.
  const STICK_MS = 150
  let lastAutoScrollAt = 0

  function handleLogScroll() {
    if (!logViewportEl) return
    if (Date.now() - lastAutoScrollAt < STICK_MS) return
    autoScroll = logViewportEl.scrollHeight - logViewportEl.scrollTop - logViewportEl.clientHeight <= 48
  }

  function scrollLogsToBottom() {
    if (!logViewportEl) return
    lastAutoScrollAt = Date.now()
    autoScroll = true
    logViewportEl.scrollTop = logViewportEl.scrollHeight
  }

  $effect(() => {
    void logs.length
    if (autoScroll && logViewportEl) {
      lastAutoScrollAt = Date.now()
      logViewportEl.scrollTop = logViewportEl.scrollHeight
    }
  })
</script>

<div class="">
  <Card>
    <div class="flex items-center justify-between gap-2 px-4 pt-3 pb-2">
      <div class="flex items-center gap-2 min-w-0">
        <span class="relative inline-flex h-2 w-2 shrink-0" aria-hidden="true">
          {#if connection === 'connected'}
            <span class="absolute inset-0 rounded-full bg-success"></span>
          {:else if connection === 'connecting'}
            <span class="absolute inset-0 rounded-full bg-warning animate-pulse"></span>
          {:else if connection === 'reconnecting'}
            <span class="absolute inset-0 rounded-full bg-danger animate-pulse"></span>
          {:else}
            <span class="absolute inset-0 rounded-full bg-text-subtle"></span>
          {/if}
        </span>
        <span class="text-xs text-text-muted truncate" role="status" aria-live="polite">
          {#if connection === 'connected'}
            Live
          {:else if connection === 'connecting'}
            Connecting…
          {:else if connection === 'reconnecting'}
            Reconnecting…
          {:else}
            Paused
          {/if}
        </span>
      </div>
      <div class="flex items-center gap-2">
      <label class="text-xs text-text-muted" for="console-log-level">Level</label>
      <select
        id="console-log-level"
        class="text-xs bg-surface border border-border rounded-brand px-2 py-1.5 text-text-primary disabled:opacity-50"
        value={logLevel}
        disabled={levelBusy}
        onchange={handleLevelChange}
      >
        {#each LOG_LEVELS as lvl}
          <option value={lvl}>{lvl}</option>
        {/each}
      </select>
      <Button size="sm" variant="outline" onclick={handleClear} disabled={clearBusy}>
        <Trash2 class="w-3.5 h-3.5" />
        Clear
      </Button>
      {#if !autoScroll}
        <Button size="sm" variant="ghost" onclick={scrollLogsToBottom}>
          Jump to latest
        </Button>
      {/if}
      </div>
    </div>
    {#if levelError}
      <div class="px-4 pb-2 text-xs text-warning truncate" role="status">
        Log level unavailable ({levelError}) — the selector still applies changes.
      </div>
    {/if}
    {#if connection === 'reconnecting'}
      <div class="flex flex-wrap items-center justify-between gap-2 px-4 pb-2 text-xs text-danger" role="alert">
        <!-- A dead gateway can end the stream cleanly (EOF) or fail mid-read;
             both are surfaced here, and Retry now covers the second case. -->
        <span class="min-w-0 truncate">
          {lastStreamError ? `Stream error: ${lastStreamError} — retrying automatically.` : 'Stream ended — reconnecting.'}
        </span>
        <Button size="sm" variant="danger" onclick={() => reconnectNow?.()}>Retry now</Button>
      </div>
    {/if}
    <div
      bind:this={logViewportEl}
      onscroll={handleLogScroll}
      aria-live="polite"
      aria-label="Console log output"
      class="custom-scrollbar bg-black rounded-b-lg p-4 text-xs font-mono h-[calc(100vh-220px)] overflow-y-auto"
    >
      {#if logs.length === 0}
        <span class="text-text-muted">No console logs yet.</span>
      {:else}
        <div class="space-y-0.5">
          {#each logs as line, i (i)}
            <div><span class={levelColor(line)}>{stripAnsi(line)}</span></div>
          {/each}
        </div>
      {/if}
    </div>
  </Card>
</div>

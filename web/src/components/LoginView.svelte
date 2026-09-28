<script lang="ts">
  import { onMount, tick } from 'svelte'
  import { api } from '../api/client'

  let {
    onSuccess,
    sessionExpired = false,
  }: {
    onSuccess?: () => void
    /** True when the shell was showing and the session ended mid-use, so the
     * page explains why it is asking for the password again. */
    sessionExpired?: boolean
  } = $props()

  let password = $state('')
  let showPassword = $state(false)
  let isLoading = $state(false)
  let errorMessage = $state('')
  let resetHint = $state('')
  let retryAfter = $state(0)
  let hasPassword = $state<boolean | null>(null)
  let usesDefaultPassword = $state(false)
  let authMode = $state('password')
  let ssoType = $state('oidc')
  let oidcConfigured = $state(false)
  let oidcLoginLabel = $state('Sign in with OIDC')
  let samlConfigured = $state(false)
  let samlLoginLabel = $state('Sign in with SAML SSO')
  let mustChange = $state(false)
  let pagePort = $state('20130')
  let passwordInput = $state<HTMLInputElement | undefined>()

  // Countdown for rate-limit lockout (upstream retryAfter).
  $effect(() => {
    if (retryAfter <= 0) return
    const id = setInterval(() => {
      retryAfter = retryAfter > 0 ? retryAfter - 1 : 0
    }, 1000)
    return () => clearInterval(id)
  })

  onMount(() => {
    pagePort = window.location.port || (window.location.protocol === 'https:' ? '443' : '80')
    // Surface SSO callback failures (?error=...) like upstream /login.
    const queryError = new URLSearchParams(window.location.search).get('error')
    if (queryError) errorMessage = queryError
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 5000)
    fetch('/api/auth/status', { signal: controller.signal })
      .then(async (res) => {
        clearTimeout(timeoutId)
        if (!res.ok) {
          // Safe fallback to avoid an infinite loading state.
          hasPassword = true
          return
        }
        const data = await res.json()
        if (data.authenticated === true || data.requireLogin === false) {
          if (onSuccess) {
            onSuccess()
          } else {
            window.location.assign('/dashboard')
          }
          return
        }
        hasPassword = !!data.hasPassword
        usesDefaultPassword = data.usesDefaultPassword === true
        authMode = data.authMode || 'password'
        ssoType = data.ssoType || 'oidc'
        oidcConfigured = data.oidcConfigured === true
        oidcLoginLabel = data.oidcLoginLabel || 'Sign in with OIDC'
        samlConfigured = data.samlConfigured === true
        samlLoginLabel = data.samlLoginLabel || 'Sign in with SAML SSO'
      })
      .catch(() => {
        clearTimeout(timeoutId)
        hasPassword = true
      })
  })

  type LoginFailure = Error & {
    retryAfter?: number
    resetHint?: string
    mustChangePassword?: boolean
  }

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault()
    if (!password.trim() || isLoading || retryAfter > 0) return

    isLoading = true
    errorMessage = ''
    resetHint = ''

    try {
      const res = await api.login(password)
      if (res.success) {
        if (onSuccess) {
          onSuccess()
        } else {
          window.location.assign('/dashboard')
        }
      } else {
        errorMessage = res.error || 'Invalid password'
      }
    } catch (err: unknown) {
      const failure = err as LoginFailure
      if (failure?.mustChangePassword) {
        // Remote fresh install on the well-known default: rotate first. The
        // login attempt never issues a session, so set the new password from
        // the local host (or set INITIAL_PASSWORD) before retrying.
        mustChange = true
        errorMessage = err instanceof Error ? err.message : 'Default password must be changed before remote access.'
      } else {
        errorMessage = err instanceof Error ? err.message : 'Invalid password'
        if (typeof failure?.retryAfter === 'number') retryAfter = failure.retryAfter
        if (typeof failure?.resetHint === 'string') resetHint = failure.resetHint
      }
    } finally {
      isLoading = false
    }
  }

  function handleOidcLogin() {
    window.location.href = '/api/auth/oidc/start'
  }

  function handleSamlLogin() {
    window.location.href = '/api/auth/saml/start'
  }

  const isSsoEnabled = $derived(['sso', 'oidc', 'saml', 'both'].includes(authMode))
  const activeSsoType = $derived(ssoType || (authMode === 'saml' ? 'saml' : 'oidc'))
  const samlAvailable = $derived(isSsoEnabled && activeSsoType === 'saml' && samlConfigured)
  const oidcAvailable = $derived(isSsoEnabled && activeSsoType === 'oidc' && oidcConfigured)
  const ssoAvailable = $derived(samlAvailable || oidcAvailable)
  const passwordAvailable = $derived(authMode === 'password' || authMode === 'both' || !ssoAvailable)

  const methodLabel = $derived(
    samlAvailable
      ? 'SAML 2.0 Single Sign-On'
      : oidcAvailable
        ? 'OIDC Single Sign-On'
        : 'Operator password'
  )
  // Focus only after the field actually mounts. Native autofocus fires during
  // navigation and steals focus from SSO and assistive-technology controls.
  $effect(() => {
    if (hasPassword === null) return
    const focusPassword = !mustChange && passwordAvailable && !oidcAvailable
    let active = true
    tick().then(() => {
      if (!active) return
      if (focusPassword && document.activeElement === document.body) passwordInput?.focus()
    })
    return () => { active = false }
  })
</script>

<div class="relative flex min-h-dvh w-full flex-col bg-bg">
  <div class="landing-grid pointer-events-none absolute inset-0" aria-hidden="true"></div>

  <div class="relative z-10 flex flex-1 items-center justify-center px-4 py-10 sm:px-6 lg:px-10">
    <div class="grid w-full max-w-5xl grid-cols-1 gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,26rem)] lg:items-center lg:gap-16">
      <!-- Identity: monumental, calm, no ornament -->
      <section class="flex flex-col items-start gap-5">
        <div class="flex items-center gap-3">
          <span
            class="flex size-12 items-center justify-center overflow-hidden rounded-brand border border-border-subtle bg-surface p-2.5 shadow-[var(--shadow-soft)]"
          >
            <img src="/favicon.svg" alt="" class="h-full w-full object-contain" />
          </span>
          <span class="ui-kicker">Local gateway</span>
        </div>

        <div class="flex flex-col gap-3">
          <h1 class="ui-heading text-text-main">9router-go</h1>
          <p class="max-w-md text-sm leading-relaxed text-text-muted">
            A high-throughput AI gateway that routes your clients to the providers you
            configured. Sign in to reach the control surface on this host.
          </p>
        </div>

        <!-- Facts, stated plainly; nothing here is decorative -->
        <dl
          class="grid w-full max-w-md grid-cols-1 gap-px overflow-hidden rounded-brand border border-border-subtle bg-border-subtle sm:grid-cols-3"
        >
          <div class="flex flex-col gap-1 bg-surface px-3.5 py-3">
            <dt class="font-code text-[10px] uppercase tracking-[0.16em] text-text-subtle">Port</dt>
            <dd class="ui-stat text-sm text-text-main">{pagePort}</dd>
          </div>
          <div class="flex flex-col gap-1 bg-surface px-3.5 py-3">
            <dt class="font-code text-[10px] uppercase tracking-[0.16em] text-text-subtle">Method</dt>
            <dd class="truncate text-sm text-text-main" title={methodLabel}>{methodLabel}</dd>
          </div>
          <div class="flex flex-col gap-1 bg-surface px-3.5 py-3">
            <dt class="font-code text-[10px] uppercase tracking-[0.16em] text-text-subtle">Session</dt>
            <dd class="text-sm text-text-main">
              {hasPassword === null ? 'Checking…' : hasPassword ? 'Password set' : 'Not set'}
            </dd>
          </div>
        </dl>
      </section>

      <!-- Authentication -->
      <section class="w-full">
        {#if hasPassword === null}
          <div class="ui-panel flex items-center justify-center gap-3 p-8">
            <span
              class="inline-block size-5 animate-spin rounded-full border-2 border-border border-t-primary"
            ></span>
            <span class="ui-kicker">Checking session</span>
          </div>
        {:else}
          <div class="ui-panel flex flex-col gap-5 p-6 shadow-elevated sm:p-7">
            <div class="flex flex-col gap-1">
              <h2 class="font-headline text-xl text-text-main">Sign in</h2>
              <p class="text-[13px] text-text-muted">
                {#if samlAvailable}
                  Continue with your SAML 2.0 identity provider.
                {:else if oidcAvailable}
                  Continue with your OIDC provider to reach the dashboard.
                {:else}
                  Enter the operator password for this gateway.
                {/if}
              </p>
            </div>

            {#if sessionExpired && !mustChange}
              <p
                class="rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning"
                role="status"
              >
                Your session ended. Sign in again to continue.
              </p>
            {/if}

            {#if mustChange}
              <div class="rounded-brand border border-warning/30 bg-warning/10 px-3 py-3 text-xs text-warning" role="alert">
                <p>{errorMessage || 'Default password must be changed before remote access.'}</p>
                <p class="mt-2">
                  Open this dashboard on the machine running the gateway, sign in, and change the
                  password in Settings. Alternatively, set <code class="ui-code px-1 py-0.5">INITIAL_PASSWORD</code>
                  and restart the gateway, then sign in here again.
                </p>
              </div>
            {:else}
              <div class="flex flex-col gap-4">
                {#if samlAvailable}
                  <button
                    type="button"
                    onclick={handleSamlLogin}
                    class="h-10 w-full cursor-pointer rounded-brand bg-primary px-4 text-sm font-semibold text-bg shadow-soft transition-colors hover:bg-primary-hover"
                  >
                    {samlLoginLabel}
                  </button>
                {/if}

                {#if oidcAvailable}
                  <button
                    type="button"
                    onclick={handleOidcLogin}
                    class="h-10 w-full cursor-pointer rounded-brand bg-primary px-4 text-sm font-semibold text-bg shadow-soft transition-colors hover:bg-primary-hover"
                  >
                    {oidcLoginLabel}
                  </button>
                {/if}

                {#if ssoAvailable && passwordAvailable}
                  <div class="ui-divider" role="separator"></div>
                {/if}

                {#if passwordAvailable}
                  <form onsubmit={handleSubmit} class="flex flex-col gap-4">
                    {#if isSsoEnabled && !ssoAvailable}
                      <p class="rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning">
                        {activeSsoType === 'saml' ? 'SAML SSO' : 'OIDC'} is enabled but its
                        configuration is incomplete. Password sign-in remains available for recovery.
                      </p>
                    {/if}

                    {#if authMode === 'both' && ssoAvailable}
                      <p class="text-xs text-text-muted">
                        Password and {activeSsoType === 'saml' ? 'SAML SSO' : 'OIDC'} are both
                        enabled.
                      </p>
                    {/if}

                    <div class="flex flex-col gap-2">
                      <label for="login-password" class="text-[13px] font-medium text-text-main">
                        Password
                      </label>
                      <div class="relative">
                        <input
                          bind:this={passwordInput}
                          id="login-password"
                          type={showPassword ? 'text' : 'password'}
                          placeholder="Enter password"
                          bind:value={password}
                          required
                          aria-invalid={errorMessage ? 'true' : undefined}
                          class="ui-input w-full pr-11 text-sm"
                        />
                        <button
                          type="button"
                          onclick={() => (showPassword = !showPassword)}
                          class="absolute inset-y-0 right-0 flex cursor-pointer items-center pr-3 text-text-muted transition-colors hover:text-text-main"
                          aria-label={showPassword ? 'Hide password' : 'Show password'}
                        >
                          <span class="material-symbols-outlined text-[20px]" aria-hidden="true">
                            {showPassword ? 'visibility_off' : 'visibility'}
                          </span>
                        </button>
                      </div>

                      {#if errorMessage}
                        <p class="text-xs text-danger" role="alert">{errorMessage}</p>
                      {/if}
                      {#if retryAfter > 0}
                        <p class="text-xs text-warning" role="status">
                          Too many attempts. Retry in
                          <span class="ui-stat font-semibold">{retryAfter}s</span>.
                        </p>
                      {/if}
                      {#if resetHint}
                        <p class="text-xs text-text-muted">{resetHint}</p>
                      {/if}
                    </div>

                    <button
                      type="submit"
                      disabled={isLoading || !password || retryAfter > 0}
                      class="flex h-10 w-full cursor-pointer items-center justify-center gap-2 rounded-brand bg-primary px-4 text-sm font-semibold text-bg shadow-soft transition-colors hover:bg-primary-hover disabled:cursor-not-allowed disabled:bg-surface-3 disabled:text-text-muted"
                    >
                      {#if isLoading}
                        <span
                          class="inline-block size-4 animate-spin rounded-full border-2 border-bg/30 border-t-bg"
                        ></span>
                        <span>Signing in…</span>
                      {:else if retryAfter > 0}
                        <span class="ui-stat">Wait {retryAfter}s</span>
                      {:else}
                        <span>Sign in</span>
                      {/if}
                    </button>

                    {#if usesDefaultPassword}
                      <p class="text-center text-xs text-text-muted">
                        Default password is
                        <code class="ui-code px-1.5 py-0.5 text-[11px]">123456</code>
                      </p>
                    {/if}
                    {#if hasPassword === false}
                      <p class="rounded-brand border border-warning/30 bg-warning/10 px-3 py-2 text-center text-xs text-warning">
                        No password is set. You will be asked to set one when signing in remotely.
                      </p>
                    {/if}
                  </form>
                {:else if errorMessage}
                  <p class="text-xs text-danger" role="alert">{errorMessage}</p>
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      </section>
    </div>
  </div>
</div>

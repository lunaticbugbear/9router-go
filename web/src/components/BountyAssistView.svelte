<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type BountyHelper, type BountyHelperKind, type BountyProfile } from '../api/client'
  import Card from '../lib/ui/Card.svelte'
  import Button from '../lib/ui/Button.svelte'
  import ConfirmModal from '../lib/ui/ConfirmModal.svelte'
  import Input from '../lib/ui/Input.svelte'

  let profiles = $state<BountyProfile[]>([])
  let helpers = $state<BountyHelper[]>([])
  let selectedId = $state('')
  let editing = $state(false)
  let loading = $state(true)
  let saving = $state(false)
  let building = $state(false)
  let deleting = $state(false)
  let confirmDeleteId = $state('')
  let error = $state('')
  let notice = $state('')
  let preview = $state('')
  let previewId = $state('')
  let helperKind = $state<BountyHelperKind>('safe-plan')
  let evidence = $state('')
  let helperPrompt = $state('')
  let helperProfileId = $state('')
  let inScopeText = $state('')
  let outOfScopeText = $state('')
  let draft = $state<BountyProfile>({
    id: '', program: '', programUrl: '', inScope: [], outOfScope: [], rules: '', customContext: '',
  })

  // Selecting a profile twice must not let the slower response win.
  let previewSeq = 0
  let buildSeq = 0

  const selected = $derived(profiles.find((p) => p.id === selectedId) ?? null)
  // The backend bounds these with Go's len(), i.e. UTF-8 BYTES, so a non-ASCII
  // program or rule can be well under the character count and still be rejected.
  // Mirroring the byte semantics here keeps the inline message truthful.
  const ID_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/
  const MAX_ID = 64
  const MAX_PROGRAM = 160
  const MAX_PROGRAM_URL = 2048
  const MAX_SCOPE_ITEMS = 100
  const MAX_SCOPE_ITEM = 512
  const MAX_RULES = 4000
  // The gateway caps the whole helper POST body at 64 KiB. Evidence can contain
  // quotes, backslashes or newlines that JSON escaping roughly doubles, so the
  // client measures the exact serialized payload it will send rather than
  // reserving a fixed budget for the envelope.
  const MAX_HELPER_BODY = 64 * 1024

  const utf8Length = (value: string) => new TextEncoder().encode(value).length

  // An existing profile's id is the header value clients already send; making it
  // editable would silently orphan those requests.
  const isEditingExisting = $derived(Boolean(draft.id) && profiles.some((p) => p.id === draft.id))
  const headerInvalid = $derived.by(() => {
    const id = draft.id.trim()
    if (!id) return ''
    if (utf8Length(id) > MAX_ID) return `Profile id must be at most ${MAX_ID} bytes (UTF-8).`
    if (!ID_PATTERN.test(id)) return 'Profile id must start with a letter or digit and use only letters, digits, ".", "_" or "-".'
    return ''
  })
  const programInvalid = $derived.by(() => {
    const name = draft.program.trim()
    if (!name) return ''
    if (utf8Length(name) > MAX_PROGRAM) return `Program name must be at most ${MAX_PROGRAM} bytes (UTF-8; non-ASCII text counts more than one byte per character).`
    return ''
  })
  const programUrlInvalid = $derived.by(() => {
    // saveProfile sends the trimmed URL, so measure the same value: leading or
    // trailing whitespace is removed before the backend ever sees it.
    const url = (draft.programUrl ?? '').trim()
    if (utf8Length(url) > MAX_PROGRAM_URL) return `Program URL must be at most ${MAX_PROGRAM_URL} bytes (UTF-8).`
    return ''
  })
  const inScopeInvalid = $derived.by(() => scopeIssue(splitLines(inScopeText), 'in-scope'))
  const outOfScopeInvalid = $derived.by(() => scopeIssue(splitLines(outOfScopeText), 'out-of-scope'))
  const rulesInvalid = $derived.by(() => byteIssue(draft.rules ?? '', MAX_RULES, 'Program testing rules'))
  const customContextInvalid = $derived.by(() => byteIssue(draft.customContext ?? '', MAX_RULES, 'Additional authorization context'))
  // The exact bytes the helper POST will carry, so JSON escaping of quotes,
  // backslashes and newlines is counted the same way the gateway counts it.
  // api.buildBountyHelper serializes exactly { profileId, kind, evidence }.
  const helperPayloadBytes = $derived(utf8Length(JSON.stringify({ profileId: selectedId, kind: helperKind, evidence })))
  const evidenceInvalid = $derived(
    helperPayloadBytes > MAX_HELPER_BODY
      ? `This request body is ${helperPayloadBytes} bytes; the gateway limit is ${MAX_HELPER_BODY} bytes. Shorten the evidence (quotes, backslashes and newlines count as escaped bytes).`
      : ''
  )
  // A body mismatch is reported as an HTTP 400, so block it before the request.
  const hasBlockingProblem = $derived(
    Boolean(headerInvalid || programInvalid || programUrlInvalid || inScopeInvalid || outOfScopeInvalid
      || rulesInvalid || customContextInvalid || !draft.program.trim() || splitLines(inScopeText).length === 0)
  )
  const canBuild = $derived(Boolean(selectedId) && helpers.length > 0 && !building && !evidenceInvalid)

  onMount(() => { void reload() })

  async function reload() {
    loading = true
    error = ''
    try {
      const [p, h] = await Promise.all([api.getBountyProfiles(), api.getBountyHelpers()])
      profiles = p.profiles ?? []
      helpers = h.helpers ?? []
      const hadSelection = selectedId !== ''
      const vanishedId = hadSelection ? selectedId : ''
      const selectionSurvived = profiles.some((p) => p.id === selectedId)
      if (selectionSurvived) {
        await loadPreview(selectedId)
      } else {
        // Never silently move the operator to a different program: auto-select
        // only on the initial load, and say so when a chosen profile disappears.
        preview = ''
        previewId = ''
        helperPrompt = ''
        helperProfileId = ''
        buildSeq++
        building = false
        if (hadSelection) {
          selectedId = ''
          notice = `The selected profile “${vanishedId}” no longer exists. Select or create a profile.`
        }
        if (!hadSelection && profiles.length > 0) {
          selectedId = profiles[0]!.id
          await loadPreview(selectedId)
        }
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  function startNew() {
    draft = { id: '', program: '', programUrl: '', inScope: [], outOfScope: [], rules: '', customContext: '' }
    inScopeText = ''
    outOfScopeText = ''
    editing = true
    error = ''
  }

  function startEdit(p: BountyProfile) {
    // p comes from the $state profiles array, so it is a reactive Proxy and
    // structuredClone rejects it; copy the known fields into a plain draft.
    draft = {
      id: p.id,
      program: p.program,
      programUrl: p.programUrl ?? '',
      inScope: [...p.inScope],
      outOfScope: [...(p.outOfScope ?? [])],
      rules: p.rules ?? '',
      customContext: p.customContext ?? '',
    }
    inScopeText = p.inScope.join('\n')
    outOfScopeText = (p.outOfScope ?? []).join('\n')
    editing = true
    error = ''
  }

  function cancelEdit() {
    editing = false
    error = ''
    draft = { id: '', program: '', programUrl: '', inScope: [], outOfScope: [], rules: '', customContext: '' }
    inScopeText = ''
    outOfScopeText = ''
  }

  function selectProfile(id: string, { loadPreview: fetchPreview = true }: { loadPreview?: boolean } = {}) {
    selectedId = id
    // Both the preview and any built prompt embed a specific profile. Clear them
    // immediately and invalidate an in-flight build so a late response cannot
    // land next to a different program's scope.
    preview = ''
    previewId = ''
    helperPrompt = ''
    helperProfileId = ''
    buildSeq++
    building = false
    if (id && fetchPreview) void loadPreview(id)
  }

  async function loadPreview(id: string) {
    const seq = ++previewSeq
    try {
      const result = await api.getBountyPromptPreview(id)
      if (seq !== previewSeq) return
      preview = result.context
      previewId = result.profile || id
    } catch (e) {
      if (seq !== previewSeq) return
      preview = ''
      previewId = ''
      error = e instanceof Error ? e.message : String(e)
    }
  }

  function splitLines(value: string): string[] {
    return value.split('\n').map((v) => v.trim()).filter(Boolean)
  }

  function scopeIssue(items: string[], label: string): string {
    if (items.length > MAX_SCOPE_ITEMS) return `${items.length} ${label} entries exceed the ${MAX_SCOPE_ITEMS}-entry limit.`
    const index = items.findIndex((item) => utf8Length(item) > MAX_SCOPE_ITEM)
    if (index >= 0) return `${label} entry ${index + 1} exceeds ${MAX_SCOPE_ITEM} bytes (UTF-8).`
    return ''
  }

  function byteIssue(value: string, max: number, label: string): string {
    const bytes = utf8Length(value)
    return bytes > max ? `${label} is ${bytes} bytes (UTF-8); the limit is ${max} bytes.` : ''
  }

  async function saveProfile() {
    error = ''
    notice = ''
    const profile: BountyProfile = {
      ...draft,
      id: draft.id.trim(),
      program: draft.program.trim(),
      programUrl: draft.programUrl?.trim() ?? '',
      inScope: splitLines(inScopeText),
      outOfScope: splitLines(outOfScopeText),
    }
    // Keep the trimmed values visible so the stored profile can be reviewed as sent.
    draft.id = profile.id
    draft.program = profile.program
    draft.programUrl = profile.programUrl
    inScopeText = profile.inScope.join('\n')
    outOfScopeText = profile.outOfScope.join('\n')

    if (!profile.id) { error = 'Profile id is required.'; return }
    if (headerInvalid) { error = headerInvalid; return }
    if (!profile.program) { error = 'Program name is required.'; return }
    if (programInvalid) { error = programInvalid; return }
    if (programUrlInvalid) { error = programUrlInvalid; return }
    if (profile.inScope.length === 0) { error = 'Add at least one in-scope asset; an empty scope is not authorization.'; return }
    if (inScopeInvalid || outOfScopeInvalid || rulesInvalid || customContextInvalid) {
      error = inScopeInvalid || outOfScopeInvalid || rulesInvalid || customContextInvalid
      return
    }

    saving = true
    try {
      await api.putBountyProfile(profile)
      editing = false
      // Reuse the selection path so switching profiles invalidates any in-flight
      // helper build and clears preview/prompt state the same way a click does.
      // reload() refreshes the list and loads the preview, so selectProfile's own
      // loadPreview would be a duplicate request.
      selectProfile(profile.id, { loadPreview: false })
      notice = `Saved scope profile “${profile.id}”.`
      await reload()
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
  }

  function requestDelete(id: string) {
    error = ''
    confirmDeleteId = id
  }

  async function confirmDelete() {
    const id = confirmDeleteId
    if (!id) return
    deleting = true
    try {
      await api.deleteBountyProfile(id)
      confirmDeleteId = ''
      profiles = profiles.filter((p) => p.id !== id)
      notice = `Deleted “${id}”.`
      if (selectedId === id) {
        // Reuse reload() so selection, preview and pending helper state follow
        // one path instead of a second ad-hoc copy.
        editing = false
        selectedId = ''
        await reload()
      }
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
      confirmDeleteId = ''
    } finally {
      deleting = false
    }
  }

  async function buildPrompt() {
    if (!selectedId) { error = 'Create or select a bounty profile first.'; return }
    if (helpers.length === 0) { error = 'This gateway did not return a helper template for the selected task.'; return }
    if (evidenceInvalid) { error = evidenceInvalid; return }
    error = ''
    notice = ''
    const profileId = selectedId
    const seq = ++buildSeq
    building = true
    try {
      const result = await api.buildBountyHelper(profileId, helperKind, evidence)
      if (seq !== buildSeq) return
      // The gateway embeds the profile context it looked up server-side, so only
      // label the output with the profile the response names.
      helperProfileId = result.profile || profileId
      helperPrompt = result.prompt
    } catch (e) {
      if (seq !== buildSeq) return
      helperPrompt = ''
      helperProfileId = ''
      error = e instanceof Error ? e.message : String(e)
    } finally {
      if (seq === buildSeq) building = false
    }
  }

  function clearEvidence() {
    evidence = ''
    error = ''
    // The built prompt embeds this evidence; keeping it after a clear would show
    // output the operator just asked to discard.
    helperPrompt = ''
    helperProfileId = ''
    // Invalidate the in-flight build AND release its spinner here: the request's
    // own `finally` sees the seq mismatch and will not reset `building`, so
    // leaving it set would keep Build disabled forever.
    buildSeq++
    building = false
  }

  async function copyText(value: string, label: string) {
    error = ''
    try {
      await navigator.clipboard.writeText(value)
      notice = `${label} copied.`
    } catch {
      error = 'Clipboard access was denied. Select the text and copy it manually.'
    }
  }
</script>

<div class="mx-auto flex w-full min-w-0 max-w-6xl flex-col gap-6">
  <header class="flex flex-col gap-2">
    <p class="ui-kicker">Tools</p>
    <h1 class="ui-heading text-text-main">Bug Bounty Assist</h1>
    <p class="max-w-3xl text-sm leading-relaxed text-text-muted">
      Store the program scope you are authorized to test and attach it to a request as visible engagement context, instead of leaving authorization implicit.
    </p>
    <p class="max-w-3xl text-xs leading-relaxed text-text-subtle">
      It adds scope facts to your own request and cannot change a provider’s policy. It never reveals or replaces a provider’s hidden system prompt.
    </p>
  </header>

  {#if error}
    <div role="alert" class="rounded-[4px] border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{error}</div>
  {/if}
  {#if notice}
    <div role="status" class="rounded-[4px] border border-success/40 bg-success/10 px-3 py-2 text-sm text-success">{notice}</div>
  {/if}

  <div class="grid min-w-0 gap-6 xl:grid-cols-[minmax(250px,0.8fr)_minmax(0,1.7fr)]">
    <Card class="min-w-0">
      <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
        <div class="min-w-0">
          <p class="ui-kicker">Programs</p>
          <h2 class="mt-0.5 font-headline text-lg font-medium leading-tight text-text-main">Program profiles</h2>
        </div>
        <Button onclick={startNew}>New profile</Button>
      </div>
      {#if loading}
        <div class="flex flex-col gap-2" role="status" aria-live="polite">
          <span class="sr-only">Loading profiles…</span>
          <div class="h-16 animate-pulse rounded-[4px] bg-surface-2" aria-hidden="true"></div>
          <div class="h-16 animate-pulse rounded-[4px] bg-surface-2" aria-hidden="true"></div>
        </div>
      {:else if profiles.length === 0}
        <div class="ui-empty text-sm">
          <p class="font-medium text-text-main">No scope profiles yet.</p>
          <p class="mt-1 leading-relaxed">
            Start with the program’s exact in-scope and out-of-scope assets. Empty scope is refused, so a request cannot imply authorization to an unspecified target.
          </p>
          <Button class="mt-3" variant="secondary" onclick={startNew}>New profile</Button>
        </div>
      {:else}
        <ul class="flex flex-col gap-2">
          {#each profiles as p (p.id)}
            {@const isSelected = selectedId === p.id}
            <li>
              <button
                type="button"
                aria-pressed={isSelected}
                class="w-full min-w-0 rounded-[4px] border px-3 py-2.5 text-left transition-colors duration-150 cursor-pointer {isSelected
                  ? 'border-primary/60 bg-surface-2'
                  : 'border-border-subtle hover:border-border hover:bg-surface-2/60'}"
                onclick={() => selectProfile(p.id)}
              >
                <span class="flex min-w-0 items-center gap-2">
                  {#if isSelected}
                    <span class="material-symbols-outlined shrink-0 text-[16px] text-primary" aria-hidden="true">check</span>
                  {/if}
                  <span class="min-w-0 flex-1 truncate text-sm font-medium text-text-main">{p.program}</span>
                </span>
                <span class="mt-0.5 block truncate font-code text-[11px] text-text-subtle">{p.id}</span>
                <span class="ui-stat mt-1 block text-[11px] text-text-muted">{p.inScope.length} in · {p.outOfScope?.length ?? 0} out</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      {#if selected && !editing}
        <div class="mt-4 flex flex-wrap gap-2 border-t border-border-subtle pt-3">
          <Button onclick={() => startEdit(selected!)}>Edit</Button>
          <Button variant="danger" onclick={() => requestDelete(selected!.id)}>Delete</Button>
        </div>
      {/if}
    </Card>

    <div class="flex min-w-0 flex-col gap-6">
      {#if editing}
        <Card class="min-w-0">
          <p class="ui-kicker">{isEditingExisting ? 'Edit' : 'Create'}</p>
          <h2 class="mt-0.5 mb-4 font-headline text-lg font-medium leading-tight text-text-main">{draft.id ? `Edit ${draft.program}` : 'New program profile'}</h2>
          <div class="grid min-w-0 gap-4 md:grid-cols-2">
            <Input
              label="Profile id"
              bind:value={draft.id}
              placeholder="h1-program-slug"
              disabled={isEditingExisting}
              error={headerInvalid}
              hint="Letters, digits, '.', '_' or '-'. The id is the header value sent by clients."
            />
            <Input label="Program name" bind:value={draft.program} placeholder="Program name" error={programInvalid} />
            <Input
              label="Program URL (optional)"
              bind:value={draft.programUrl}
              placeholder="https://hackerone.com/..."
              klass="md:col-span-2"
              error={programUrlInvalid}
            />
            <div class="min-w-0 text-sm">
              <label for="bounty-in-scope" class="block text-sm font-medium text-text-main">In-scope assets <span class="text-text-muted">(one per line)</span></label>
              <textarea
                id="bounty-in-scope"
                aria-describedby="bounty-in-scope-hint"
                aria-invalid={inScopeInvalid ? 'true' : undefined}
                rows={6}
                style={inScopeInvalid ? 'border-color: var(--color-danger)' : ''}
                class="ui-input mt-1 w-full font-code text-xs leading-relaxed"
                bind:value={inScopeText}
                placeholder="api.example.com&#10;app.example.com"
              ></textarea>
              <p id="bounty-in-scope-hint" class="ui-stat mt-1 text-[11px] text-text-muted">{splitLines(inScopeText).length} / {MAX_SCOPE_ITEMS} entries</p>
              {#if inScopeInvalid}
                <p class="mt-1 flex items-start gap-1 text-xs text-danger">
                  <span class="material-symbols-outlined text-[14px]" aria-hidden="true">error</span>
                  {inScopeInvalid}
                </p>
              {/if}
            </div>
            <div class="min-w-0 text-sm">
              <label for="bounty-out-of-scope" class="block text-sm font-medium text-text-main">Out-of-scope assets <span class="text-text-muted">(one per line)</span></label>
              <textarea
                id="bounty-out-of-scope"
                aria-describedby="bounty-out-of-scope-hint"
                aria-invalid={outOfScopeInvalid ? 'true' : undefined}
                rows={6}
                style={outOfScopeInvalid ? 'border-color: var(--color-danger)' : ''}
                class="ui-input mt-1 w-full font-code text-xs leading-relaxed"
                bind:value={outOfScopeText}
                placeholder="billing.example.com"
              ></textarea>
              <p id="bounty-out-of-scope-hint" class="ui-stat mt-1 text-[11px] text-text-muted">{splitLines(outOfScopeText).length} / {MAX_SCOPE_ITEMS} entries</p>
              {#if outOfScopeInvalid}
                <p class="mt-1 flex items-start gap-1 text-xs text-danger">
                  <span class="material-symbols-outlined text-[14px]" aria-hidden="true">error</span>
                  {outOfScopeInvalid}
                </p>
              {/if}
            </div>
            <div class="min-w-0 text-sm md:col-span-2">
              <label for="bounty-rules" class="block text-sm font-medium text-text-main">Program testing rules</label>
              <textarea
                id="bounty-rules"
                aria-invalid={rulesInvalid ? 'true' : undefined}
                rows={4}
                style={rulesInvalid ? 'border-color: var(--color-danger)' : ''}
                class="ui-input mt-1 w-full text-xs leading-relaxed"
                bind:value={draft.rules}
                placeholder="No destructive testing. No access to other users’ data."
              ></textarea>
              {#if rulesInvalid}
                <p class="mt-1 flex items-start gap-1 text-xs text-danger">
                  <span class="material-symbols-outlined text-[14px]" aria-hidden="true">error</span>
                  {rulesInvalid}
                </p>
              {/if}
            </div>
            <div class="min-w-0 text-sm md:col-span-2">
              <label for="bounty-custom-context" class="block text-sm font-medium text-text-main">Additional engagement context <span class="text-text-muted">(optional)</span></label>
              <textarea
                id="bounty-custom-context"
                aria-invalid={customContextInvalid ? 'true' : undefined}
                rows={3}
                style={customContextInvalid ? 'border-color: var(--color-danger)' : ''}
                class="ui-input mt-1 w-full text-xs leading-relaxed"
                bind:value={draft.customContext}
                placeholder="Account type, testing window, or other program-specific limits."
              ></textarea>
              {#if customContextInvalid}
                <p class="mt-1 flex items-start gap-1 text-xs text-danger">
                  <span class="material-symbols-outlined text-[14px]" aria-hidden="true">error</span>
                  {customContextInvalid}
                </p>
              {/if}
            </div>
          </div>
          <p class="mt-3 rounded-[4px] border border-warning/40 bg-warning/10 px-3 py-2 text-xs leading-relaxed text-warning">
            The profile is stored in the local database and sent to the AI provider when selected. Do not include API keys, passwords, or customer data. This is prompt context, not technical enforcement of network scope: it does not restrict where traffic goes and does not stop testing outside these assets.
          </p>
          <div class="mt-4 flex flex-wrap gap-2">
            <Button onclick={saveProfile} disabled={saving} loading={saving} title={hasBlockingProblem ? 'Fix the messages above before saving.' : ''}>
              {saving ? 'Saving…' : 'Save profile'}
            </Button>
            <Button variant="secondary" onclick={cancelEdit}>Cancel</Button>
          </div>
        </Card>
      {:else if selected}
        <Card class="min-w-0">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0">
              <p class="ui-kicker">Selector</p>
              <h2 class="mt-0.5 font-headline text-lg font-medium leading-tight text-text-main">Apply to a client request</h2>
              <p class="mt-1 text-sm leading-relaxed text-text-muted">The gateway adds this scope context to the outgoing system prompt only when the request selects this profile with the header below. No profile is applied by default.</p>
            </div>
            <Button onclick={() => copyText(`X-9Router-Bounty-Profile: ${selected!.id}`, 'Request header')}>Copy request header</Button>
          </div>
          <pre class="ui-code mt-3 px-3 py-2 text-xs">X-9Router-Bounty-Profile: {selected.id}</pre>
          <dl class="mt-4 space-y-2 text-xs">
            <div class="flex flex-wrap items-baseline gap-1.5">
              <dt class="text-text-muted">Stored program:</dt>
              <dd>{selected.program}</dd>
            </div>
            <div class="flex flex-wrap items-baseline gap-1.5">
              <dt class="text-text-muted">Stored scope:</dt>
              <dd class="ui-stat">{selected.inScope.length} in-scope · {selected.outOfScope?.length ?? 0} out-of-scope</dd>
            </div>
          </dl>
          <p class="mt-3 border-t border-border-subtle pt-3 text-xs leading-relaxed text-text-muted">
            Check these against the preview below before sending. The selector header is consumed locally and not forwarded upstream. The generated context does not override provider safety policies, and it does not prevent the model, the client, or you from testing outside the listed assets.
          </p>
        </Card>
      {/if}

      <Card class="min-w-0">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="ui-kicker">Preview</p>
            <h2 class="mt-0.5 font-headline text-lg font-medium leading-tight text-text-main">Scope context preview</h2>
            <p class="mt-1 text-xs leading-relaxed text-text-muted">The exact text the gateway prepends to the system prompt for the selected profile. It is not the provider’s hidden internal prompt, and it is not applied to a request that sends no selector header.</p>
          </div>
          {#if selected && preview}<Button onclick={() => copyText(preview, 'Context')}>Copy</Button>{/if}
        </div>
        {#if !selected}
          <div class="ui-empty mt-3 text-sm">
            <p class="font-medium text-text-main">No profile selected.</p>
            <p class="mt-1 leading-relaxed">Select a profile to preview the exact context the gateway would prepend.</p>
          </div>
        {:else}
          {#if previewId && previewId !== selected.id}
            <p class="mt-3 rounded-[4px] border border-warning/40 bg-warning/10 px-3 py-2 text-xs leading-relaxed text-warning">
              This preview belongs to “{previewId}”, not “{selected.id}”. Re-select “{selected.id}” to load its context.
            </p>
          {/if}
          <p class="mt-3 rounded-[4px] border border-border-subtle bg-surface-2 px-3 py-2 text-xs leading-relaxed text-text-muted">
            This text tells a provider which assets you say are authorized. It is a statement of scope, not a control: it does not restrict where traffic is sent and does not stop testing outside these assets.
          </p>
          <pre class="ui-code mt-3 max-h-64 overflow-auto whitespace-pre-wrap px-3 py-2 text-xs">{preview || 'Loading…'}</pre>
        {/if}
      </Card>

      <Card class="min-w-0">
        <p class="ui-kicker">Helpers</p>
        <h2 class="mt-0.5 font-headline text-lg font-medium leading-tight text-text-main">Hunter helpers</h2>
        <p class="mt-1 text-xs leading-relaxed text-text-muted">Build a scoped prompt for analysis or reporting. Evidence is sent to the local gateway to build the prompt and returned to this page; it is not written to the database and this helper does not call an AI provider.</p>
        <p class="mt-2 text-xs leading-relaxed text-text-subtle">The built prompt is a template you still choose to send. It does not authorize anything by itself and does not enforce the scope you listed.</p>
        <div class="mt-4 flex flex-col gap-4">
          {#if helpers.length === 0}
            <div class="ui-empty text-sm">
              <p class="font-medium text-text-main">No helper templates returned.</p>
              <p class="mt-1 leading-relaxed">This gateway build registered no bounty helper tasks, so there is nothing to build a prompt from.</p>
            </div>
          {/if}
          <div class="min-w-0">
            <label class="block text-sm font-medium text-text-main" for="bounty-helper-kind">Helper task</label>
            <select
              id="bounty-helper-kind"
              class="ui-input mt-1 w-full text-sm"
              bind:value={helperKind}
            >
              {#each helpers as helper (helper.id)}
                <option value={helper.id}>{helper.title} — {helper.description}</option>
              {/each}
            </select>
          </div>
          <div class="min-w-0">
            <label class="block text-sm font-medium text-text-main" for="bounty-helper-evidence">Evidence to analyze <span class="text-text-muted">(optional)</span></label>
            <textarea
              id="bounty-helper-evidence"
              aria-describedby="bounty-helper-evidence-hint"
              aria-invalid={evidenceInvalid ? 'true' : undefined}
              rows={5}
              style={evidenceInvalid ? 'border-color: var(--color-danger)' : ''}
              class="ui-input mt-1 w-full font-code text-xs leading-relaxed"
              bind:value={evidence}
              placeholder="Observed status, redacted response snippet, reproduction notes. Remove tokens and other users’ data."
            ></textarea>
            <p id="bounty-helper-evidence-hint" class="mt-1 text-xs leading-relaxed text-text-muted">
              Not saved in this browser and not stored by the gateway. Clearing it after use is still recommended; it remains in page memory while this tab is open.
            </p>
            {#if evidenceInvalid}
              <p class="mt-1 flex items-start gap-1 text-xs text-danger">
                <span class="material-symbols-outlined text-[14px]" aria-hidden="true">error</span>
                {evidenceInvalid}
              </p>
            {/if}
          </div>
          <div class="flex flex-wrap gap-2">
            <Button onclick={buildPrompt} disabled={!canBuild} loading={building}>{building ? 'Building…' : 'Build prompt'}</Button>
            {#if evidence}<Button variant="secondary" onclick={clearEvidence}>Clear evidence</Button>{/if}
            {#if helperPrompt}<Button variant="secondary" onclick={() => copyText(helperPrompt, `Prompt for “${helperProfileId}”`)}>Copy prompt</Button>{/if}
          </div>
          {#if helperPrompt}
            <div>
              <p class="text-xs leading-relaxed text-text-muted">Built with the scope profile “{helperProfileId}” and the selected helper task.</p>
              <pre class="ui-code mt-2 max-h-72 overflow-auto whitespace-pre-wrap px-3 py-2 text-xs">{helperPrompt}</pre>
            </div>
          {/if}
        </div>
      </Card>
    </div>
  </div>

  <ConfirmModal
    isOpen={confirmDeleteId !== ''}
    title="Delete profile"
    message={confirmDeleteId
      ? `Delete scope profile “${confirmDeleteId}”? Client requests that still send X-9Router-Bounty-Profile: ${confirmDeleteId} will start failing instead of being served without this context.`
      : 'Delete this profile?'}
    confirmText="Delete"
    loading={deleting}
    onClose={() => { if (!deleting) confirmDeleteId = '' }}
    onConfirm={confirmDelete}
  />
</div>

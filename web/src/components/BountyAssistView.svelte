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

<div class="mx-auto max-w-6xl space-y-4 p-4 md:p-6">
  <header>
    <h1 class="text-xl font-semibold">Bug Bounty Assist</h1>
    <p class="mt-1 max-w-4xl text-sm text-text-muted">
      Describe the program and its authorized scope so the assistant has explicit, visible engagement context instead of guessing at authorization. It only adds scope context to your own request and cannot change a provider’s policy. It never reveals or replaces a provider’s hidden system prompt.
    </p>
  </header>

  {#if error}
    <div role="alert" class="rounded border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">{error}</div>
  {/if}
  {#if notice}
    <div role="status" class="rounded border border-emerald-700/40 bg-emerald-950/30 px-3 py-2 text-sm text-emerald-300">{notice}</div>
  {/if}

  <div class="grid gap-4 xl:grid-cols-[minmax(250px,0.8fr)_minmax(0,1.7fr)]">
    <Card>
      <div class="mb-3 flex items-center justify-between">
        <h2 class="font-semibold">Program profiles</h2>
        <Button onclick={startNew}>New profile</Button>
      </div>
      {#if loading}
        <p class="text-sm text-text-muted">Loading…</p>
      {:else if profiles.length === 0}
        <div class="rounded border border-slate-700 bg-slate-900/40 p-3 text-sm text-text-muted">
          No profiles yet. Start with the program’s exact in-scope and out-of-scope assets. Empty scope is refused so a request cannot imply authorization to an unspecified target.
        </div>
      {:else}
        <ul class="space-y-2">
          {#each profiles as p (p.id)}
            {@const isSelected = selectedId === p.id}
            <li>
              <button
                type="button"
                aria-pressed={isSelected}
                class="w-full rounded border px-3 py-2 text-left {isSelected
                  ? 'border-brand-500 bg-brand-950/30'
                  : 'border-slate-700 hover:border-slate-500'}"
                onclick={() => selectProfile(p.id)}
              >
                <span class="block font-medium">{p.program}</span>
                <span class="block font-mono text-xs text-text-muted">{p.id}</span>
                <span class="block text-xs text-text-muted">{p.inScope.length} in-scope · {p.outOfScope?.length ?? 0} excluded</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      {#if selected && !editing}
        <div class="mt-3 flex gap-2">
          <Button onclick={() => startEdit(selected!)}>Edit</Button>
          <Button variant="danger" onclick={() => requestDelete(selected!.id)}>Delete</Button>
        </div>
      {/if}
    </Card>

    <div class="space-y-4">
      {#if editing}
        <Card>
          <h2 class="mb-3 font-semibold">{draft.id ? `Edit ${draft.program}` : 'New program profile'}</h2>
          <div class="grid gap-3 md:grid-cols-2">
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
            <div class="text-sm">
              <label for="bounty-in-scope" class="block font-medium text-text-main">In-scope assets (one per line)</label>
              <textarea
                id="bounty-in-scope"
                class="mt-1 min-h-28 w-full rounded border border-slate-700 bg-slate-950 p-2 font-mono text-xs"
                bind:value={inScopeText}
                placeholder="api.example.com&#10;app.example.com"
              ></textarea>
              {#if inScopeInvalid}
                <p class="mt-1 text-xs text-danger">{inScopeInvalid}</p>
              {/if}
            </div>
            <div class="text-sm">
              <label for="bounty-out-of-scope" class="block font-medium text-text-main">Out-of-scope assets (one per line)</label>
              <textarea
                id="bounty-out-of-scope"
                class="mt-1 min-h-28 w-full rounded border border-slate-700 bg-slate-950 p-2 font-mono text-xs"
                bind:value={outOfScopeText}
                placeholder="billing.example.com"
              ></textarea>
              {#if outOfScopeInvalid}
                <p class="mt-1 text-xs text-danger">{outOfScopeInvalid}</p>
              {/if}
            </div>
            <div class="text-sm md:col-span-2">
              <label for="bounty-rules" class="block font-medium text-text-main">Program testing rules</label>
              <textarea
                id="bounty-rules"
                class="mt-1 min-h-20 w-full rounded border border-slate-700 bg-slate-950 p-2 text-xs"
                bind:value={draft.rules}
                placeholder="No destructive testing. No access to other users’ data."
              ></textarea>
              {#if rulesInvalid}
                <p class="mt-1 text-xs text-danger">{rulesInvalid}</p>
              {/if}
            </div>
            <div class="text-sm md:col-span-2">
              <label for="bounty-custom-context" class="block font-medium text-text-main">Additional engagement context (optional)</label>
              <textarea
                id="bounty-custom-context"
                class="mt-1 min-h-16 w-full rounded border border-slate-700 bg-slate-950 p-2 text-xs"
                bind:value={draft.customContext}
                placeholder="Account type, testing window, or other program-specific limits."
              ></textarea>
              {#if customContextInvalid}
                <p class="mt-1 text-xs text-danger">{customContextInvalid}</p>
              {/if}
            </div>
          </div>
          <p class="mt-3 text-xs text-warning">
            The profile is stored in the local database and sent to the AI provider when selected. Do not include API keys, passwords, or customer data. This is prompt context, not technical enforcement of network scope.
          </p>
          <div class="mt-3 flex gap-2">
            <Button onclick={saveProfile} disabled={saving} title={hasBlockingProblem ? 'Fix the messages above before saving.' : ''}>
              {saving ? 'Saving…' : 'Save profile'}
            </Button>
            <Button variant="secondary" onclick={cancelEdit}>Cancel</Button>
          </div>
        </Card>
      {:else if selected}
        <Card>
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 class="font-semibold">Apply to a client request</h2>
              <p class="mt-1 text-sm text-text-muted">The gateway adds this scope context to the outgoing system prompt only when the request selects this profile.</p>
            </div>
            <Button onclick={() => copyText(`X-9Router-Bounty-Profile: ${selected!.id}`, 'Request header')}>Copy request header</Button>
          </div>
          <pre class="mt-3 overflow-x-auto rounded bg-black/30 p-3 text-xs">X-9Router-Bounty-Profile: {selected.id}</pre>
          <dl class="mt-3 space-y-1 text-xs">
            <div class="flex flex-wrap gap-1">
              <dt class="text-text-muted">Stored program:</dt>
              <dd>{selected.program}</dd>
            </div>
            <div class="flex flex-wrap gap-1">
              <dt class="text-text-muted">Stored scope:</dt>
              <dd>{selected.inScope.length} in-scope · {selected.outOfScope?.length ?? 0} out-of-scope</dd>
            </div>
          </dl>
          <p class="mt-2 text-xs text-text-muted">Check these against the preview below before sending. The selector header is consumed locally and not forwarded upstream. The generated context does not override provider safety policies and does not prevent the model or a client from testing outside the listed assets.</p>
        </Card>
      {/if}

      <Card>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h2 class="font-semibold">Scope context preview</h2>
            <p class="mt-1 text-xs text-text-muted">The exact text the gateway prepends to the system prompt for the selected profile. It is not the provider’s hidden internal prompt, and it is not applied to any request that does not send the selector header.</p>
          </div>
          {#if selected && preview}<Button onclick={() => copyText(preview, 'Context')}>Copy</Button>{/if}
        </div>
        {#if !selected}
          <p class="mt-3 text-sm text-text-muted">Select a profile to preview the context.</p>
        {:else}
          {#if previewId && previewId !== selected.id}
            <p class="mt-3 rounded border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
              This preview belongs to “{previewId}”, not “{selected.id}”. Re-select “{selected.id}” to load its context.
            </p>
          {/if}
          <pre class="mt-3 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-black/30 p-3 text-xs">{preview || 'Loading…'}</pre>
        {/if}
      </Card>

      <Card>
        <h2 class="font-semibold">Hunter helpers</h2>
        <p class="mt-1 text-xs text-text-muted">Build a scoped prompt for analysis or reporting. Evidence is sent to the local gateway to build the prompt and returned to this page; it is not written to the database and this helper does not call an AI provider.</p>
        <div class="mt-3 space-y-3">
          {#if helpers.length === 0}
            <p class="text-sm text-text-muted">This gateway returned no helper templates.</p>
          {/if}
          <label class="block text-sm font-medium text-text-main" for="bounty-helper-kind">Helper task</label>
          <select
            id="bounty-helper-kind"
            class="mt-1 w-full rounded border border-slate-700 bg-slate-950 p-2"
            bind:value={helperKind}
          >
            {#each helpers as helper (helper.id)}
              <option value={helper.id}>{helper.title} — {helper.description}</option>
            {/each}
          </select>
          <label class="block text-sm font-medium text-text-main" for="bounty-helper-evidence">Evidence to analyze (optional)</label>
          <textarea
            id="bounty-helper-evidence"
            aria-describedby="bounty-helper-evidence-hint"
            class="mt-1 min-h-24 w-full rounded border border-slate-700 bg-slate-950 p-2 font-mono text-xs"
            bind:value={evidence}
            placeholder="Observed status, redacted response snippet, reproduction notes. Remove tokens and other users’ data."
          ></textarea>
          <p id="bounty-helper-evidence-hint" class="text-xs text-text-muted">
            Not saved in this browser and not stored by the gateway. Clearing it after use is still recommended; it remains in page memory while this tab is open.
          </p>
          {#if evidenceInvalid}
            <p class="text-xs text-danger">{evidenceInvalid}</p>
          {/if}
          <div class="flex flex-wrap gap-2">
            <Button onclick={buildPrompt} disabled={!canBuild}>{building ? 'Building…' : 'Build prompt'}</Button>
            {#if evidence}<Button variant="secondary" onclick={clearEvidence}>Clear evidence</Button>{/if}
            {#if helperPrompt}<Button variant="secondary" onclick={() => copyText(helperPrompt, `Prompt for “${helperProfileId}”`)}>Copy prompt</Button>{/if}
          </div>
          {#if helperPrompt}
            <p class="text-xs text-text-muted">Built with the scope profile “{helperProfileId}” and the selected helper task.</p>
            <pre class="max-h-72 overflow-auto whitespace-pre-wrap rounded bg-black/30 p-3 text-xs">{helperPrompt}</pre>
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

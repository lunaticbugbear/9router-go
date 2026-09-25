<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type Persona } from '../api/client'
  import Card from '../lib/ui/Card.svelte'
  import Button from '../lib/ui/Button.svelte'
  import ConfirmModal from '../lib/ui/ConfirmModal.svelte'
  import Input from '../lib/ui/Input.svelte'

  let personas = $state<Persona[]>([])
  let enabled = $state(false)
  let defaultPersona = $state('')
  let selectedId = $state('')
  let editing = $state(false)
  let loading = $state(true)
  let saving = $state(false)
  let deleting = $state(false)
  let confirmDeleteId = $state('')
  let error = $state('')
  let notice = $state('')
  let preview = $state('')
  let previewId = $state('')
  // The preview endpoint reports the plane state as it was at lookup time, so
  // "why does this persona apply?" is answered from the response, not guessed
  // from the form above (which may hold unsaved edits).
  let previewPlaneEnabled = $state<boolean | null>(null)
  let previewDefaultPersona = $state('')
  let draft = $state<Persona>({ id: '', name: '', systemPrompt: '', appendExisting: true })

  // Selecting a persona twice must not let the slower response win.
  let previewSeq = 0

  const selected = $derived(personas.find((p) => p.id === selectedId) ?? null)
  // The backend bounds ids and prompts with Go's len(), i.e. UTF-8 BYTES, so a
  // non-ASCII prompt can be well under the character count and still be
  // rejected. Mirroring the byte semantics here keeps the inline message
  // truthful.
  const ID_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/
  const MAX_ID = 64
  const MAX_NAME = 120
  // The stored prompt is bounded by the RENDERED block, not the raw text: the
  // backend measures MarkPersonaOpening + prompt + MarkPersonaClosing against
  // MaxSystemPromptLength (8000 bytes). Mirroring only the raw budget would let
  // the UI accept a prompt the gateway rejects with a 400, so the fixed marker
  // overhead is subtracted here instead.
  const MAX_PROMPT = 8000
  const PROMPT_MARKER_BYTES = 209

  const utf8Length = (value: string) => new TextEncoder().encode(value).length

  // An existing persona's id is the header value clients already send; making it
  // editable would silently orphan those requests.
  const isEditingExisting = $derived(Boolean(draft.id) && personas.some((p) => p.id === draft.id))
  const idInvalid = $derived.by(() => {
    const id = draft.id.trim()
    if (!id) return ''
    if (utf8Length(id) > MAX_ID) return `Persona id must be at most ${MAX_ID} bytes (UTF-8).`
    if (!ID_PATTERN.test(id)) return 'Persona id must start with a letter or digit and use only letters, digits, ".", "_" or "-".'
    return ''
  })
  // The backend bounds the human-readable name in CHARACTERS (Go string len over
  // the raw field), while the prompt is bounded in BYTES. Measure each the way
  // the validator does so neither message overstates the limit.
  const nameInvalid = $derived.by(() => {
    const name = draft.name ?? ''
    if (name.length > MAX_NAME) return `Persona name must be at most ${MAX_NAME} characters.`
    return ''
  })
  const promptInvalid = $derived.by(() => {
    const bytes = utf8Length(draft.systemPrompt ?? '')
    if (bytes + PROMPT_MARKER_BYTES <= MAX_PROMPT) return ''
    return `System prompt is ${bytes} bytes (UTF-8); the gateway stores at most ${MAX_PROMPT - PROMPT_MARKER_BYTES} bytes of text because its ${PROMPT_MARKER_BYTES}-byte marker and guard lines count against the same ${MAX_PROMPT}-byte limit. Shorten it by ${bytes + PROMPT_MARKER_BYTES - MAX_PROMPT} bytes.`
  })
  // The gateway rejects an empty prompt as well as an oversized one; both are
  // reported as an HTTP 400, so block them before the request.
  const hasBlockingProblem = $derived(
    Boolean(idInvalid || nameInvalid || promptInvalid || !draft.id.trim() || !draft.systemPrompt.trim())
  )
  // Toggling the plane off while a default is still configured is allowed, but
  // the operator should see that the default is inert until the plane is on.
  const disabledDefaultWarning = $derived(
    !enabled && defaultPersona !== ''
      ? `The persona plane is off, so the default “${defaultPersona}” is not applied to any request. Only an explicit X-9Router-Persona header is honored while the plane is off.`
      : ''
  )

  onMount(() => { void reload() })

  async function reload() {
    loading = true
    error = ''
    try {
      const result = await api.getPersonas()
      const stored = result.personas ?? {}
      personas = Object.keys(stored).sort().map((id) => stored[id]!)
      enabled = result.enabled ?? false
      defaultPersona = result.defaultPersona ?? ''
      const hadSelection = selectedId !== ''
      const vanishedId = hadSelection ? selectedId : ''
      const selectionSurvived = personas.some((p) => p.id === selectedId)
      if (selectionSurvived) {
        await loadPreview(selectedId)
      } else {
        // Never silently move the operator to a different persona: auto-select
        // only on the initial load, and say so when a chosen persona disappears.
        preview = ''
        previewId = ''
        previewPlaneEnabled = null
        previewDefaultPersona = ''
        if (hadSelection) {
          selectedId = ''
          notice = `The selected persona “${vanishedId}” no longer exists. Select or create a persona.`
        }
        if (!hadSelection && personas.length > 0) {
          selectedId = personas[0]!.id
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
    draft = { id: '', name: '', systemPrompt: '', appendExisting: true }
    editing = true
    error = ''
  }

  function startEdit(p: Persona) {
    // p comes from the $state personas array, so it is a reactive Proxy and
    // structuredClone rejects it; copy the known fields into a plain draft.
    draft = {
      id: p.id,
      name: p.name ?? '',
      systemPrompt: p.systemPrompt,
      appendExisting: p.appendExisting,
    }
    editing = true
    error = ''
  }

  function cancelEdit() {
    editing = false
    error = ''
    draft = { id: '', name: '', systemPrompt: '', appendExisting: true }
  }

  function selectPersona(id: string, { loadPreview: fetchPreview = true }: { loadPreview?: boolean } = {}) {
    selectedId = id
    // The preview embeds one specific persona. Clear it immediately so a late
    // response cannot land next to a different persona's text.
    preview = ''
    previewId = ''
    previewPlaneEnabled = null
    previewDefaultPersona = ''
    if (id && fetchPreview) void loadPreview(id)
  }

  async function loadPreview(id: string) {
    const seq = ++previewSeq
    try {
      const result = await api.getPersonaPreview(id)
      if (seq !== previewSeq) return
      preview = result.addition
      previewId = result.persona || id
      previewPlaneEnabled = result.planeEnabled ?? null
      previewDefaultPersona = result.defaultPersona ?? ''
    } catch (e) {
      if (seq !== previewSeq) return
      preview = ''
      previewId = ''
      previewPlaneEnabled = null
      previewDefaultPersona = ''
      error = e instanceof Error ? e.message : String(e)
    }
  }

  async function savePersona() {
    error = ''
    notice = ''
    const persona: Persona = {
      ...draft,
      id: draft.id.trim(),
      name: (draft.name ?? '').trim(),
      systemPrompt: draft.systemPrompt,
    }
    // Keep the trimmed values visible so the stored persona can be reviewed as sent.
    draft.id = persona.id
    draft.name = persona.name

    if (!persona.id) { error = 'Persona id is required.'; return }
    if (idInvalid) { error = idInvalid; return }
    if (nameInvalid) { error = nameInvalid; return }
    if (!persona.systemPrompt.trim()) {
      error = 'A system prompt is required; an empty persona would only relabel existing instructions.'
      return
    }
    if (promptInvalid) { error = promptInvalid; return }

    saving = true
    try {
      await api.putPersona(persona.id, persona)
      editing = false
      // Reuse the selection path so switching personas clears preview state the
      // same way a click does. reload() refreshes the list and loads the
      // preview, so selectPersona's own loadPreview would be a duplicate request.
      selectPersona(persona.id, { loadPreview: false })
      notice = `Saved persona “${persona.id}”.`
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
      const result = await api.deletePersona(id)
      confirmDeleteId = ''
      personas = personas.filter((p) => p.id !== id)
      if (result.defaultStillReferencesIt) {
        // The gateway leaves the default pointing at the removed key on purpose
        // (resolution then fails closed). Clear it in the UI so the operator
        // does not keep a default that will break every request.
        defaultPersona = ''
        notice = `Deleted “${id}”. It was the default persona, so the default was cleared here; requests sending X-9Router-Persona: ${id} now get a 400 until it is recreated.`
      } else {
        notice = `Deleted “${id}”.`
      }
      if (selectedId === id) {
        // Reuse reload() so selection and preview state follow one path instead
        // of a second ad-hoc copy.
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

  async function savePlane() {
    error = ''
    notice = ''
    // Fail closed client-side the way the gateway does: an enabled plane naming a
    // persona that is not stored is refused rather than silently inert.
    if (enabled && defaultPersona !== '' && !personas.some((p) => p.id === defaultPersona)) {
      error = `“${defaultPersona}” is not a stored persona. Clear the default or pick an existing one before enabling the plane.`
      return
    }
    saving = true
    try {
      const result = await api.putPersonaPlane({ enabled, defaultPersona })
      enabled = result.enabled ?? enabled
      defaultPersona = result.defaultPersona ?? defaultPersona
      notice = result.enabled
        ? (result.defaultPersona
          ? `Persona plane enabled; requests without a selector header now receive “${result.defaultPersona}”.`
          : 'Persona plane enabled with no default; only requests sending the selector header receive a persona.')
        : 'Persona plane disabled; only requests sending the selector header receive a persona.'
    } catch (e) {
      error = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
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
    <h1 class="text-xl font-semibold">Persona Loader</h1>
    <p class="mt-1 max-w-4xl text-sm text-text-muted">
      Store operator-written system-prompt instructions and apply them per request with a local selector header, or as a configured default while the plane is enabled. It only adds prompt context you wrote yourself: it cannot change a provider’s policy, cannot retrieve a provider’s hidden system prompt, and is not applied to any request that does not select it.
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
        <h2 class="font-semibold">Stored personas</h2>
        <Button onclick={startNew}>New persona</Button>
      </div>
      {#if loading}
        <p class="text-sm text-text-muted">Loading…</p>
      {:else if personas.length === 0}
        <div class="rounded border border-slate-700 bg-slate-900/40 p-3 text-sm text-text-muted">
          No personas yet. Write the exact instruction text you want the gateway to send; an empty persona is refused because it would only relabel instructions you did not write.
        </div>
      {:else}
        <ul class="space-y-2">
          {#each personas as p (p.id)}
            {@const isSelected = selectedId === p.id}
            <li>
              <button
                type="button"
                aria-pressed={isSelected}
                class="w-full rounded border px-3 py-2 text-left {isSelected
                  ? 'border-brand-500 bg-brand-950/30'
                  : 'border-slate-700 hover:border-slate-500'}"
                onclick={() => selectPersona(p.id)}
              >
                <span class="block font-medium">{p.name?.trim() ? p.name : p.id}</span>
                <span class="block font-mono text-xs text-text-muted">{p.id}</span>
                <span class="block text-xs text-text-muted">
                  {utf8Length(p.systemPrompt)} bytes · {p.appendExisting ? 'appends to caller prompt' : 'replaces caller prompt'}
                  {#if defaultPersona === p.id}· <span class="text-warning">default</span>{/if}
                </span>
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
      <Card>
        <h2 class="font-semibold">Persona plane</h2>
        <p class="mt-1 text-xs text-text-muted">
          When the plane is off (the default) a persona is used only for requests that send the selector header. Turning it on also applies the default below to requests that send no header.
        </p>
        <label class="mt-3 flex items-center gap-2 text-sm font-medium text-text-main" for="persona-plane-enabled">
          <input
            id="persona-plane-enabled"
            type="checkbox"
            class="h-4 w-4 rounded border-slate-600 bg-slate-950"
            bind:checked={enabled}
          />
          Apply personas automatically to requests that send no selector header
        </label>
        <label class="mt-3 block text-sm font-medium text-text-main" for="persona-plane-default">Default persona</label>
        <select
          id="persona-plane-default"
          class="mt-1 w-full rounded border border-slate-700 bg-slate-950 p-2"
          bind:value={defaultPersona}
          disabled={personas.length === 0}
        >
          <option value="">No default — nothing applied without the header</option>
          {#each personas as p (p.id)}
            <option value={p.id}>{p.id}{p.name?.trim() ? ` — ${p.name}` : ''}</option>
          {/each}
        </select>
        {#if personas.length === 0}
          <p class="mt-1 text-xs text-text-muted">Create a persona before choosing a default.</p>
        {/if}
        {#if disabledDefaultWarning}
          <p class="mt-2 rounded border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">{disabledDefaultWarning}</p>
        {/if}
        <div class="mt-3">
          <Button onclick={savePlane} disabled={saving}>{saving ? 'Saving…' : 'Save plane settings'}</Button>
        </div>
        <p class="mt-2 text-xs text-text-muted">
          A default is a standing instruction: while it is set and the plane is on, every request without its own selector header carries this persona. Turning the plane off does not delete the default, it only stops applying it.
        </p>
      </Card>

      {#if editing}
        <Card>
          <h2 class="mb-3 font-semibold">{draft.id ? `Edit ${draft.name?.trim() ? draft.name : draft.id}` : 'New persona'}</h2>
          <div class="grid gap-3 md:grid-cols-2">
            <Input
              label="Persona id"
              bind:value={draft.id}
              placeholder="careful-reviewer"
              disabled={isEditingExisting}
              error={idInvalid}
              hint="Letters, digits, '.', '_' or '-'. The id is the header value sent by clients."
            />
            <Input
              label="Display name (optional)"
              bind:value={draft.name}
              placeholder="Careful reviewer"
              error={nameInvalid}
              hint="Shown in this list only; the id is what clients send."
            />
            <div class="text-sm md:col-span-2">
              <label for="persona-system-prompt" class="block font-medium text-text-main">System prompt</label>
              <textarea
                id="persona-system-prompt"
                aria-describedby="persona-system-prompt-hint"
                class="mt-1 min-h-40 w-full rounded border border-slate-700 bg-slate-950 p-2 font-mono text-xs"
                bind:value={draft.systemPrompt}
                placeholder="Answer in short paragraphs. State uncertainty instead of guessing. Cite the file you changed."
              ></textarea>
              <p id="persona-system-prompt-hint" class="mt-1 text-xs text-text-muted">
                {utf8Length(draft.systemPrompt ?? '')} of {MAX_PROMPT} bytes (UTF-8) — the gateway counts bytes, so non-ASCII text uses more than one per character.
              </p>
              {#if promptInvalid}
                <p class="mt-1 text-xs text-danger">{promptInvalid}</p>
              {/if}
            </div>
          </div>
          <label class="mt-3 flex items-start gap-2 text-sm text-text-main" for="persona-append-existing">
            <input
              id="persona-append-existing"
              type="checkbox"
              class="mt-0.5 h-4 w-4 rounded border-slate-600 bg-slate-950"
              bind:checked={draft.appendExisting}
            />
            <span class="font-medium">Append to an existing caller system prompt</span>
          </label>
          {#if !draft.appendExisting}
            <p class="mt-2 rounded border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
              Replaces the caller’s system prompt instead of appending — make sure you want that. Any tool policy, output format or language instruction the caller sent is dropped from the request this persona is applied to.
            </p>
          {/if}
          {#if draft.appendExisting}
            <p class="mt-2 text-xs text-text-muted">
              The persona text is added below whatever system content the caller already sent, so caller instructions survive.
            </p>
          {/if}
          <p class="mt-3 text-xs text-warning">
            The persona is stored in the local database and sent to the AI provider when applied. Do not include API keys, passwords, or customer data. This is prompt context, not enforcement: it does not override provider safety policies.
          </p>
          <div class="mt-3 flex gap-2">
            <Button onclick={savePersona} disabled={saving} title={hasBlockingProblem ? 'Fix the messages above before saving.' : ''}>
              {saving ? 'Saving…' : 'Save persona'}
            </Button>
            <Button variant="secondary" onclick={cancelEdit}>Cancel</Button>
          </div>
        </Card>
      {:else if selected}
        <Card>
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <h2 class="font-semibold">Apply to a client request</h2>
              <p class="mt-1 text-sm text-text-muted">The gateway adds this persona only when the request selects it, either by header or by the default above.</p>
            </div>
            <Button onclick={() => copyText(`X-9Router-Persona: ${selected!.id}`, 'Request header')}>Copy request header</Button>
          </div>
          <pre class="mt-3 overflow-x-auto rounded bg-black/30 p-3 text-xs">X-9Router-Persona: {selected.id}</pre>
          <dl class="mt-3 space-y-1 text-xs">
            <div class="flex flex-wrap gap-1">
              <dt class="text-text-muted">Stored name:</dt>
              <dd>{selected.name?.trim() ? selected.name : '(none — the id is shown)'}</dd>
            </div>
            <div class="flex flex-wrap gap-1">
              <dt class="text-text-muted">Stored prompt:</dt>
              <dd>{utf8Length(selected.systemPrompt)} bytes · {selected.appendExisting ? 'appended to the caller prompt' : 'replaces the caller prompt'}</dd>
            </div>
            <div class="flex flex-wrap gap-1">
              <dt class="text-text-muted">Default:</dt>
              <dd>{defaultPersona === selected.id ? 'yes — applied when the plane is on and no header is sent' : 'no'}</dd>
            </div>
          </dl>
          <p class="mt-2 text-xs text-text-muted">
            The selector header is consumed locally and not forwarded upstream. The persona does not override provider safety policies, and it does not retrieve or reveal a provider’s hidden system prompt.
          </p>
        </Card>
      {/if}

      <Card>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h2 class="font-semibold">Persona preview</h2>
            <p class="mt-1 text-xs text-text-muted">
              The exact text the gateway sends for this persona, including its fixed marker and guard lines. It is not the provider’s hidden internal prompt, and it is not applied to any request that does not select the persona.
            </p>
          </div>
          {#if selected && preview}<Button onclick={() => copyText(preview, 'Persona text')}>Copy</Button>{/if}
        </div>
        {#if !selected}
          <p class="mt-3 text-sm text-text-muted">Select a persona to preview its text.</p>
        {:else}
          {#if previewId && previewId !== selected.id}
            <p class="mt-3 rounded border border-warning/40 bg-warning/10 px-3 py-2 text-xs text-warning">
              This preview belongs to “{previewId}”, not “{selected.id}”. Re-select “{selected.id}” to load its text.
            </p>
          {/if}
          {#if previewPlaneEnabled !== null}
            <p class="mt-3 text-xs text-text-muted">
              {#if selected.id === previewDefaultPersona && previewPlaneEnabled}
                Resolution: this id is the stored default and the plane is enabled, so requests sending no selector header receive it.
              {:else if selected.id === previewDefaultPersona}
                Resolution: this id is the stored default, but the plane is off — it applies only to requests that send <span class="font-mono">X-9Router-Persona: {selected.id}</span>.
              {:else if previewPlaneEnabled}
                Resolution: the plane is enabled with default “{previewDefaultPersona || 'none'}”, so this persona applies only to requests that send <span class="font-mono">X-9Router-Persona: {selected.id}</span>.
              {:else}
                Resolution: the plane is off and this id is not the default, so it applies only to requests that send <span class="font-mono">X-9Router-Persona: {selected.id}</span>.
              {/if}
            </p>
          {/if}
          <p class="mt-2 text-xs text-text-muted">
            {selected.appendExisting
              ? 'This text is appended below whatever system prompt the caller already sent.'
              : 'This text REPLACES the caller’s system prompt — unrelated caller instructions are dropped from requests it applies to.'}
          </p>
          <pre class="mt-3 max-h-64 overflow-auto whitespace-pre-wrap rounded bg-black/30 p-3 text-xs">{preview || 'Loading…'}</pre>
        {/if}
      </Card>

      <Card>
        <h2 class="font-semibold">How personas reach a request</h2>
        <dl class="mt-2 space-y-2 text-xs">
          <div>
            <dt class="font-medium text-text-main">1. Explicit selector header</dt>
            <dd class="text-text-muted">A client sending <span class="font-mono">X-9Router-Persona: &lt;id&gt;</span> always receives that persona, even when the plane is off. An unknown or deleted id is an error, never a silent fallback to the default.</dd>
          </div>
          <div>
            <dt class="font-medium text-text-main">2. Configured default</dt>
            <dd class="text-text-muted">Only when the plane is enabled and the request sends no selector header.</dd>
          </div>
          <div>
            <dt class="font-medium text-text-main">3. Nothing</dt>
            <dd class="text-text-muted">With the plane off and no header, no persona is applied — the request is passed through untouched.</dd>
          </div>
        </dl>
        <p class="mt-3 text-xs text-text-muted">
          This page cannot prove what a provider does with the text, and it does not measure whether the instruction was followed. Verify the preview is the text you meant before relying on it.
        </p>
      </Card>
    </div>
  </div>

  <ConfirmModal
    isOpen={confirmDeleteId !== ''}
    title="Delete persona"
    message={confirmDeleteId
      ? `Delete persona “${confirmDeleteId}”? Any request still sending X-9Router-Persona: ${confirmDeleteId} will get a 400 instead of being served without this context${defaultPersona === confirmDeleteId ? '. It is the current default, so the default will be cleared as well' : ''}.`
      : 'Delete this persona?'}
    confirmText="Delete"
    loading={deleting}
    onClose={() => { if (!deleting) confirmDeleteId = '' }}
    onConfirm={confirmDelete}
  />
</div>

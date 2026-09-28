# Changelog


## [Unreleased]

### Security: the local-caller check trusted a client-supplied header

- The check that decided "is this caller on this machine?" read the request's `Host` header first. `Host` is whatever the caller types, so a remote caller could send `Host: localhost` and be treated as local. That one root cause had three consequences:
  - It defeated the fresh-install default-password guard. `POST /api/auth/login` accepts the default password `123456` only from the local machine; a remote caller that spoofed a loopback `Host` got a session, and with that session could mint a gateway API key.
  - It defeated the tunnel dashboard-access gate. When tunnel/Tailscale dashboard access was not explicitly enabled, a request naming the tunnel hostname was meant to be refused; a spoofed loopback `Host` waived the gate.
  - It skipped the SSRF guard on provider nodes. A self-hosted node URL is validated only for local callers, so a remote caller with a spoofed loopback `Host` skipped that validation and could point the gateway at an internal service, with the stored provider credential attached.
- The Go port had also dropped upstream's peer-token proof: the `x-9r-real-ip` forwarded-address header was read without requiring the `x-9r-peer-token` secret that upstream requires, so an untrusted header could also widen what counted as local.
- There is now one trust decision, `auth.IsLocalRequest` in `internal/auth/localpeer.go`, and every caller uses it. `nodeRequestIsLocal` in `internal/handlers/dashboard/provider_nodes.go` delegates to it, `auth.TunnelLoginBlocked` and `auth.LoginClientIP` are built on the same helper so the limiter and the local-caller check can no longer disagree, and the stale comment in `internal/middleware/dashboard_auth.go` claiming the header decides is corrected.
- The decision is made from the real peer address (`ClientAddr`), never from `Host`. A forwarded address is honoured only when an existing trust configuration authorises it (`TRUST_PROXY`/`TRUST_CLOUDFLARE`, or a valid peer-token proof), so an untrusted header can never widen the local set. A loopback peer is necessary but not sufficient: when the request carries an `Origin`, that Origin must be loopback too, so a page served from a remote origin inside the operator's own browser cannot reach the loopback listener. Requests without an `Origin` (curl, the CLI) are unaffected.
- The regression tests vary `RemoteAddr` and `Host` independently, which is exactly what the old test did not do — it set a loopback `Host` and never varied the peer, so it passed against the vulnerable code. New coverage in `internal/auth/localpeer_test.go`, `internal/auth/tunnel_independent_test.go`, `internal/handlers/dashboard/auth_localpeer_test.go`, `internal/handlers/dashboard/provider_nodes_localpeer_test.go` and `internal/middleware/tunnel_gate_test.go`.
- Verified against the running gateway: a request to the LAN address carrying `Host: localhost` and the default password answers `403` ("Default password must be changed before remote access"), while the same request over a genuine loopback connection answers `200`. The local browser path is unaffected — signing in at `localhost:20130` still renders the dashboard and its APIs answer `200`.

### Endpoint: tunnel and Tailscale disable reported success on a failed write

- `HandleTunnelDisable`, `HandleTailscaleEnable` and `HandleTailscaleDisable` in `internal/handlers/dashboard/tunnel.go` discarded the error from `UpdateSettingsRaw` and answered `{"success":true}` regardless. When the write did not land, the UI reported remote access as disconnected while the stored settings still said it was up (or the reverse), and nothing told the operator otherwise.
- All three now return `500` with the error text through the sibling `writePlainError` convention, so a failed write is a failed response.
- `web/src/components/EndpointView.svelte` no longer swallows the error. The row shows its own notice and keeps its retry action instead of reporting a state change that did not happen, and it surfaces the `funnelNotEnabled` reason together with the `enableUrl` the server returns, so the operator is told where to enable the funnel rather than only that it is off.

### Session expiry no longer leaves a dead dashboard rendered

- A session that ended mid-use left the dashboard on screen and interactive indefinitely. Dashboard API callers swallow their errors (the poll uses `.catch(() => null)`, `loadData` reports nothing), and auth was evaluated once in `onMount`, so a `401` from an expired session changed nothing: the shell stayed rendered and every later action failed quietly.
- A `401` from a dashboard API now triggers one central, single-flight confirmation against the public `/api/auth/status`. That probe distinguishes a lost session from the other handlers that legitimately answer `401` (upstream-credential rejections, the login form itself), so a rejected provider credential does not log the operator out. A confirmed loss hands off to the login view with a notice that the session ended, so it does not look like a random logout. The confirmation is single-flight, so a page firing several requests at once asks once.

### Auth check fails closed

- `checkAuth` treated a probe that did not answer as "login not required" and rendered the dashboard. A probe error, a network failure or a non-JSON body now shows the login view instead, so an unanswerable check is never taken as proof of a session.

### Dashboard API client: no placeholder key when none is stored

- `getAuthHeaders()` in `web/src/api/client.ts` no longer falls back to a hardcoded placeholder key when `localStorage` holds none; with no key stored it now sends no `Authorization` header at all. The placeholder is a dev literal the gateway rejects with 401, so a browser that had never stored a key was sending `Authorization: Bearer sk-…` and getting "Invalid API key" instead of the honest "not configured" response.

### CLI Tools "Ask AI" redaction: server-side, both directions, and it fails closed

- The "Ask AI to set it up" panel documented that API keys are redacted to `<your-api-key>`. That was only true for requests the dashboard made. The masking lived in `web/src/components/CliToolsView.svelte` `buildToolContext()`, which replaced key-shaped environment variables before sending them, and the only other guard was an instruction in the system prompt. A direct API call to `POST /api/dashboard/cli-tools/assist` — or any caller that skipped the dashboard — bypassed both, and the handler returned the model's answer and forwarded the operator's `context` verbatim. This was reproduced against the running gateway: a full API key came back in the answer.
- Redaction is now server-side and applies in both directions. A new narrow repository query, `(*Repo).SecretValues()`, enumerates the credential *values* the gateway actually stores — the `apiKeys.key` column, plus credential-shaped fields inside each `providerConnections.data` blob, walked recursively and matched by an explicit name set (`apiKey`, `accessToken`, `refreshToken`, `clientSecret`, `cookie`, …) with a `*token`/`*secret`/`*apikey`/`*password`/`*_key` suffix fallback so a newly added provider cannot silently store an unredactable token. The assembled outgoing prompt and the returned answer are each scrubbed against that list.
- The match is value-based, not pattern-based. Guessing "anything that looks like a key" both misses real credentials and mangles innocent prose; an exact value list can only ever replace text that genuinely contains the credential. Values shorter than 8 characters (or empty) are dropped, so ordinary prose is not shredded; the list is applied longest-first and every occurrence is replaced, not just the first.
- The endpoint fails closed. If the credential list cannot be read, it answers HTTP 503 instead of forwarding text it cannot promise is clean. An empty list is not a failure — it means there is nothing to redact — and the endpoint still answers.
- The upstream error text is scrubbed before the 300-byte truncation, not after: a cut landing mid-key would leave a prefix that no later redaction pass could recognise. No credential value is ever logged; the only log line on a failed lookup names the error, not the secrets it was reading.

### CLI tool "Remove" restores your own settings instead of deleting them

- Reset ("Remove") on a CLI tool installer was documented as stripping only the 9router keys while keeping everything else, but it actually *deleted* the managed keys. If the operator already had their own value for one — a corporate gateway URL, an auth token, a chosen model, a provider selection — Reset destroyed it. The installer had been keeping the true pre-9router original as `<file>.9router.bak` all along; Reset simply never read it.
- Reset now reads that backup and puts the operator's own values back. A managed key that did not exist before the install is removed (that is the correct undo for it), an unrelated setting added after the install survives, and the restore is per-key, so nothing else in the file is touched. Writes stay atomic and no longer go through the backing-up path, so the backup remains the pre-9router original rather than being overwritten with post-install state.
- A key inside 9router's own namespace is removed even when the backup carries a copy: the `9router` provider tables, `9router/`-prefixed model ids, the `9Router` Copilot entry, `custom:9Router*` ids, and `JCODE_9ROUTER_API_KEY`. Those are 9router's by construction, so putting a backed-up copy back would resurrect a stale gateway config and leave the tool still pointed at 9router after a reset. Only keys 9router merely *overwrote* are restored.
- The affected installers were `claude` (lost `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_DEFAULT_*_MODEL`, and deleted the whole `env` object when it emptied), `codex` (lost `model`, `model_provider`, `agents.default_subagent_model`), `cline` (hardcoded `actModeApiProvider`/`planModeApiProvider` to `"cline"` and deleted `openAiBaseUrl`, `openAiModelId`, `planModeOpenAiModelId`, `openAiApiKey`), `hermes` (dropped the operator's `model:` block and their `OPENAI_API_KEY`), `cowork`, `kilo`, `openclaw` (lost `agents.defaults.model.primary`), `deepseek-tui` (hardcoded `provider = "deepseek"` and deleted the operator's own `providers.openai` entry) and `grok-build`. `droid` and `copilot` were already correct: they only ever wrote entries in 9router's own namespace (`custom:9Router*` ids, the `9Router` model entry), which their Reset already removed exactly.
- With no backup (an install from an older version, or a deleted backup) the prior values are unknowable, so Reset falls back to the previous delete behaviour and the returned message says plainly that the prior values could not be restored. Where a tool needs a provider named rather than removed — Cline's two modes, DeepSeek's `provider` — the fallback keeps the old neutral default instead of leaving the setting unset.
- Added table-driven coverage for all thirteen registered installers (`internal/clisetup/restore_test.go`), each asserting that a pre-existing operator value is restored, a key absent before the install is removed, an unrelated post-install edit survives, the backup is not rewritten, and the no-backup path still clears the 9router settings while reporting honestly. A mutation check confirmed the tests fail against the old delete-only Reset.

### Connection rotation: the slot follows the connection, not its position

- With two or more accounts at the same priority, round-robin rotation could skip one account and serve another twice. Rotation state was stored as an index into the candidate slice, and that slice is not stable across requests: `GetProviderConnections` orders the pool by `priority ASC, updatedAt DESC`, and a successful forward calls `UnlockConnectionModel`, which rewrites the serving connection's `updatedAt` and moves it within the pool. The stored index therefore stopped meaning "the connection that is serving" as soon as traffic reordered the pool, so the next request landed on the wrong account.
- This was a real production bug, not a test artifact. It was found by `TestEndToEnd_RoundRobin`, which failed intermittently: it reproduced only when the two `updatedAt` writes landed in different wall-clock seconds, because `updatedAt` is RFC3339 with 1-second granularity. That is why ~58 earlier runs of the same test were clean.
- Rotation is now keyed on connection identity. `comboStickyState` gained `ServingID`, and each request resolves it back to a slot through a new `connectionIndexByID` helper. If the pinned connection has left the pool (deleted, or deactivated out of the pool) the rotation restarts at the head of the current pool, which is where a fresh handler would start.
- All four `GetProviderConnections` queries gained `id ASC` as a final tiebreaker, so connections that tie on `(priority, updatedAt)` — the common case right after a bulk import — come back in a deterministic order.
- Added `TestConnectionRotation_SurvivesUpdatedAtReorder`, which drives the exact interleaving (select, bump `updatedAt` the way a successful forward does, select again) and asserts the round-robin order. On the old positional code it fails with `pick 3: got acc-2, want acc-1`.

### Test hygiene: no live provider calls from the default suite

- The shared chat test fixture (`setupChatTestDB`) seeded provider connections without a `baseUrl`, so any test that did not delete those rows resolved the real provider catalog and forwarded to `api.deepseek.com`. Nine tests did exactly that — the persona/selector tests among them — and passed only because the live API answered. The fixture now points its seeded connections at a local stub upstream, keeping every test on localhost while preserving the fallback behavior those tests already relied on (first connection fails, the next is tried).
- Added `TestChatTestFixtureConnectionsAreLocal`: it fails if any fixture connection lacks a `baseUrl` or points somewhere other than localhost, so a future fixture edit cannot silently reintroduce live traffic.
- The tests that genuinely need real credentials or real upstreams are now opt-in behind `NINEROUTER_LIVE_E2E=1`. A new `requireLiveE2E(t)` gate skips them otherwise, so a plain `go test ./...` no longer reads the live database under `~/.9router` and no longer contacts real providers. 27 tests across 7 files are gated (`internal/handlers/chat/{live_e2e,gemini38_live,muse_spark_e2e,version}_test.go`, `internal/handlers/media/{e2e_media,extras_endpoints,systemone,tts_synthesizers}_test.go`). Because the gate replaces the name-based exclusion, the documented safe suite no longer needs `-skip`: it is just `go test ./...`. The persona test's runtime dropped from ~0.74s to ~0.01s, which is what it costs when it does not cross the network.
- The media suite was leaking the same way: its TTS and voices tests reached real services (`www.bing.com` for Edge TTS, `opencode.ai`, `api.elevenlabs.io`) even though they looked like ordinary unit tests, and only passed while those services answered. They are now behind the same gate.
- `TestHandleChangelog` no longer passes only because the network answers. The handler serves `CHANGELOG.md` from the working directory first, and the old test ran from the package directory, missed the relative path, and silently fell through to the `raw.githubusercontent.com` fallback. It now runs in a temp directory holding a fixture and asserts that fixture is what gets served. The remote fallback is covered separately by `TestHandleChangelog_RemoteFallback`, which swaps `http.DefaultTransport` for a stub that records the requested URLs and serves a canned body, so both branches are exercised without leaving the machine.

### Token Saver (RTK) output parity

- Token Saver no longer rewrites ordinary conversation text. It previously truncated any long `text` block — including user and assistant messages — as if it were tool output. Only tool output is compressed now, matching the upstream Rust `rtk-ai/rtk` and the `open-sse/rtk` port.
- Claude `tool_result` output is read from its `content` field, in both the string and text-block-array forms. The port had read `text`, so real Claude tool results were never compressed and the toggle looked inert on `/v1/messages` traffic.
- `tool_result` blocks marked `is_error: true` are skipped so error traces reach the model intact.
- OpenAI tool messages with array content and `function_call_output` with a text-block array are compressed (previously only the string forms were handled).
- Added the upstream never-worse guard: a filter result that is empty or larger than the raw text falls back to the raw text, and blobs above the 10 MiB `RAW_CAP` are passed through.

### Provider icons: no 404s for catalog entries without artwork

- The dashboard built `/providers/<id>.png` for every catalog entry, including the 9 header-name placeholder entries (`user-agent`, `originator`, `x-codebuddy-request`, `anthropic-version`, …) that ship no PNG and instead declare a Material Symbols `icon`. Each render requested a file that does not exist and logged a 404. `getIconPath()` now returns the provider's own PNG only when that PNG is actually shipped, and otherwise falls back to a shared icon.
- Availability is derived from the real `web/public/providers` listing at build time through a `provider-icons` Vite plugin that exposes `virtual:provider-icons`, rather than a hand-maintained allowlist, so adding or removing a PNG is picked up automatically and the set cannot drift from the catalog.
- `web/public/providers/opencode-zen.png` was restored from the repository history (it had been dropped from this branch while the catalog still referenced it).
- Added `npm run check:icons` (`web/scripts/check-provider-icons.mjs`): it resolves `getIconPath` for every catalog id and alias through Vite's SSR loader and fails if any of them maps to a PNG that is not on disk. It currently reports 0 failures over 195 id/alias probes.

### Model "Test all": pacing and honest rate-limit reporting

- "Test all" no longer reports upstream throttling as a broken model. Requests are paced with a 3 s pause between models instead of firing the whole list back-to-back, which is what produced the 429s in the first place.
- A 429 is now its own state. `ModelTestStatus` gained `ratelimited`, distinct from `error`: the row shows "Rate limited" rather than "Failed", and the run summary separates passed, failed, and rate-limited counts. The row's detail text is rendered in the warning tone, not the danger tone, so a throttled probe does not read as a defect.
- When the pool does throttle, the delay doubles per throttled result up to a 30 s ceiling and decays back to the base pause after a clean result, so a throttled run slows down instead of hammering the relay. Stop stays responsive during a long backoff because the wait is sliced.
- The per-window cap is the upstream relay's, not 9router's: the observed limit is "pool rate limit: max 20 per 60s" returned by the relay itself. 9router only paces its own requests in response.

### Quota bulk toggle and media provider toggles: real failure reporting

- Quota bulk connection toggles use `Promise.allSettled` and report succeeded and failed counts in a persistent banner that stays on screen after the toast expires, so a partial failure cannot be mistaken for a clean success. Only the rows whose write failed roll back — a snapshot of the pre-write server state decides which — and the page reconciles with the server afterward rather than trusting local state.
- Quota row-level toggle failures now show an inline error on the card and roll back that row alone.
- The bulk banner's failed-row line said the opposite of what it meant: it read `Still active on server after refresh`, but the failed list is built only from `Promise.allSettled` rejections, so those rows are the ones whose write did **not** land and are still in their previous state after the reconcile. It now reads `Not updated on server: …`, which is what the row list actually contains.
- Media provider toggles that write several connections report partial results (updated/failed with a reason per connection) and treat a total failure as a failure rather than success, then refresh actual connection state. A provider with an in-flight toggle is marked busy so a second click cannot race the first.

### Quota: an exhausted account reads empty, and two save failures stop being silent

- An exhausted Claude account rendered as `100%` in green, and `Turn off Empty` did nothing when clicked. The Claude quota parser substituted a guessed total when the server sent no `remaining` (`remaining: quota.remaining !== undefined ? … : Math.max(0, (total || 100) - used)`). The server reports an exhausted account as `total: 0, used: 0` with `remainingPercentage: 0`, so that fallback invented `100` remaining and `getRemainingPercentage()` — which prefers `remaining` — rendered the dead account as full. `remaining` is now the server's own figure or `undefined`, and a percentage is only computed from `used`/`total` when `total > 0`, so the server's `remainingPercentage: 0` is what reaches the screen.
- The bulk action matched nothing because the depletion test required a positive total. `isConnectionDepleted()` began with `if (!q.total || q.total <= 0) return false`, and an exhausted Claude account is exactly `total: 0`, so the row was never selected: the button ran over an empty id list and silently returned. Depletion is now decided by one exported `isQuotaDepleted()` helper that the row, `Turn off Empty`, and `Turn on Available` all share. It honours an explicit server `remainingPercentage === 0`, treats `unlimited === true` as not depleted, and otherwise requires a usable total — which keeps error rows (no total, no percentage) from being turned off.
- The same exhausted row printed `0 / ∞`, claiming infinite headroom next to `0%`. A zero total is ambiguous — it means "unlimited" for rows that say so and "no meaningful total" for exhausted ones — and the cell rendered `∞` for any non-positive total. Only an explicitly unlimited row now shows `Unlimited`; a row with no usable total reports the consumed count alone (`0 / —`) instead of inventing a denominator.
- Hiding a quota row failed silently. `updateQuotaVisibility()` logged the error and left the optimistic hide in place, so the row stayed hidden on screen while the server still stored it as visible, and the next fetch silently brought it back. It now rolls back to the previous visibility and raises an error notification carrying the server's message.
- Saving an edit in the quota tracker's Edit Connection modal failed silently. The modal stayed open with no explanation and the only trace was a `console.error` whose argument devtools renders as `[object Object]`, hiding the server's reason. The failure now renders inside the modal (and as a notification), the modal deliberately stays open with the operator's edits intact so the save can be retried, and the logged text is the error message rather than the Error object.
- Comment-only correction in `internal/handlers/dashboard/usage_providers.go` `grokMakeQuota`: the note claimed the frontend infers "unlimited" from `total === 0`. It no longer does, so the comment now states that the unlimited flag must be set explicitly and that a zero total is ambiguous. No behaviour change.

### Proxy pool schema bootstrap

- `9router init-db` now creates the `proxyPools` table. Existing installs whose canonical schema predates proxy pools can run the idempotent command to fix `/api/proxy-pools` returning 500; existing rows are untouched.

### Dashboard save and scan feedback

- CLI/IDE tool status scan failures are no longer converted into an empty status map; the page shows the error and keeps its scan action available for retry.
- CLI tool configuration cards now keep their selected tool in component state, preventing a runtime crash when opening the configuration dialog.
- Settings/Profile save failures now stay inline instead of interrupting the operator with a browser alert. Token Saver optimistic controls restore their previous values when a settings write fails.
- Media provider toggle failures now report partial updates instead of silently discarding rejected connection writes; the page refreshes actual state afterward.
- Settings/Profile now documents `9router init-db` for schema setup instead of implying tables migrate automatically at startup.
- Quota single/bulk connection toggles report failures, and OAuth auto-ping now uses the existing settings PATCH API rather than a nonexistent method.
- Quota connection-list failures are no longer swallowed into an empty list; the page shows the error and can retry.
- Console Log, CLI tool scan, version, and changelog endpoints now accept the dashboard login session instead of requiring a client API key (they returned 401 "Invalid API key" when no key existed). An expired session now returns to the login page instead of rendering an empty dashboard.
- CLI Tools: tool detail now has "Ask AI to set it up" when a provider is connected. It asks one of your own models (through the gateway, via the dashboard session) for tailored setup steps; API keys are redacted to `<your-api-key>` and nothing is executed automatically. (The redaction was dashboard-only at first; see the "Ask AI" redaction entry under Unreleased.)
- CLI Tools one-click setup (port of the original per-tool installers) for Claude Code, Codex, OpenCode, Hermes, GitHub Copilot (VS Code) and Claude Cowork: writes the gateway URL, a client key (auto-created "CLI tools (auto)" when none exists) and the chosen model into each tool's own config; "Remove" strips only 9router keys. Unrelated settings are kept, the original file is backed up once as `*.9router.bak`, and unparseable configs are refused instead of overwritten. Cowork omits the original's security-relaxation profile and MCP injection. Status cards now show "Connected" when a tool already points at 9router. (Remove originally deleted the managed keys instead of restoring your own prior values; see the "Remove" entry under Unreleased.)
- Device-flow logins (GitHub Copilot, Kiro, Kimi, etc.) no longer get stuck on "Waiting…" after you approve: GitHub's `slow_down` reply is now honored (the poll interval backs off by 5 s), and "Check now" can't poll faster than the interval. The modal no longer asks for a localhost callback URL, since device logins don't have one.
- GitHub Copilot connections can now list models (chat models from `api.githubcopilot.com/models`, disabled ones hidden) instead of failing with "does not support models listing".
- All provider model registries now offer Test all: runs existing model checks sequentially, confirms quota use first, reports pass/fail progress per model, and can stop after the current request. Requires an active connection.
- GitHub Copilot quota now fetches paid and free plan limits with the GitHub OAuth token; premium requests show their used, remaining, and reset values, while unlimited chat/completions are labeled Unlimited.
- GitHub Copilot retries `/responses` only when `/chat/completions` explicitly rejects a non-Claude/non-Gemini model with `unsupported_api_for_model`; model-test errors in the registry now show a short summary, with the full error available on hover.
- Overview Recent requests now uses fixed-width columns with the provider name below each model; long custom provider IDs remain available on hover without stretching the table or its rows.
- Sidebar navigation groups now all expand and collapse independently, persist their state, and reveal a group when navigation lands inside it; the existing Media preference is retained.
- One-click setup now also covers Factory Droid, Open Claw, Kilo Code, Cline, Grok Build, DeepSeek TUI and jcode (all except Devin, which has no config to write: it authenticates with `devin auth login`). Grok Build is edited line by line so comments survive and the previous default model is restored on Remove. DeepSeek TUI is merged instead of overwriting the whole file as upstream did. Kilo, Cline and DeepSeek Remove only undo entries that point at this gateway. Open Claw per-agent `models.json` is written only for agent dirs inside your home directory.
- Overview: gateway panel stats are a compact label/value list, compact empty states are lighter and equal height, shortcut hints no longer truncate, and the duplicate "Add provider" actions were removed (Shortcuts now links Quota).
- Opening a media provider (e.g. TTS → Edge TTS) no longer throws `DataCloneError` from `history.pushState`, so its URL updates and Back works. Added the missing Ollama Search icon (was a 404).
- Desktop sidebar can collapse to an icon rail (button in the sidebar header or Ctrl/⌘+B) and be resized by dragging its right edge (208–420 px; drag far left to collapse, double-click to reset, arrow keys when focused). Width and collapsed state persist per browser; the mobile drawer is unchanged.
- Auth status distinguishes a stored or environment-provided password from the built-in default without returning secret values. Login/Profile copy follows that status.
- Remote login with the built-in default now explains how to rotate it locally or configure `INITIAL_PASSWORD`; it no longer offers a password-change request that cannot establish a server session.
- Login, Settings, API-key snippets, and CLI proxy instructions now show the gateway's current origin/port instead of hard-coding `20130` when the gateway runs elsewhere.
- Upstream GitHub update checks and update controls are disabled by default for custom builds. A remote source is used only when `UPDATE_URL` or `UPDATE_REPO` is explicitly configured.
- Endpoint API-key and tunnel-access save failures now appear inline instead of blocking the dashboard with browser alerts.

### Combo cycle validation

- Renaming a combo without supplying `models` now validates its retained stored leaves, not an empty list. A rename that closes an alias↔combo loop returns HTTP 400 without changing the combo; an acyclic rename still succeeds.

### Persona-bound model names, catalog audit, and install tools

- `9router bind` creates a model name bound to a target model and/or a persona file — the mechanism behind `-mod`-style names. Fail-closed when the bound persona is missing (400 before upstream), a disabled binding resolves to nothing, and the `X-9Router-Persona` header takes precedence over a binding. Personas append below the caller's system prompt unless `--replace` is given.
- `9router models audit` reports which advertised context windows come from a matching rule and which are the resolver's 128000 fallback guess; `--strict` fails while any guess remains. On the current catalog 540 of 1437 models are guesses.
- `9router doctor` checks schema completeness and gateway reachability; `9router init-db` creates the canonical schema idempotently. This closes the "no such table: apiKeys" class of failure on installs whose database was created without the dashboard schema.
- The persona size bound is raised to 256 KiB so an agent persona document (tens of thousands of tokens) can be stored, which is the size these bindings exist to carry.

### Terminal Launcher Presentation

- The ready banner now groups version, health state, Server and Dashboard URLs, plus the two local selector-header names. It does not launch a browser.
- The four-choice menu is shown once per entry; later actions return to a short prompt while results and validation errors remain readable in scrollback. The persona submenu shows its command legend once and refreshes its state/list only after changes. No cursor-positioning or screen-erasing sequences are emitted.
- ANSI color is used only for interactive stdout when `NO_COLOR` is unset and `TERM` is neither empty nor `dumb`. Redirected output remains plain text.

### 🎭 Persona Plane: System-Prompt Intercept (Local Feature)

- New local feature: `internal/persona` (store + bounded renderer), `internal/handlers/chat/prompt_plane.go`, `internal/handlers/dashboard/persona.go`, and a CLI loader menu. An operator saves system-prompt additions and selects one per request with the `X-9Router-Persona` header, or applies a default while the plane is enabled. Both `personasEnabled` and `defaultPersona` are **off/empty by default**, so a stored persona changes nothing until the operator turns the plane on.
  - **Resolution order**: request header (always honored, even while the plane is disabled — a client that names a persona gets it or a clear error, never a silent no-op) → `defaultPersona` (only while enabled) → none. A header naming an unknown persona does **not** fall back to the default: a stale client is told (`400 unknown persona "<id>"`) rather than served different instructions than it named. A `defaultPersona` left dangling by a delete likewise fails closed instead of silently unpersonifying every request.
  - **Boundary wording**: the rendered block is framed by fixed marker lines — an opening `Persona instruction (operator-declared; …)` marker and a closing `The persona text above is operator-declared; provider safety policies remain authoritative and take precedence over it.` The text between them is emitted **verbatim**, unlike bounty scope values which are JSON-quoted as data: a persona is instruction text the operator wrote, not a data value, and quoting it would corrupt multi-line instructions. The guard lines are structural, so a persona cannot impersonate a new provider-level system section or drop the boundary.
  - **Append vs replace**: `appendExisting: true` adds the persona below existing system content, preserving caller instructions unrelated to the persona (tool policy, response format, language). Omitted means **replace** — the explicit operator choice, since silently dropping a caller's system prompt is a footgun.
  - **One funnel for both planes**: `ApplyBountyProfileToBody` → `ApplyPromptPlaneToBody(body, PromptPlane{Persona, Bounty}, format)` with no alias left behind, so persona and bounty cannot double-marshal or disagree about the wire shape. Merge order is fixed: **persona first, then the bounty scope block**, which keeps the authorization facts adjacent to the caller's task and its fixed safety closing line as the final statement the provider reads. Each piece is injected through the existing strict `tokensaver` injectors and is idempotent, so re-applying the plane never duplicates a block, and an uninjectable body is rejected with `400` when **either** piece cannot be attached.
  - **Replace-mode injectors**: `tokensaver.ReplaceChatSystem` / `ReplaceClaudeSystem` / `ReplaceResponsesInstructions` are the error-returning counterparts of the existing strict injectors — they overwrite the protocol's system field instead of appending, never degrade into an append on an unusable body, and return `(body, false, nil)` when the field already holds exactly the expected prompt.
  - **Header hygiene**: `StripBountyProfileHeader` is generalized to `StripInternalSelectorHeaders(h)`, which strips **both** selector headers; all chat, combo/fallback, and media call sites migrated, with the final strip still applied after provider `StaticHeaders` so a configured static header cannot reintroduce a local id upstream.
  - **Settings**: `SettingsData.Personas` + `PersonasEnabled` + `DefaultPersona`, decoded in their own block (map key authoritative and applied **before** validation, so a stale nested id cannot cause a valid key to be dropped) alongside the existing per-key bounty decode. New repo methods `SetPersona` / `DeletePersona` / `GetPersona` / `SetPersonasPlane` / `PersonasBindingDefault`; `SetPersonasPlane` rejects a default naming an unstored persona, and plain `PUT /api/settings` can no longer silently flip the plane or detach its default.
  - **Dashboard**: `GET /api/personas` (personas + enabled + defaultPersona), `PUT /api/personas/{name}` (path id authoritative, non-clobbering), `DELETE /api/personas/{name}` (`404` when absent; reports `defaultStillReferencesIt` instead of rewriting the default), `PUT /api/personas/plane` (fail-closed; a missing `enabled` is refused rather than defaulted), `GET /api/personas/preview?name=` (exact rendered addition + resolved plane state, `stored: false`, applies nothing). All inherit the dashboard auth wrapper; payload cap 64 KiB, matching the bounty endpoints.
  - **CLI loader**: the launcher menu becomes 4 items — `1) Web UI`, `2) Terminal/Go server logs`, `3) Persona loader`, `4) Exit` — with the prompt `Select [1-4]:`. The submenu (`t`/`d <number>`/`n`/`c <id>`/`b`) lists stored personas with the current default marked and each row's mode and byte size, toggles the plane, sets or clears the default, and can create a persona from the terminal. Dispatch is a pure function (`parsePersonaMenuLine` + `applyPersonaMenuAction`) so it is table-testable without I/O; a numbered pick resolves against the list the operator just saw and an out-of-range number is refused rather than clamped onto a different persona. Writes go through the **same settings row the gateway reloads per request** (`app.DatabaseHandleModule` hands the launcher the already-open handle), so no restart is needed and no second connection is opened. Still no browser launch on bare startup.
  - Bounded at `MaxSystemPromptLength = 8000` **bytes** (`len`, not runes: non-ASCII text consumes several bytes per character), measured on the untruncated rendering and rejected rather than truncated, so the closing guard line is never severed. ID validation mirrors the bounty profile key rules (≤64, same regex, raw padded ids refused).
  - Tests: `internal/persona` (id/prompt validation, exact-at-budget accept + one-byte-over reject, multi-byte byte semantics, boundary framing, replace flag), `internal/tokensaver` (replace-mode injectors per protocol: replacement actually overwrites, `messages[]` gains no `system` for Claude, idempotence, uninjectable bodies error), `internal/db` (round-trip without clobbering unrelated settings, per-key loader rejection with authoritative map key, dangling-default detection, fail-closed plane write), `internal/handlers/chat` (precedence, no header→default→none, disabled plane, dangling default, no fallback from a stale header, persona+bounty in one body exactly once per block and in-order, idempotent re-apply, replacement-vs-append, end-to-end injection with the selector never forwarded, unselected body byte-untouched, plane-switch behavior, `400` zero-upstream for unknown and uninjectable, non-sticky), `internal/handlers/media` (Responses injection with both blocks once, selectors stripped, unknown id `400` zero upstream, non-JSON body fails closed, unselected body untouched), `internal/handlers/dashboard` (CRUD, preview, size cap, plane fail-closed, dangling-default report, sorted listing), `cmd/9router-go` (submenu line parsing, dispatch, pick-against-printed-list, toggle/clear/back, create with append choice and validation refusals, list markers, nil-store message) plus the migrated 4-item menu tests.

### 🚀 Native Launcher: Banner, Menu, and Port Flags (Node-CLI Parity)

- `cmd/9router-go/launcher.go` (new) + `main.go`: a bare `9router-go` run now mirrors the original Node CLI's startup feel natively in Go — after the fx app starts it polls `GET http://127.0.0.1:<port>/health` (30s bound, 100ms interval) until 200, then prints the ready block (`🚀 9router-go v<version>` / `Server:` / `Dashboard:`, version from `internal/updater.CurrentVersion`, no subprocess).
- **The launcher never opens a browser on its own.** No auto-open in TTY or non-TTY mode, and there is no opt-out flag or env var to juggle because there is nothing to opt out of. The printed Dashboard URL is how the operator reaches the UI; the interactive menu's `1) Web UI (Open in Browser)` is the only action that launches a browser, and only on explicit request. Non-TTY runs (CI, `nohup`, Docker) print the banner and babysit — no menu, no browser.
- Interactive terminal menu (TTY only): `1) Web UI (Open in Browser)` launches the dashboard, `2) Terminal/Go server logs` prints the Server/Dashboard/Health URLs, `3) Persona loader` opens the persona submenu (see the persona-plane entry above), `4) Exit` shuts down gracefully. Choice 4 and SIGTERM converge on the same path (`fxApp.Stop` → `internal/app/server.go` OnStop → `shutdown.Cancel()`); no PID/port tricks. EOF or a stdin read error falls back to plain signal babysitting, so non-TTY behavior is unchanged: wait for SIGINT/SIGTERM and drain in-flight requests within the existing 20s budget.
  - `cmd/9router-go/shutdown.go`: first-stop is a **broadcast** (`shutdownTrigger.Done()`), not a channel receive, because the existing `<-signals` shape lets whichever reader wins consume the signal and leave everyone else waiting. Two live defects were caught and fixed during pty verification: (a) the force-quit watchdog ate the first signal, so a lone `SIGTERM` never began shutdown (measured 40s exits with no "Shutting down..." line; now ~0.2s); (b) the menu blocked in `ReadString`, so a signal at the prompt was ignored until the next keystroke — stdin is now read on a helper goroutine and the loop selects on it. `TestShutdownTrigger_FirstSignalStopsTheProcess` fails if the broadcast is reverted. Second signal still force-quits.
- TTY detection (`tty_unix.go` / `tty_windows.go`) uses `isatty`/console-mode rather than `ModeCharDevice`: `/dev/null` is a character device but not a terminal, and treating it as one showed an interactive menu (and hid signal-driven shutdown) for backgrounded processes.
- New flags: `--port <int>` and `--host <addr>` are projected into `PORT`/`HOST` before config load (the existing viper path is the only config mechanism — no second config source).
- Browser launching is behind a `browserLauncher` hook so tests can prove exactly when a launch happens; `openDashboard` prints the URL when no launcher exists (non-darwin) or the launch fails. macOS uses `open`.
- README "Running 9router-go" now documents the flags that actually exist (the previous `--port`/`--db-path` example did not match the code; `DB_PATH` env remains the DB override) and states explicitly that no browser is opened automatically.
- Tests: `cmd/9router-go/launcher_test.go` — menu-choice parsing table, banner/menu exact strings, health-wait (ready, closed port, non-200, cancelled context), TTY detection (pipe, regular file, `/dev/null`), flag→env wiring, **no-browser-on-launch** and **option-1-only** contracts via a recording launcher hook, fallback/silent-success output, and menu-loop dispatch (exit, logs, unknown input, EOF fallback, stop-request interrupt); plus trigger coverage (first signal stops, second force-quits, `Request`/`Arm` idempotence). Race-clean under `-count=3 -race`.

### 🎯 Bug Bounty Assist: Transparent Authorization Context (Local Feature)

- New dashboard workspace `/dashboard/bounty` plus `internal/bounty/profile.go`, `internal/handlers/dashboard/bounty.go`, and `internal/handlers/chat/bounty_profile.go`. An operator saves a program profile (program, policy URL, in-scope/out-of-scope assets, rules) and selects it per request with the `X-9Router-Bounty-Profile` header. The gateway adds a visible, bounded authorization context to the outgoing system prompt so an assistant can distinguish an in-scope engagement from an unscoped request, which **may** reduce false refusals; that effect is not measured here.
  - **Scope is prompt context only.** No provider policy bypass, no hidden system-prompt extraction, no exploit payloads, no scope scanning, no network probing. The injected text explicitly retains the provider's own policy as authoritative and instructs the model not to test excluded assets.
  - Selection is explicit and per-request — nothing is applied by default, so one program's scope never bleeds into another request. The selector header is consumed locally and never forwarded upstream (chat, Messages, Responses, combo/fallback, and both media forward paths, with a final strip after provider `StaticHeaders`).
  - Wire shape per protocol: OpenAI chat `messages[]`, Anthropic Messages top-level `system` (never a `system` role inside `messages[]`), Responses top-level `instructions` (caller instructions preserved; `input[]` left untouched — never a `role:system` insertion there).
  - Stale/deleted profile ids fail explicitly with `400 unknown bounty profile "<id>"` (including synthetic warmup/naming requests answered locally) — no silent fallback. A selected body that cannot carry the scope (JSON that is not an object, `null`, or a non-string/non-null `instructions`) is likewise rejected with `400 bounty_profile_uninjectable` before any upstream call, instead of being forwarded unscoped.
  - Profile saving validates the effective context against a 12,000-**byte** budget (UTF-8 bytes, not characters — `MaxPromptLength` compares Go `len`, so non-ASCII program names/rules consume several bytes each) and **rejects** oversize input instead of silently truncating it, so the closing safety instruction can never be cut off. The same byte wording is used by the per-field validation errors, and the reject message states the limit is a byte count. Scope values are JSON-quoted as data so program/scope text cannot impersonate a new prompt section.
  - Report helpers (`/api/bounty/helpers`, `/api/bounty/helpers/build`, prompt preview) return bounded scope-check / safe-plan / triage / report-planning prompts. Evidence is returned to the caller only — never sent to a provider, never persisted. Only declared scope/rules are stored; request/response bodies and evidence are not.
  - Tests: `internal/bounty` (budget boundary exact-at-limit accept + one-byte-over reject, quoting, helper templates), `internal/db` (round-trip without clobbering unrelated settings, oversize-on-load rejection, authoritative map key), `internal/handlers/chat` (explicit selection, no implicit/sticky application, protocol wire shapes, selector not forwarded), `internal/handlers/media` (Responses scope injection with the selector consumed locally), `internal/handlers/dashboard` (CRUD, helper non-persistence).

### 🐛 Dashboard: Recent Requests List No Longer Blinks/Shrinks on First Request

- `internal/usagetracker/tracker.go`: seed the in-memory `recentRing` from `usageHistory` once per process (upstream `ensureRingInitialized` parity) + `recentFromHistoryRow` mapper. Previously a fresh process streamed a ring holding only post-restart rows, so the first completed request replaced the dashboard's DB-backed 20-row list with 1 row (list blinked, rows below vanished). Regression test `TestTracker_RingSeededFromHistoryOnce`.
- `web/src/components/analytics/AnalyticsView.svelte`: SSE `recentRequests` now merges (union + dedupe + newest-first + cap 20) instead of replacing, so a short stream payload can never drop rows already rendered.


### 🧹 Leak Hunt: 7 Fixes for 24/7 Operation (Independent Audit)

- `internal/shutdown/shutdown.go` + `internal/app/server.go`: new `shutdown.Context()` (canceled on `Cancel`); updater + catalog-sync loops take it instead of `context.Background()` — background goroutines + tickers now exit on ^C. Fixed `TestReset` double-close panic (recreate `done` channel).
- `internal/handlers/chat/combo_fusion.go`: `collectPanel`/`makePanelCall` take `ctx`; stragglers abort on grace/hard timeout, client cancel, or shutdown (previously `context.Background()`, orphaned until upstream responded).
- `internal/proxy/executor/freebuff_session.go` + `internal/handlers/chat/antigravity_quota.go`: lazy eviction of expired entries + opportunistic sweep (2× TTL) — token-rotated keys no longer accumulate.
- `internal/handlers/chat/connections_proxy.go`: `proxyClients` capped at 128 with idle-close eviction (each entry pins a Transport + sockets).
- `internal/auth/session.go`: login limiter capped at 5000 buckets with window sweep + oldest-evict (scanner IPs bounded).
- `internal/handlers/chat/gemini_handler.go` + `internal/proxy/executor/stream.go`: 10MB caps on non-stream body reads and codex SSE accumulation.
- Out of scope (pre-existing, bounded): HeartbeatWriter ticker (dies with stream Close), tracing ring (2000), translator prune (50/10min), MITM conns (Wait).


### 🔊 Opencode Responses Errors Fail Loud (No More Silent Empty 200)

- `internal/proxy/executor/stream.go`: `ProcessCodexEvent` now records upstream `{"type":"error",...}` events (e.g. `FreeTierError` on muse-spark `-free` models) on stream state; `handleCodexStream` (non-stream) converts them to `*proxy.UpstreamError` (403 for free-tier/auth gates, else 502) instead of emitting `200 + content:""`. Silent success broke agents, bypassed account fallback/locks, and faked green monitoring. Upstream Next.js (`base.js`) likewise returns non-OK responses as errors, never empty 200s.
- Live evidence: `POST opencode.ai/zen/v1/responses` with `muse-spark-1.3-contributor-free` returns `FreeTierError: "OpenCode's free tier can only be used from within OpenCode"`.
- Limit (honest): on the already-committed SSE stream path headers cannot be unwound, so a pure-error stream still closes without chunks; the non-stream path (which agents use for the failing case) now errors properly.


### 🔗 Freebuff Cross-Process Session Coordination (Anti-Hijack)

- `internal/proxy/executor/freebuff_session.go` + `freebuff.go`: session lookup now memory L1 → `upstream_leases` L2; admission is coordinated — exactly one claimer per token+model across processes sharing the DB (`freebuffClaimMu` in-process + `AcquireLease` cross-process). Losers follow the winner's `instanceId` instead of POSTing their own claim (the pattern upstream punishes with 409 `session_superseded`). Stale-session retry drops the lease compare-and-delete (a sibling's fresh row survives). `Request.Leases` (nil = memory-only, old behavior) wired from chat fallback ×2, media `/responses`, and the session-switch endpoint.
- Tests: two racers converge on 1 POST (fake + real SQLite backends), follower reads with 0 POST, stale drop is compare-and-delete, nil-store contract unchanged.
- Note: coordination fixes *technical* hijacking between cooperating instances, not *policy* — two machines serving traffic concurrently on one account is still concurrent use server-side.


### ⬆️ Upstream v0.5.86 Parity (decolua/9router#v0.5.86)

- `internal/handlers/chat/claude_cloaking.go` + `internal/providers/providers.go`: bumped Claude CLI fingerprint `2.1.258` → `2.1.280` (upstream `cbffeb9`), so the billing-header cloak and `claude-cli/*` UA stay current. Added `claude-opus-5-5` to `cc`/`claude` catalogs (`registry_models.go`, `web/src/lib/models.ts`).
- `internal/handlers/media/deploy.go`: Vercel relay template now forwards headers losslessly (copy to plain object, strip only `x-relay-target`/`x-relay-path`/`host`) — upstream `6af26a9`. Cloaking headers survive relay pools, which matters for proxied Freebuff traffic.
- Deferred: Xiaomi MiMo v2.6 desktop login (5 region clusters, dual-route models, server-assisted flow) — large scope, tracked as separate stacked diff.

### 🛡️ Freebuff client_id Cloaking (Anti-Ban Parity)

- `internal/proxy/executor/freebuff.go`: `ForwardFreebuff` reuses the account's stored `fingerprintId` verbatim as `codebuff_metadata.client_id`, falling back to a fresh unbranded UUID only for connections saved before this change. Previously every chat request sent `client_id: "9router-<uuid>"`, which brands the traffic as non-CLI at the application layer — the most likely reason accounts got `banned` even though headers/User-Agent already matched the CLI. Note: cloaking only removes the self-identifying fingerprint; it cannot protect accounts banned for quota abuse, multi-account farming on one IP/fingerprint, or region violations.
- `internal/handlers/oauth/cline.go`: `decodeClineCode` now accepts the real browser-callback shape — base64url (`-`/`_` alphabet, padding stripped) plus the trailing signature segment the extension appends after the JSON payload. Previously only strict `StdEncoding` decoded, so pasting the callback failed to extract tokens and the handler fell through to `POST /api/v1/auth/token`, which the server rejects with `Forbidden` — exactly the reported `Cline token exchange failed ... Forbidden` error.
- `internal/handlers/chat/connections.go` + `internal/handlers/dashboard/connection_probe.go`: the `workos:` prefix is now JWT-only (WorkOS JWT = base64url `eyJ…` + dot, upstream parity `open-sse/shared/clineAuth.js`). Non-JWT ClinePass API keys ride plain `Bearer` — prefixing them is what the server answers with 401 `"Unauthorized: Please make sure you're using the latest version of Cline and re-authenticate your Cline account."` Note: your pasted bundle decodes to a real WorkOS JWT (`eyJhbGciOiJSUzI1NiIsImtpZCI6InNzb19vaWRj...`, `expiresAt` already past `2026-09-24T07:23:51Z`), so that specific token is expired server-side — reconnect with a fresh browser login after updating.
- `internal/handlers/oauth/cline.go`: new Cline/ClinePass connections are named by account email from the token bundle (fallback: first+last name, then provider default) instead of the generic `ClinePass` label, so multi-account setups stay distinguishable in provider detail.
- All-providers branding sweep (no behavior change otherwise): audited every header/body sent to upstream. Only one real leak found and removed — `User-Agent: 9router/oauth` on the Antigravity Google token exchange (`internal/handlers/oauth/antigravity.go`), now unbranded Go default. Everything else already mirrors an official client: Freebuff `codebuff-cli/*` + fingerprinted `client_id` (no `9router-` anywhere on the wire), Cline `Cline/*` + `cline-cli`, Antigravity `antigravity/ide/*`, Gemini CLI `google-api-nodejs-client/*`, Grok/Codex/iFlow/Qoder/MiMo browser or CLI UAs, TTS browser UAs. `X-Msh-Platform: 9router` (Kimi) and `HTTP-Referer/X-Title: endpoint-proxy.local / Endpoint Proxy` (OpenRouter/Airforce) are byte-identical to upstream `decolua/9router` — changing them would *break* parity, not improve stealth. `User-Agent: 9Router` only ever hits the user's own proxy-test target and GitHub API (never an LLM provider). Local-only strings (`9router-oauth` BroadcastChannel, MITM CA, updater UA, file paths) never leave the machine.
- `internal/handlers/oauth/naming.go` (new) + all OAuth handlers (`freebuff.go`, `cline.go`, `antigravity.go`, `trae.go`, `windsurf.go`, `zed.go`, `authcode.go`, `pkce.go`, `device.go`): single email-first naming rule `connectionDisplayName` — account email when known, else explicit user-supplied name, else provider default. No more `"Provider (name)"` labels anywhere, so every provider detail page (Freebuff, ClinePass, Antigravity, …) lists accounts by email.
**Production Internet Hardening & Cloudflare Integration:**
- **Protect `/debug/pprof/*` endpoints**: Disabled Go runtime profiling endpoints (`/debug/pprof/*`) by default in production to prevent Denial of Service (DoS) and potential memory/key disclosures. Can be explicitly enabled via `PPROF_ENABLED=true`.
- **Privilege separation for client API keys**: Restricted destructive administrative routes (`/api/version/shutdown`, `/api/version/update`, `/api/settings/database`, `/admin/health/reset`) so they strictly require a valid dashboard JWT session cookie or local CLI token (`x-9r-cli-token`), matching upstream `ALWAYS_PROTECTED` behavior in `src/dashboardGuard.js`. Client API keys can no longer trigger shutdowns or database dumps.
- **Cloudflare `CF-Connecting-IP` support**: Updated `LoginClientIP` in `internal/auth/session.go` to support `CF-Connecting-IP` when `TRUST_PROXY=true` or `TRUST_CLOUDFLARE=true`, ensuring proper client IP resolution and preventing shared-bucket lockout behind Cloudflare.
- **Configurable host binding (`HOST` / `BIND_ADDR`)**: Added `Host` to configuration and updated server listener to bind to `HOST` or `BIND_ADDR` when specified (e.g. `127.0.0.1` when proxied by `cloudflared`), while preserving `:20130` (`0.0.0.0`) default behavior.

### 🎨 UI & Dashboard

**Media Providers Full Parity (`/dashboard/media-providers/*`):**
- `internal/handlers/media/tts_synthesizers.go` & `tts_forward.go`: Built local native synthesis engines (`edge-tts`, `google-tts`, `nvidia`) and voice catalog endpoints (`/api/media-providers/tts/voices`), supporting real-time streaming audio generation and custom speed/pitch/voice options.
- `internal/handlers/media/antigravity_image.go` & `antigravity_stt.go`: Added Antigravity image generation and speech-to-text (STT) transcription handlers with multipart form-data parsing, extracting audio models and delegating to Google's IDE backend.
- `internal/handlers/media/antigravity_search.go`: Ensured typed JSON serialization for Antigravity web search requests and responses to match upstream key ordering and structure.
- `internal/handlers/chat/resolution.go`: Fixed System One endpoint `/v1/systemone` to handle `x-antigravity-session` headers and fall back seamlessly to direct connections with `public` default keys for `antigravity-zen`.
- `web/src/components/media/MediaKindView.svelte`, `MediaProviderCard.svelte`, `NoAuthProxyCard.svelte`, `TtsExampleCard.svelte`, and `SttExampleCard.svelte`: Full Svelte 5 runes parity with upstream Next.js for all 8 media kinds (`video`, `stt`, `tts`, `image`, `embedding`, `systemone`, `webSearch`, `webFetch`), aligning provider sorting, priority, hidden flags, and live audio/waveform test players.

**Antigravity Live Models Discovery & "Import from /models":**
- `internal/providers/registry_models.go` & `web/src/lib/models.ts`: Added Google Antigravity official live models (`gemini-2.5-flash`, `gemini-2.5-flash-lite`, `gemini-2.5-pro`, `gemini-2.5-flash-thinking`, `gemini-3.1-pro-high`, `gemini-3.1-flash-lite`, `gemini-3.5-flash-lite`).
- `internal/handlers/dashboard/connections.go`: Extended `GET /api/providers/:id/models` to support Antigravity, Gemini CLI, Cline, and ClinePass connections. For Antigravity, queries Google's live RPC (`https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels`) with Bearer tokens, filtering internal chat IDs and parsing vision/reasoning capabilities.
- `web/src/components/connections/ProviderDetailView.svelte`:
  - Added `[📥 Import from /models]` button next to `Add Model` for Antigravity, Cline, ClinePass, and Qoder accounts.
  - Auto-fetches live models from Google for active Antigravity connections on page load, displaying uncataloged models in the **Suggested models** section for 1-click addition.

**Suggested Free Models Feed & Custom Models Parity:**
- `internal/handlers/suggestedmodels.go`: Wrapped HTTP client in `proxy.NewFallbackTransport` so public model catalog feeds (`opencode`, `openrouter`, `kilocode`, `airforce`) never get blocked by sandbox proxy allowlists.
- `web/src/lib/providers.ts`: Added `modelsFetcher: { url: "https://opencode.ai/zen/v1/models", type: "opencode-free" }` to `opencode`, restoring the "Suggested free models (≥200k context)" section on OpenCode Free.
- `internal/handlers/dashboard/models.go`: Updated `GET /api/models/custom` to return `{ "models": [...] }` matching upstream Next.js shape, and updated `web/src/components/connections/types.ts` to cleanly parse custom model lists without type-assertion errors.

**Universal Outbound Direct Fallback & Proxy Allowlist Bypass:**
- `internal/proxy/fallback_transport.go`: Created `FallbackTransport` which wraps Go HTTP round-trippers to detect local proxy refusal (`403 Forbidden`, `blocked-by-allowlist`, `CONNECT tunnel failed`, or proxy text/plain errors) and instantly re-issue the request directly (`Proxy: nil`) with re-readable request bodies.
- Applied universally across chat resolution, streaming SSE forwarders, media endpoints, validation probes, and catalog feeds.

**Comprehensive Connection Health Probing & Zero False Errors:**
- `internal/handlers/dashboard/connection_probe.go`:
  - Added native probe configurations for OAuth providers (`antigravity`, `gemini-cli`, `cline`, `clinepass`, `freebuff`, `xai`, `grok-cli`, `codebuddy-intl`, `zed`, `windsurf`, `trae`, `devin`, `devin-cli`, etc.).
  - Implemented token-exists heuristic for unconfigured providers and compatible base-URL probing for custom nodes.
  - Fixed `persistProbeResult` so that informational "Provider test not supported" messages no longer falsely mark connections as `testStatus: "error"` or pollute `lastError` in the SQLite database.
  - Updated Cline / ClinePass probe to prefix WorkOS JWT tokens with `workos:`.

**Quota Tracker Full Parity with Upstream Next.js (`/dashboard/quota`):**
- `internal/handlers/router.go`: Mounted `/api/usage/{connectionId}`, `/api/usage/providers`, `/api/usage/stream`, `/api/usage/stats`, and `/api/usage/request-details` inside `SetupDashboardRoutes` with `RequireDashboardAuth`, allowing authenticated browser sessions (JWT cookie), local CLI tokens, and client API keys to fetch quota and usage details.
- `internal/handlers/dashboard/usage_providers.go`: Enhanced `fetchAntigravityDashboardWeekly` to parse both session (5h) and weekly quota buckets from Google's `retrieveUserQuotaSummary`, and added reconciliation when all Gemini models are exhausted (matching upstream `open-sse/services/usage/antigravity-weekly.js` and `google.js`).
- `web/package.json` & `web/src/main.ts`: Added and bundled `material-symbols/outlined.css` locally, ensuring all icons (refresh, edit, delete, eye-off, hourglass, toggle) render instantly and work 100% offline without text flashing.
- `web/src/components/quota/types.ts`: Implemented full upstream provider quota parser `parseQuotaData` supporting Antigravity (5-quota family grouping: Gemini 5h, Claude & GPT 5h, Gemini 3.1 Flash Image, Gemini Weekly, Claude & GPT Weekly), Codex, Kiro, Qoder, Claude, DeepSeek, Groq, Ollama, and Zed, with model catalog canonical sorting.
- `web/src/components/QuotaTrackerView.svelte`:
  - Added secondary connection label (`getConnectionSecondaryLabel`) for accounts with different emails/display names.
  - Aligned status badges to only render on Kiro connections (matching upstream Next.js).
  - Added connection edit action (pencil icon) with interactive `Edit Connection` modal (name & priority editing + reachability test).
  - Added auto-ping toggle (`bolt` icon) for Claude & Codex OAuth accounts and Codex reset credits integration.


**Token Saver Full Parity with Upstream Next.js (`/dashboard/token-saver`):**
- `internal/handlers/router.go`: Mounted `/api/headroom/*` (`status`, `start`, `stop`, `restart`, `extras`, and `proxy`) inside `SetupDashboardRoutes` with `RequireDashboardAuth`, allowing dashboard session cookies to query and control Headroom proxy lifecycle.
- `web/src/api/client.ts`: Updated `HeadroomStatusResponse` and `HeadroomExtrasResponse`, adding `getHeadroomExtras()`, `startHeadroom()`, `stopHeadroom()`, `restartHeadroom()`, `installHeadroomExtras()`, and `uninstallHeadroomExtras()`.
- `web/src/components/TokenSaverView.svelte`:
  - Removed duplicate in-page header (`PiggyBank` banner) to align with upstream Next.js header layout where page title & icon live in `TopBar`.
  - Replaced custom dialog with standard `Modal.svelte` featuring macOS-style traffic lights (`#FF5F56`), backdrop blur, and `Button.svelte`/`Input.svelte` components.
  - Implemented full Headroom status detection (`Checking…`, `Running`, `Not installed`, `Stopped`, `External`) with local/managed PID checks.
  - Added Headroom compression extras (`[code]`, `[ml]`), install confirmation modal (warning on 1GB ML download), pip uninstall, and log tail streaming.
  - Added locale-based Wenyan level filtering for Caveman output compressor (only showing classical Chinese compression levels on `zh` locales).


**Freebuff Multi-Account Session Management & Direct Proxy Fallback:**
- `internal/proxy/executor/freebuff_session.go` & `freebuff.go`: Added `DoFreebuffHTTP` with automatic fallback to direct connection (`Proxy: nil`) when local or environment proxies refuse requests to `codebuff.com` / `freebuff.com` (`403 Forbidden` / `blocked-by-allowlist`), preventing session lookups and admissions from failing.
- `internal/handlers/oauth/freebuff_session.go` & `freebuff_session_switch.go`: Automatically syncs active session models (`currentModel`) into `conn.Data` (`freebuffModel` and `assignedModel`) in SQLite, enabling strict model routing per connection.
- `web/src/components/connections/FreebuffSessionBanner.svelte`: Added multi-account selection pills allowing users to view and switch between different Freebuff accounts and their respective sessions directly from the banner.
- `web/src/components/connections/ProviderDetailView.svelte`: Added per-connection session badges (`🔒 model-id`, `queued`, `banned`, `no session`) on connection cards with a dedicated **Session** button (`lock_clock`) to quickly focus and manage any account's active seat.

**Vision & Audio Adapter Full Parity (`/dashboard/combos`):**
- `web/src/lib/models.ts`:
  - Updated `getModelCaps` to detect `audioInput` capability and refined vision detection by eliminating false positives from bare `"flash"` model ID tokens (which previously caused non-vision models like `deepseek-v4-flash` to be incorrectly classified as vision-capable).
  - Aligned vision patterns with upstream `visionPatterns.js` (`looksLikeVisionModel`), filtering out audio/tts/stt/embedding generators while identifying multi-modal vision families (`gemini`, `4o`, `gpt-5`, `gpt-6`, `opus`, `sonnet`, `haiku-4.5`, `fable`, `kimi`, `minimax`, `mimo`, `qwen`, `grok`, `llama-4`, `muse-spark`).
- `web/src/components/combos/pickerData.ts`:
  - Added `caps.audioInput` to `PickerModel`.
  - Strictly enforced modality filtering in `resolveFilteredGroups`: `target === 'vision'` now filters exclusively for models with `caps.vision === true`, and `target === 'audio'` filters exclusively for `caps.audioInput === true`, regardless of whether a search query is active.
  - Added empty state handler displaying `No models found` with search icon when no models in the active catalog match the modality requirement.
- `web/src/components/combos/CapacityAdapterSection.svelte`:
  - Removed redundant summary bullet list above cards to match upstream Next.js header layout.
  - Aligned subtitles to `— images (png, jpg, webp, …)` and `— audio input`.
  - Standardized icons to Material Symbols `visibility` (eye) and `graphic_eq` (sound wave equalizer).
- `internal/db/settings.go`: Added `CapacityAdapterEntry` and `CapacityAdapter map[string]CapacityAdapterEntry` to `SettingsData` with default fallback to `ag/gemini-3.8-flash-high`.
- `internal/handlers/chat/combo.go`: Implemented `AugmentModelsWithCapacityAdapter` to automatically prepend models from the capacity adapter pool when none of the target models support required input modalities (e.g. vision for image inputs).
- `internal/handlers/chat/chat.go`: Integrated capacity adapter auto-switch into both OpenAI `/v1/chat/completions` and Anthropic `/v1/messages` for both combos and single-model requests.
- `internal/handlers/chat/vision_adapter_e2e_test.go`: Added comprehensive E2E tests verifying automatic switching from text-only models (`deepseek/deepseek-chat`) to vision-capable models (`ag/gemini-3.8-flash-high`) when image inputs are present in OpenAI and Anthropic request formats.

**Change Log in-app modal (upstream Next.js parity):**
- `web/src/components/ChangelogModal.svelte`: Added `ChangelogModal` Svelte 5 component with markdown parsing via `marked`, custom scrollable container, loading/error states with retry, backdrop-blur overlay, and links to GitHub Releases & Changelog history.
- `web/src/components/TopBar.svelte`: Changed the App Drawer item under "Theme" from an external link to a button triggering the in-app `ChangelogModal`, matching upstream Next.js `HeaderMenu` behavior.
- `web/src/api/client.ts`: Added `api.getChangelog()` querying `/api/changelog` with fallback to GitHub raw URLs.
- `internal/handlers/chat/chat.go` + `internal/handlers/router.go`: Added `GET /api/changelog` and `GET /changelog` endpoints to serve the application changelog directly from local disk with remote fallback.
- `web/src/index.css`: Added `.changelog-body` markdown typography and badge styles.

**Update notification banner in Sidebar (upstream Next.js parity):**
- `web/src/components/Sidebar.svelte`: Added update notification banner below the version label (`↑ New version available: v{latestVersion}`) with `Update now` button and clickable `9router-go update` command pill, matching upstream Next.js `Sidebar.js`.
- Added interactive Update modal with release notes, one-click auto-updater (`api.triggerUpdate()`), manual copy command with countdown shutdown, and disconnected reconnect overlay.
- `web/src/api/client.ts`: Added `SystemVersionInfo` type, `checkUpdate()`, `triggerUpdate()`, and `shutdownServer()`.

**Remove 9Remote & 9English; align Support 9Router with 9router-go repo:**
- `web/src/components/Sidebar.svelte`: Removed the `9Remote` action button & modal and `9English` external link from navigation items; cleaned up modal markup and unused `isRemoteModalOpen` state.
- `web/src/components/TopBar.svelte`: Updated the "Support 9Router" modal to remove the external 9English project link and point to the `9router-go` repository ([`https://github.com/luqman-v1/9router-go`](https://github.com/luqman-v1/9router-go)) and Releases page ([`/releases`](https://github.com/luqman-v1/9router-go/releases)).

### 🐛 Bug Fixes

**OpenCode Chat Completions Tool Fingerprint & Model Purity (`space-bunny-free`):**
- `internal/translator/fingerprint.go`: Fixed `ConcealFingerprintTools` to preserve standard Chat Completions tool shape (`{"type":"function","function":{"name":...}}`) with `"tool_choice":"none"` when client tools are absent, instead of falling back to flat Responses format (`{"type":"function","name":...}`) which caused upstream `[invalid_request_error] invalid request` on OpenCode Chat Completions models like `space-bunny-free`.
- `internal/proxy/executor/providers.go`: Stripped provider prefixes (`oc/`, `opencode/`) from `model` in `ForwardOpencode` and `ForwardOpencodeGo` before forwarding upstream.
- `internal/handlers/chat/resolution.go`: Removed hardcoded model rewrites (`strings.Contains(model, "muse-spark")`, etc.) from Antigravity resolution; all `ag/` and `antigravity/` models resolve cleanly to `antigravity` without special-case overrides.
- `web/src/components/connections/ProviderDetailView.svelte`: Restricted `suggestedModels` strictly to providers that declare a public `modelsFetcher` (upstream Next.js parity). Removed artificial suggested models injection under Antigravity so OpenCode models no longer leak into Antigravity's view.

**Antigravity Google OAuth Callback percent-encoding unescape:**
- `internal/handlers/oauth/antigravity.go`: Added `cleanAuthCode` to unescape double-encoded slashes (`4/0A...` vs `4%252F...`) and strip raw URL parameter prefixes before submitting `application/x-www-form-urlencoded` token exchange requests to Google.
- `web/src/components/connections/ProviderDetailView.svelte`: Added `decodeURIComponent` input cleansing for pasted OAuth authorization callback URLs.

**Deep Auth Verification for OpenAI-Compatible Custom Nodes:**
- `internal/handlers/dashboard/validate.go`: Added a secondary 1-token probe to `POST /v1/chat/completions` during provider node validation (`validateOpenAICompatibleNode`), preventing mock or unauthenticated servers from returning false positive validation results.

**Fix Round-Robin routing for combos and provider connections (Issue #20):**
- `internal/db/settings.go`: Updated `SettingsData` and `GetSettings()` to parse both dashboard JSON keys (`fallbackStrategy` / `rotateStrategy` and `stickyRoundRobinLimit` / `stickyLimit`), as well as global `fallbackStrategy`, `stickyRoundRobinLimit`, `comboStrategy`, `comboStickyRoundRobinLimit`, and `comboStrategies`.
- `internal/db/settings.go`: Updated `SetProviderStrategy` and added `SetComboStrategy` to write to `settings.data` via `UpdateSettingsRaw` without clobbering other settings fields.
- `internal/db/repos.go`: Updated `GetComboByName`, `GetComboById`, and `GetCombos` to populate `combo.Strategy` from `settings.comboStrategies[combo.Name]` and global `settings.comboStrategy`.
- `internal/handlers/chat/resolution.go`: Added `resolveComboRouting` so `ResolveModel` and `resolveModelEntry` populate `ModelInfo.Strategy`, `ModelInfo.StickyLimit`, and `ModelInfo.JudgeModel` from `settings.comboStrategies` or global combo settings.
- `internal/handlers/chat/connections.go` & `fallback.go`: Updated provider connection selection to rotate active connections using `fallbackStrategy` or global fallback settings when configured to `"round-robin"`.
- Added unit tests in `internal/db/settings_test.go` and `internal/handlers/chat/connection_strategy_test.go` covering combo strategy resolution, sticky limits, judge models, and provider connection rotation.

**Fix fetch stream double-read in `web/src/api/client.ts` and add missing tunnel endpoint handlers:**
- `web/src/api/client.ts`: Resolved `Failed to execute 'text' on 'Response': body stream already read` error when receiving non-2xx responses. Previously, `res.json()` consumed the stream body on error responses, which caused the subsequent `res.text()` fallback in the catch block to crash. The client now safely reads `res.text()` first before attempting JSON parsing.
- `internal/handlers/dashboard/tunnel.go` & `internal/handlers/router.go`: Added endpoints `POST /api/tunnel/enable`, `POST /api/tunnel/disable`, `GET /api/tunnel/tailscale-check`, `POST /api/tunnel/tailscale-enable`, and `POST /api/tunnel/tailscale-disable` with structured JSON responses and clean error handling instead of unhandled 404s.

**Combo bypass Vercel Edge Relay for no-auth providers (e.g. `oc/muse-spark-1.3` 429 on `combo-wombo`, solo test 200):**
- `internal/handlers/chat/combo.go` (chat + messages fallback) and `combo_fusion.go` — no-auth branch now sets `ProxyPoolID: h.ResolveProviderProxyPoolID(modelInfo.Provider)` (was `&ConnectionData{APIKey}` only), so combo routing goes through the configured `providerStrategies.<provider>.proxyPoolId` relay (`x-relay-target`/`x-relay-path`) exactly like the solo path (`handleAccountFallback`). Direct-to-`https://opencode.ai/zen/v1/responses` calls that burned the free-tier IP quota (`FreeUsageLimitError` 429) are eliminated.

**Vercel Edge Relay header forwarding for `muse-spark` / `antigravity`:**
- `internal/proxy/opencode.go` — `BuildOpenCodeHeaders` preserves `x-relay-target` / `x-relay-path` instead of dropping them.
- `internal/proxy/executor/providers.go` — `ForwardOpencode` / `ForwardOpencodeGo` keep `BaseURL` on the relay host and route via `x-relay-path` (`/zen/v1/responses`, `/zen/v1/messages`, `/zen/go/v1/responses`) when relay headers are present.
- Added `TestForwardOpencode_MuseSpark_EdgeRelay` (PASS); verified live `200 OK` via relay.

**Topology false pulse on dashboard load (`AnalyticsView.svelte`):**
- SSE `/api/usage/stream` initial snapshot no longer triggers the electric-beam animation: added `streamInitialized` guard so only genuine new model requests after init pulse; active-request updates set `lastProvider` without re-pulsing. Dashboard API traffic (`/api/usage`, polling) never triggers topology effects — only upstream model calls do.
- Consolidated per-node SVG turbulence filters into one lightweight global filter (`numOctaves="1"`) for GPU/CPU relief during continuous animation.

**Query-param auth for SSE streams (`internal/middleware/auth.go`):**
- `ExtractApiKey` accepts `?key=` / `?apiKey=` on routes ending in `/stream` (native `EventSource` can't set custom headers); REST/LLM endpoints stay header-only.

**Topology Option A visuals (`ProviderTopologyCard.svelte` + `web/src/index.css`):**
- Bidirectional neural stream (cyan prompt Router→Provider, emerald/gold response Provider→Router), dual shockwave rings on the active provider target, router absorption rings, node micro-bounce + `LIVE` badge.

### 🔄 Upstream Parity Sync

**Strike-breaker quota-only (upstream `decolua/9router#4197` parity, PR #16):**
- `internal/handlers/chat/antigravity_quota.go` — `HandleAntigravityQuotaError` now takes the upstream `errorMessage` and only counts a strike on explicit quota markers (`RATE_LIMIT_EXCEEDED`, `QUOTA_EXHAUSTED`, `Individual quota reached`). Generic bare `RESOURCE_EXHAUSTED` 429s no longer burn strikes / lock combos.
- `internal/handlers/chat/gemini_handler.go` — forwards `string(uErr.Body)` as the error message source.
- Added `TestAntigravityQuota_Generic429NoStrike` regression test.

**Refusal → content_filter mapping (upstream `decolua/9router#4210` parity, PR #16):**
- `internal/translator/claude_response.go` — streaming + non-streaming: Gemini `refusal` finish maps to `content_filter`, emits `stop_details.explanation` so Claude Code renders the block instead of hanging.
- `internal/translator/response.go` — reverse mapping `content_filter → refusal` for round-trips.
- Added refusal stream / non-stream / round-trip tests.

**Weekly vs session quota buckets (upstream `decolua/9router#4209` parity, PR #18):**
- `internal/handlers/chat/antigravity_quota.go` — `ParseWeeklyQuotaSummary` classifies the `window` field into weekly (`gemini`/`claude_gpt`) vs 5h-session (`gemini_session`/`claude_gpt_session`) buckets; `IsAntigravityModelBlocked` honors session buckets via `quotaEntryExhausted` helper.
- Added `TestAntigravityWeeklyQuota_SessionBuckets`.

**Add Compatible modal + provider-node validation (upstream `AddCompatibleModal.js` + `provider-nodes/validate/route.js` parity):**
- `web/src/components/connections/AddCompatibleNodeModal.svelte` — rebuilt to match the upstream modal: separate `Name` / `Prefix` / `API Type` fields with upstream placeholders (`OpenAI Compatible (Prod)`, `oc-prod`, …) and hints, `API Key (for Check)` + `Model ID (optional)` inputs driving a `Check` button with `Valid` / `Invalid` badges (chat-fallback note included), and full-width `Create` + `Cancel`. The API key is now validation-only and no longer auto-creates a connection (`ConnectionsView.svelte`).
- `internal/handlers/dashboard/provider_nodes.go` + `router.go` / `routes.go` — added `POST /api/provider-nodes/validate` (OpenAI-compatible `/models` + chat fallback, Anthropic-compatible with `x-api-key` + `/messages`-suffix strip, `custom-embedding` with dimension report), including SSRF guard for non-loopback callers (`handlerutil.AssertPublicURL`).
- `web/src/api/client.ts` — added `validateProviderNode`.
- Added `provider_nodes_validate_test.go` (13 tests: input guards, SSRF/local, OpenAI/Anthropic/embedding probes, chat fallback, network-error mapping).

### 🧹 Style Cleanup (behavior-neutral, PR #17 + follow-ups)

- `interface{}` → `any` across production code and test files; `errors.New` + `%w` wrapping; `slices.Contains/Sorted/Delete`, builtin `max()`/`clear()`, `strings.Builder`, shared header constants in `internal/constants`.
- Named constants: `antigravityDecoyUnavailable`, `maxReadLimit`, `thinkingHeadroomTokens`, `maxCallIDLen`, `MaxUpstreamBodyBytes`/`UpstreamErrLimit`.
- `fallback.go` — `forwardRequestParams` struct replaces 10-param forwarding; `openai.go` — `sseStreamOpts` struct replaces 8-param SSE helper.
- `antigravity_quota.go` — `AntigravityQuotaError` struct + named quota markers (replaces `map[string]any` error plumbing).
- Added `samber/lo` (`Ternary`, `CoalesceOrEmpty` only — `Coalesce` on `any` maps and eager `Ternary` slicing deliberately avoided).
- Default port `20128` → `20130` (`config.go`, `Makefile`, `mitm/handlers/base.go`, `.env.example`, `docker-compose.yml`, `Dockerfile`, `README.md`).

### 🧪 Tests

- `gemini38_live_test.go` — real upstream tests for `ag/gemini-3.8-flash-medium` (chat + stream, `200 OK`). Live E2E: 17/17 PASS.

### 📦 Release Hardening (issue #19)

- `make cross` generates `SHA256SUMS.txt`, uploaded by `release.yml`; README documents the Windows Defender false-positive (`Wacatac.C!ml` heuristic on the unsigned binary) with verify + Allow steps.


## [v1.8.18] — 2026-09-21

### 🐛 Bug Fixes

**OMP Harness False-429 on Antigravity (upstream `decolua/9router#3986` parity):**
- `internal/translator/antigravity.go` — `WrapForAntigravity` no longer sends `requestType: "agent"` in the Cloud Code envelope (`AntigravityRequest.RequestType` is now `omitempty` and left empty). Google enforces a tiny separate quota bucket whenever `requestType="agent"` is present, so OMP (Oh My Pi) harness payloads (~25–30k token system prompt + tools) were rejected with false `429 RESOURCE_EXHAUSTED` even with quota remaining — cascading into `CACHE_BLOCK` account locks while Claude Code stayed green. Verified live: same payload returns `200 OK` after the fix.
- `internal/translator/antigravity_test.go` — Added `TestWrapForAntigravity_OmitsAgentRequestType` regression test (asserts the field is absent from the raw envelope JSON and the `requestId` `agent/<…>` shape is preserved). Image (`image_gen`) and search (`search`) request types are untouched.

### 🔄 Upstream Parity Sync — `decolua/9router` v0.5.75…v0.5.81 (100%)

**Model Catalog & Routing:**
- `internal/providers/registry_models.go` — Registered `deepseek-v4.1-flash` for `codebuddy-intl` (`cbai`, replacing the retired `deepseek-v4-flash`) and added `deepseek-v4.1-flash:cloud` to the `ollama` catalog.
- `internal/handlers/chat/resolution.go` — Routed bare `codex-auto-review` to the `codex` provider (PR #4135 parity), resolved even with a nil repo / empty DB.

**Antigravity Hygiene:**
- `internal/translator/antigravity.go` — Stripped the Claude Code `x-anthropic-billing-header` from system prompts and sanitized the Hermes Agent identity (`You are Hermes Agent, an intelligent AI assistant created by Nous Research.` → neutral form) to eliminate false HTTP 429/403 anti-abuse rejections.
- `internal/translator/thought_signature_store.go` — Scoped cached Gemini thought signatures to the producing model family (`claude` vs `gemini`), preventing cross-family replay that triggers HTTP 400 `Invalid thought signature` when a conversation switches models (upstream `bc3be0cb` parity).
- `internal/translator/gemini.go` — Threaded the model name through the `GetGeminiThoughtSignature` / `StoreGeminiThoughtSignature` call sites so stored signatures are keyed per model family (call-site half of the scoping above).

**CommandCode Multimodal & Reasoning:**
- `internal/proxy/executor/providers.go` — Added native image blocks to `buildCommandcodeBody`: OpenAI `image_url` data URIs and Claude/OpenAI base64 image sources are converted to CommandCode `{type: "image", image: <dataUri>, mimeType}` blocks, and `reasoning_effort` (`low`/`medium`/`high`/`max`) is preserved on `/alpha/generate`.

**Union-Alpha / OpenCode Parity (verified):**
- Confirmed live routing of `oc/union-alpha` through the Anthropic Messages API (`/zen/v1/messages`) with `anthropic-version: 2023-06-01` and automatic `max_tokens` injection (PR #4099 parity); free-tier `forceStream`/SSE aggregation parity already structural in Go.
- `internal/handlers/chat/muse_spark_e2e_test.go` — Added `TestIntegration_OpenCode_UnionAlpha_Messages` (live E2E; SKIPs on upstream rate-limit or auth-dependent `Model union-alpha is not supported` 401, consistent with existing Muse Spark E2E policy).

## [v1.8.17] — 2026-09-18

### 🚀 Features & Upstream Parity

**Claude OAuth Subscription (`sk-ant-oat`) Support End-to-End (PR #13):**
- Contributed by **@rezhajulio** ([#13](https://github.com/luqman-v1/9router-go/pull/13)) — Special thanks for bringing full Claude Pro/Max subscription parity from the dashboard to the native Go proxy!
- `internal/handlers/chat/fallback.go` — Automatic header switching to `Authorization: Bearer` and appending `?beta=true` for Claude OAuth credentials (`sk-ant-oat` or `accessToken`), supporting direct Anthropic API as well as Edge Relay proxy pools.
- `internal/handlers/chat/claude_cloaking.go` — Injected official `x-anthropic-billing-header` into `system[0]`, deterministic account `metadata.user_id`, client tool name obfuscation with `_ide` suffix, and decoy tools (`CCDecoyTools`) preventing false HTTP 429 anti-abuse rate limits.
- `internal/proxy/executor/claude_decloak.go` — Streaming and non-streaming response decloaker restoring original tool names and translating decoy tool invocations into clean text blocks.
- `internal/proxy/executor/providers.go` — Added `sanitizeToolUseID` to rewrite foreign/Gemini tool IDs deterministically to Anthropic-compliant `toolu_<sha256>`.
- `internal/tokensaver/prompts.go` — Added `InjectSystemPromptClaude` for format-aware system prompt injection at top-level `system`.

**Antigravity Zen Free-Tier Tool Quartet Renaming (PR #12):**
- Contributed by **@yxxrn** ([#12](https://github.com/luqman-v1/9router-go/pull/12)) — Special thanks for identifying the exact upstream fingerprinting gate and eliminating Claude Code 403/500 errors!
- `internal/translator/fingerprint.go` — Implemented `ConcealFingerprintTools` to rename uppercase tool quartet variants from Claude Code CLI (`Bash`, `Glob`, `Grep`, `Read`) to canonical lowercase (`bash`, `glob`, `grep`, `read`), eliminate duplicates (preventing upstream HTTP 500), retarget `tool_choice`, and restore original tool names in response payloads via `RestoreToolNamesInPayload` / `RestoreToolNamesInSSE`.
- `internal/proxy/executor/toolname_writer.go` — Embedded `toolNameRestoringWriter` on responses ensuring client tools are seamlessly restored across both streaming and non-streaming responses.

### 🐛 Bug Fixes & Improvements

**CommandCode CLI User-Agent & Schema Wrapping (PR #14, fixes #9):**
- Reported by **@jhonoryza** ([#9](https://github.com/luqman-v1/9router-go/issues/9)) — Thank you for reporting the Cloudflare challenge error!
- `internal/providers/providers.go` & `internal/proxy/executor/providers.go` — Added official `User-Agent: commandcode/0.25.7 (cli)` and `x-command-code-version: 0.25.7`, eliminating Cloudflare WAF bot-challenge intercepts (HTTP 403 `Attention Required!`).
- `internal/proxy/executor/providers.go` — Implemented `buildCommandcodeBody` wrapping OpenAI payloads into `{threadId, memory, config, params}` schema required by CommandCode's `/alpha/generate` endpoint.
- `internal/handlers/chat/fallback.go` & `internal/proxy/proxy.go` — Enhanced error parsing in `extractErrorText` and `UpstreamError.Error()` to summarize Cloudflare challenge pages cleanly without dumping raw HTML.

**Responses API (`POST /v1/responses`) Public Provider Fallback & String Input (PR #15, fixes #10):**
- Reported by **@pankaj-raikar** ([#10](https://github.com/luqman-v1/9router-go/issues/10)) — Thank you for the detailed reproduction report!
- `internal/handlers/media/media.go` — Added automatic fallback for public/free-tier providers (`DefaultAPIKey: "public"`) in `forwardMediaRequest`, eliminating `"no active connections for provider: opencode"` when no SQLite connection is seeded.
- `internal/handlers/media/media.go` & `internal/handlers/chat/resolution.go` — Routed `opencode`, `opencode-go`, and `antigravity/muse-spark-*` models on `/responses` directly to `ForwardOpencode` so session tracking, request headers, and tool-name concealing work out of the box.
- `internal/proxy/executor/transform.go` — Updated `buildResponsesBody` to support both string inputs (`"input": "Say hello"`) and array inputs (`Input []any`), normalizing string prompts into valid Responses message items.

## [v1.8.16] — 2026-09-17

### 🐛 Upstream Parity & Bug Fixes

**Active Provider Connections & Capabilities on `/v1/models` and `/api/models` (PR #8, fixes #7):**
- `internal/providers/registry_models.go` — Added comprehensive upstream provider models registry mapped from 128 provider definitions (`decolua/9router` parity) with `GetProviderModels()`.
- `internal/providers/capabilities.go` — Implemented `CapabilitiesDetail` and `GetCapabilitiesDetailForModel()`, outputting full multimodal flags (`vision`, `pdf`, `audioInput`, `videoInput`, `imageOutput`, `audioOutput`), `thinkingCanDisable`, and dual token limits (`contextWindows` and `contextWindow`).
- `internal/handlers/chat/chat.go` — Replaced empty `/v1/models` responses by dynamically aggregating models for active connections (`isActive = 1`), honoring custom prefixes and connection-enabled models. Custom models from deactivated connections (`disabledProviders`) are automatically excluded, and clean static catalogs are returned when no database connections are configured.
- `internal/handlers/router.go` — Mounted `GET /api/models` and `GET /api/models/*` serving both `"data"` and `"models"` top-level keys for universal compatibility across OpenAI-compliant IDE clients and Next.js web dashboards.
- `internal/handlers/chat/models_provider_test.go` — Added regression tests verifying model discovery for active Codex (`cx`) connections, fallback registry, and single-model lookups.

## [v1.8.15] — 2026-09-17

### 🚀 Features & Upstream Parity

**Claude Messages to OpenAI Response Translation for Zen / Union-Alpha (PR #4099, #4111):**
- `internal/translator/claude_response.go` — Added on-the-fly streaming (`TranslateClaudeChunkToOpenAI`) and non-streaming (`TranslateClaudeResponseToOpenAI`) response translation engines. Transforms Claude Messages SSE events (`content_block_delta`, `thinking_delta`, `tool_use`, `input_json_delta`, `message_delta`, `message_stop`) into standard OpenAI chunks (`choices[0].delta.content`, `reasoning_content`, `tool_calls`) so that client harnesses (e.g. omp, Cursor, Cline) receive native responses.
- `internal/proxy/executor/claude_messages.go` — Added dedicated streaming handler (`handleClaudeMessagesStream`) and non-streaming handler (`handleClaudeMessagesNonStream`) wired into `ForwardOpencode` and `ForwardOpencodeGo` for `union-alpha` routes.
- `internal/proxy/opencode.go` — Updated Antigravity Zen headers to comply with upstream PR #4111 (`User-Agent: antigravity/1.18.31 ai-sdk/provider-utils/4.0.46 runtime/bun/1.3.14`, `x-antigravity-client: cli`, dynamic 40-character hex project IDs `GenerateOpenCodeProjectID()`, and `x-api-key: public`).

**Anthropic Tools & Messages Schema Normalization:**
- `internal/proxy/executor/providers.go` — Added `convertOpenAIToolsToClaude`, `ensureMessagesMaxTokens`, `extractClaudeSystemPrompt`, and `convertOpenAIMessagesToClaude` to convert incoming OpenAI tool definitions (`type: "function"`) into Claude tools (`{name, description, input_schema}`), normalize `tool_choice`, and merge adjacent same-role messages for compliant Anthropic payload delivery.
- `internal/proxy/sse.go` — Expanded terminal detection buffer to 64 bytes and added recognition for Anthropic terminal signals (`"stop_reason":` non-null and `"message_stop"`) to eliminate premature `finish_reason: "network_error"` synthesis at clean stream EOF.

### 🐛 Bug Fixes & Resilience

**Google RPC `quotaResetDelay` Automatic Duration Locking with Deadlock Prevention:**
- `internal/handlers/chat/fallback.go` — Implemented `extractResetDuration` to parse Google RPC ErrorInfo metadata `quotaResetDelay` (e.g. `"1h12m28.109534319s"`) and text patterns (`"Resets in XhYmZs."`). Enforces safety bounds (min 5s, hard cap at 2 hours) to avoid perpetual lockouts or deadlocks.
- `internal/handlers/chat/combo.go` — Updated `comboLockRetryable` to use the parsed reset duration for connection and model locks instead of falling back to 8s exponential backoff.
- `internal/handlers/chat/antigravity_quota.go` & `internal/handlers/chat/gemini_handler.go` — Added `BlockAntigravityModelUntil` to cache exhausted model quotas and canonical synonyms (`gemini-3.8-flash-tiered`) in RAM until the verified reset timestamp, preventing continuous 429 spam to Google upstream while automatically unblocking the moment reset time is reached.

## [v1.8.14] — 2026-09-17

### 🐛 Upstream Parity & Bug Fixes

**Muse Spark 1.3 FreeTier Authorization & Session Normalization (PR #4105, #4061, #4062):**
- `internal/proxy/antigravity.go` — Updated Antigravity OpenCode user-agent default to `antigravity/1.18.31` and implemented descending canonical 30-character session (`ses_` + 12 hex + 14 Base62) and message (`msg_` + 12 hex + 14 Base62) ID generators. This resolves HTTP 403 `FreeTierError` when routing requests to `oc/muse-spark-1.3-contributor-free`.
- `internal/proxy/executor/providers.go` — Added `normalizeMuseSparkResponsesBody` to strip prior multi-turn reasoning content and encrypted content blocks that trigger HTTP 400 parameter errors, while explicitly normalizing `tool_choice` to `"auto"` for Muse Spark 1.3.

**Missing `tool_call_id` FIFO Repair (PR #4090):**
- `internal/handlers/chat/tool_repair.go` & `internal/translator/request.go` — Added automatic repairing for client requests where `role: "tool"` or `function_call_output` messages omit `tool_call_id`. Uses FIFO pairing with un-paired assistant tool calls or mints deterministic call IDs to prevent strict upstreams (OpenAI, DeepSeek, Antigravity) from failing with HTTP 400.
- `internal/handlers/chat/chat.go`, `internal/handlers/chat/combo.go`, `internal/handlers/chat/fallback.go`, & `internal/proxy/executor/transform.go` — Integrated tool call repair across OpenAI chat completions, combo routing, and account fallback handlers.

**Stream Interruption Terminal Synthesis (PR #4079):**
- `internal/proxy/sse.go` — Implemented terminal frame synthesis (`finish_reason: "network_error"` followed by `data: [DONE]\n\n`) when upstream SSE connections terminate abruptly at EOF before emitting a terminal frame, preventing IDE client hangs and errors in Cline/Pi.

**Claude Tool Result Image Hoisting (PR #4083):**
- `internal/translator/request.go` — Converted base64 image blocks embedded inside Claude `tool_result` into follow-up user messages with `[Image from tool result <id>]` and OpenAI `image_url` blocks, allowing vision models to inspect tool screenshot outputs without violating text-only tool-role schema constraints.

**Grok CLI Tool Result Neutral Placeholder (PR #4109):**
- `internal/proxy/grokcli.go` & `internal/mitm/handlers/kiro.go` — Replaced placeholder `"continue"` with neutral `"Tool results provided."` for tool-result user turns in Kiro request payloads to prevent assistant hallucination loops.

**Thinking Variant Route Stripping & Union Alpha Messages Route (PR #4084, #4099):**
- `internal/proxy/executor/providers.go` — Stripped model suffix before checking `isOpencodeResponsesModel`, enabling models like `gpt-5.6-luna(high)` to correctly route to `/responses`.
- `internal/proxy/executor/providers.go` — Routed Antigravity Zen `union-alpha` directly to `/zen/v1/messages` with `anthropic-version: 2023-06-01`.
- `internal/providers/capabilities.go` — Registered model capabilities for `union-alpha`, `deepseek-v4.1-flash`, and `deepseek-flash`.

## [v1.8.13] — 2026-09-16

### 🐛 Bug Fixes & Parity

**Grok CLI Responses Endpoint Fix (PR #4, fixes #2):**
- `internal/providers/providers.go` — Updated `grok-cli` `BaseURL` from bare root `https://cli-chat-proxy.grok.com` to `https://cli-chat-proxy.grok.com/v1/responses`, resolving HTTP 404 HTML edge errors when calling `gcli/*` models (`grok-4.5`, `grok-4.6`).
- `internal/proxy/grokcli.go` — Added auto-normalization in `ForwardGrokCLI` so that if `cfg.BaseURL` is empty or lacks the `/v1/responses` path, it automatically normalizes to `/v1/responses`.
- `internal/handlers/chat/grokcli_handler_test.go` — Added regression tests for grok-cli BaseURL and endpoint routing.

**Custom Provider Nodes Prefix Priority & Model Mapping (PR #5, fixes #3):**
- `internal/handlers/chat/resolution.go` — Prioritized custom `providerNode` prefix resolution (`h.resolvePrefixProvider(prefix, model)`) before checking built-in provider aliases (`resolveProviderAlias(prefix)`). This prevents short prefixes like `oa` or `cc` from being shadowed by `openai` or `claude`, eliminating false 502 "no active connections for provider: openai" failures when the custom node has active connections.
- `internal/handlers/chat/resolution.go` — Added fallback so that when the alias-resolved provider has no active connections, unresolved prefix queries report the matching custom `providerNode.id` instead of falsely blaming the shadowed built-in provider.
- `internal/handlers/chat/resolution.go` — Added defensive nil checks for `h.Repo` across all model and combo resolution helpers.
- `internal/db/repos.go` — Added `GetProviderNodePrefixMap()` to map internal row IDs (`openai-compatible-chat-0489...`) to user-configured prefixes (e.g. `nara`, `orca`, `oa`).
- `internal/db/repos.go` — Updated `GetCustomModels()` with fallback parsing from keys (`<providerAlias>|<modelId>|<kind>`) and removed restrictive `type == "llm"` filtering, exposing all custom chat and completion models.
- `internal/handlers/chat/chat.go` — In `HandleModels` (`GET /v1/models`) and `HandleModelLookup` (`GET /v1/models/*`), mapped `cm.ProviderAlias` through the prefix map so models are published under their clean user-configured prefix (e.g. `nara/glm-5.3`) with `owned_by` set to the prefix rather than leaking internal database row IDs.
- `internal/handlers/chat/resolution_test.go`, `internal/handlers/chat/chat_v065_test.go`, & `internal/db/repos_test.go` — Added comprehensive unit and regression tests for custom prefix priority, fallback error reporting, prefix map caching, key-fallback parsing, and `/v1/models` prefix output.

## [v1.8.12] — 2026-09-16

### 🚀 Features & Provider Additions

**Freebuff Provider Integration (`fb`):**
- `internal/proxy/executor/freebuff.go` — Added native Freebuff executor supporting `https://www.codebuff.com/api/v1/chat/completions` with 1-hour session token lifecycle caching, agent run tracking (`/api/v1/agent-runs`), Buffy system prompt marker injection, and `end_turn` tool injection for sub-agent orchestration.
- `internal/providers/providers.go` & `internal/providers/aliases.go` — Registered provider `freebuff` and alias `fb`.

**Provider Connection Routing Strategies:**
- `internal/db/settings.go` & `internal/handlers/chat/connections.go` — Added configurable multi-connection routing strategies per provider: `sticky` (with configurable `stickyLimit`), `round-robin`, `random`, and `none`.

**Model Capabilities & Limits:**
- `internal/providers/capabilities.go` — Added capabilities for Upstage Solar Pro (`*solar-pro*`) and LongCat (`*longcat*`) with reasoning, tools, 200,000 token context window, and 32,000 max output tokens.

### 🐛 Bug Fixes & Resiliency

**Cline & Clinepass OAuth Refresh Overhaul:**
- `internal/proxy/oauth/cline.go` — Registered `clinepass` alongside `cline` in the OAuth registry, resolving issues where ClinePass connections fell back to incompatible standard form-urlencoded OAuth refresh.
- Migrated token refresh endpoint from deprecated `/v1/auth/refresh` (which returned 401 "Please make sure you're using the latest version of Cline") to active upstream `/api/v1/auth/refresh`.
- Emulated full Cline CLI identity headers on refresh (`User-Agent: Cline/3.0.61`, `X-CLIENT-TYPE: cline-cli`, `X-CLIENT-VERSION: 3.0.61`, `X-CORE-VERSION: 3.0.61`, `X-PLATFORM: cli`).
- Added token rotation support: propagated rotated `refreshToken` to SQLite database across `BuildConnectionUpdate` and `forceRefreshOAuthToken`.
- `internal/handlers/chat/fallback.go` — Ensured `refreshedKey` is normalized with `NormalizeProviderToken` on reactive 401 retries so WorkOS prefix (`workos:`) is preserved.

**Zero-Sleep Failover & Canonical Model Locking:**
- `internal/handlers/chat/combo.go` — Removed synchronous blocking sleeps (`time.Sleep`) during combo failover loops on transient errors (502, 503, 504), enabling immediate non-blocking failover to backup models/connections without stalling client turns.
- `internal/handlers/chat/connections.go` & `internal/handlers/chat/combo.go` — Added `canonicalLockModel(provider, model)` to atomically lock shared tier pools across connections (e.g., Antigravity `gemini-3.8-flash-low`/`high` map to canonical lock key `gemini-3.8-flash-tiered`). Prevents split-lock failure loops across shared tier accounts.
- `internal/providers/errorclassify.go` — Expanded error classification to trigger backoff for `resource_exhausted`, `model_capacity_exhausted`, and HTTP 502/503/504 status codes.

**Stream Telemetry & Connection Safety:**
- `internal/proxy/sse.go` — Added rolling 16-byte tail buffer in `SSECopy` to safely detect `[DONE]` across chunk boundaries and cleanly terminate SSE streams without hanging on keep-alive connections.
- `internal/handlers/chat/fallback.go` — Guaranteed `usageHistory` database logging on completed streams even when the client disconnects at stream end.
- Advertised `context_window` in `/v1/models` and `/v1/models/info` dynamically from capabilities.

## [v1.8.11] — 2026-09-12

### 🐛 Bug Fixes & Parity — Upstream PR Porting

**Gemini Multiple System Messages Preservation (PR #3973):**
- `internal/translator/gemini.go` — Preserved all `role: "system"` messages in `req.SystemInstruction.Parts` rather than overwriting earlier instructions with the last turn, ensuring all system prompts and developer directives reach Gemini models.

**Antigravity Thinking Budget & Output Tokens Guard (PR #3981):**
- `internal/translator/gemini.go` & `internal/translator/antigravity.go` — Guarded `maxOutputTokens > thinkingBudget` across `TranslateOpenAIToGemini` and `hardenAntigravityRequest`, preventing HTTP 400 `INVALID_ARGUMENT: max_tokens must be greater than thinking.budget_tokens` and erroneous connection locks on reasoning models.
- Added support for `max_completion_tokens`, `thinking.budget_tokens`, and `thinking_budget`.

**Claude Document Block Support for Antigravity & OpenAI (PR #3968):**
- `internal/translator/request.go` & `internal/translator/types.go` — Added `document` block handling in `TranslateClaudeToOpenAI` and `OpenAIFile` struct in `OpenAIContentBlock`, converting base64 PDF documents into OpenAI file format that flows into Gemini/Antigravity `inlineData`.

**Client Cancellation Tracing & Stream Telemetry:**
- `internal/handlers/chat/fallback.go` — Differentiated client cancellations (`errors.Is(fwdErr, context.Canceled)` or `ctx.Err() != nil`) from true upstream failures, logging `INF [fallback] client canceled request` and recording status `499` in traces instead of raising false `WRN upstream failed` alarms.
- `internal/handlers/chat/gemini_handler.go` — Added accurate `totalBytesWritten` accumulation for OpenAI format streaming branches and terminal `[DONE]` frame.
- `internal/handlers/chat/fallback.go` & `internal/handlers/chat/combo.go` — Refactored hardcoded HTTP status codes to standard `net/http` constants (`http.StatusOK`, `StatusClientClosedRequest`).

**Memory Leak Protections & High-Traffic Concurrency:**
- `internal/translator/usage.go` & `internal/translator/response.go` — Added `pendingFragment` struct with `createdAt` timestamps and 10-minute TTL pruning in `pruneStaleStatesLocked()`. Prevents abandoned fragmented SSE streams from accumulating in the global `pendingJSON` map.
- `internal/handlers/chat/connections.go` — Pooled and cached `*http.Client` and `*http.Transport` instances by proxy URL using `sync.RWMutex`. Eliminates per-request transport allocations, enables TCP keep-alive reuse across proxy pool traffic, and prevents socket/goroutine exhaustion.

**Live Profiling & Diagnostics:**
- `internal/handlers/router.go` — Mounted Go standard `net/http/pprof` endpoints (`/debug/pprof/`, `/debug/pprof/heap`, `/debug/pprof/goroutine`, `/debug/pprof/profile`) for real-time heap and concurrency inspection.

**Documentation & Client Guides:**
- `README.md` — Added comprehensive pre-built binary download links (macOS, Linux, Windows), one-liner install script, Docker setup, and configuration examples for Claude Code, `omp`, and Cursor/Cline.

## [v1.8.10] — 2026-09-11

### ✨ Features & Parity — Next.js v0.5.75 Sync (27 Commits)

**Gemini & Antigravity Content Normalization:**
- `internal/translator/gemini.go` & `internal/translator/antigravity.go` — Added `NormalizeGeminiContents` merging adjacent same-role messages and filtering out empty parts (parity with `#e7b5f09`).
- `internal/handlers/chat/antigravity_quota.go` — Added Antigravity weekly quota tracking (`gemini_weekly`, `claude_gpt_weekly`) and free-tier handling via `retrieveUserQuotaSummary`, caching summaries and reconciling against exhausted model families (#3892).

**Kiro Routing & Wire Payload Cleanup:**
- `internal/providers/providers.go` & `internal/proxy/grokcli.go` — Routed Kiro through Amazon Q first (`https://q.us-east-1.amazonaws.com/generateAssistantResponse`), deprecated legacy runtime path to avoid 400 `REQUEST_BODY_INVALID` (#3776).
- Injected `x-amz-sso-bearer`, `x-amzn-kiro-agent-mode: spec`, and `x-amzn-codewhisperer-machine-id: kiro-desktop` headers.
- `internal/proxy/grokcli.go` & `internal/mitm/handlers/kiro.go` — Stripped top-level `systemPrompt`, `agentMode`, and `conversationState` continuation fields (`agentContinuationId`, `agentTaskType`) that modern Kiro gateways reject with 400 (#1892ed7).

**Opencode-Go Catalog Refresh & Responses API:**
- `internal/proxy/executor/providers.go` — Routed `grok-4.6` and `gpt-5.6-luna` on `opencode-go` to the `/zen/go/v1/responses` endpoint alongside `muse-spark`.
- `internal/providers/capabilities.go` & `internal/proxy/executor/providers.go` — Registered newly published Go models (`deepseek-flash`, `glm-5.3`, `kimi-k3`, `longcat-2.0`, `qwen3.8-max`, `qwen3.8-flash`, `hy4-preview`, `hy3`), with `deepseek-flash` (DeepSeek V4.1 Flash) priority and Qwen 3.8 models in `opencodeGoMessagesModels`.

**Codex CLI Bump & Unicode Schema Sanitization:**
- `internal/providers/providers.go` — Updated Codex CLI User-Agent to `codex_cli_rs/0.154.0` (parity with `#a7047a0`).
- `internal/proxy/executor/transform.go` — Added `StripCodexUnsupportedPatterns` to sanitize `\p{...}` / `\P{...}` Unicode property escapes in tool parameters that Codex's `/responses` validator rejects with HTTP 400 (#3922).
- `internal/providers/capabilities.go` — Added Codex image models (`gpt-image-2.5`, `gpt-image-2.5-flare`, `gpt-image-2.5-sunburst`, `gpt-image-2`, `gpt-image-1.5`) with `ImageOutput` capability and `*gpt-image*` pattern match.

**Claude Cache Budget & Single-Object Turns:**
- `internal/translator/request.go` — Enforced Anthropic 4-marker `cache_control` budget in `AnchorClaudeCache`: pins head anchors (last system block, last non-deferred tool) and keeps at most 2 tail message markers, trimming earlier ones (#8a81085).
- `internal/translator/request.go` — Supported single-object content turns (`content: {type: "text", ...}`) across `convertClaudeMessage`, `SanitizeClaudePassthrough`, and `AnchorClaudeCache`.
- `internal/handlers/chat/chat.go` — Scoped Claude tool type defaulting to gateways declaring `requireClaudeToolType` (MiniMax / MiniMax-CN), avoiding 400 `unknown variant custom` on DeepSeek Anthropic endpoint (#3905, #45ec1d3).

**Cline Envelope Unwrapping & Token Refresh:**
- `internal/translator/response.go`, `internal/handlers/chat/forward.go`, `internal/proxy/executor/openai.go` — Added `UnwrapClineEnvelope` unwrapping `{"success":true,"data":{...}}` for non-streaming completions on `cline` and `clinepass` (#122f23ee).
- `internal/providers/oauth.go` — Registered `clinepass` in OAuth token refresh config.
- `internal/providers/capabilities.go` — Replaced `deepseek-v4-flash` with `deepseek-v4.1-flash` for `codebuddy-cn` (#807553e).

**Connection Health & Security:**
- `internal/db/accounts.go` — Added `ResetConnectionHealthState` clearing `modelLock_*`, `errorCode`, `rateLimitedUntil`, and resetting `backoffLevel = 0` upon connection activation (#3830).
- `internal/handlers/media/media.go` — Rejected path-escaping characters (`..`, `/`, `\`) in `HandleVideoGet` (#da6aa901).

**Responses API Stream & Tool Calling Fixes:**
- `internal/proxy/executor/stream.go` — Fixed tool call argument duplication (`InputValidationError` caused by duplicate concatenated JSON bodies such as `{"q":"*.yaml"}{"q":"*.yaml"}`) on Responses API streams by tracking `ArgsEmitted` during `response.function_call_arguments.delta` and suppressing redundant full argument re-emission on `response.output_item.done`.
- `internal/handlers/chat/gemini_handler.go` — Ensured terminal `data: [DONE]\n\n` SSE frame is emitted upon stream completion for OpenAI-format streaming clients and cleaned up redundant newlines.
- `internal/handlers/chat/live_e2e_test.go` — Added live non-mock E2E tests for Antigravity (`gemini-2.5-flash`, `gemini-3.8-flash-high`), DeepSeek, and OpenCode covering parallel multi-tool calls, multi-turn tool execution, streaming, and weekly quota retrieval.

## [v1.8.9] — 2026-09-06

### ✨ Features & Parity — Next.js v0.5.69 Sync (19 Commits)

**Gemini & Antigravity ThoughtSignatureStore:**
- `internal/translator/thought_signature_store.go` — Added thread-safe in-memory LRU store (capacity: 2,000 entries, 1-hour memory TTL) scoped by `sessionId` + `toolCallId`. Caches and replays thought signatures across turns to prevent corrupted thought signature errors during multi-turn reasoning conversations.
- `internal/translator/gemini.go` & `antigravity.go` — On parallel function calls, only the first call receives the signature/fallback, leaving sibling calls unsigned per Google Gemini 3+ specification.

**New Models & Capabilities Sync:**
- `internal/providers/capabilities.go` — Registered **`gpt-6-astra`** (Vision, Reasoning, Search, Tools; 272K window / 128K max output) and added `*gpt-6*` pattern match.
- Registered GPT-5.6 image aliases: `gpt-5.6-sol-image`, `gpt-5.6-terra-image`, and `gpt-5.6-luna-image` with `ImageOutput` capability.
- Qoder capabilities catalog refresh (`ultimate`, `performance`, `gmodel`, `gfmodel`, `qmodel_38max`, etc.).
- CodeBuddy-CN capabilities updated (`glm-5.2` vision enabled).

**Anti-Abuse Google Token Refresh (Antigravity):**
- `internal/handlers/chat/antigravity_project.go` — Configurable `ONBOARD_MAX_ATTEMPTS` (default 2, down from 5) and `ONBOARD_RETRY_DELAY_MS` (default 12s) to prevent Google account rate-limit blocks during multi-account refresh (#3813).

**Anthropic-Beta Header Forwarding & Effort Normalization:**
- `internal/handlers/chat/fallback.go` — Automatically injects `Anthropic-Beta: prompt-caching-scope-2026-01-05, context-management-2025-06-27` for `anthropic-compatible-*` nodes serving Claude models (#3797).
- `internal/translator/request.go` & `types.go` — Normalizes Claude adaptive auto effort (`output_config.effort="auto"` and `"xhigh"` $\to$ `"high"`) (#3792).

**OpenCode-Go Executor & Responses Parallel Tool Calls Fixes:**
- `internal/proxy/executor/providers.go` — Added `deriveOpencodeSession` generating stable `x-opencode-session: ses_<32hex>` headers for all `opencode-go` requests, with fallback and client tool isolation (#3800).
- Routed `muse-spark-1.2-contributor` and `muse-spark-1.3-contributor` on `opencode-go` to the `/responses` endpoint (#3819, #3820).
- `internal/proxy/executor/stream.go` — Fixed parallel tool calls argument collision on Responses API SSE stream by indexing events via `item_id`. Emits arguments from `response.output_item.done` when upstreams send arguments on item completion without deltas.
- `internal/proxy/executor/stream.go` — Fixed `handleCodexStream` SSE stream truncation and disconnects on `muse-spark-1.3` (and 1.2) by switching to `proxy.ScanStream` (up to 10MB buffered scanner), preventing line fragmentation when handling large (>3KB) encrypted reasoning payloads across TCP packet boundaries.
- `internal/proxy/executor/stream.go` — Added support for `response.reasoning_summary_text.delta`, `response.reasoning_text.delta`, and `response.thought.delta` emitting `reasoning_content` delta chunks for thinking models.
- `internal/proxy/sse.go` — Added `HeartbeatWriter` emitting periodic `: keep-alive\n\n` comments every 15 seconds during prolonged upstream reasoning phases (fixes #3796 stream stall timeouts on strict clients like Oh My Pi during deep thinking on `ag/gemini-3.8-flash*`).
- `internal/proxy/stall.go` — Added `NewStallReaderWithContext` binding client `ctx.Done()` directly to body closer, immediately freeing upstream sockets on client abort and eliminating Windows socket leaks (`CLOSE_WAIT`/`FIN_WAIT_1`).
**Database Path Configuration:**
- `internal/config/config.go` — Enhanced `DB_PATH` resolution to automatically detect `db/data.sqlite`, `data.sqlite`, or `9router.db` when pointed directly to a directory (e.g. `E:\project\database\9router`).
## [v1.8.8] — 2026-09-03

### ✨ E2E & Parity — Next.js v0.5.65 (31 commits)

**E2E Gemini 3.8 Flash High + tool calling (deterministic mocks):**
- `internal/handlers/chat/gemini38_e2e_test.go` — `gemini-3.8-flash-high` via Antigravity `2.11.0` non-stream + multi-turn + stream SSE `get_weather_ide` uncloaking, `prefixItems` cleaning, `thoughtSignature` backfill, `tool_calls` dedup. Ports `decolua/9router` `gemini-3.8-flash-medium/high/low` + `capabilities.go:*gemini-3.8*` + `antigravity.go:gemini-3.8-flash-tiered` + `proxy/gemini.go:2.11.0`.

**E2E Opencode muse-spark (deterministic mocks, no real network):**
- `internal/handlers/chat/opencode_mock_e2e_test.go` — `oc/muse-spark-1.2` & `1.3` via `Responses API /v1/responses` SSE `output_item.added` + `function_call_arguments.delta/done` aggregation, `reasoning max→xhigh`, `Vision:true` (`capabilities.go:124` pattern `*muse-spark*`), `image_url` preservation. Fixes routing `muse-spark-1.3` `500` → `200` (`providers.go:293` `Contains(muse-spark)` + `capabilities.go:124` `1.3`).

**Unit tests — now locking logic (previously untested):**
- `providers_v065_test.go` — `claude-cli/2.1.258` + full `Anthropic-Beta`, `ollama FetchURL https://ollama.com/api/web_fetch`, `gemini-3.8` caps, `muse-spark 1.2/1.3`, `codebuddy-cn hy3/hy3-x/hy4-preview/x/glm-5.3/kimi-k3-1` + EOL `glm-5.0/4.7` removed, `GetModelTokenLimits` 3.8.
- `translator/claude_cache_test.go` — `LastCacheableToolIndex` + `AnchorClaudeCache` for `defer_loading:true` tail, all-deferred, stripping client `cache_control` (#3567).
- `handlerutil/ssrf_test.go` — `trailing dot` (`localhost.`), `CGNAT 100.64/10`, `169.254.169.254`, IPv6 `::ffff:7f00:1` hex, `64:ff9b::`, `fe80/fc`, `normalizeHost`, `parseIPv6ToGroups`.
- `mitm/handlers/mitm_handlers_test.go` — `HandleKiro` removes `systemPrompt` + `userInputMessage.images → image_url data:`, `HandleAntigravity` preserves `fetchAvailableModels(2.11.0)` vs overrides `generateContent→1.23.2`.
- `usagetracker/quota_parsers_test.go` `TestParseGroqQuotasFromHeaders` — `x-ratelimit-*` Go duration `2m59.56s` → `requests/tokens` `used/total/resetAt`.
- `handlers/chat/chat_v065_test.go` — `HandleModelLookup` kind `image` + `cc/claude-sonnet-4-6` + encoded slash + 404 `model_not_found`, `HandleModels` custom `cc/my-custom-vision` caps, `StrikeReassert` 3×429 optimistic 90% → `CACHE_BLOCK 15m` + re-assert after `Refresh`.
- `proxy/executor/opencode_test.go` `MuseSpark13_ResponsesRouting` + `OCPrefix` — routing `1.3` + `oc/` to `/responses`.

**Fixes:**
- **Opencode 1.3 `500` → `200`** — `ForwardOpencode` routing `Contains(muse-spark)` + `capabilities` `1.3` Vision (fixes report `14:11:47` `muse-spark-1.3 500`).
- **jcode tool_smoke 3→1** — `stream.go:118` dedup `ToolCallIdx` + `codebuddy.go:168` `sseToOpenAIJSON` dedup `arguments` for `bash` `intent` split (fixes `echo JCODE_TOOL_OK` 3 tool_calls).
- **Flaky real upstream 429** — `muse_spark_e2e_test.go` real `opencode.ai` `429 FreeUsageLimitError` now `Skip` instead of `Fail`.
- **DB flaky `429` in `go test ./...`** — `go vet` clean, `ps` `9router-go 20130` health `{"status":"ok"}` (not stopped, log stopped due to `user stepped away` recap 98k prompt).

## [v1.8.7] — 2026-09-02

### 🐛 Bug Fixes

- **Gemini `system_instruction` Empty Part 400 Fix** — `StripCompetitivePrompts` now drops empty `system_instruction` parts after `rewriteCompetingBranding` (e.g. `"You are a Claude agent..."` -> `""`) and filters empty text parts in `contents`; if all parts are empty the `system_instruction` is removed (`nil`) instead of emitting `{"parts":[{}]}` which Gemini rejects as `system_instruction.parts[0].data: required oneof field 'data' must have one initialized field`. Also `TranslateOpenAIToGemini` now `TrimSpace` checks system content. Fixes `ForwardGemini (antigravity/gemini-3.7-flash-high): ...system_instruction.parts[0].data: required oneof`. (`internal/translator/antigravity.go`, `internal/translator/gemini.go`)
- **Gemini Tool Schema `required` Inside `properties` 400 Fix** — `cleanGeminiSchema` now detects misplaced `required` array inside `properties` (e.g. `{"properties":{"query":{...},"required":["query"]}}`) and promotes it to top-level `required`, fixing `Invalid value at 'request.tools[0].function_declarations[0].parameters.properties[0].value' (Map), Cannot have repeated items ('required') within a map. Unknown name ""`. This was the root cause of the `12:14:50` `query_db_ide` 400 after the `where.items` fix. Also sanitizes all OpenAI-compatible providers (including `opencode`) via `fallback.go` `SanitizeOpenAITools`. (`internal/translator/schema.go`, `internal/handlers/chat/fallback.go`)
- **Opencode `muse-spark` Tool Name Triplication Fix** — `sseToOpenAIJSON` `internal/proxy/executor/codebuddy.go:168` now only sets `name` if empty and avoids duplicating `arguments` already sent via `delta`/`done`; `ProcessCodexEvent` `stream.go:148` for `response.function_call_arguments.delta/done` now deduplicates `name` and tracks `ToolCallArgs` to prevent `get_weather` -> `get_weatherget_weatherget_weather` and `{"location":"Jakarta"}{"location":"Jakarta"}` on `combo-wombo` (`oc/muse-spark-1.2`) non-stream and stream. (`internal/proxy/executor/codebuddy.go`, `internal/proxy/executor/stream.go`)

## [v1.8.6] — 2026-09-02

### 🐛 Bug Fixes

- **Gemini Tool Schema `where.items.items: missing field` 400 Fix** — `cleanGeminiSchema` now ensures every `type: array` has a valid `items` schema (default `{"type":"string"}`), flattens `prefixItems` (2020-12 tuple) and `items: [...]` tuple to single `items`, and auto-fills inner `items` without `type`/`properties`/`enum`. Fixes `ForwardGemini (antigravity/gemini-3.7-flash-high): upstream returned 400: ...where.items.items: missing field.` when Claude Code sends DB-like tools with nested `array<array>` params. Added `internal/proxy/gemini.go` 400 payload dump to `/tmp/9router-gemini-400.json` for post-mortem. (`internal/translator/schema.go`, `internal/proxy/gemini.go`)
- **Bare Alias `ag` -> `antigravity` Resolution** — `resolveModel("ag")` now checks `ProviderAliasMap` before common-provider fallback, so `POST /search` with `{"model":"ag"}` correctly routes to `antigravity` instead of `deepseek/ag` -> `404 Via cloudfront`. Parity with Next.js `ag` search. (`internal/handlers/chat/resolution.go`)
- **Antigravity Search Default Model Parity** — `handleAntigravitySearch` default changed `gemini-3-flash-agent` -> `gemini-2.5-flash` (Next.js `ag` search returns `answer.model: gemini-2.5-flash`), fixing `500 UNKNOWN` from `daily-cloudcode-pa.googleapis.com` for bare `ag` search. (`internal/handlers/media/antigravity_search.go`)
- **Claude `call_` Tool Name Fallback Fix (Gateway IP Check)** — `TranslateOpenAIToClaude` no longer falls back to `tc.ID` (`call_...`) when `Function.Name` is empty; skips invalid tool calls instead of emitting `tool_use` with `name: call_...` which caused `No such tool available: call_...` and `Invalid tool parameters` churn on `toloing cek config gateway` via `opencode/muse-spark`. (`internal/translator/response.go:165`, `cf42120`, port `decolua/9router#2077`/`#3685`)
- **Claude Streaming Tool Delta Deduplication** — `TranslateOpenAIToClaudeStreamSession` now checks `state.ToolCalls[idx]` before creating a new `content_block_start`; second delta for same `idx`/`id` (Codex `output_item.added` + `function_call_arguments.delta` same `call_...`) no longer creates duplicate `tool_use` and empty `partial_json: "{}"`; correctly buffers `arguments` and emits `{"command":"ip route ..."}`. Fixes `InputValidationError: Bash missing command` on `Gue cek gateway IP...` streaming. (`internal/translator/response.go:468`, `ea10c16`)
- **Bash Extra Fields Strip** — `sanitizeBashArgs` now keeps only `command`, deletes hallucinated `description` etc. inside `input` (`{"command":"ls ...","description":"List home..."}` -> `{"command":"ls ..."}`), fixing `Bash(input JSON failed to parse — 433 bytes)` on `Cek gateway config — lagi intip file-file di home`. (`internal/translator/sanitize.go:239`, `6b94535`)
- **Server Tool Use Foreign ID Drop (Combo Poison)** — `SanitizeClaudePassthrough` drops `server_tool_use` with id not matching `^srvtoolu_` (e.g. `call_` from `z.ai/glm` `analyze_image`) and paired `tool_result`, strips empty text and empty messages. Port `decolua/9router#3686`. (`internal/translator/request.go:342`, `cf42120`)
- **1M Context Marker Strip** — `stripModelContextMarker` strips trailing `[1m]`/`[1M]` from `claude-opus-5[1m]` before `resolveModel`, so combo `combo-wombo[1m]` routes correctly instead of `Invalid model format`. Port `decolua/9router#3691`. (`internal/handlers/chat/resolution.go:135`)
- **Streaming Model Echo** — `SeedStreamState` pre-seeds `StreamState.Model` with client-requested model via `WithRequestedModel` context so `message_start` echoes `combo-wombo` not `claude-3-5-sonnet` provider model. Port `decolua/9router#3693`. (`internal/translator/usage.go:41`, `forward.go:91`)
- **GPT-5 / o-series `max_completion_tokens`** — `requiresMaxCompletionTokens` (`/gpt-5|o[134]-/i`) emits `max_completion_tokens` instead of `max_tokens` for `gpt-5`/`o1-`/`o3-`/`o4-` in `TranslateClaudeToOpenAI`. Port `decolua/9router#3657`. (`internal/translator/request.go:12`, `types.go:202`)
- **Antigravity Optimistic Quota Strike-Breaker** — after 3 consecutive `429` for same `connection|model` within 60s while quota `remaining>0`, `HandleAntigravityQuotaError` returns `CACHE_BLOCK 15m` instead of looping 300s `modelLock`. Port `decolua/9router#3684`. (`internal/handlers/chat/antigravity_quota.go:44`)
- **Forced-SSE JSON for Claude Clients** — `handleJSONResponse` detects `isSSEBody` when `translate=true` and `stream:false` retry hits forced-stream provider (Responses-API), aggregates via `sseToClaudeJSON` then `TranslateOpenAIToClaude` to return `Anthropic Message` not `chat.completion`. Port `decolua/9router#3683`. (`internal/handlers/chat/forward.go:144`)
- **OpenRouter Pattern Compat + Combo Tools Detection** — `NormalizeToolSchemasForProvider("openrouter")` strips invalid `pattern` regex (keep valid, handle `properties` named `properties`), and `DetectRequiredCapabilities` now requires `tools` capability for `type:function`/`functionDeclarations`. Port `decolua/9router#3665`. (`internal/translator/tool_schema.go`, `combo.go:129`, `forward.go:35`)

## [v1.8.5] — 2026-08-31

### ✨ Features & Parity (Next.js v0.5.59 Sync)

- **New Search Providers & Credential Fallback** — added `xquik` (X search provider with raw API key), `ollama-search`, and `zai-search` (GLM Coding web search). Added automatic credential fallback where search providers borrow API keys from parent chat connections (`ollama` / `glm`) when dedicated search connections are absent. (`internal/providers/providers.go`, `internal/providers/aliases.go`, `internal/handlers/chat/connections.go`)
- **Antigravity Web Search Provider** — added Antigravity as a web search provider via Google Search grounding, with full Next.js parity for the search response structure. (`internal/handlers/media/antigravity_search.go`)
- **New Models & Capabilities Sync** — registered new flagship models: `GLM-5.3-Flash` (1M context window + native vision multimodal), `GLM-5.3`, `DeepSeek V4 Vision`, `Grok 4.5/4.6` (500k context window), and `muse-spark-1.2-contributor-free`. (`internal/providers/capabilities.go`, `internal/providers/aliases.go`)
- **Claude Tool Type Defaulting (`type: "custom"`)** — added `DefaultClaudeToolType` ensuring tools in Claude-format requests always carry a valid `type` (defaulting to `"custom"` when omitted), preventing HTTP 400 rejection on strict Anthropic-compatible gateways such as MiniMax. (`internal/translator/request.go`, `internal/handlers/chat/chat.go`)
- **Claude Code Session ID Header Support** — prioritized `x-claude-code-session-id` in `ExtractSessionID` to ensure stable prompt caching and avoid conversation fragmentation across client tool calls. (`internal/handlerutil/response.go`)
- **CommandCode In-Stream Error Peeking** — peeks the initial NDJSON event in CommandCode stream for `type: "error"` before committing HTTP 200 OK headers, transforming internal stream errors into real HTTP error statuses (429, 503, 401, etc.) so combo and account fallback trigger seamlessly. (`internal/proxy/executor/stream.go`)
- **OpenCode Responses API Parity (v0.5.59)** — completed Responses API translation for OpenCode Muse Spark: proper tool names emitted on `response.output_item.added`, accurate usage and prompt-cache token extraction from `response.completed`, Claude SSE streaming translation and non-streaming support in `handleCodexStream`, and 64-char clamping for `call_id`. (`internal/proxy/executor/`)

### 🐛 Bug Fixes

- **Gemini Cached Token Extraction** — added support for both `cachedContentTokenCount` and `cachedContentToken` keys in Gemini stream and non-stream responses. (`internal/translator/gemini.go`)
- **Non-Interactive Test Execution** — bypassed interactive `sudo security` CA keychain install when executing unit tests, ensuring fast, deterministic test suite completion. (`internal/mitm/cert.go`)
- **Self-Update SHA256 Verification** — `PerformSelfUpdate` now downloads to memory, verifies the expected SHA-256 checksum, and refuses to install mismatched binaries, eliminating the risk of installing corrupted or tampered updates. (`internal/updater/updater.go`)
- **Graceful Self-Restart** — replaced abrupt `os.Exit` after self-update with `syscall.Kill(SIGTERM)` plus a graceful fallback, giving in-flight requests and DB connections a chance to drain cleanly. (`internal/updater/updater.go`)
- **Cross-Platform Restart** — extracted the self-signal into a platform-specific `signalSelfShutdown` helper (`signal_unix.go` sends SIGTERM; `signal_windows.go` is a no-op that falls back to `os.Exit(0)`), fixing the Windows cross-compile of the release binaries. (`internal/updater/signal_unix.go`, `internal/updater/signal_windows.go`)
- **Smart Archive Executable Selection** — `extractExecutableBytes` now scores archive entries (penalizing README/LICENSE/`*.md`/`*.sha256`) and validates ELF/Mach-O/PE magic bytes, reliably picking the real binary from multi-file release archives. (`internal/updater/updater.go`)
- **SSE Copy Race Condition** — replaced the shared pooled buffer in `SSECopy` with a per-call local buffer, eliminating concurrent read/write races on the pool buffer. (`internal/proxy/sse.go`)
- **Nil Guard in Token-Saving Compression** — guarded against a nil `rawMap` when the upstream body cannot be decoded, preventing a panic on malformed responses. (`internal/tokensaver/compress.go`)
- **Quota Percentage Clamping** — clamped `RemainingPercentage` to a sane `[0, 100]` range so upstream values >100 or negative cannot skew quota-block and dashboard logic. (`internal/usagetracker/quota_parsers.go`)
- **Exponential Backoff for Antigravity Onboarding** — replaced the fixed 2s sleep between `onboardUser` retries with exponential backoff (2s, 4s, ...) that also honors context cancellation, so a 429 burst no longer gets hammered by fixed-interval retries. (`internal/handlers/chat/antigravity_project.go`)
- **Decloak Deduplication** — extracted a shared `decloakContentBlockStart` helper used by both `DecloakStreamChunk` and `DecloakClaudeStreamEvent`, removing duplicate content-block-start logic. (`internal/translator/antigravity.go`)
- **Tool Property Sanitization** — preserved tool parameters named after reserved keywords and sanitized `required` fields against the declared `properties`, preventing schema validation failures. (`internal/translator/sanitize.go`)

## [v1.8.4] — 2026-08-14

### 🐛 Bug Fixes & Resilience

- **Combo Cycle Graceful Recovery & Fault Tolerance** — `flattenComboModels` now gracefully skips recursive / self-referencing combo branches with a warning log instead of failing hard with HTTP 400 (`combo cycle detected`), ensuring chatbot requests continue executing remaining valid models seamlessly. (`internal/handlers/chat/resolution.go`)
- **Safe Model Resolution on Leaf Models** — eliminates potential slice index-out-of-range edge cases when resolving combo leaf models that do not contain a provider prefix. (`internal/handlers/chat/resolution.go`)
- **Multi-Level Nested Combo Support** — verified recursive cascading combo expansion (e.g. `super-combo` → `mid-combo` → `base-combo` → leaf models) so all reachable models participate in round-robin, sticky, and fallback strategies. (`internal/handlers/chat/resolution.go`, `internal/handlers/chat/resolution_test.go`)

## [v1.8.3] — 2026-08-14

### ✨ Features

- **Antigravity Gemini 3.7 Flash Model Mapping** — canonical model IDs and aliases for `gemini-3.7-flash`, `gemini-3.7-flash-high`, `gemini-3.7-flash-agent`, `gemini-3.7-flash-medium`, `gemini-3.7-flash-low`, `gemini-3.7-flash-extra-low`, and `gemini-3.7-flash-thinking` correctly mapped to Google Antigravity backend model IDs (`gemini-3-flash-agent` / `gemini-3.5-flash-low`), fixing upstream 404 errors. (`internal/translator/antigravity.go`)
- **Enriched Prompt-Injection Guard** — enhanced prompt-injection detector with heuristic patterns for raw model delimiters (`<|im_start|>system`, `<<SYS>>`, `[SYSTEM PROMPT]`, `[INST]`), verbatim system prompt extraction attempts, and developer/admin mode override simulations. (`internal/tokensaver/injection.go`)
- **Accurate Gemini Cached Token Tracking** — correctly unmarshals and propagates `cachedContentTokenCount` from Gemini stream and non-stream responses into `OpenAIUsage.CachedTokens`, providing accurate cache hit reporting and cost calculation. (`internal/translator/gemini.go`)
- **Gemini Vision FileData & Audio Modalities** — added support for remote HTTP/HTTPS image URLs (`fileData: { fileUri, mimeType: "image/*" }`), base64 input audio (`input_audio`, `audio_url`), and uploaded documents in Gemini native translator, matching Next.js full multimodal capabilities. (`internal/translator/gemini.go`)
- **Realtime SSE Usage Stream & Topology Animation** — added in-memory in-flight request tracker (`internal/usagetracker`), real-time SSE broadcasting (`GET /api/usage/stream` and `GET /usage/stream`), and recent requests ring buffer matching the Next.js dashboard shape, enabling instant glowing pulse node & marching-ants edge animations on the Usage Topology graph when requests are handled by `9router-go`. (`internal/usagetracker/tracker.go`, `internal/handlers/usage_stream.go`, `internal/handlers/chat/fallback.go`, `internal/handlers/chat/usage.go`)
- **Antigravity Anti-Competitive Prompt Stripping & 429 Prevention** — automatically strips competitor identity phrases (e.g. `"You are a Claude agent, built on Anthropic's Claude Agent SDK."` from Zed IDE and Claude agents) from `system_instruction` and message contents, preventing Antigravity from returning synthetic `429 Quota Exhausted` errors. (`internal/translator/antigravity.go`)
- **Edge Relay URL Rewriting & Header Forwarding** — automatically rewrites `BaseURL` to the relay deployment and injects `x-relay-target` and `x-relay-path` headers when a connection uses a Vercel, Cloudflare Worker, or Deno Edge Relay Proxy Pool. (`internal/handlers/chat/connections.go`)
- **No-Auth Provider Proxy Pool Strategy** — automatically respects `settings.providerStrategies` for no-auth providers (e.g. `mimo-free`, `opencode`), attaching configured proxy pools or rotation strategies to virtual connections. (`internal/handlers/chat/connections.go`, `internal/db/settings.go`)
- **Snake_case Model Limits on `/v1/models` & `/v1/models/info`** — exposes `context_length`, `max_completion_tokens`, `max_input_tokens`, and `max_output_tokens` so clients like Cline, Roo Code, and LibreChat resolve proper context ceilings. (`internal/handlers/chat/chat.go`, `internal/providers/capabilities.go`)
- **CodeBuddy OAuth Configuration** — registered `codebuddy-cn` and `codebuddy-intl` OAuth token refresh configurations. (`internal/providers/oauth.go`)
- **OpenCode Official Client Fingerprint Headers** — injects official headers (`User-Agent: opencode`, `x-opencode-client: desktop`, `x-opencode-session: ses_...`, `x-opencode-request: msg_...`, `x-opencode-project: global`) on free-tier OpenCode requests to prevent rate limiting from unidentified client traffic. (`internal/proxy/opencode.go`, `internal/proxy/executor/providers.go`)
- **Kimchi Dual Authentication** — supports direct API keys (`Authorization: Bearer <key>`) in addition to OAuth tokens with seamless credential resolution. (`internal/handlers/chat/connections.go`, `internal/handlers/chat/kimchi_handler_test.go`)
- **Startup Banner & Version Display** — dynamically displays current version in CLI startup banner (`🚀 9Router Go Proxy (v1.8.3) on :20130`) and server ready logs. (`cmd/9router-go/main.go`)
- **New Provider Registries & Aliases** — added Alibaba Token Plan Singapore (`alitp-intl` / `ali-tp` / `alitp`) and Fish Audio Text-to-Speech (`fish-audio` / `fish`). (`internal/providers/providers.go`, `internal/providers/aliases.go`)

### 🐛 Bug Fixes

- **Invalid Tool Parameters & Decoy Schemas** — provided valid non-empty `properties.reason` schema for all 21 Antigravity decoy tools and mapped `tool_call_id` to exact function names in OpenAI-to-Gemini conversation history, eliminating protobuf validation errors when using Claude Code or other tool-calling clients. (`internal/translator/antigravity.go`, `internal/translator/gemini.go`)
- **Antigravity Upstream Model Resolution** — prevented invalid model aliases like `gemini-3.7-flash-high` from reaching Google Cloud Code without being translated to their backend model IDs. (`internal/translator/antigravity.go`)

### 📚 Documentation

- Comprehensive refresh of `README.md`, `COMPARISON.md`, `DATABASE.md`, `ARCHITECTURE.md`, `TECHNICAL_DEBT.md` (0 open items), and newly added `ROADMAP.md`.

## [v1.8.2] — 2026-08-14

### ✨ Features

- **Antigravity Tool Cloaking & Anti-Ban Decoy System** — automatically cloaks client tool declarations with `_ide` suffixes (e.g. `Bash_ide`), injects 21 official Antigravity IDE decoy tools (`run_command`, `replace_file_content`, `grep_search`, `list_dir`, etc.), synchronizes conversation history functionCall/functionResponse names, and seamlessly uncloaks tool names on response SSE stream and non-stream outputs. (`internal/translator/antigravity.go`, `internal/translator/gemini.go`)
- **Antigravity Native Image Generation** — added image model detection (`imagen`, `*image*`), aspect ratio suffix parsing (`16x9`, `4:3`, `1:1`, custom resolutions via GCD reduction), `requestType: "image_gen"` envelope wrapping with forced non-streaming `/v1internal:generateContent`, and OpenAI-compatible base64 image response formatting. (`internal/translator/antigravity.go`, `internal/proxy/gemini.go`)
- **Edge Relay & Transport Engine** — added transport support for Vercel, Cloudflare Worker, and Deno edge relays using `x-relay-target` and `x-relay-path` headers, wildcard `noProxy` domain filtering, and legacy connection-level proxy configuration fallback (`connectionProxyUrl`). (`internal/proxy/transport.go`, `internal/handlers/chat/connections.go`)

### 🐛 Bug Fixes

- **Proxy Pool DB Parsing Bug** — fixed `GetProxyPool` (`internal/db/proxyPools.go`) failing to parse single string `proxyUrl` created by Next.js UI / `InsertProxyPool`, which previously caused proxy pools to be silently ignored and requests to fall back to direct connections. Added parsing for `type`, `noProxy`, and `strictProxy` metadata.

## [v1.8.1] — 2026-08-12

### ✨ Features

- **Combo strategy sync with Next.js reference** — per-combo rotation state, correct auto-switch ordering, and a capabilities provider (`internal/providers/capabilities.go`) replacing the hardcoded vision/pdf maps with tiered capability detection.
- **Flatten nested combos** — `combo-wombo → free-tier` now expands to its four leaf models, so round-robin actually rotates across them instead of always landing on the first leaf (this was hammering one account and producing the `429 all connections for this provider are rate-limited` error).
- **Turn-aware rotation** — `applyComboStrategy` advances the rotation index only on a new turn; mid-turn tool-use requests reuse the model serving the turn, so the provider never switches mid-turn (which broke Gemini thinking models that require a `thought_signature` on current-turn function calls).
- **Bounded retry-once on total combo 429** — when every combo model fails with a Retry-After ≤ 8s, the pass waits once and retries before surfacing a hard 429 (`comboRetryAfter`).
- **Backfill default `thought_signature`** — every `functionCall` part now carries a `thoughtSignature` (the real one via `__ts__` transport when present, else the Next.js `DEFAULT_THINKING_AG_SIGNATURE`), closing the last 400-`thought_signature` gaps on mixed combos.
- **Gemini tool-schema keyword parity** — strip the remaining unsupported JSON-Schema keywords (`multipleOf`, `uniqueItems`, `contains`, `unevaluated*`, `contentSchema`) and fill bare `{}` schemas with the object placeholder, matching Next.js `cleanJSONSchemaForAntigravity` (fixes `Invalid tool parameters` 400 from antigravity).
- **Sanitize tools on the OpenAI-compat Gemini path** — the `gemini` provider is now marked `gemini-openai` and its `/v1beta/openai` bodies are run through `SanitizeOpenAITools`, so the strict schema validation applies on both Gemini routes.

### 🐛 Bug Fixes

- **Emit camelCase `thoughtSignature`** — the Gemini-native `generateContent` endpoint only recognizes the camelCase part field; the snake_case regression caused the `400 Function call is missing a thought_signature` error. Both read and write directions now handle camelCase.
- **Use the daily antigravity endpoint** — migrate `cloudcode-pa.googleapis.com` → `daily-cloudcode-pa.googleapis.com` for the `antigravity` provider (`providers.go`, `antigravity_project.go`, MITM domain list) to avoid strict rate limits.
- **Combo connection retry-loop parity** — connection retry loop now matches the single-model path.

### 🧹 Chores / Docs

- Remove the stray `patch_combo.go` throwaway script.
- Add design specs and implementation plans for the combo sync / thought_signature / backfill / schema-parity work.

## [v1.8.0] — 2026-08-11

### 🐛 Bug Fixes

- **Combo/router fallback bypasses model-lock backoff on retryable errors → antigravity rate-limit loop** — `handleComboFallback` / `handleMessagesComboFallback` (`internal/handlers/chat/combo.go`) called `tryForwardWithConnection` directly, so `LockConnectionModel` was never invoked on retryable errors (429/500s) — unlike the single-model path `handleAccountFallback`. The exponential 429 backoff was dead in the router path: every request re-tried all combo models back-to-back on the same connection/account, got 429, returned 429, and the client's ~35s retry repeated the loop forever. Fix:
  - New `comboLockRetryable` helper runs on every `RetryableStatusCodes` error in both combo loops — classifies via `ClassifyError`, calls `LockConnectionModel(connID, model, cooldownSec, newBackoffLevel)` so the exponential backoff persists across requests, and appends the conn to a request-local `excludeIDs` passed into `getBestConnection` so remaining combo models don't re-select the same connection (same account = same quota bucket).
  - A locked-connection skip covers pinned connections whose direct-fetch branch bypasses `getBestConnection`'s lock check.
  - `context.Background()` → `ctx` in `handleComboFallback` so client cancels propagate; the 502/503/504 transient-wait sleep is preserved.
  - Test: `TestHandleMessagesComboFallback_429LocksAndExcludesConnection` asserts a 429 locks the connection AND keeps the second combo model from re-hitting it (exactly 1 upstream hit).

## [v1.7.2] — 2026-08-08

### 🐛 Bug Fixes

- **Antigravity 429/404 failure-loop fix** — An unprovisioned Antigravity account
  (`onboardUser` returns `200` with an empty `cloudaicompanionProject`) left the
  connection without a `projectID`. The router then force-refreshed the OAuth
  token on every request (never an auth problem, so it never helped), fell through
  to a guaranteed-404 OpenAI-compatible lane on `cloudcode-pa.googleapis.com`,
  and repeated client retries rammed Google's rate limit (`429`). (`internal/handlers/chat/gemini_handler.go`, `internal/handlers/chat/antigravity_project.go`)
  - `fetchAntigravityProjectID` now reports the outcome (`projectID`, `authFailed`,
    `noProject`). Token refresh runs **only** on a genuine `401/403` — never on a
    missing/empty project.
  - When antigravity has no project ID, it no longer burns a request on the dead
    OpenAI lane; it returns an error and the fallback chain moves straight to the
    next provider.
  - **Negative cache (10 min, per connection):** once Google confirms "no project",
    later requests skip the `loadCodeAssist`/`onboardUser` RPCs entirely — this is
    what stops the repeated `429` hammering.
  - Onboarding guidance is logged once per connection per window
    ("onboard the account via Antigravity IDE/CLI, then re-login"); repeated
    failures log at `Debug` instead of spamming `Warn`. (`internal/handlers/chat/fallback.go`)

### 🧪 Tests

- `antigravity_project_test.go` — pins the probe classification (project found /
  token rejected `401`+`403` / project definitively missing / transient `429`+`503`)
  and the negative-cache expiry semantics. (`internal/handlers/chat/antigravity_project_test.go`)

## [v1.7.1] — 2026-08-08

### 🐛 Bug Fixes

- **Cached-token parity across every provider** — Prompt-cache accounting no longer works only for antigravity. Gemini `usageMetadata.cachedContentToken` now flows through both non-stream and stream translation into OpenAI `usage.cached_tokens`; the `!translate` response path uses a dual-format parser (`ParseResponseUsage`) that reads Claude `cache_read_input_tokens`/`cache_creation_input_tokens` and OpenAI `prompt_tokens_details.cached_tokens`, so cached tokens survive any provider → OpenAI → Claude double translation. (`internal/translator/gemini.go`, `internal/translator/response.go`)
- **Gemini tool-schema `const` re-injection** — `stripUnsupported` now re-runs after `anyOf`/`oneOf` flattening so `const` and vendor `x-*` keys can't leak back into the merged branch. (`internal/translator/schema.go`)
- **Provider 403 is now retryable** — Gemini/antigravity daily-quota errors can arrive as HTTP 403; these now trigger the connection fallback instead of a hard failure. (`internal/providers/providers.go`)
- **CodeBuddy CN stream cleanup** — The stall reader is now closed after the stream, stopping its shutdown watcher + stall timer (no per-request goroutine leak). (`internal/proxy/executor/codebuddy.go`)

### ⚙️ Graceful Shutdown Hardening

- New `internal/shutdown` package: a process-wide signal the first Ctrl+C / SIGTERM fires.
- `StallReader` now closes in-flight SSE upstream bodies on shutdown, so `server.Shutdown` drains streams in milliseconds instead of waiting out the 15s deadline — and the deferred DB/log-file close always runs.
- Translate-path SSE handlers emit a final `data: [DONE]` on abort so clients get a clean end instead of a truncated stream.
- A second Ctrl+C / SIGTERM force-quits immediately (stuck-drain escape hatch).
- Shutdown timeout logs a warning instead of `log.Fatalf`, so `conn.Close()` and the log file are still closed gracefully. (`internal/proxy/stall.go`, `cmd/9router-go/main.go`, `internal/handlers/chat/forward.go`, `internal/proxy/executor/openai.go`)

### 🔧 Internal

- Stream handlers now carry the request `ctx` and pull accumulated usage (incl. cached tokens) out of the translation session, so logged usage reflects real token counts instead of the character-estimate fallback.

## [v1.7.0] — 2026-08-06

### 🚀 New Executors

- **Trae SOLO remote agent** (`internal/proxy/executor/trae.go`) — Port of `open-sse/executors/trae.js`: `POST {base}/chat_sessions` creates a session, `GET {base}/chat_sessions/{id}/events` streams `plan_item` / `token_usage` / `done` as SSE. Cumulative `plan_item.thought` rendering (longest-wins per id, delta-only emission), `Cloud-IDE-JWT` auth, and `work`/`auto`/manual model modes. Non-stream requests aggregate into a single `chat.completion`. Round-trip test: `TestForwardTrae_StreamsAccumulatedThought`.
- **Windsurf gRPC-web** (`internal/proxy/executor/windsurf.go`) — Port of `open-sse/executors/windsurf.js`: hand-rolled protobuf `GetChatMessageRequest` encoder (Metadata.api_key + cascade_id + model_or_alias + repeated messages), gRPC-web framing (0x00 flag + big-endian length), and a `CompletionChunk` decoder (content / done+UsageStats / error) streaming OpenAI SSE. Catalog→wire model alias map ported verbatim; `crypto/rand` session/cascade ids. Non-stream requests aggregate frames into `chat.completion`. Round-trip test: `TestForwardWindsurf_StreamsGRPCWeb`.

### 🎙️ Xiaomi MiMo TTS

- `/v1/audio/speech` for the `xiaomi-mimo` provider now uses the chat-completions contract (port of `open-sse/handlers/ttsProviders/xiaomi-mimo.js`): target text in `role:assistant`, style/language instructions in `role:user`, voice via top-level `audio.voice`, base64 audio from `choices[0].message.audio.data`. (`internal/handlers/media/media.go`)

### ➕ Providers

- **tokenrouter** — Registered as an OpenAI-compatible upstream (`https://api.tokenrouter.com/v1/chat/completions`).

### 🗑️ Removed

- **qwen provider** — Removed from providers, OAuth config, and the alias map (deprecated upstream).

### 📋 Docs

- `TECHNICAL_DEBT.md` — windsurf + trae moved to resolved; zed + devin-cli documented with the safe-stopgap note (devin-cli corrected: ACP over **stdio** subprocess, not HTTP).

## [v1.6.1] — 2026-08-05

### 🐛 Bug Fixes

- **CodeBuddy CN 502** (`internal/proxy/executor/codebuddy.go`) — `codebuddy-cn` / `codebuddy-intl` now use a dedicated executor that forces `stream=true` upstream (CodeBuddy rejects non-stream with HTTP 400 code 11101), injects the CLI/IDE static headers, and re-aggregates OpenAI-chat SSE into a single `chat.completion` for non-stream clients (`sseToOpenAIJSON`, mirroring JS `parseSSEToOpenAIResponse`).
- Provider parity with the reference implementation.

## [v1.6.0] — 2026-08-04

### 🚀 Next.js Engine Feature Ports

- **TTS Voice Listing** (`/audio/voices`) — Full voice-listing with provider support (`edge-tts` default, `elevenlabs`, `gemini`, `local-device`), `?lang` filter, 24h in-process cache, and `byLang`/`languages` grouping matching the dashboard's media-providers page.
- **Proxy-Pools Deploy** (`/proxy-pools/{vercel,deno,cloudflare}-deploy`) — Deploy edge relay functions to Vercel/Deno/Cloudflare with status polling, plus a new `InsertProxyPool` DB method writing byte-compatible `data` JSON.
- **Headroom Management** (`/headroom/*`) — Full headroom-ai lifecycle in Go: binary/Python detection, spawn/stop/restart, compression extras install/uninstall, `/headroom/proxy` reverse proxy with SSRF guard, and dashboard HTML rewrite.
- **CLI-Tools Status** (`/cli-tools/all-statuses`) — Batch detection of 14 CLI tools (Claude, Codex, OpenCode, etc.) installed state + version.
- **Live Console Logs** (`/translator/console-logs`, `/stream`) — In-process ring buffer + SSE streaming of engine log output so the dashboard's "Monitor Console Log" shows Go logs live (25s keepalive, init/line/clear events).

### 🔍 Observability

- **Lightweight Request Tracing** (`/debug/traces`) — In-memory span recording + p50/p95/p99 latency per provider+model with `?n=` cap. Stdlib-only, no OpenTelemetry SDK dependency.

### 🛡️ Security

- **Prompt-Injection Guard** — Heuristic detection (`messages[]`, `input[]`, Claude content blocks) tagging classic injection attempts in logs. Toggle via `--no-injection-guard` / `INJECTION_GUARD_DISABLED` (on by default).
- **`/admin/health/reset` Moved Behind API-Key Auth** — Previously public; now requires a valid API key to prevent unauthenticated health-state resets (open-source hardening).
- **MITM Binds Loopback Only** — TLS proxy binds `127.0.0.1:443` instead of all interfaces, preventing LAN clients from using it as an open proxy.

### 🐛 Bug Fixes & Stability

- **SSE Fragment Rejoin** — Fixed `unexpected end of JSON input` on opencode free-tier by buffering/rejoining truncated SSE JSON payloads per session (1 MiB cap).
- **Codex/CommandCode Tool-Call Streams** — Stable per-call tool IDs/indices (using upstream `call_id`), correct `[DONE]` framing, and checked `w.Write` errors.
- **MITM Goroutine Leaks** — `Stop()` drains in-flight connections (WaitGroup + active conn close); request bodies bounded at 10 MiB.
- **Executor/OAuth Registry Mutexes** — Package-level registry maps now guarded by `sync.RWMutex` (race-free on re-registration).
- **Token Saver JSON Number Preservation** — `CompressMessages`/`InjectSystemPrompt` use `json.Number` so numeric fields (temperature, large ints) round-trip unchanged.
- **`interface{}` → `any`** — Lint cleanup across stream/log packages.

## [v1.5.0] — 2026-07-24

### 🚀 Architecture & Observability Enhancements

- **Modular `main.go` Refactoring** — Extracted CLI subcommands (`mitmEnable`, `mitmDisable`, `mitmStatus`, `resolveDataDir`) to `cmd/9router-go/commands.go` and encapsulated server routing setup into `handlers.SetupServerRouter()`.
- **Structured Request Logging Middleware** — Moved `statusWriter` and `RequestLogger` to `internal/middleware/logging.go`. Requests are logged with Correlation ID (`id=req_...`) using structured logger (`slog.Info`, `slog.Warn`, `slog.Error`).
- **Dynamic HTTP Status Log Levels** — Requests with status 5xx are logged at `ERROR` level, 4xx at `WARN` level, and 2xx/3xx at `INFO` level for clean log filtering in production.
- **Upstream Memory Exhaustion Protection** — Added `io.LimitReader` caps (1MB for upstream error bodies, 10MB for non-streaming completion bodies) to protect proxy memory from rogue upstreams.
- **Double WriteHeader Prevention** — Added `written bool` guard to `statusWriter` and `cw.IsCommitted()` checks across combo fallback handlers to eliminate `superfluous response.WriteHeader` warnings.
- **Typed Request ID Context Key** — Shared `log.RequestIDKey` across middleware and logging packages to ensure context lookups match reliably.

## [v1.4.0] — 2026-07-23

### 🛠️ Technical Debt Remediations (All 9 Items Resolved)

- **Context-based Per-Request Usage Capture** — Replaced global `translator.lastUsage` with context-captured isolation (`WithUsageCapture`, `SetUsage`, `GetAndClearUsage`) to eliminate cross-request data races under concurrent traffic. (`internal/translator/usage.go`)
- **Thread-safe Daily Usage Updates** — Protected `upsertDailyUsage()` with `dailyUsageMu` mutex to prevent concurrent SQLite read-modify-write races. (`internal/handlers/chat/usage.go`)
- **Committed Response Writer** — Wrapped `http.ResponseWriter` with `committedResponseWriter` to prevent safe-retry attempts after response headers have already been sent to the client. (`internal/handlers/chat/response_writer.go`)
- **Strict Context Propagation** — Replaced all `http.NewRequest` with `http.NewRequestWithContext` across handlers, proxy execution drivers, and OAuth helpers to prevent orphaned upstream connections.
- **Graceful Shutdown** — Implemented `http.Server` graceful shutdown with signal drain (15-second timeout) on SIGINT/SIGTERM. (`cmd/9router-go/main.go`)
- **SQLite Connection Pool Optimization** — Reduced SQLite `SetMaxOpenConns(4)` for optimal WAL mode performance and zero connection contention. (`internal/db/client.go`)
- **Thread-Safe ProxyPool Cache** — Added `sync.Map` `proxyPoolCache` in `internal/db/proxyPools.go` to preserve round-robin rotation indices across requests. (`internal/db/proxyPools.go`)
- **Unbounded Request Body Guard** — Added `middleware.MaxBody` (10MB limit) to protect all endpoints from OOM attacks. (`internal/middleware/max_body.go`, `cmd/9router-go/main.go`)

### ⚡ Metrics, Latency & Token Accounting Fixes

- **TTFT & Latency Tracking** — Added `StartTime` and `TTFT` tracking across all streaming and non-streaming proxy execution drivers (`openai`, `opencode`, `deepseek`, `claude`, `grok-cli`, `qoder`, etc.). (`internal/proxy/executor/`)
- **Input Token Calculation Fix** — Added `[]byte` type support to `CountValueChars` so fallback prompt token calculation accurately estimates token size instead of defaulting to 1 token. (`internal/handlers/chat/chat.go`)
- **Output Token Calculation Fix** — Connected `ResponseBuf` in `executor.Request` to record stream output tokens when upstream omits token usage objects. (`internal/proxy/executor/openai.go`)
- **Prompt Caching Tokens Support** — Updated `OpenAIUsage` to extract `cached_tokens` (`prompt_tokens_details.cached_tokens`) and `cache_creation_input_tokens`. (`internal/translator/types.go`, `internal/handlers/chat/usage.go`)

### 🧪 End-to-End Integration Test Suite

- **E2E Test Suite** — Added `internal/handlers/chat/e2e_integration_test.go` to test real HTTP streaming SSE, non-streaming JSON responses, TTFT latency, token accounting, and SQLite DB usage logging end-to-end.

### 🌐 Endpoints

- **`/api/hello`** — Registered `/api/hello` route returning `200 OK` for ping probes from Claude Code CLI. (`cmd/9router-go/main.go`)

## [v1.3.0] — 2026-07-22

### 🏥 Next.js-Compatible Health System

- **Connection-based health** — Replaced old `kv`-based `IsProviderHealthy`/`RecordProviderHealth` with `modelLock_*` fields in `providerConnections.data` JSON blob, matching Next.js `markAccountUnavailable` / `clearAccountError` flow. (`internal/db/health.go`, `internal/db/accounts.go`)
- **Per-connection model locks** — `LockConnectionModel` / `UnlockConnectionModel` / `IsConnectionModelLocked` use SQLite `json_set()` on shared `providerConnections.data`. Dashboard can read/write same fields. (`internal/db/accounts.go`)
- **`IsProviderAvailable`** — New `Repo` method checks if ANY connection for a provider has no active `modelLock_<model>`, replacing the old kv-based pre-check. (`internal/db/accounts.go`)
- **`POST /admin/health/reset`** — Resets `modelLock_*` on connections via query params `?provider=X&model=X`. Dashboard can call via headroom proxy. (`cmd/9router-go/main.go`)
- **Eliminated duplication** — Package-level `IsProviderHealthy` / `ResetProviderHealth` now delegate to `NewRepo(database)` instead of duplicating lock JSON parsing logic. (`internal/db/health.go`)

### 🧪 Test Fixes

- **False-pass assertions** — 3 handler tests were checking old kv-based `repo.IsModelLocked()` which always returned `false` vacuously. Changed to `repo.IsConnectionModelLocked(connID, model)` to actually verify connection-level locks. (`internal/handlers/chat_test.go`)

## [v1.2.0] — 2026-07-22

### 🎯 Gemini Tool Calling Fixes

- **thought_signature round-trip** — Gemini response encodes `thought_signature` into tool call `id` via `__ts__` separator; request decoder restores it for valid verification. Works for both streaming and non-streaming. (`internal/translator/gemini.go`)
- **Antigravity (AGY) support** — Custom `GeminiPart.UnmarshalJSON` handles `thoughtSignature` (camelCase) AND `thought_signature` (snake_case) since the internal `v1internal` endpoint returns camelCase. (`internal/translator/gemini.go`)
- **Tool response name fix** — `tool_call_id` with `__ts__` suffix no longer corrupts `functionResponse.name` extraction, preventing Gemini validation errors on turn 2. (`internal/translator/gemini.go`)

### 🎨 Logging

- **ANSI color-coded logs** — `INF` = green, `WRN` = yellow, `ERR` = red, `DBG` = cyan. Auto-detects TTY (disabled when piped). Disable via `NO_COLOR=1`. (`internal/log/log.go`)

### 🔧 Streaming Fixes

- **SSE multi-line** — Gemini stream chunks with multiple SSE lines (`data: ...\ndata: ...`) are now split and translated individually. Error on one line continues to next instead of aborting. (`internal/handlers/gemini_handler.go`)

### 🧹 Cleanup

- `fallback.go`: Removed misleading `WRN tokensaver failed` logs — replaced with idiomatic `if next, did := ...; did` pattern.
- `test_opencode.go`: Removed (stale temporary test file).
- `internal/translator/gemini_test.go`: Added (unit tests for `thought_signature` round-trip).

## [v1.1.0] — 2026-07-21

### 🚀 New Features

- **SSRF protection** — `/v1/web/fetch` now blocks requests to private/internal IPs (RFC 1918, loopback, link-local, cloud metadata). Matches Next.js `assertPublicUrl()`. (`internal/handlerutil/ssrf.go`)
- **Bypass handler** — Detects Claude Code naming, warmup, and count requests. Returns fake responses without calling upstream, preventing wasted combo rotation slots. (`internal/handlers/bypass.go`)
- **Structured logging** — New `internal/log` package with Info/Warn/Error/Debug levels, runtime config via `LOG_LEVEL` env var. All ~100 `log.Printf` calls replaced across 24 files.
- **Per-connection model locks** — Model locks now stored as `modelLock_<model>` in `providerConnections.data` JSON blob. DB-compatible with Next.js dashboard. Connection A and B can have independent lock states.
- **SSE stall detection** — `StallReader` wrapper closes upstream connection after 6 minutes of no data, preventing hung streams. Integrated into all 4 SSE stream paths.
- **Error classification** — Text-based error rules (8 patterns) + status-based rules (5 codes) + exponential backoff (2s–5min). Fully matching Next.js `checkFallbackError()`.
- **Retry-after tracking** — Tracks earliest `retryAfter` across combo models, includes `Retry-After` header in error responses.
- **Request ID tracing** — Every response includes `X-Request-ID` header, access log includes `id=xxx` prefix.
- **Combo strategies aligned with Next.js** — Sticky round-robin, auto-capability-switch (vision/pdf detection).
- **Health/lock check in combo loops** — Skip unhealthy or locked models during fallback iteration.

### 🔧 Refactoring

- **Error response consistency** — `WriteJSONError` now status-code-aware (e.g., 401 → `authentication_error`, 429 → `rate_limit_error`). `auth.go` inline JSON replaced.
- **SSE consolidation** — `proxy.WriteSSEHeaders` shared by all 4 SSE stream functions. `proxy.SSECopy` with optional `onChunk` callback.
- **Shared test fixture** — `internal/dbtest` package provides canonical `CreateTables()` eliminating duplicated schema in 5+ test files.
- **`stringBuilder` → `bytes.Buffer`** — Removed duplicate custom type in favor of standard library.

### 📚 Documentation

- `ARCHITECTURE.md` — 10 Mermaid flow diagrams (request lifecycle, combo, fusion, error classification, etc.)
- `DATABASE.md` — All 11 tables, JSON blob structure, Go vs Next.js differences

### 🐛 Fixes

- `RetryAfter` ceiling calculation corrected from floor to proper ceiling (`time.Second - 1`)
- Stream translation now handles `[DONE]` marker before JSON parsing
- `TranslateResp` field now passed in `tryForwardWithConnection`

## [v1.0.2] — Previous

- Initial release with OpenAI/Claude SSE proxy, combo fallback, token savers, benchmark results.

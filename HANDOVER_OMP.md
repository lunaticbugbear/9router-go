# OMP Handover

Updated: 2026-09-27

## Start OMP in VS Code

Open the integrated terminal at the repository root and run:

```sh
cd ~/Projects/refute/9router-go
omp --cwd "$PWD" \
  --tools=read,bash,edit,write,grep,glob,lsp,browser,task,todo,web_search \
  --append-system-prompt="$PWD/HANDOVER_OMP.md"
```

Verified locally with OMP v18.3.2. OMP has its own tools; the VS Code Copilot
"Configure Tools" selection does not automatically configure OMP.

Tool mapping:
- VS Code `execute` -> OMP `bash`
- VS Code `search` -> OMP `grep` and `glob`
- VS Code `web` -> OMP `web_search`
- VS Code `agent` -> OMP `task`
- OMP also has `read`, `edit`, `write`, `lsp`, `browser`, and `todo`.
- OMP `browser` is its own Puppeteer automation, not automatically the shared
  VS Code browser page.
- OMP v18.3.2 does not list a built-in `vscode` tool. VS Code-only APIs need a
  separate compatible extension/bridge; do not claim the Copilot `vscode`
  tool is available in OMP.
- Python/notebook tools are optional for this Go/Svelte project. OMP's Python
  tool requires `omp setup python`; add `python,notebook` to `--tools` only if
  needed. Leave `computer` disabled unless desktop control is explicitly needed.

## Task And Guardrails

Continue the existing 9router-go dashboard overhaul. The user wants real
workflow, copy, interaction, and backend improvements, not a CSS-only reskin.
Use concise English UI copy and follow `DESIGN.md` (Imperial Dusk). Preserve the
current public APIs except where a fix requires otherwise.

The worktree is intentionally dirty and contains substantial user/session work.
Inspect before editing. Never reset, clean, checkout, or overwrite unrelated
changes. Local commits are OK when the user asks; never push. The fork is
custom: never pull or merge upstream updates. In particular, preserve the untracked file named
`Attached Element Context from Integrated`. The interrupted OMP run also
created `.pi/fabric/mcp-cache.json`; inspect before touching it and do not
delete it as cleanup.

The local database at `$HOME/.9router` contains real user data and provider
credentials. Never print, copy into this handover, or expose secret values. Do
not reset or modify live provider data just to test UI flows. On 2026-09-27
the user chose a full reset: the live DB now has 0 rows in every table and the
password is the default `123456`. Pre-reset backup:
`~/.9router/db-backup-20260927-151327/data.sqlite`.

## Project And Validation

- Frontend: Svelte 5, TypeScript, Tailwind CSS v4, Vite in `web/`.
- Backend: Go. Run `go test ./...` from the repository root.
- Frontend checks: `cd web && npx tsc -b && npx vite build`.
- `npm run lint` has had existing warnings; check its output before treating
  warnings in untouched components as regressions.
- The last process check found no listeners on 5199, 20131, or 20199. Start
  isolated services only when needed; never point validation at `$HOME/.9router`
  unless the user explicitly asks for a live-data check.

## Current Worktree State

The redesign is incomplete; do not describe all pages as finished. Existing
work includes the Imperial Dusk design system and shared UI primitives, the
Command Deck overview, navigation/command palette changes, connected-first
provider views, provider model registries, Feature Flags, Combo routing UI, and
backend fixes.

Important backend behavior already changed:
- Dashboard model tests use `/api/dashboard/models/test` behind dashboard auth.
- `/api/models/test` remains protected by client API-key auth.
- Compatible model discovery accepts legacy `openai`/`anthropic` node types and
  normalizes `/chat/completions` and `/responses` URL suffixes before fetching
  `/models`.
- Tests cover the auth boundary and provider-node update behavior. Do not merge
  the two model-test auth domains again.
- The canonical `9router init-db` schema now creates `proxyPools`; the shared
  dashboard test schema does too. The local `$HOME/.9router/db/data.sqlite` was
  confirmed to lack that table, so the documented idempotent `init-db` command
  was run against that exact DB. It reported 10 tables verified and existing
  rows untouched. Do not rerun it against another DB path without checking the
  target first.
- Upstream update checks are now opt-in via `UPDATE_URL` or `UPDATE_REPO`.
  Without either source, startup polling and auto-update remain off, the CLI
  update command refuses, and the dashboard/API report only the local version.
- Sidebar update banner/modal and frontend update/shutdown wrappers were
  removed. `getSystemVersion()` remains only to display the locally built
  version. Do not restore upstream polling or a `9router-go update` action by
  default.
- Auth/settings responses now distinguish the default fallback from stored or
  `INITIAL_PASSWORD` credentials with booleans only; no secret value is returned.
Recent frontend fixes in this handover:
- `CombosView.svelte`: removed a state-mutating `$effect` that logged update
  errors when opening the modal. Browser check confirmed the picker stages a
  model in order, Cancel discards it, and reopening starts empty. No test combo
  was saved.
- `EndpointView.svelte`: Tailscale's `needsLogin` response now keeps its Login
  action visible after the connect request finishes; stale auth URLs clear on
  retry. Browser smoke confirmed a failed connect can be retried and then
  presents the Login action on `needsLogin`. Require API key and tunnel-access
  save failures now show inline errors and preserve rollback.
- `QuotaTrackerView.svelte`: auto-ping writes serialize per provider, show
  pending/failure state, and roll back the optimistic toggle if persistence
  fails. Failure smoke caught a nonexistent `api.updateSettingsRaw` call; it
  now uses `api.patchSettings`. Connection and bulk toggles surface failed
  writes and row switches have descriptive labels. Provider-list fetch errors
  propagate from the API client and show a retry instead of an empty state.
  Browser injection confirmed a 500 remains visible and Retry sends a second
  request; auto-ping failure also rolled back after a real PATCH. The bulk
  banner's failed-row line read `Still active on server after refresh` for rows
  whose write had actually failed; it now reads `Not updated on server: …`,
  matching the list, which is built only from `Promise.allSettled` rejections.
- `TerminalView.svelte`: the interrupted OMP run added stream status/error and
  retry, clear failure feedback, log-level GET errors, and scroll pinning with a
  "Jump to latest" action. Embedded gateway smoke verified Live over SSE, clear
  failure retains buffered lines, and automatic stream recovery returns to
  Live without a page reload after the gateway restarts.
- `AnalyticsView.svelte` and `RequestDetailsTab.svelte`: fetch/malformed-response
  errors now have visible retry states instead of looking like empty usage
  data. The empty-data overview renders correctly on the isolated gateway.
- `ProxyPoolsView.svelte`: initial list-fetch errors are shown separately from
  a genuinely empty pool list, with Retry. Browser smoke shows the real empty
  state and the backend now returns 200 after the schema fix.
- `TokenSaverView.svelte`: persistence failures notify, and RTK/Headroom
  toggles roll back. Caveman, Ponytail, and Headroom extras toggles/levels now
  also roll back on save failure; Headroom does not restart after failed save.
- `CliToolsView.svelte` and `api/client.ts`: CLI scan errors are no longer
  swallowed into an empty status map; the page explains scan failure and its
  existing scan button retries. A missing `selectedTool` state that crashed the
  configuration modal was found in browser smoke and fixed; the modal now opens
  in the embedded Go dashboard.
- `ProfileSettingsView.svelte`: saving settings reports failures inline rather
  than using a blocking browser alert. Its database panel now correctly says
  schema setup uses `9router init-db`, not automatic startup migration.
- `LoginView.svelte`: the fallback password hint only appears when the backend
  confirms the built-in default is active; env-provided passwords are reported
  as configured without revealing the value. A remote default-password rejection
  now explains local rotation or `INITIAL_PASSWORD` instead of showing a PATCH
  form that cannot establish a server session. Its displayed port follows the
  current page origin.
- Dashboard port labels and client examples in Settings, API Keys, and CLI Tools
  now follow the embedded gateway's current origin; browser smoke showed 20131
  consistently rather than the old fixed 20130.
- `Sidebar.svelte`: upstream update banner, modal, copy command, and UI polling
  are removed; the local version label remains. The frontend API client no
  longer exposes update-trigger or updater-coupled shutdown methods.
- `MediaKindView.svelte`: provider toggles now report partial or total update
  failures, then refresh actual connection state.
- `ProxyPoolsView.svelte`: list fetch errors are separated from the empty state.
  Root cause for the logged 500 was also fixed: `proxyPools` was missing from
  the canonical `init-db` schema. Added an init-db regression test and a
  handler-level empty-list test.

Latest validation:
- Safe Go validation passed with:
  `env -u DB_PATH -u DATA_DIR -u UPDATE_URL -u UPDATE_REPO go test ./...`.
  No `-skip` is needed: the tests that read the live DB or contact real
  providers are now opt-in behind `NINEROUTER_LIVE_E2E=1` (`requireLiveE2E`),
  so a plain run touches neither `$HOME/.9router` nor the network. The persona
  selector test previously reached real DeepSeek despite its local test setup
  and had to be excluded; that is fixed (see "Open Items"). Do not set
  `NINEROUTER_LIVE_E2E=1` for routine validation — the live tests spend real
  quota and the relay rate-limits.
- `npx tsc -b` and `npx vite build` passed after the Login recovery and dynamic
  origin/port updates. Focused Go auth tests passed for remote default-password
  rejection and password-source status.
- The intermittent `TestEndToEnd_RoundRobin` failure was traced to a real
  rotation bug, not a flaky test. Provider-connection rotation kept its slot as
  an index into the candidate slice, but that slice comes from
  `GetProviderConnections` ordered by `priority ASC, updatedAt DESC`, and a
  successful forward rewrites the serving connection's `updatedAt` via
  `UnlockConnectionModel`, reordering the pool. The slot therefore moved with
  the pool: with two accounts at the same priority, one was skipped and another
  served twice. It only reproduced when the two `updatedAt` writes landed in
  different wall-clock seconds, since the value is RFC3339 with 1-second
  granularity, which is why ~58 earlier runs were clean. Rotation is now keyed
  on connection identity (`comboStickyState.ServingID` resolved through
  `connectionIndexByID`), a pinned connection that has left the pool restarts
  the rotation at the pool head, and all four `GetProviderConnections` queries
  end with `id ASC` so ties on `(priority, updatedAt)` are deterministic.
  `TestConnectionRotation_SurvivesUpdatedAtReorder` reproduces the exact
  interleaving and fails on the old positional code. Treat further
  round-robin flakes as suspect until this is ruled out.
- Vite still reports accessibility warnings in untouched Media/Proxy Pool and
  custom-model components, plus the existing large-chunk warning.
- Browser tests must not save a dummy combo or alter the configured provider.
- Isolated single-origin browser smoke on `http://localhost:20131` verified:
  Proxy Pools returns 200 and shows its empty state; Usage renders zero-data
  state; Console Log reaches Live over SSE; CLI Tools list and detail modal
  render without the `selectedTool` ReferenceError; Profile and Login render.
  The Go gateway served the embedded UI on port 20131 (no separate Vite port).
  The smoke used a disposable DB under `/tmp` and a synthetic test key, never
  the live provider database. Temporary services and DB were stopped/removed.
- Failure injection on the disposable DB verified Token Saver rollback + toast,
  Profile inline save errors, Media all-failed and partial connection toggles,
  Quota connection/bulk toggles, Quota auto-ping PATCH rollback/status, Endpoint
  API-key toggle rollback + inline error, and Console Log clear failure while
  retaining buffered lines. Auth smoke verified `INITIAL_PASSWORD` reports
  configured without exposing its value; fallback Login shows `123456` only
  when the backend marks the fallback active.
- Console Log recovery was verified without reloading the page: stopping the
  embedded gateway changed the status to `Reconnecting…`; restarting it on
  port 20131 restored `Live` and delivered the new startup log.
- Updater smoke started the gateway with `AUTO_UPDATE=true` and no source;
  startup logged checks disabled, `/api/version` returned only current local
  version with `hasUpdate:false`, and `/api/version/status` reported
  `autoUpdateEnabled:false`. Sidebar showed no update banner. Test service and
  disposable database were removed afterward.

## Security Fixes 2026-09-28 (both HIGH, both found by exercising the features live)

Both of these were found by actually driving the running gateway and the real
installers, not by reading the code or by unit tests. That matters because the
previous handover recorded both features as unverified (see Open Items 2): the
gaps only showed up once the features were used the way a user uses them.

- **"Ask AI" leaked full API keys (HIGH).** `POST /api/dashboard/cli-tools/assist`
  had no server-side redaction. The only masking was client-side, in
  `web/src/components/CliToolsView.svelte` `buildToolContext()`, plus a
  system-prompt instruction, so any direct API caller bypassed both and the
  handler returned the model's answer and forwarded the operator's `context`
  verbatim. Reproduced against the live gateway: a full API key came back in the
  answer. Fixed by a new narrow repo query `(*Repo).SecretValues()`
  (`internal/db/secrets.go`) that enumerates the credential *values* the gateway
  actually stores — the `apiKeys.key` column plus credential-shaped fields in
  each `providerConnections.data` blob, walked recursively and matched by an
  explicit name set with a `*token`/`*secret`/`*apikey`/`*password`/`*_key`
  suffix fallback. `internal/handlers/chat/cli_assist.go` now scrubs **both**
  the assembled outgoing prompt and the returned answer against that list,
  replacing occurrences with the literal `<your-api-key>` the UI already
  promised. The match is value-based rather than pattern-based, so it cannot
  miss a real key or mangle ordinary prose; values under 8 characters are
  dropped, the list is applied longest-first, and every occurrence is replaced.
  The upstream error text is scrubbed **before** the 300-byte truncation,
  otherwise a cut mid-key would leave an unrecognisable prefix. It fails
  **closed**: HTTP 503 if the credential list cannot be read (an empty list is
  not a failure and still answers). No secret value is logged. The earlier
  handover note "Ask AI setup panel (keys redacted)" described the client-side
  masking only; that is what this fixes.
- **Installer "Remove"/Reset destroyed the operator's own settings (HIGH).**
  The UI and CHANGELOG promised "Remove strips only 9router keys / other
  settings are kept", but Reset *deleted* the managed keys rather than
  restoring what was there before 9router wrote to the file, so a user's own
  `ANTHROPIC_BASE_URL`, their own `model = "gpt-5-codex"`, or their own Cline
  provider choice were destroyed. The install already kept the true original as
  `<path>.9router.bak`; Reset simply never read it. Fixed by
  `internal/clisetup/restore.go`: Reset now restores the pre-9router value of
  each managed key from that backup, per key. A key 9router merely *overwrote*
  is restored; a key in 9router's own namespace (`9router` provider tables,
  `9router/`-prefixed model ids, the `9Router` Copilot entry, `custom:9Router*`
  ids, `JCODE_9ROUTER_API_KEY`) is **removed** even when the backup has a copy,
  so a stale gateway config is not resurrected; a managed key absent from the
  backup is removed; unrelated settings and later user edits survive. With no
  backup it falls back to deletion and says so honestly in the message. Reset
  never rewrites the backup (new `writeReset`/`writeJSONReset`/`writeTOMLReset`
  path). 10 installers were converted; `copilot` and `droid` were audited and
  confirmed already correct, because their managed unit is their own namespace
  so removal is already the exact inverse. All 13 registered installers are
  covered by tests. Also fixed: `claudeConfigured` now requires the URL to look
  like this gateway, otherwise a *restored* corporate URL still reported "Using
  9router"; and OpenClaw reset now clears the per-agent `models.json` gateway
  entry the old code left behind.

How these were exercised: Ask AI was run against a real connected provider
through the live gateway (which is what produced the leaked key and then
confirmed the fix), and the installers were driven in an **isolated temp
`HOME`**, never the operator's real configs, so no live CLI config was modified.
The live gateway on 20130 was rebuilt, reinstalled and restarted with these
changes and reports `{"status":"ok"}`.

## Security Fixes 2026-09-28 (found by review, not by tests)

These were found by reading the trust path, not by a failing test or by using
the feature. The distinction matters: the tests that existed for this code
**passed against the vulnerable build**, so no test run would have surfaced it.

- **The local-caller check trusted a client-supplied header (HIGH).** The
  decision "is this caller on this machine?" read the request's `Host` header
  first, and `Host` is whatever the caller types. Sending `Host: localhost`
  therefore made a remote caller local. One root cause, three exploits:
  it defeated the fresh-install default-password guard (`POST /api/auth/login`
  accepts `123456` only from the local machine, so a remote caller could get a
  session and mint a gateway API key); it defeated the tunnel dashboard-access
  gate (a spoofed loopback `Host` waived the gate that is supposed to refuse
  tunnel-hostname dashboard access); and it skipped the provider-node SSRF guard
  (a self-hosted node URL is validated only for local callers, so a remote
  caller could point the gateway at an internal service with the stored provider
  credential attached). The Go port had additionally dropped upstream's
  peer-token proof: `x-9r-real-ip` was read without requiring the
  `x-9r-peer-token` secret that upstream requires, so an untrusted header could
  also widen the local set. Fixed by making `auth.IsLocalRequest`
  (`internal/auth/localpeer.go`) the single trust decision, taken from the real
  peer address (`ClientAddr`) and never from `Host`. A forwarded address is
  honoured only under the existing trust config (`TRUST_PROXY`/
  `TRUST_CLOUDFLARE`, or a valid peer-token proof); a loopback peer is
  necessary but not sufficient, because a request carrying a non-loopback
  `Origin` is refused so a page served from a remote origin inside the
  operator's own browser cannot reach the loopback listener. `nodeRequestIsLocal`,
  `auth.TunnelLoginBlocked` and `auth.LoginClientIP` all route through the one
  helper, so the login limiter and the local-caller check cannot disagree. The
  existing test set a loopback `Host` and never varied the peer address, which
  is precisely why it passed; the new regression tests vary `RemoteAddr` and
  `Host` **independently**. Verified live: LAN address + `Host: localhost` +
  default password answers `403`, the same request over a genuine loopback
  connection answers `200`, and the local browser path is unaffected.
- **Endpoint write failures reported success (HIGH).** `HandleTunnelDisable`,
  `HandleTailscaleEnable` and `HandleTailscaleDisable` discarded the
  `UpdateSettingsRaw` error and always answered `{"success":true}`, so the UI
  reported remote access as disconnected when the write never landed. They now
  return `500` via the sibling `writePlainError` convention, and
  `EndpointView.svelte` shows a row-level notice with its retry action instead
  of a state change that did not happen, plus the `funnelNotEnabled` reason and
  its `enableUrl`.
- **A dead dashboard stayed rendered after session expiry (HIGH + MEDIUM-HIGH).**
  Dashboard API callers swallow their errors and auth was evaluated once in
  `onMount`, so a mid-session `401` left the shell on screen and interactive
  forever. A `401` now triggers one central, single-flight confirmation against
  the public `/api/auth/status`, which distinguishes a lost session from the
  handlers that legitimately answer `401` (upstream-credential rejections, the
  login form), and hands off to login with a notice. `checkAuth` now fails
  closed: a probe error or a non-JSON body shows login instead of the dashboard.

Validation: `go build ./...`, `go vet ./...` and
`env -u DB_PATH -u DATA_DIR -u UPDATE_URL -u UPDATE_REPO go test ./... -count=1`
all clean (35 packages ok), `npx tsc -b` clean, `npm run lint` exits 0 with only
the two pre-existing warnings (`TerminalView.svelte`,
`ProviderDetailView.svelte`), `npx vite build` succeeds, and `gofmt -l` is empty
on every file in the change. The live gateway on 20130 was rebuilt, reinstalled
and restarted with these changes; `/health` reports `{"status":"ok"}`, the
served asset hash matches the fresh build, and a real local browser login
(`/dashboard` -> login -> `/dashboard/overview`) renders the dashboard with its
APIs answering `200` and no login loop.

## OMP Runtime Incident

The previous long OMP session entered Vibe mode and launched parallel workers.
The parent/worker session persistence then failed with `Session file changed
before rewrite`; subsequent `vibe_spawn`, `vibe_send`, and kill attempts also
reported indeterminate session persistence. A global Fabric plugin separately
failed to load because `@jitl/quickjs-ffi-types` was missing. Do not repeat the
parallel-worker workflow in that damaged OMP process. Restart OMP before
continuing; this is an OMP/plugin runtime problem, not evidence that the repo
worktree is corrupt.

The repository's `AGENTS.md` points to `/Users/luqmannul.hakim/htdocs/9router`,
but that clone was absent on this workstation. For future upstream parity work,
record that limitation and use the official upstream only if needed; do not
loop on the nonexistent local path.

## Session 2026-09-27 (VS Code Copilot) Changes

All committed locally (no push). Highlights:
- Run the gateway with just `9router` (binary in `~/.local/bin`, also
  `9router-go`). After every code change, rebuild and install:
  `cd web && npx tsc -b && npx vite build; cd .. && v=$(cat VERSION) && go build -ldflags="-s -w -X '9router/proxy/internal/updater.CurrentVersion=$v'" -o /tmp/9router-go.new ./cmd/9router-go/ && install -m 0755 /tmp/9router-go.new ~/.local/bin/9router && install -m 0755 /tmp/9router-go.new ~/.local/bin/9router-go && rm /tmp/9router-go.new`
- Restart the live gateway (port 20130): `kill -TERM $(lsof -tiTCP:20130 -sTCP:LISTEN)`,
  then `cd ~ && env -u DB_PATH -u DATA_DIR -u UPDATE_URL -u UPDATE_REPO -u PORT -u HOST -u INITIAL_PASSWORD 9router`.
- Route auth: Console Log, CLI tool statuses and configure, `/api/version`,
  `/api/changelog`, `/api/dashboard/models` and `/api/dashboard/cli-tools/assist`
  accept the dashboard session (`SetupDashboardRoutes` and the dashboard group
  in `SetupServerRouter`). Client `/v1/*` still requires an API key.
- `App.svelte` `checkAuth` trusts only the server `authenticated` flag, so a
  stale localStorage flag no longer renders an empty dashboard.
- CLI Tools: "Installed (n)" tab; "Ask AI" setup panel (`internal/handlers/chat/cli_assist.go`,
  keys redacted); one-click installers in `internal/clisetup/` (`tools.go`,
  `tools_more.go`, `grok.go`) for claude, codex, opencode, hermes, copilot,
  cowork, droid, openclaw, kilo, cline, grok-build, deepseek-tui and jcode.
  Devin has no installer (no config file; `devin auth login`). Handler:
  `internal/handlers/dashboard/cli_setup.go` (POST/DELETE
  `/api/cli-tools/{tool}/configure`; auto-creates the key "CLI tools (auto)").
  Writes are atomic, back up once to `*.9router.bak`, and refuse unparseable files.
  Tests use a temp `HOME`: `internal/clisetup/*_test.go`.
- Sidebar: collapsible icon rail plus drag-resize (Ctrl/⌘+B, double-click resets,
  persisted in localStorage). Overview layout was compacted. Media pushState
  DataCloneError fixed.
- Gotcha: newly created `.go` files sometimes get a duplicated `package x`
  line; check `head -2` before building.

## Open Items (for OMP)

1. **Persona test still hits the network. — FIXED 2026-09-28.**
   Root cause was the shared fixture, not the persona test: `setupChatTestDB`
   seeded `conn-1` (deepseek) and `conn-2` (groq) with no `baseUrl`, so they
   resolved the real provider catalog and forwarded to `api.deepseek.com`. Nine
   tests leaked live traffic, not just the persona one. The fixture now points
   those rows at a local `httptest` stub that answers 401, which preserves the
   existing fallback behavior (first connection fails, the next is tried). The
   persona test no longer needs `-skip`, and
   `TestChatTestFixtureConnectionsAreLocal` guards against reintroduction.
   The live-test gate now covers chat **and** media: `requireLiveE2E` is applied
   to 27 tests across 7 files, including the media TTS/voices tests that were
   silently reaching `www.bing.com`, `opencode.ai`, and `api.elevenlabs.io`.
2. **Live verification done 2026-09-28 — and it found two HIGH bugs.**
   Both features have now been exercised for real, not only unit-tested, and
   that is exactly how the two security fixes above were found. "Ask AI" was
   run against a real connected provider through the live gateway: the
   dashboard-only redaction was bypassable, and a full API key came back in the
   answer. The installers were driven end to end in an **isolated temp `HOME`**,
   never the operator's real configs, which is where the "Remove deletes your
   own settings" behaviour showed up. Both are fixed and covered by tests (see
   "Security Fixes 2026-09-28"). Still open: the installers have not been
   pointed at a tool that then makes a real request through the gateway — that
   round trip (install, request, Remove) against a real CLI remains unverified,
   and the live DB still has 0 providers, so Ask AI's fix was confirmed against
   the provider connection present at the time rather than re-run since. Do not
   apply installers to the user's real configs without asking.

## Next Work

The all-pages overhaul remains incomplete. The safe Go suite and frontend
checks pass; provider-live E2E tests are opt-in via `NINEROUTER_LIVE_E2E=1`
because they need real credentials/network access. The three previously
outstanding browser failure cases are now accounted for:

- Console Log clear/reconnect — verified earlier in this handover (clear failure
  retains buffered lines; stopping and restarting the embedded gateway moved the
  status `Live` -> `Reconnecting…` -> `Live` without a page reload).
- Quota bulk partial failure — implemented and verified: bulk toggles use
  `Promise.allSettled`, report succeeded/failed counts in a persistent banner,
  roll back only the failed rows, and reconcile with the server afterward. Row
  failures show an inline error.
- Media partial multi-connection failure — implemented and verified: multi-
  connection writes report partial results per connection, a total failure does
  not read as success, and state refreshes afterward.

Honest gap: those UI changes were verified by type-check, lint, and build, plus
the failure-injection runs recorded earlier in this handover. This session did
not re-run browser failure injection for them. Provider icons were validated by
`npm run check:icons` (0 failures) rather than by browser inspection of the
9 placeholder entries, and the "Test all" pacing/rate-limit reporting was
validated by type-check/lint/build and by reading the implementation, not by a
live throttled run. Treat a browser re-check as the remaining verification step.

Continue with deeper Login/Endpoint/Quota review. Keep the single-port embedded
gateway and temporary DB for browser tests; avoid OMP subagents until its
session-persistence failure is understood.
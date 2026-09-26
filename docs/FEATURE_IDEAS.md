# Feature Ideas Backlog — 9router-go

Goal: beat 9router (decolua), OmniRoute (diegosouzapw), Plexus (mcowger).
Researched: upstream repos + LiteLLM + Portkey + Helicone (GitHub API, read directly).

## From round 1 (competitor analysis, sourced from upstream READMEs)

1. **Free-tier budget computer, honest** — compute from own usageHistory ledger, not catalog claims (beats OmniRoute's ~1.62B headline that needs biweekly manual re-audit).
2. **e2e_performance routing** — learn from observed TTFT/tokens-per-sec/error-rate per connection (Plexus has selectors but only config-driven; nobody has learned-from-own-data).
3. **Quota tracking + Quota-Share scheduling** — per-connection remaining/reset, live dashboard, proportional load split (OmniRoute v3.8.50 added Quota-Share; 9router-go has nothing).
4. **Vision fallthrough / modality bridge** — capacity adapter pools exist but empty; wire detection → reroute (Plexus + OmniRoute have it).
5. **Verified token saver** — measure actual savings per request from own data; replace README percentage claims (RTK claims 20-40%, caveman 15-95%).
6. **OpenAPI spec-first + CI gate** — reject PRs changing API without spec update (Plexus does this).
7. **Encryption at rest + `rekey`** — AES-256-GCM for stored API keys/OAuth tokens (Plexus has; verify 9router-go state).
8. **MCP/A2A proxy** — both OmniRoute and Plexus have it.
9. **Multi-account rotation per provider** — 9router has round-robin; 9router-go has getBestConnection but no fair rotation.
10. **Catalog honesty as differentiator** — models audit (540/1437 guesses, --strict) is unique; deepen with runtime fingerprint + identity drift detection.

## From round 2 (LiteLLM / Portkey / Helicone, GitHub API reads)

11. **Guardrails** (Portkey: 50+ guardrails; output guardrail deny/retry) — declarative input/output checks with retry-on-violation. 9router-go has injection guard flag only; no general guardrail config.
12. **Conditional routing** (Portkey) — route by request content/headers (e.g., long-context requests → 1M-context model automatically). Pairs with #4.
13. **Virtual keys + budgets** (LiteLLM) — per-key spend caps, per-key model allowlists, key-level tracking. 9router-go has apiKeys but no budgets.
14. **Spend tracking per user/team** (LiteLLM, Helicone) — org/team hierarchy over usage. Personal gateway → per-persona or per-project tagging instead.
15. **Prompt management / versioning** (Helicone) — version prompts, deploy via gateway without code changes. 9router-go persona plane is close; extend to general prompt templates + version history.
16. **Sessions & agent tracing** (Helicone) — group requests by session id (9router-go already extracts session IDs!), trace agent chains, per-session cost/latency. Data exists, no view.
17. **Playground** (Helicone) — test prompts/models in the dashboard before committing. Natural extension of persona preview + bind --check.
18. **A2A protocol support** (LiteLLM /a2a, OmniRoute) — invoke LangGraph/Vertex/Bedrock agents through the gateway as models.
19. **Batches endpoint** (LiteLLM) — /v1/batches with async completion across providers.
20. **Rerank endpoint** (LiteLLM) — /v1/rerank normalized across providers.
21. **Unified embeddings/images/audio** (all three) — Plexus has embeddings; LiteLLM full multimodal surface.
22. **Cloudflare Workers / edge deployment** (Portkey) — sub-ms latency claims from edge runtime. 9router-go is Go binary: target WASM edge or embeddable library mode.
23. **Rust core / performance claim** (LiteLLM) — "fastest, litest", 8ms P95 at 1k RPS with benchmarks published. 9router-go already Go: publish benchmarks, own the "single static binary" angle.
24. **Auto-update price/context-window data** (LiteLLM has a workflow for this) — 9router-go has 540 guessed context windows; a data pipeline that pulls published specs would close the honesty gap mechanically.
25. **Cost API / pricing database** (Helicone: 300+ models open-source pricing DB) — expose /api/pricing so other tools consume 9router-go's measured costs.
26. **Request logs with body inspection** (Plexus, Helicone) — full request/response bodies in logs (opt-in, redacted). 9router-go stores usage but not bodies (good for privacy — make opt-in differentiator).
27. **Datasets & fine-tune export** (Helicone) — export request history as fine-tune datasets (OpenPipe/Autonomi partners).
28. **Enterprise auth: OIDC/SAML, org management** (LiteLLM, Helicone SOC2) — 9router-go has admin key + dashboard auth; no SSO.
29. **Dead-man resilience: stalled stream detection** (Plexus) — detect silent stream stalls and fail over. 9router-go has usagetracker but check stall handling.
30. **Auto-retries with backoff as config** (Portkey: retry attempts in config) — 9router-go has fallback but expose declarative retry policy per combo/model.

## Round 3 — sub2api (Wei-Shaw, 42.8k stars, Go + LGPL v3)

Note: sub2api's core purpose is subscription-quota resale/sharing ("pinche" / carpool-sharing of Claude/OpenAI/Gemini/Grok subscriptions with built-in payment). That business model conflicts with provider ToS by their own admission. Ideas below take only the **technical features**, not the resale model.

31. **Composite groups** (sub2api) — admin routing layer that resolves a requested model name to concrete providers for multi-provider groups. 9router-go's combos are close but not an admin-managed model→multi-provider mapping layer. Distinct from binding: binding = one name → one model + persona; composite = one name → N providers with selection strategy.
32. **Sticky sessions on account selection** (sub2api) — smart scheduling with sticky sessions so the same session keeps hitting the same upstream account. 9router-go has sticky limit on combos; not on account/connection selection. Nginx note in their docs even shows the header (`session_id`) that makes it work — 9router-go already extracts session IDs.
33. **Concurrency control per user AND per account** (sub2api) — two-sided limits. 9router-go has neither.
34. **Token-level rate limiting** (sub2api) — limits in tokens/min, not just requests/min. Complements existing request-based limiting.
35. **Setup wizard + one-click install script** (sub2api) — curl | bash installer, systemd service, first-run browser wizard for DB + admin account. 9router-go: binary + env vars; `init-db` exists but no wizard. Onboarding is the first battle.
36. **One-click in-dashboard upgrade with rollback** (sub2api) — check version, apply update, roll back from the web UI. 9router-go has a CLI `update` command; a dashboard-integrated version with rollback is next level.
37. **Per-account health + auto-disable** (sub2api implied by scheduling) — accounts that fail get benched automatically with recovery probing. 9router-go has breaker logic in of-route (from omniforge audit work) — port that.
38. **Mobile admin console** (sub2api community: sub2api-mobile, Expo/React Native) — a community project pattern worth planning for: keep admin API clean enough that a mobile client is buildable.
39. **Multi-language README** (sub2api: EN/CN/JA) — cheap reach multiplier, all three competitors do it.
40. **Release tooling: goreleaser matrix** (sub2api: .goreleaser.yaml + release_matrix.py + tested matrix) — professional multi-platform releases. 9router-go has a release script from omniforge work; goreleaser is the standard.

## Cross-cutting idea (original, from this session's measured findings)

41. **Provider capability ledger** — the generalization of everything measured this session: per provider, record what was PROVEN (system-prompt support threshold, token accounting honesty, identity stability, real context window) with timestamp + evidence. Every other feature (routing, persona delivery mode, cost estimates) consults it. No competitor has this because none of them measure — they claim. This is the moat.

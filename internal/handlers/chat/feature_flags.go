package chat

import (
	"9router/proxy/internal/featureflags"
)

// Feature-flag ids this package gates behavior on. They are named constants
// rather than literals so a typo cannot silently disable a stable feature: a
// misspelled id would resolve to "unknown flag" and fail closed, which looks
// exactly like an operator turning the feature off. TestFlagIDsAreRegistered
// pins each constant against the registry so a rename breaks a test instead of
// a behavior.
const (
	// flagPersonaPlane is "prompt.personas": the master switch for the persona
	// plane. Off means no persona is resolved from any source.
	flagPersonaPlane = "prompt.personas"
	// flagModelBindings is "prompt.bindings": the master switch for operator
	// model bindings. Off means a bound name resolves as if it were unbound.
	flagModelBindings = "prompt.bindings"
	// flagRTKSaver is "tokensavers.rtk": gates RTK tool_result compression only.
	// Caveman and Ponytail are separate, still-planned flags and are not gated
	// here.
	flagRTKSaver = "tokensavers.rtk"
	// flagRoutingCombos is "routing.combos": gates combo expansion in model
	// resolution. Off means a combo name resolves through alias/catalog as if
	// the combo table were empty.
	flagRoutingCombos = "routing.combos"
	// flagStickySessions is "routing.sticky-sessions": gates the sticky part of
	// combo rotation. Off means each request picks the next model fresh; combos
	// themselves keep working.
	flagStickySessions = "routing.sticky-sessions"
	// flagBountyContext is "prompt.bounty": gates bounty scope context. Off means
	// no profile is resolved and no scope block is attached.
	flagBountyContext = "prompt.bounty"
	// flagSessionTracing is "observation.session-tracing": gates recording the
	// client session id on each usage row. Off means rows are written without a
	// session id, which is exactly the behavior every existing row has.
	flagSessionTracing = "observation.session-tracing"
	// flagModelAudit is "observation.model-audit". Its gate lives in the CLI
	// (cmd/9router-go's runModelsAudit, which refuses to run when the flag is
	// off), not in the request plane, so no code in this package reads it. It is
	// listed in WiredFlagIDs below because that list is the ledger of every
	// wired Stable flag, not just the ones this package gates.
	flagModelAudit = "observation.model-audit"
)

// WiredFlagIDs is the set of feature-flag ids this package actually gates
// behavior on. It is the honest counterpart to the registry's Stage: the
// registry says a flag is Stable, meaning "this toggle controls live behavior
// today", and this list is the evidence.
//
// internal/featureflags' TestStableFlagsAreWired cross-checks the two, so a flag
// cannot be labeled Stable while gating nothing — which is the exact lie the
// Stage type exists to prevent. Adding a Stable flag therefore requires wiring
// it and adding its id here; adding it here without a gate fails
// TestWiredFlagIDsAreRegistered or the per-flag OFF-state tests.
//
// It is built from the constants above rather than written out separately, so a
// rename cannot leave this list pointing at a flag that no longer exists.
var WiredFlagIDs = []string{
	flagPersonaPlane,
	flagModelBindings,
	flagRTKSaver,
	flagRoutingCombos,
	flagStickySessions,
	flagBountyContext,
	flagSessionTracing,
	flagModelAudit,
}

// featureFlags resolves the effective on/off state of every registered flag.
//
// This is the single read path for flag gating in this package, so every gate
// resolves the same way and a new gate cannot invent its own precedence.
//
// Resolution goes to the settings row, like every other per-request setting in
// this package (provider strategies, combo routing, capacity adapters), so a
// flag flipped in the dashboard takes effect on the next request without a
// restart and without a cached copy going stale. GetFeatureFlags folds the
// operator's stored choices over the registry defaults, so an untouched flag
// reports the state it was designed to ship with.
//
// Every failure path falls back to the registry defaults rather than to "off".
// A missing settings row, a storage error, or a handler constructed without a
// repo (as several tests do) must not silently disable a stable feature that the
// operator never turned off — that would turn an unrelated storage problem into
// an outage of the feature. Default-off flags stay off, which is the designed
// fresh-install state.
//
// Callers that gate on more than one flag should take the map once and read both
// entries from it, so a single decision does not pay for two settings reads.
func (h *ChatHandler) featureFlags() map[string]bool {
	if h.Repo != nil {
		if flags, err := h.Repo.GetFeatureFlags(); err == nil && flags != nil {
			return flags
		}
	}
	defaults := make(map[string]bool, len(featureflags.All()))
	for _, f := range featureflags.All() {
		defaults[f.ID] = f.Default
	}
	return defaults
}

// featureFlagOn reports whether one registered feature flag is on.
func (h *ChatHandler) featureFlagOn(id string) bool {
	def, known := featureflags.Get(id)
	if !known {
		// An unknown id is a bug in this package, not an operator choice, and
		// the caller is asking whether behavior should run. Fail closed: a gate
		// on a flag that does not exist must not enable anything.
		return false
	}
	on, ok := h.featureFlags()[id]
	if !ok {
		return def.Default
	}
	return on
}

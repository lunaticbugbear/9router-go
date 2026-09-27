package chat

import (
	"9router/proxy/internal/constants"
	"9router/proxy/internal/db"
	"9router/proxy/internal/log"
	"9router/proxy/internal/models"
	"9router/proxy/internal/providers"
	internalproxy "9router/proxy/internal/proxy"
	"9router/proxy/internal/translator"
	json "encoding/json/v2"
	"fmt"
	"math/rand/v2"
	"strings"
)

// CredentialFallbacks maps search/tool providers to the primary chat provider whose API key can be reused.
var CredentialFallbacks = map[string]string{
	"ollama-search": "ollama",
	"zai-search":    "glm",
	"cline":         "clinepass",
	"clinepass":     "cline",
}

// ResolveProviderProxyPoolID returns the active proxy pool ID configured for a provider,
// checking both the canonical provider name and its alias/counterpart (e.g. antigravity <-> opencode).
func (h *ChatHandler) ResolveProviderProxyPoolID(provider string) string {
	if h.Repo == nil {
		return ""
	}
	settings, err := h.Repo.GetSettings()
	if err != nil || settings == nil || settings.ProviderStrategies == nil {
		return ""
	}

	checkList := []string{provider}
	switch provider {
	case "antigravity", "ag":
		checkList = append(checkList, "ag", "antigravity")
	case "opencode", "oc":
		checkList = append(checkList, "oc", "opencode")
	case "cline":
		checkList = append(checkList, "clinepass")
	case "clinepass":
		checkList = append(checkList, "cline")
	}

	for _, p := range checkList {
		if strat, ok := settings.ProviderStrategies[p]; ok {
			if strat.ProxyPoolID != "" && strat.ProxyPoolID != "__none__" {
				return strat.ProxyPoolID
			}
		}
	}
	return ""
}

// GetBestConnection retrieves the highest-priority active connection for a provider.
// When connectionID is non-empty, it fetches that specific connection directly.
func (h *ChatHandler) GetBestConnection(provider string, connectionID string, excludeIDs []string, model string) (*models.ProviderConnection, *ConnectionData, error) {
	return h.getBestConnection(provider, connectionID, excludeIDs, model)
}

func (h *ChatHandler) getBestConnection(provider string, connectionID string, excludeIDs []string, model string) (*models.ProviderConnection, *ConnectionData, error) {
	if model != "" && !h.Repo.IsProviderAvailable(provider, model) {
		log.Warn("health", "unhealthy provider", "provider", provider, "model", model)
	}

	var conn *models.ProviderConnection
	var err error

	if connectionID != "" {
		conn, err = h.Repo.GetProviderConnectionByID(connectionID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch connection %s: %w", connectionID, err)
		}
		if conn == nil {
			return nil, nil, fmt.Errorf("connection %s not found", connectionID)
		}
	} else {
		connections, queryErr := h.Repo.GetProviderConnections(provider, true)
		if queryErr != nil {
			return nil, nil, fmt.Errorf("failed to query connections for %s: %w", provider, queryErr)
		}
		if len(connections) == 0 {
			if fallbackProvider, ok := CredentialFallbacks[provider]; ok {
				fallbackConns, fallbackErr := h.Repo.GetProviderConnections(fallbackProvider, true)
				if fallbackErr == nil && len(fallbackConns) > 0 {
					connections = fallbackConns
				}
			}
		}
		if len(connections) == 0 {
			if cfg, ok := providers.KnownProviders[provider]; ok && cfg.NoAuth {
				// Inject virtual connection for no-auth provider with optional proxy pool strategy from settings
				connData := &ConnectionData{
					AccessToken: "public",
				}
				connData.ProxyPoolID = h.ResolveProviderProxyPoolID(provider)
				publicName := "Public"
				conn := &models.ProviderConnection{
					ID:       "noauth",
					Provider: provider,
					Name:     &publicName,
					IsActive: 1,
				}
				return conn, connData, nil
			}
			return nil, nil, fmt.Errorf("no active connections for provider: %s", provider)
		}

		settings, settingsErr := h.Repo.GetSettings()
		connections = filterConnectionsForModel(provider, connections, model, settings)
		if len(connections) == 0 {
			return nil, nil, fmt.Errorf("no connection assigned to model %s for provider %s under strict assignment", model, provider)
		}

		// Rotate only connections eligible for the requested model.
		if len(connections) > 1 && settingsErr == nil && settings != nil {
			strat := db.ProviderStrategy{}
			hasStrat := false
			if settings.ProviderStrategies != nil {
				if s, ok := settings.ProviderStrategies[provider]; ok {
					strat = s
					hasStrat = true
				}
			}
			if !hasStrat || strat.RotateStrategy == "" {
				if settings.FallbackStrategy != "" && settings.FallbackStrategy != "fill-first" {
					strat.RotateStrategy = settings.FallbackStrategy
					strat.StickyLimit = settings.StickyRoundRobinLimit
				}
			}
			if strat.RotateStrategy != "" && strat.RotateStrategy != "none" {
				connections = h.applyConnectionStrategy(provider, connections, strat)
			}
		}

		excludeSet := make(map[string]bool, len(excludeIDs))
		for _, id := range excludeIDs {
			excludeSet[id] = true
		}

		conn = nil
		for _, c := range connections {
			if excludeSet[c.ID] {
				continue
			}
			// Skip connections that have an active per-connection model lock
			if model != "" {
				lockKey := canonicalLockModel(provider, model)
				if locked, _ := h.Repo.IsConnectionModelLocked(c.ID, lockKey); locked {
					continue
				}
				if lockKey != model {
					if locked, _ := h.Repo.IsConnectionModelLocked(c.ID, model); locked {
						continue
					}
				}
				if provider == "antigravity" && IsAntigravityModelBlocked(c.ID, model) {
					continue
				}
			}
			conn = c
			break
		}
		if conn == nil {
			return nil, nil, fmt.Errorf("no available connections for provider: %s (all excluded)", provider)
		}
	}

	var connData ConnectionData
	if conn.Data != "" {
		if err := json.Unmarshal([]byte(conn.Data), &connData); err != nil {
			return nil, nil, fmt.Errorf("failed to parse connection data: %w", err)
		}
	}
	// The dashboard's proxy assignment endpoint (upstream parity) stores the
	// binding in providerSpecificData.proxyPoolId, while the top-level field is
	// what this resolver reads. Accept both so a pool bound from either writer
	// takes effect.
	if connData.ProxyPoolID == "" {
		if poolID, ok := connData.ProviderSpecificData["proxyPoolId"].(string); ok && poolID != "" && poolID != "__none__" {
			connData.ProxyPoolID = poolID
		}
	}

	return conn, &connData, nil
}

// GetProviderConfig returns the upstream configuration for a provider.
func (h *ChatHandler) GetProviderConfig(provider string, connData *ConnectionData) (*providers.ProviderConfig, error) {
	return h.getProviderConfig(provider, connData)
}

func (h *ChatHandler) getProviderConfig(provider string, connData *ConnectionData) (*providers.ProviderConfig, error) {
	var baseCfg *providers.ProviderConfig

	if connData != nil && connData.BaseURL != "" {
		if cfg, ok := providers.KnownProviders[provider]; ok {
			cloned := cfg
			cloned.BaseURL = connData.BaseURL
			baseCfg = &cloned
		} else {
			baseCfg = &providers.ProviderConfig{
				BaseURL:    connData.BaseURL,
				AuthHeader: constants.HeaderAuthorization,
				AuthScheme: constants.AuthSchemeBearer,
			}
		}
	} else if cfg, ok := providers.KnownProviders[provider]; ok {
		// Clone config so per-request headers don't mutate global registry
		cloned := cfg
		baseCfg = &cloned
	} else {
		node, nodeData, err := h.Repo.GetProviderNodeByID(provider)
		if err != nil {
			return nil, fmt.Errorf("failed to look up provider node %s: %w", provider, err)
		}
		if node != nil && nodeData != nil && nodeData.BaseURL != "" {
			baseURL := nodeData.BaseURL
			if !strings.HasSuffix(baseURL, "/chat/completions") {
				if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/v1/") {
					baseURL = strings.TrimRight(baseURL, "/") + "/chat/completions"
				} else {
					baseURL = strings.TrimRight(baseURL, "/") + "/v1/chat/completions"
				}
			}
			baseCfg = &providers.ProviderConfig{
				BaseURL:    baseURL,
				AuthHeader: constants.HeaderAuthorization,
				AuthScheme: constants.AuthSchemeBearer,
			}
		}
	}

	if baseCfg == nil {
		return nil, fmt.Errorf("provider %q has no baseUrl in connection data and is not in KnownProviders", provider)
	}

	// Check if this connection uses an Edge Relay Proxy Pool (Vercel, Cloudflare, Deno)
	if connData != nil {
		var relayURL string
		var noProxy string

		if connData.ProxyPoolID != "" {
			if pool, err := h.Repo.GetProxyPool(connData.ProxyPoolID); err == nil && pool != nil && pool.IsActive {
				if pool.Type == "vercel" || pool.Type == "cloudflare" || pool.Type == "deno" {
					relayURL = pool.NextURL()
					noProxy = pool.NoProxy
					logProxyOnce(connData.ProxyPoolID, relayURL, pool.Type)
				}
			}
		}
		if relayURL == "" && connData.ProviderSpecificData != nil {
			if u, ok := connData.ProviderSpecificData["vercelRelayUrl"].(string); ok && u != "" {
				relayURL = u
				if np, ok := connData.ProviderSpecificData["connectionNoProxy"].(string); ok {
					noProxy = np
				}
			}
		}

		if relayURL != "" && !internalproxy.ShouldBypassNoProxy(baseCfg.BaseURL, noProxy) {
			cloned := *baseCfg
			cloned.StaticHeaders = internalproxy.BuildEdgeRelayHeaders(baseCfg.BaseURL, cloned.StaticHeaders)
			cloned.BaseURL = relayURL
			return &cloned, nil
		}
	}

	return baseCfg, nil
}

// ExtractAPIKey gets the API key from a connection's data.
func ExtractAPIKey(connData *ConnectionData) string {
	return extractAPIKey(connData)
}

func extractAPIKey(connData *ConnectionData) string {
	if connData.APIKey != "" {
		return connData.APIKey
	}
	return connData.AccessToken
}

// NormalizeProviderToken normalizes credentials for providers with specific token requirements.
// Only Cline OAuth tokens — WorkOS JWTs (base64url "eyJ…" with a dot) — take
// the "workos:" prefix; ClinePass API keys (e.g. "clp_…") ride plain Bearer
// (upstream parity: open-sse/shared/clineAuth.js getClineAccessToken).
func NormalizeProviderToken(provider, token string) string {
	if (provider == "cline" || provider == "clinepass") && token != "" {
		t := strings.TrimSpace(token)
		if isClineWorkOSJWT(t) {
			return "workos:" + t
		}
		return t
	}
	return token
}

// isClineWorkOSJWT reports whether token looks like a Cline OAuth WorkOS JWT:
// base64url "eyJ…" header followed by a dot (upstream parity:
// open-sse/shared/clineAuth.js getClineAccessToken). Non-JWT ClinePass API
// keys (e.g. "clp_…") fail this check and ride plain Bearer.
func isClineWorkOSJWT(token string) bool {
	if strings.HasPrefix(token, "workos:") || !strings.HasPrefix(token, "eyJ") {
		return false
	}
	dot := strings.IndexByte(token, '.')
	if dot <= 3 {
		return false
	}
	for i := 0; i < dot; i++ {
		c := token[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func extractAssignedModel(dataStr string) string {
	if dataStr == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(dataStr), &m); err != nil {
		return ""
	}
	if am, ok := m["assignedModel"].(string); ok && am != "" {
		return am
	}
	if fm, ok := m["freebuffModel"].(string); ok && fm != "" {
		return fm
	}
	if psd, ok := m["providerSpecificData"].(map[string]any); ok {
		if am, ok := psd["assignedModel"].(string); ok && am != "" {
			return am
		}
		if fm, ok := psd["freebuffModel"].(string); ok && fm != "" {
			return fm
		}
	}
	return ""
}

func filterConnectionsForModel(provider string, connections []*models.ProviderConnection, model string, settings *db.SettingsData) []*models.ProviderConnection {
	if settings == nil || settings.ProviderStrategies == nil || model == "" {
		return connections
	}
	strat, ok := settings.ProviderStrategies[provider]
	if !ok || !strat.StrictModelAssignment {
		return connections
	}

	var filtered []*models.ProviderConnection
	for _, c := range connections {
		if c == nil {
			continue
		}
		if extractAssignedModel(c.Data) == model {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// canonicalLockModel normalizes model names for providers sharing a backend
// quota/capacity pool (such as Antigravity gemini-3.8-flash-low/high -> gemini-3.8-flash-tiered).
func canonicalLockModel(provider, model string) string {
	if model == "" {
		return ""
	}
	if provider == "antigravity" {
		return translator.NormalizeAntigravityModel(model)
	}
	return model
}

// ApplyConnectionStrategy rotates candidate connections according to the provider's configured strategy.
func (h *ChatHandler) ApplyConnectionStrategy(provider string, conns []*models.ProviderConnection, strat db.ProviderStrategy) []*models.ProviderConnection {
	return h.applyConnectionStrategy(provider, conns, strat)
}

// applyConnectionStrategy rotates candidate connections according to the provider's configured strategy.
//
// The sticky half is gated on routing.sticky-sessions, like the combo-side gate in
// applyComboStrategy: with the flag off, sticky rotation is reduced to one request
// per connection, so load still spreads instead of pinning to the first
// connection. Gating only the combo side would leave a provider configured for
// sticky rotation still sticking while the operator had switched stickiness off.
func (h *ChatHandler) applyConnectionStrategy(provider string, conns []*models.ProviderConnection, strat db.ProviderStrategy) []*models.ProviderConnection {
	return h.applyConnectionStrategyWithFlags(provider, conns, strat, h.featureFlagOn(flagStickySessions))
}

// applyConnectionStrategyWithFlags is applyConnectionStrategy with the
// stickiness decision passed in, so tests can exercise both states without a
// settings row and the exported wrapper keeps its original signature.
func (h *ChatHandler) applyConnectionStrategyWithFlags(provider string, conns []*models.ProviderConnection, strat db.ProviderStrategy, stickyEnabled bool) []*models.ProviderConnection {
	if len(conns) <= 1 {
		return conns
	}
	if !stickyEnabled {
		// One request per connection: rotation continues, stickiness does not.
		strat.StickyLimit = 1
	}

	strategy := strings.ToLower(strings.TrimSpace(strat.RotateStrategy))
	switch strategy {
	case "round-robin", "roundrobin":
		stickyLimit := 1
		if strat.StickyLimit > 0 {
			stickyLimit = strat.StickyLimit
		}
		return h.rotateConnectionsSticky(provider, conns, stickyLimit)

	case "sticky":
		stickyLimit := strat.StickyLimit
		if stickyLimit <= 0 {
			stickyLimit = 1
		}
		return h.rotateConnectionsSticky(provider, conns, stickyLimit)

	case "random":
		offset := rand.IntN(len(conns))
		rotated := make([]*models.ProviderConnection, len(conns))
		for i := range conns {
			rotated[i] = conns[(offset+i)%len(conns)]
		}
		return rotated

	default:
		// "none", "fallback", or empty: keep DB priority order
		return conns
	}
}

func (h *ChatHandler) rotateConnectionsSticky(provider string, conns []*models.ProviderConnection, stickyLimit int) []*models.ProviderConnection {
	h.stickyMu.Lock()
	defer h.stickyMu.Unlock()
	if h.stickyState == nil {
		h.stickyState = make(map[string]*comboStickyState)
	}

	key := "conn:" + provider
	state, exists := h.stickyState[key]
	if !exists {
		state = &comboStickyState{}
		h.stickyState[key] = state
	}

	// Resolve the serving slot by connection identity, not by slice position.
	//
	// The candidate slice is not stable between requests: it comes from
	// GetProviderConnections, which orders by `priority ASC, updatedAt DESC`,
	// and a successful forward bumps the serving connection's updatedAt
	// (UnlockConnectionModel, called from the success path). A positional
	// pointer therefore stops meaning "the connection that is serving" as soon
	// as traffic reorders the pool, and the next request lands on the wrong
	// account — skipping one and repeating another.
	//
	// Holding the identity makes rotation a property of the connections
	// themselves, so a reorder cannot move the slot. If the pinned connection
	// is gone (deleted, or deactivated out of the pool) the rotation restarts
	// at the head of the current pool, which is the same place a fresh
	// handler would start.
	servingIndex := 0
	if state.ServingID != "" {
		if p := connectionIndexByID(conns, state.ServingID); p >= 0 {
			servingIndex = p
		}
	}
	state.ServingID = conns[servingIndex].ID

	state.ConsecutiveUseCount++
	if state.ConsecutiveUseCount >= stickyLimit {
		state.ServingID = conns[(servingIndex+1)%len(conns)].ID
		state.ConsecutiveUseCount = 0
	}
	state.ServingIndex = servingIndex

	rotated := make([]*models.ProviderConnection, len(conns))
	for i := range conns {
		rotated[i] = conns[(servingIndex+i)%len(conns)]
	}
	return rotated
}

// connectionIndexByID returns the position of the connection with the given id,
// or -1 when it is not part of the current candidate pool.
func connectionIndexByID(conns []*models.ProviderConnection, id string) int {
	for i, c := range conns {
		if c != nil && c.ID == id {
			return i
		}
	}
	return -1
}

// ResetConnectionState clears rotation state for a provider (or all providers if provider="").
func (h *ChatHandler) ResetConnectionState(provider string) {
	h.stickyMu.Lock()
	defer h.stickyMu.Unlock()
	if h.stickyState == nil {
		return
	}
	if provider == "" {
		for k := range h.stickyState {
			if strings.HasPrefix(k, "conn:") {
				delete(h.stickyState, k)
			}
		}
		return
	}
	delete(h.stickyState, "conn:"+provider)
}

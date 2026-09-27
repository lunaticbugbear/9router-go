package chat

import (
	"net/http"
	"sync"

	"9router/proxy/internal/db"
	"9router/proxy/internal/handlers/shared"
	"9router/proxy/internal/proxy"
)

type comboStickyState struct {
	Index               int
	ConsecutiveUseCount int
	// ServingIndex is the model index currently owning the turn. Mid-turn
	// requests reuse it so a tool-use sequence stays on the same provider,
	// even after Index has advanced for the next turn.
	ServingIndex int
	// ServingID is the connection currently owning the rotation slot, keyed by
	// identity rather than position. Provider-connection rotation uses it
	// instead of Index because the candidate order is not stable across
	// requests: GetProviderConnections orders by updatedAt DESC, and a
	// successful forward bumps the serving connection's updatedAt (see
	// UnlockConnectionModel), so the pool can reorder between two calls. A
	// positional pointer would then skip or repeat connections.
	ServingID string
}

// ChatHandler handles /v1/chat/completions (OpenAI) and /v1/messages (Claude) endpoints.
type ChatHandler struct {
	Repo        *db.Repo
	Client      *http.Client
	TokenSaver  *shared.TokenSaverConfig
	stickyMu    sync.Mutex
	stickyState map[string]*comboStickyState
}

// Type aliases for shared types
type ModelInfo = shared.ModelInfo
type ConnectionData = shared.ConnectionData
type UsageLogInfo = shared.UsageLogInfo
type streamMetrics = shared.StreamMetrics
type upstreamError = proxy.UpstreamError

package dashboard

import (
	"io"
	"net/http"
	"strings"

	json "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"9router/proxy/internal/clisetup"
	"9router/proxy/internal/handlerutil"
)

// HandleCliToolConfigure handles POST /api/cli-tools/{tool}/configure: writes
// the gateway base URL, a client API key and the chosen model(s) into the
// tool's own config file (original 9router one-click installer).
func (h *DashboardHandler) HandleCliToolConfigure(w http.ResponseWriter, r *http.Request) {
	tool, ok := clisetup.Lookup(chi.URLParam(r, "tool"))
	if !ok {
		writePlainError(w, http.StatusNotFound, "no installer for this tool")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writePlainError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	var req struct {
		BaseURL string   `json:"baseUrl"`
		Model   string   `json:"model"`
		Models  []string `json:"models"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writePlainError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	models := req.Models
	if strings.TrimSpace(req.Model) != "" {
		models = append([]string{req.Model}, models...)
	}

	key, created, err := h.cliGatewayKey()
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "could not prepare an API key: "+err.Error())
		return
	}
	res, err := tool.Apply(clisetup.Options{BaseURL: req.BaseURL, APIKey: key, Models: models})
	if err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"message":    res.Message,
		"configPath": res.ConfigPath,
		"backupPath": res.BackupPath,
		"keyCreated": created,
	})
}

// HandleCliToolReset handles DELETE /api/cli-tools/{tool}/configure: removes
// only the 9router-managed settings from the tool's config.
func (h *DashboardHandler) HandleCliToolReset(w http.ResponseWriter, r *http.Request) {
	tool, ok := clisetup.Lookup(chi.URLParam(r, "tool"))
	if !ok {
		writePlainError(w, http.StatusNotFound, "no installer for this tool")
		return
	}
	res, err := tool.Reset()
	if err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	handlerutil.WriteJSON(w, http.StatusOK, map[string]any{"success": true, "message": res.Message, "configPath": res.ConfigPath})
}

// cliGatewayKey returns the first active client key, creating one when the
// install has none (client routes always require a stored key).
func (h *DashboardHandler) cliGatewayKey() (key string, created bool, err error) {
	keys, err := h.Repo.GetApiKeys()
	if err != nil {
		return "", false, err
	}
	for _, k := range keys {
		if k.IsActive == 1 && k.Key != "" {
			return k.Key, false, nil
		}
	}
	key = "sk-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := h.Repo.CreateApiKey(uuid.NewString(), key, "CLI tools (auto)", ""); err != nil {
		return "", false, err
	}
	return key, true, nil
}

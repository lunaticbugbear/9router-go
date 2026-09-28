package dashboard

import (
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"9router/proxy/internal/db"
)

func TestHandleTunnelEndpoints(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()

	h := NewDashboardHandler(repo)

	t.Run("TunnelStatus_Empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tunnel/status", nil)
		rec := httptest.NewRecorder()

		h.HandleTunnelStatus(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var res struct {
			Tunnel struct {
				Enabled   bool   `json:"enabled"`
				TunnelURL string `json:"tunnelUrl"`
				Running   bool   `json:"running"`
			} `json:"tunnel"`
			Tailscale struct {
				Enabled bool `json:"enabled"`
			} `json:"tailscale"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal status response: %v", err)
		}

		if res.Tunnel.Enabled || res.Tunnel.Running {
			t.Errorf("expected tunnel to be disabled and not running initially")
		}
	})

	t.Run("TunnelStatus_WithSettings", func(t *testing.T) {
		_ = repo.UpdateSettingsRaw(map[string]any{
			"tunnelEnabled": true,
			"tunnelUrl":     "https://test.trycloudflare.com",
		})

		req := httptest.NewRequest(http.MethodGet, "/api/tunnel/status", nil)
		rec := httptest.NewRecorder()

		h.HandleTunnelStatus(rec, req)

		var res struct {
			Tunnel struct {
				Enabled   bool   `json:"enabled"`
				TunnelURL string `json:"tunnelUrl"`
				Running   bool   `json:"running"`
			} `json:"tunnel"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal status response: %v", err)
		}

		if !res.Tunnel.Enabled || !res.Tunnel.Running {
			t.Errorf("expected tunnel to be enabled and running when url is configured")
		}
		if res.Tunnel.TunnelURL != "https://test.trycloudflare.com" {
			t.Errorf("unexpected tunnel URL: %s", res.Tunnel.TunnelURL)
		}
	})

	t.Run("TunnelEnable_ReturnsCleanError", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tunnel/enable", nil)
		rec := httptest.NewRecorder()

		h.HandleTunnelEnable(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}

		var res struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if res.Error == "" {
			t.Errorf("expected non-empty error message")
		}
	})

	t.Run("TunnelDisable_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tunnel/disable", nil)
		rec := httptest.NewRecorder()

		h.HandleTunnelDisable(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var res struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal disable response: %v", err)
		}
		if !res.Success {
			t.Errorf("expected success=true")
		}
	})

	t.Run("TailscaleCheck", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tunnel/tailscale-check", nil)
		rec := httptest.NewRecorder()

		h.HandleTailscaleCheck(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var res struct {
			Installed bool `json:"installed"`
			LoggedIn  bool `json:"loggedIn"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal check response: %v", err)
		}
	})

	t.Run("TailscaleEnable_ReturnsCleanError", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tunnel/tailscale-enable", nil)
		rec := httptest.NewRecorder()

		h.HandleTailscaleEnable(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", rec.Code)
		}

		var res struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal error response: %v", err)
		}
		if res.Error == "" {
			t.Errorf("expected non-empty error message")
		}
	})

	t.Run("TailscaleDisable_Success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tunnel/tailscale-disable", nil)
		rec := httptest.NewRecorder()

		h.HandleTailscaleDisable(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d", rec.Code)
		}

		var res struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("unmarshal disable response: %v", err)
		}
		if !res.Success {
			t.Errorf("expected success=true")
		}
	})
}

// blockSettingsWrites installs a BEFORE UPDATE trigger on the settings row so
// every UPDATE aborts. UpdateSettingsRaw writes with
// `INSERT ... ON CONFLICT(id) DO UPDATE`, which fires it, so the repository
// returns an error while the stored row keeps its previous value — the exact
// shape of a write that never landed.
func blockSettingsWrites(t *testing.T, repo *db.Repo) {
	t.Helper()
	if _, err := repo.RawDB().Exec(
		`CREATE TRIGGER block_settings_update BEFORE UPDATE ON settings
		 BEGIN SELECT RAISE(ABORT, 'settings write blocked by test trigger'); END`,
	); err != nil {
		t.Fatalf("create settings write trigger: %v", err)
	}
}

// seedTunnelSettings stores a tunnel/tailscale state the way a running gateway
// would, so a failed disable has something to leave behind.
func seedTunnelSettings(t *testing.T, repo *db.Repo) {
	t.Helper()
	if err := repo.UpdateSettingsRaw(map[string]any{
		"tunnelEnabled":    true,
		"tunnelUrl":        "https://test.trycloudflare.com",
		"tailscaleEnabled": true,
		"tailscaleUrl":     "https://test.tailnet.ts.net",
	}); err != nil {
		t.Fatalf("seed tunnel settings: %v", err)
	}
}

// TestHandleTunnelDisable_FailedWriteIsNotSuccess pins the HIGH defect: the
// handler discarded the repository error and answered {"success": true}, so a
// disable that never reached the database looked like a disconnect.
func TestHandleTunnelDisable_FailedWriteIsNotSuccess(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	seedTunnelSettings(t, repo)
	blockSettingsWrites(t, repo)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/disable", nil)
	rec := httptest.NewRecorder()
	h.HandleTunnelDisable(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 for a failed write, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal disable response: %v", err)
	}
	if res.Success != nil && *res.Success {
		t.Errorf("expected no success=true when the write failed, got %s", rec.Body.String())
	}
	if res.Error == "" {
		t.Errorf("expected an error message, got %s", rec.Body.String())
	}

	// The row must still say the tunnel is up: the failure is real, not cosmetic.
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if enabled, _ := raw["tunnelEnabled"].(bool); !enabled {
		t.Errorf("tunnelEnabled = %v, want true (the blocked write must not have landed)", raw["tunnelEnabled"])
	}
}

// TestHandleTailscaleDisable_FailedWriteIsNotSuccess is the Tailscale twin.
func TestHandleTailscaleDisable_FailedWriteIsNotSuccess(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	seedTunnelSettings(t, repo)
	blockSettingsWrites(t, repo)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/tailscale-disable", nil)
	rec := httptest.NewRecorder()
	h.HandleTailscaleDisable(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500 for a failed write, got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal disable response: %v", err)
	}
	if res.Success != nil && *res.Success {
		t.Errorf("expected no success=true when the write failed, got %s", rec.Body.String())
	}
	if res.Error == "" {
		t.Errorf("expected an error message, got %s", rec.Body.String())
	}

	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if enabled, _ := raw["tailscaleEnabled"].(bool); !enabled {
		t.Errorf("tailscaleEnabled = %v, want true (the blocked write must not have landed)", raw["tailscaleEnabled"])
	}
	if url, _ := raw["tailscaleUrl"].(string); url != "https://test.tailnet.ts.net" {
		t.Errorf("tailscaleUrl = %q, want the seeded URL unchanged", url)
	}
}

// TestHandleTunnelDisable_SuccessStillReportsSuccess guards the other half: a
// write that lands keeps answering success, so the new error path did not turn
// the endpoint into a permanent failure.
func TestHandleTunnelDisable_SuccessStillReportsSuccess(t *testing.T) {
	repo, cleanup := setupTestDB(t)
	defer cleanup()
	h := NewDashboardHandler(repo)

	seedTunnelSettings(t, repo)

	req := httptest.NewRequest(http.MethodPost, "/api/tunnel/disable", nil)
	rec := httptest.NewRecorder()
	h.HandleTunnelDisable(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	raw, err := repo.GetSettingsRaw()
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if enabled, _ := raw["tunnelEnabled"].(bool); enabled {
		t.Errorf("tunnelEnabled = %v, want false after a successful disable", raw["tunnelEnabled"])
	}
}

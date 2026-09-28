package middleware

import (
	"net/http"

	"9router/proxy/internal/auth"
	"9router/proxy/internal/db"
	"9router/proxy/internal/handlerutil"
)

// RequireDashboardAuth gates the dashboard REST API behind the login session
// when requireLogin is on. It mirrors upstream dashboardGuard: a request is let
// through when requireLogin is disabled, when it carries a valid auth_token
// cookie, when it presents the local CLI token header, or when it authenticates
// with a valid API key (so CLI tools and the built-in dashboard key keep
// working). Anything else gets a flat { error: "Unauthorized" } 401.
// IsAlwaysProtectedPath reports whether the path requires full admin session or CLI token,
// mirroring upstream ALWAYS_PROTECTED in src/dashboardGuard.js.
// Standard client API keys and requireLogin=false are forbidden here.
func IsAlwaysProtectedPath(path string) bool {
	switch path {
	case "/api/shutdown",
		"/api/settings/database",
		"/api/version/shutdown",
		"/api/version/update",
		"/admin/health/reset",
		"/api/oauth/cursor/auto-import",
		"/api/oauth/kiro/auto-import":
		return true
	default:
		return false
	}
}

// IsPublicDashboardPath reports whether the path is exempt from dashboard login requirements.
func IsPublicDashboardPath(path string) bool {
	return path == "/api/oauth/antigravity/callback"
}

// RequireAdminAuth ensures that only requests with a valid dashboard session (auth_token cookie)
// or local CLI token (x-9r-cli-token) can proceed. Client API keys are rejected.
func RequireAdminAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.SessionValid(r) || auth.ValidCLIToken(r.Header.Get(auth.CLITokenHeader)) {
				next.ServeHTTP(w, r)
				return
			}
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized: admin session or CLI token required"})
		})
	}
}

func RequireDashboardAuth(repo *db.Repo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsPublicDashboardPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			// Always-protected routes strictly require valid session cookie or CLI token (upstream parity).
			// API keys and requireLogin=false are forbidden here.
			if IsAlwaysProtectedPath(r.URL.Path) {
				if auth.SessionValid(r) || auth.ValidCLIToken(r.Header.Get(auth.CLITokenHeader)) {
					next.ServeHTTP(w, r)
					return
				}
				handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized: admin session or CLI token required"})
				return
			}

			if !auth.RequireLogin(repo) || auth.SessionValid(r) {
				next.ServeHTTP(w, r)
				return
			}
			if auth.ValidCLIToken(r.Header.Get(auth.CLITokenHeader)) {
				next.ServeHTTP(w, r)
				return
			}
			if key := ExtractApiKey(r); key != "" {
				if obj, err := repo.GetApiKeyByKey(key); err == nil && obj != nil && obj.IsActive == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}
			handlerutil.WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": "Unauthorized"})
		})
	}
}

// RequireDashboardPage redirects browser navigations to /login when login is
// required and the session cookie is missing, mirroring upstream's
// dashboardGuard redirect for /dashboard routes. A tunnel/tailscale host with
// dashboard access disabled is also bounced to /login (upstream parity).
func RequireDashboardPage(repo *db.Repo) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.RequireLogin(repo) && tunnelPageBlocked(repo, r) {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			if !auth.RequireLogin(repo) || auth.SessionValid(r) {
				next.ServeHTTP(w, r)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
		})
	}
}

// tunnelPageBlocked reports whether the navigation arrives via a
// tunnel/tailscale front door whose dashboard access is disabled. The decision
// is made from the real peer address, so a caller cannot waive the gate by
// sending a loopback Host header (see auth.TunnelLoginBlocked).
func tunnelPageBlocked(repo *db.Repo, r *http.Request) bool {
	raw, err := repo.GetSettingsRaw()
	if err != nil || raw == nil {
		return false
	}
	return auth.TunnelLoginBlocked(r, raw)
}

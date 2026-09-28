package auth

import (
	"net/http"
	"net/url"
	"strings"
)

// tunnelConfigured reports whether a tunnel/tailscale front door is configured.
// With none configured the gate is inert and must not affect anyone.
func tunnelConfigured(raw map[string]any) bool {
	for _, key := range []string{"tunnelUrl", "tailscaleUrl"} {
		if v, _ := raw[key].(string); strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// authorityMatchesTunnel reports whether the request authority (r.Host) names
// one of the configured tunnel/tailscale front doors.
func authorityMatchesTunnel(r *http.Request, raw map[string]any) bool {
	host := hostnameOf(r.Host)
	if host == "" {
		return false
	}
	for _, key := range []string{"tunnelUrl", "tailscaleUrl"} {
		rawURL, _ := raw[key].(string)
		if rawURL == "" {
			continue
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		if tunnelHost := hostnameOf(u.Hostname()); tunnelHost != "" && host == tunnelHost {
			return true
		}
	}
	return false
}

// TunnelLoginBlocked mirrors upstream's tunnel gate (login/route.js
// isTunnelRequest + dashboardGuard's dashboard branch): when tunnel dashboard
// access is not explicitly enabled, a request arriving via the configured
// tunnel/tailscale hostname must not log in (or view the dashboard).
//
// Upstream decides this from the Host header alone, which the caller supplies,
// so the gate was advisory: a caller who reached the gateway through the tunnel
// sent `Host: localhost`, the authority stopped matching the configured tunnel
// hostname, and the gate was skipped. The check therefore has two arms:
//
//  1. The authority names the configured front door — the honest case, and the
//     only signal that identifies a tunnel (see the residual note below).
//  2. The caller claims a loopback authority while the real peer address is not
//     loopback. That is a remote caller impersonating a local browser, which is
//     the exact shape of the bypass: the gate is not waived by asserting a local
//     Host.
//
// Arm 2 deliberately does not fire for a remote caller using its own honest
// authority (e.g. a LAN browser at http://10.0.0.5:20130). Disabling tunnel
// dashboard access says nothing about LAN access, which stays governed by
// requireLogin; blocking it here would strand operators who use both.
//
// Residual, stated plainly: a tunnel that terminates on this host and forwards
// to the loopback listener is indistinguishable by peer address from a local
// browser, so such traffic that also sends a loopback authority is still
// treated as local. Closing that case needs the front door to forward the
// client address under TRUST_PROXY/TRUST_CLOUDFLARE (making the peer
// non-loopback so arm 2 applies); it cannot be closed by inspecting headers the
// client controls.
func TunnelLoginBlocked(r *http.Request, raw map[string]any) bool {
	if v, ok := raw["tunnelDashboardAccess"].(bool); ok && v {
		return false
	}
	if authorityMatchesTunnel(r, raw) {
		return true
	}
	if !tunnelConfigured(raw) {
		return false
	}
	return claimsLoopbackButIsRemote(r)
}

// claimsLoopbackButIsRemote reports whether the caller presents a loopback
// authority while its real peer address is not loopback.
func claimsLoopbackButIsRemote(r *http.Request) bool {
	if IsLocalRequest(r) {
		return false
	}
	return IsLoopbackAddr(hostnameOf(r.Host))
}

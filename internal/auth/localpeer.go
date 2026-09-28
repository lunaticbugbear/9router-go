package auth

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// isNumericPort reports whether port is a plain decimal port number, so a
// malformed address such as "127.0.0.1:not-a-port" is not split as host:port.
func isNumericPort(port string) bool {
	if port == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 0 && n <= 65535
}

// constantTimeEqual compares two secrets without leaking length or content
// through timing. An empty expectation never matches.
func constantTimeEqual(got, want string) bool {
	if want == "" || len(got) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Peer-proof headers. A trusted hop that terminates the connection in front of
// the gateway stamps the *real* client address here and proves it knows the
// per-process secret. The proof is what makes the address trustworthy: without
// it the header is ordinary attacker-supplied input. Mirrors upstream
// src/lib/auth/trustedPeer.js (`hasTrustedPeerHeaders`).
const (
	PeerTokenHeader  = "x-9r-peer-token"
	PeerRealIPHeader = "x-9r-real-ip"
)

// peerTokenProof reports whether the request carries the per-process peer
// secret, i.e. was stamped by a hop that knows NINEROUTER_PEER_TOKEN.
func peerTokenProof(r *http.Request) bool {
	token := os.Getenv("NINEROUTER_PEER_TOKEN")
	if token == "" {
		return false
	}
	return constantTimeEqual(r.Header.Get(PeerTokenHeader), token)
}

// proxyHeadersTrusted reports whether the operator opted into reading
// forwarded client addresses. Off by default: the headers are otherwise
// attacker-controlled, so they must never widen what counts as local.
func proxyHeadersTrusted() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("TRUST_PROXY")), "true") ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("TRUST_CLOUDFLARE")), "true")
}

// forwardedClientAddr returns the client address a trusted hop claims, or ""
// when nothing on this request authorises reading a forwarded address. This is
// the single place that decides whether forwarded headers may be believed;
// both the login limiter and the local-caller check go through it so the two
// can never disagree about who the caller is.
func forwardedClientAddr(r *http.Request) string {
	if peerTokenProof(r) {
		if ip := strings.TrimSpace(r.Header.Get(PeerRealIPHeader)); ip != "" {
			return ip
		}
	}
	if proxyHeadersTrusted() {
		if cfIP := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cfIP != "" {
			return cfIP
		}
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first, _, _ := strings.Cut(xff, ","); strings.TrimSpace(first) != "" {
				return strings.TrimSpace(first)
			}
		}
	}
	return ""
}

// ClientAddr returns the address of the machine that actually made the request:
// the forwarded client address when a configured trust says it may be believed,
// otherwise the unspoofable TCP peer in RemoteAddr. Never falls back to a
// client-supplied header.
func ClientAddr(r *http.Request) string {
	if fwd := forwardedClientAddr(r); fwd != "" {
		return fwd
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return strings.Trim(strings.TrimSpace(host), "[]")
}

// IsLoopbackAddr reports whether addr names the loopback interface. addr may be
// a bare IP, an IP:port pair, a bracketed IPv6 literal, a 4-in-6 form
// (::ffff:127.0.0.1) or the literal name "localhost". Splitting on the first
// colon would reduce every IPv6 form to "", so a dual-stack listener handing
// back ::1 would not read as loopback.
func IsLoopbackAddr(addr string) bool {
	name := strings.ToLower(strings.TrimSpace(addr))
	if name == "" {
		return false
	}
	// Unwrap a bracketed IPv6 literal, with or without a trailing port.
	if strings.HasPrefix(name, "[") {
		if end := strings.Index(name, "]"); end != -1 {
			name = name[1:end]
		}
	}
	name = stripIPv6Zone(name)
	if name == "localhost" {
		return true
	}
	// A bare IP (covers plain IPv4, bare IPv6 and 4-in-6 forms).
	if ip := net.ParseIP(name); ip != nil {
		return ip.IsLoopback()
	}
	// host:port (IPv4 or a bracketed literal already unwrapped above).
	if host, port, err := net.SplitHostPort(name); err == nil && isNumericPort(port) {
		host = stripIPv6Zone(strings.Trim(host, "[]"))
		if host == "localhost" {
			return true
		}
		if ip := net.ParseIP(host); ip != nil {
			return ip.IsLoopback()
		}
	}
	// Last resort: strip a trailing numeric port from an unbracketed address,
	// so an IPv6 peer written as ::ffff:127.0.0.1:1234 still parses.
	if i := strings.LastIndex(name, ":"); i != -1 {
		if _, err := strconv.Atoi(name[i+1:]); err == nil {
			if ip := net.ParseIP(stripIPv6Zone(name[:i])); ip != nil {
				return ip.IsLoopback()
			}
		}
	}
	return false
}

// stripIPv6Zone drops a trailing zone identifier (fe80::1%en0).
func stripIPv6Zone(name string) string {
	if i := strings.LastIndex(name, "%"); i != -1 {
		return name[:i]
	}
	return name
}

// hostnameOf normalizes a Host header or URL host to a bare lowercased
// hostname (port, brackets and trailing dot removed).
func hostnameOf(host string) string {
	name := strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(name); err == nil {
		name = h
	}
	return strings.TrimSuffix(strings.Trim(name, "[]"), ".")
}

// IsLocalRequest reports whether the request came from this machine itself.
//
// The decision rests on the real peer address (ClientAddr), never on a
// client-supplied header: `Host` is whatever the caller types, so trusting it
// lets any remote caller claim to be local by sending `Host: localhost`. A
// forwarded address is honoured only when an existing trust configuration
// authorises it (see forwardedClientAddr), so an untrusted header can never
// widen the local set.
//
// A loopback peer is necessary but not sufficient. A page served from a remote
// origin inside the operator's own browser can still reach the loopback
// listener, so when the request declares an Origin that Origin must also be
// loopback. Requests without an Origin (curl, the CLI) are unaffected.
func IsLocalRequest(r *http.Request) bool {
	if !IsLoopbackAddr(ClientAddr(r)) {
		return false
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return IsLoopbackAddr(u.Hostname())
	}
	return true
}

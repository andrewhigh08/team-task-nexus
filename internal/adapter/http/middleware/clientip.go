package middleware

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the address of the client that sent r: by default the peer address of the
// connection. With trustProxy it is the first valid address in X-Forwarded-For, then
// X-Real-IP, and the peer address only when neither holds one. Enable trustProxy only behind a
// proxy that overwrites both headers; otherwise every client can pick its own address.
// IPv4-mapped IPv6 addresses are returned as IPv4 and zones are dropped. The result is the
// zero Addr when not even the peer address parses.
func ClientIP(r *http.Request, trustProxy bool) netip.Addr {
	if trustProxy {
		if addr, ok := forwardedFor(r.Header.Values("X-Forwarded-For")); ok {
			return addr
		}
		if addr, ok := parseIP(r.Header.Get("X-Real-IP")); ok {
			return addr
		}
	}
	if addrPort, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return normalizeIP(addrPort.Addr())
	}
	addr, _ := parseIP(r.RemoteAddr)
	return addr
}

// forwardedFor returns the first valid address of the X-Forwarded-For header values, which
// list the client first and then every proxy the request passed.
func forwardedFor(values []string) (netip.Addr, bool) {
	for _, value := range values {
		for hop := range strings.SplitSeq(value, ",") {
			if addr, ok := parseIP(hop); ok {
				return addr, true
			}
		}
	}
	return netip.Addr{}, false
}

func parseIP(s string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return netip.Addr{}, false
	}
	return normalizeIP(addr), true
}

func normalizeIP(addr netip.Addr) netip.Addr {
	return addr.Unmap().WithZone("")
}

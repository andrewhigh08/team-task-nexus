package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/andrewhigh08/team-task-nexus/internal/adapter/http/middleware"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	const peer = "203.0.113.7:4711"
	tests := []struct {
		name         string
		remoteAddr   string
		forwardedFor []string
		realIP       string
		trustProxy   bool
		want         string // empty: no valid address
	}{
		{name: "peer address", remoteAddr: peer, want: "203.0.113.7"},
		{name: "ipv6 peer", remoteAddr: "[2001:db8::1]:4711", want: "2001:db8::1"},
		{name: "ipv4-mapped peer is unmapped", remoteAddr: "[::ffff:203.0.113.7]:4711", want: "203.0.113.7"},
		{name: "zone is dropped", remoteAddr: "[fe80::1%en0]:4711", want: "fe80::1"},
		{name: "peer without a port", remoteAddr: "203.0.113.7", want: "203.0.113.7"},
		{name: "malformed peer", remoteAddr: "pipe", want: ""},
		{
			name: "forwarding headers are ignored unless trusted", remoteAddr: peer,
			forwardedFor: []string{"198.51.100.1"}, realIP: "198.51.100.2", want: "203.0.113.7",
		},
		{
			name: "first forwarded address", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{"198.51.100.1, 10.0.0.1"}, realIP: "198.51.100.2", want: "198.51.100.1",
		},
		{
			name: "spaces around forwarded addresses", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{"  198.51.100.1 ,10.0.0.1"}, want: "198.51.100.1",
		},
		{
			name: "invalid forwarded entries are skipped", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{"unknown, 198.51.100.1:80, 198.51.100.3"}, want: "198.51.100.3",
		},
		{
			name: "forwarded addresses span header lines", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{"garbage", "198.51.100.4"}, want: "198.51.100.4",
		},
		{
			name: "forwarded ipv4-mapped address is unmapped", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{"::ffff:198.51.100.5"}, want: "198.51.100.5",
		},
		{
			name: "real ip when no forwarded address is valid", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{"unknown"}, realIP: " 2001:db8::2 ", want: "2001:db8::2",
		},
		{
			name: "peer when neither header holds an address", remoteAddr: peer, trustProxy: true,
			forwardedFor: []string{""}, realIP: "not-an-ip", want: "203.0.113.7",
		},
		{name: "peer when no header is sent", remoteAddr: peer, trustProxy: true, want: "203.0.113.7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/login", http.NoBody)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.forwardedFor {
				req.Header.Add("X-Forwarded-For", v)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}

			got := middleware.ClientIP(req, tt.trustProxy)

			if tt.want == "" {
				assert.False(t, got.IsValid(), "got %s", got)
				return
			}
			assert.Equal(t, tt.want, got.String())
		})
	}
}

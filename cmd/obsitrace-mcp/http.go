package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// runHTTP serves the MCP server over Streamable HTTP on every address until the
// process exits.
func runHTTP(server *mcp.Server, addrs []string, token string) error {
	if err := validateHTTPAddrs(addrs, token); err != nil {
		return err
	}

	handler := newHTTPHandler(server, token)
	servers := make([]*http.Server, 0, len(addrs))
	errCh := make(chan error, len(addrs))
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			closeHTTPServers(servers)
			return fmt.Errorf("listen on %s: %w", addr, err)
		}
		srv := &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		}
		// No WriteTimeout: SSE responses are long-lived.
		servers = append(servers, srv)
		go func() { errCh <- srv.Serve(ln) }()
	}

	err := <-errCh
	closeHTTPServers(servers)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve http: %w", err)
}

func closeHTTPServers(servers []*http.Server) {
	for _, srv := range servers {
		_ = srv.Close()
	}
}

// normalizeAddrs trims, drops empty values, and removes duplicates so the same
// address is not bound twice.
func normalizeAddrs(addrs []string) []string {
	seen := make(map[string]struct{}, len(addrs))
	normalized := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		normalized = append(normalized, addr)
	}
	return normalized
}

// validateHTTPAddrs runs the auth guard against every address before binding.
func validateHTTPAddrs(addrs []string, token string) error {
	for _, addr := range addrs {
		if err := validateHTTPConfig(addr, token); err != nil {
			return err
		}
	}
	return nil
}

// validateHTTPConfig refuses to expose the vault on a non-loopback address
// without authentication.
func validateHTTPConfig(addr, token string) error {
	if addr == "" || strings.TrimSpace(token) != "" || isLoopbackAddr(addr) {
		return nil
	}
	return fmt.Errorf("refusing to bind %q without --auth-token: non-loopback addresses require a bearer token", addr)
}

// isLoopbackAddr reports whether addr binds only loopback interfaces. An empty
// host (":8787") binds every interface, which is not loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// newHTTPHandler builds the Streamable HTTP handler, optionally wrapped in
// bearer authentication.
func newHTTPHandler(server *mcp.Server, token string) http.Handler {
	token = strings.TrimSpace(token)
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
			// Behind a reverse proxy the public Host header is not loopback,
			// which the SDK's DNS-rebinding guard rejects. The bearer token
			// covers the same risk, so only keep the guard without auth.
			DisableLocalhostProtection: token != "",
		},
	)
	if token == "" {
		return handler
	}

	verifier := func(_ context.Context, got string, _ *http.Request) (*auth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "local"}, nil
	}
	return auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		AllowMissingExpiration: true, // static token, no exp claim
	})(handler)
}

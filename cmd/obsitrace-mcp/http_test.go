package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Jnchk/obsitrace-mcp/internal/staging"
)

func TestValidateHTTPConfig(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		token   string
		wantErr bool
	}{
		{name: "stdio", addr: ""},
		{name: "loopback ipv4", addr: "127.0.0.1:8787"},
		{name: "loopback name", addr: "localhost:8787"},
		{name: "loopback ipv6", addr: "[::1]:8787"},
		{name: "all interfaces", addr: ":8787", wantErr: true},
		{name: "public address", addr: "203.0.113.5:8787", wantErr: true},
		{name: "all interfaces with token", addr: ":8787", token: "secret"},
		{name: "public address with token", addr: "203.0.113.5:8787", token: "secret"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHTTPConfig(tc.addr, tc.token)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateHTTPConfig(%q, %q) error = %v, wantErr %v", tc.addr, tc.token, err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeAddrs(t *testing.T) {
	got := normalizeAddrs([]string{" 127.0.0.1:8787 ", "", "127.0.0.1:8787", "192.168.1.10:8787"})
	want := []string{"127.0.0.1:8787", "192.168.1.10:8787"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("normalizeAddrs() = %v, want %v", got, want)
	}
	if empty := normalizeAddrs([]string{"", "  "}); len(empty) != 0 {
		t.Fatalf("normalizeAddrs(blank) = %v, want empty", empty)
	}
}

func TestValidateHTTPAddrs(t *testing.T) {
	loopback := []string{"127.0.0.1:8787", "localhost:8788"}
	if err := validateHTTPAddrs(loopback, ""); err != nil {
		t.Fatalf("validateHTTPAddrs(loopback) error = %v, want nil", err)
	}

	mixed := []string{"127.0.0.1:8787", "192.168.1.10:8787"}
	if err := validateHTTPAddrs(mixed, ""); err == nil {
		t.Fatal("validateHTTPAddrs(mixed, no token) = nil, want error")
	}
	if err := validateHTTPAddrs(mixed, "secret"); err != nil {
		t.Fatalf("validateHTTPAddrs(mixed, token) error = %v, want nil", err)
	}
}

func TestHTTPHandlerRequiresBearerToken(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	ts := httptest.NewServer(newHTTPHandler(server, "secret", nil))
	defer ts.Close()

	const body = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`

	post := func(authorization string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, ts.URL, strings.NewReader(body))
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		return resp.StatusCode, string(got)
	}

	if code, _ := post(""); code != http.StatusUnauthorized {
		t.Fatalf("request without token status = %d, want %d", code, http.StatusUnauthorized)
	}
	if code, _ := post("Bearer wrong"); code != http.StatusUnauthorized {
		t.Fatalf("request with wrong token status = %d, want %d", code, http.StatusUnauthorized)
	}

	code, got := post("Bearer secret")
	if code != http.StatusOK {
		t.Fatalf("request with token status = %d, body = %s", code, got)
	}
	if !strings.Contains(got, `"serverInfo"`) {
		t.Fatalf("initialize response missing serverInfo: %s", got)
	}
}

func TestUploadRoute(t *testing.T) {
	store, err := staging.New(t.TempDir(), time.Minute, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "t"}, nil)
	h := newHTTPHandler(server, "secret", store)

	// Without the bearer token the route is closed.
	req := httptest.NewRequest(http.MethodPost, "/upload?filename=x.md", strings.NewReader("body"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token status = %d", rec.Code)
	}

	// Missing filename.
	req = httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("body"))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing filename status = %d", rec.Code)
	}

	// Success returns the id that write consumes.
	req = httptest.NewRequest(http.MethodPost, "/upload?filename=doc.md", strings.NewReader("# 标题\n\n正文"))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var info staging.Info
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.ID == "" || info.Filename != "doc.md" {
		t.Fatalf("info = %#v", info)
	}
	data, err := store.Read(info.ID)
	if err != nil || string(data) != "# 标题\n\n正文" {
		t.Fatalf("staged = %q err=%v", data, err)
	}

	// Binary bodies are rejected.
	req = httptest.NewRequest(http.MethodPost, "/upload?filename=x.md", strings.NewReader("\xff\xfe"))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("binary status = %d", rec.Code)
	}
}

func TestMCPPaths(t *testing.T) {
	store, _ := staging.New(t.TempDir(), time.Minute, 1<<20)
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "t"}, nil)
	ts := httptest.NewServer(newHTTPHandler(server, "secret", store))
	defer ts.Close()

	// /mcp is canonical; "" is the compatibility alias kept for old clients.
	for _, path := range []string{"/mcp", ""} {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`))
		req.Header.Set("Authorization", "Bearer secret")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"serverInfo"`) {
			t.Fatalf("%q status = %d body = %s", path, res.StatusCode, body)
		}
	}
}

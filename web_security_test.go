package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentLoginFailuresAreRateLimitedAndSuccessClearsState(t *testing.T) {
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	now := time.Now()
	for attempt := 1; attempt < agentLoginFailureLimit; attempt++ {
		if retry := auth.recordLoginFailure("127.0.0.1", now); retry != 0 {
			t.Fatalf("attempt %d locked early for %v", attempt, retry)
		}
	}
	if retry := auth.recordLoginFailure("127.0.0.1", now); retry <= 0 {
		t.Fatal("failure limit did not lock the client")
	}
	if retry := auth.loginRetryAfter("127.0.0.1", now.Add(10*time.Second)); retry <= 0 {
		t.Fatal("locked client was allowed before lockout expired")
	}
	auth.recordLoginSuccess("127.0.0.1")
	if retry := auth.loginRetryAfter("127.0.0.1", now); retry != 0 {
		t.Fatalf("successful login did not clear lockout: %v", retry)
	}
}

func TestAgentRejectsCrossOriginMutation(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "http://agent.local/api/v1/session", nil)
	request.Host = "agent.local"
	request.Header.Set("Origin", "https://evil.test")
	if sameOriginAgentRequest(request) {
		t.Fatal("cross-origin mutation was accepted")
	}
	request.Header.Set("Origin", "http://agent.local")
	if !sameOriginAgentRequest(request) {
		t.Fatal("same-origin mutation was rejected")
	}
	request.Method = http.MethodGet
	request.Header.Set("Origin", "https://evil.test")
	if !sameOriginAgentRequest(request) {
		t.Fatal("read-only request was rejected")
	}
}

func TestAgentTrustsForwardedHeadersOnlyFromLoopbackProxy(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://agent.local/", nil)
	request.RemoteAddr = "127.0.0.1:4567"
	request.Header.Set("X-Forwarded-For", "203.0.113.9, 127.0.0.1")
	request.Header.Set("X-Forwarded-Proto", "https")
	if client := requestClientIP(request); client != "203.0.113.9" {
		t.Fatalf("proxied client IP = %q", client)
	}
	if !agentRequestSecure(request) {
		t.Fatal("loopback HTTPS proxy was not trusted")
	}
	request.RemoteAddr = "198.51.100.7:4567"
	if client := requestClientIP(request); client != "198.51.100.7" {
		t.Fatalf("untrusted forwarded client IP = %q", client)
	}
	if agentRequestSecure(request) {
		t.Fatal("non-loopback forwarded proto was trusted")
	}
}

func TestAgentHTTPLoginEnforcesOriginAndRateLimit(t *testing.T) {
	root := t.TempDir()
	app := &App{store: &Store{path: filepath.Join(root, "config.json")}}
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}
	crossOrigin := httptest.NewRequest(http.MethodPost, "http://agent.local/api/v1/session", bytes.NewBufferString(`{"password":"bad"}`))
	crossOrigin.Host = "agent.local"
	crossOrigin.RemoteAddr = "127.0.0.2:4567"
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://evil.test")
	crossResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossResponse, crossOrigin)
	if crossResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login status = %d", crossResponse.Code)
	}
	for attempt := 1; attempt <= agentLoginFailureLimit; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "http://agent.local/api/v1/session", bytes.NewBufferString(`{"password":"bad"}`))
		request.Host = "agent.local"
		request.RemoteAddr = "127.0.0.3:4567"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "http://agent.local")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusUnauthorized
		if attempt == agentLoginFailureLimit {
			want = http.StatusTooManyRequests
			if response.Header().Get("Retry-After") == "" {
				t.Fatal("rate-limited response has no Retry-After header")
			}
		}
		if response.Code != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.Code, want)
		}
	}
}

func TestAgentAuditPersistsMutatingRPCWithoutArguments(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PALSERVER_LAUNCHER_HOME", root)
	app := &App{store: &Store{path: filepath.Join(root, "config.json")}}
	auth := newAgentAuth("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	session, err := auth.createSession()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newAgentHTTPHandler(app, auth)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/rpc/SelectInstance", bytes.NewBufferString(`{"args":["srv-missing"]}`))
	request.RemoteAddr = "127.0.0.1:4567"
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: agentSessionCookie, Value: session})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("RPC status = %d, body=%s", response.Code, response.Body.String())
	}
	entries, err := app.ListAgentAudit(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Method != "SelectInstance" || entries[0].ServerID != "srv-missing" || entries[0].Successful || entries[0].RemoteIP != "127.0.0.1" {
		t.Fatalf("audit entries = %#v", entries)
	}
	data := response.Body.String()
	if strings.Contains(data, "0123456789abcdef") {
		t.Fatal("response unexpectedly exposed the administrator token")
	}
}

func TestAuditServerIDSupportsConfiguredAndStructuredIDs(t *testing.T) {
	for _, test := range []struct {
		name string
		args []json.RawMessage
		want string
	}{
		{name: "generated", args: []json.RawMessage{json.RawMessage(`"srv-0123456789abcdef"`)}, want: "srv-0123456789abcdef"},
		{name: "imported", args: []json.RawMessage{json.RawMessage(`"preview-server"`)}, want: "preview-server"},
		{name: "instance object", args: []json.RawMessage{json.RawMessage(`{"id":"server-1"}`)}, want: "server-1"},
		{name: "settings object", args: []json.RawMessage{json.RawMessage(`{"serverId":"server-2"}`)}, want: "server-2"},
		{name: "unsafe", args: []json.RawMessage{json.RawMessage(`"server\\nsecret"`)}, want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := auditServerID(test.args); got != test.want {
				t.Fatalf("auditServerID() = %q, want %q", got, test.want)
			}
		})
	}
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gin "github.com/gin-gonic/gin"
	proxyconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	sdkaccess "github.com/router-for-me/CLIProxyAPI/v6/sdk/access"
	sdkhandlers "github.com/router-for-me/CLIProxyAPI/v6/sdk/api/handlers"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	coreexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdkconfig "github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
)

func performManagementRequest(method string, path string, body []byte, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if body == nil {
		c.Request = httptest.NewRequest(method, path, nil)
	} else {
		c.Request = httptest.NewRequest(method, path, strings.NewReader(string(body)))
		c.Request.Header.Set("Content-Type", "application/json")
	}
	handler(c)
	return rec
}

func countConfigDerivedOpenAICompatAuths(items []*auth.Auth) (active int, disabled int) {
	for _, item := range items {
		if item == nil || item.Attributes == nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(item.Attributes["source"]))
		if !strings.HasPrefix(source, "config:") {
			continue
		}
		if strings.TrimSpace(item.Attributes["compat_name"]) == "" && strings.TrimSpace(item.Attributes["provider_key"]) == "" {
			continue
		}
		if item.Disabled || item.Status == auth.StatusDisabled {
			disabled++
			continue
		}
		active++
	}
	return active, disabled
}

type deadlineTrackingWriter struct {
	gin.ResponseWriter
	deadlines []time.Time
}

func (w *deadlineTrackingWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *deadlineTrackingWriter) sawZeroDeadline() bool {
	for i := range w.deadlines {
		if w.deadlines[i].IsZero() {
			return true
		}
	}
	return false
}

type staticResponseExecutor struct{}

func (e *staticResponseExecutor) Identifier() string { return "test-provider" }

func (e *staticResponseExecutor) Execute(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{Payload: []byte(`{"ok":true}`)}, nil
}

func (e *staticResponseExecutor) ExecuteStream(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (*coreexecutor.StreamResult, error) {
	return nil, errors.New("not implemented")
}

func (e *staticResponseExecutor) Refresh(ctx context.Context, file *auth.Auth) (*auth.Auth, error) {
	return file, nil
}

func (e *staticResponseExecutor) CountTokens(context.Context, *auth.Auth, coreexecutor.Request, coreexecutor.Options) (coreexecutor.Response, error) {
	return coreexecutor.Response{}, errors.New("not implemented")
}

func (e *staticResponseExecutor) HttpRequest(context.Context, *auth.Auth, *http.Request) (*http.Response, error) {
	return nil, errors.New("not implemented")
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return newTestServerWithConfig(t, nil)
}

func newTestServerWithConfig(t *testing.T, configure func(*proxyconfig.Config)) *Server {
	t.Helper()

	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{"test-key"},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
	}
	if configure != nil {
		configure(cfg)
	}

	authManager := auth.NewManager(nil, nil, nil)
	accessManager := sdkaccess.NewManager()

	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}
	return NewServer(cfg, authManager, accessManager, configPath)
}

func TestAmpProviderModelRoutes(t *testing.T) {
	testCases := []struct {
		name         string
		path         string
		wantStatus   int
		wantContains string
	}{
		{
			name:         "openai root models",
			path:         "/api/provider/openai/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "groq root models",
			path:         "/api/provider/groq/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "openai models",
			path:         "/api/provider/openai/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"object":"list"`,
		},
		{
			name:         "anthropic models",
			path:         "/api/provider/anthropic/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"data"`,
		},
		{
			name:         "google models v1",
			path:         "/api/provider/google/v1/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
		{
			name:         "google models v1beta",
			path:         "/api/provider/google/v1beta/models",
			wantStatus:   http.StatusOK,
			wantContains: `"models"`,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServer(t)

			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer test-key")

			rr := httptest.NewRecorder()
			server.engine.ServeHTTP(rr, req)

			if rr.Code != tc.wantStatus {
				t.Fatalf("unexpected status code for %s: got %d want %d; body=%s", tc.path, rr.Code, tc.wantStatus, rr.Body.String())
			}
			if body := rr.Body.String(); !strings.Contains(body, tc.wantContains) {
				t.Fatalf("response body for %s missing %q: %s", tc.path, tc.wantContains, body)
			}
		})
	}
}

func TestGroupedV1RouteConfigured(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.Routing.PathRoutes = []proxyconfig.RoutingPathRoute{
			{Path: "/pro", Group: "pro"},
		}
		cfg.SanitizeRouting()
	})

	req := httptest.NewRequest(http.MethodGet, "/pro/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestGroupedNestedV1RouteConfigured(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.Routing.PathRoutes = []proxyconfig.RoutingPathRoute{
			{Path: "/openai/pro", Group: "pro"},
		}
		cfg.SanitizeRouting()
	})

	req := httptest.NewRequest(http.MethodGet, "/openai/pro/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
}

func TestUpdateClientsDisablesRemovedConfigAuths(t *testing.T) {
	server := newTestServer(t)
	manager := server.handlers.AuthManager
	ctx := context.Background()

	for _, seed := range []*auth.Auth{
		{
			ID:       "claude-config-auth",
			Provider: "claude",
			Label:    "Claude API key",
			Status:   auth.StatusActive,
			Attributes: map[string]string{
				"source":  "config:claude[old]",
				"api_key": "sk-claude-old",
			},
		},
		{
			ID:       "kimi-config-auth",
			Provider: "kimi",
			Label:    "Kimi API key",
			Status:   auth.StatusActive,
			Attributes: map[string]string{
				"source":       "config:kimi[old]",
				"api_key":      "sk-kimi-old",
				"compat_name":  "Kimi",
				"provider_key": "kimi",
			},
		},
	} {
		if _, err := manager.Register(ctx, seed); err != nil {
			t.Fatalf("register auth %s: %v", seed.ID, err)
		}
	}

	next := *server.cfg
	next.ClaudeKey = nil
	server.UpdateClients(&next)

	for _, id := range []string{"claude-config-auth", "kimi-config-auth"} {
		updated, ok := manager.GetByID(id)
		if !ok {
			t.Fatalf("expected existing auth %s to remain as disabled runtime state", id)
		}
		if !updated.Disabled || updated.Status != auth.StatusDisabled {
			t.Fatalf("%s should be disabled after removal, got disabled=%t status=%s", id, updated.Disabled, updated.Status)
		}
	}
}

func TestManagementSaveHotAppliesOpenAICompatStateWithoutWatcher(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.OpenAICompatibility = []proxyconfig.OpenAICompatibility{
			{
				Name:    "OpenAI Main",
				BaseURL: "https://example.com/v1",
				APIKeyEntries: []proxyconfig.OpenAICompatibilityAPIKey{
					{APIKey: "sk-openai-a"},
					{APIKey: "sk-openai-b"},
				},
				Models: []proxyconfig.OpenAICompatibilityModel{{Name: "gpt-4.1"}},
			},
		}
		cfg.SanitizeOpenAICompatibility()
	})
	server.UpdateClients(server.cfg)

	activeBefore, disabledBefore := countConfigDerivedOpenAICompatAuths(server.handlers.AuthManager.List())
	if activeBefore == 0 || disabledBefore != 0 {
		t.Fatalf("expected active openai-compat auths before toggle, got active=%d disabled=%d", activeBefore, disabledBefore)
	}

	hookCalls := 0
	server.SetPostConfigMutationHook(func(updated *proxyconfig.Config) {
		hookCalls++
		if updated == nil || len(updated.OpenAICompatibility) != 1 {
			t.Fatalf("unexpected updated config in post hook: %#v", updated)
		}
		entries := updated.OpenAICompatibility[0].APIKeyEntries
		if len(entries) != 2 || !entries[0].Disabled || !entries[1].Disabled {
			t.Fatalf("expected post hook to observe disabled entries, got %#v", entries)
		}
	})

	disableBody := []byte(`[
		{
			"name": "OpenAI Main",
			"base-url": "https://example.com/v1",
			"api-key-entries": [
				{"api-key": "sk-openai-a", "disabled": true},
				{"api-key": "sk-openai-b", "disabled": true}
			],
			"models": [{"name": "gpt-4.1"}]
		}
	]`)
	rec := performManagementRequest(http.MethodPut, "/openai-compatibility", disableBody, server.mgmt.PutOpenAICompat)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", rec.Code, rec.Body.String())
	}
	if hookCalls != 1 {
		t.Fatalf("post hook calls after disable = %d, want 1", hookCalls)
	}

	activeAfterDisable, disabledAfterDisable := countConfigDerivedOpenAICompatAuths(server.handlers.AuthManager.List())
	if activeAfterDisable != 0 || disabledAfterDisable == 0 {
		t.Fatalf("expected hot-disabled compat auths after save, got active=%d disabled=%d", activeAfterDisable, disabledAfterDisable)
	}

	server.SetPostConfigMutationHook(func(updated *proxyconfig.Config) {
		hookCalls++
		if updated == nil || len(updated.OpenAICompatibility) != 1 {
			t.Fatalf("unexpected updated config in enable hook: %#v", updated)
		}
		entries := updated.OpenAICompatibility[0].APIKeyEntries
		if len(entries) != 2 || entries[0].Disabled || !entries[1].Disabled {
			t.Fatalf("expected post hook to observe one re-enabled entry, got %#v", entries)
		}
	})

	enableBody := []byte(`[
		{
			"name": "OpenAI Main",
			"base-url": "https://example.com/v1",
			"api-key-entries": [
				{"api-key": "sk-openai-a"},
				{"api-key": "sk-openai-b", "disabled": true}
			],
			"models": [{"name": "gpt-4.1"}]
		}
	]`)
	rec = performManagementRequest(http.MethodPut, "/openai-compatibility", enableBody, server.mgmt.PutOpenAICompat)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", rec.Code, rec.Body.String())
	}
	if hookCalls != 2 {
		t.Fatalf("post hook calls after re-enable = %d, want 2", hookCalls)
	}

	activeAfterEnable, _ := countConfigDerivedOpenAICompatAuths(server.handlers.AuthManager.List())
	if activeAfterEnable == 0 {
		t.Fatal("expected at least one active compat auth after re-enable")
	}
}
func TestGroupedV1RouteForbiddenByAPIKeyGroups(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.SDKConfig.APIKeys = nil
		cfg.SDKConfig.APIKeyEntries = []proxyconfig.APIKeyEntry{
			{Key: "test-key", AllowedChannelGroups: []string{"free"}},
		}
		cfg.Routing.PathRoutes = []proxyconfig.RoutingPathRoute{
			{Path: "/pro", Group: "pro"},
		}
		cfg.SanitizeRouting()
		cfg.SanitizeAPIKeyEntries()
	})

	req := httptest.NewRequest(http.MethodGet, "/pro/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "channel_group_forbidden") {
		t.Fatalf("expected channel_group_forbidden in body, got %s", rr.Body.String())
	}
}

func TestGroupedNestedV1RouteForbiddenByAPIKeyGroups(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.SDKConfig.APIKeys = nil
		cfg.SDKConfig.APIKeyEntries = []proxyconfig.APIKeyEntry{
			{Key: "test-key", AllowedChannelGroups: []string{"free"}},
		}
		cfg.Routing.PathRoutes = []proxyconfig.RoutingPathRoute{
			{Path: "/openai/plus", Group: "pro"},
		}
		cfg.SanitizeRouting()
		cfg.SanitizeAPIKeyEntries()
	})

	req := httptest.NewRequest(http.MethodGet, "/openai/plus/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "channel_group_forbidden") {
		t.Fatalf("expected channel_group_forbidden in body, got %s", rr.Body.String())
	}
}

func TestGroupedNestedResponsesSuccessReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tmpDir := t.TempDir()
	authDir := filepath.Join(tmpDir, "auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatalf("failed to create auth dir: %v", err)
	}

	cfg := &proxyconfig.Config{
		SDKConfig: sdkconfig.SDKConfig{
			APIKeys: []string{"test-key"},
		},
		Port:                   0,
		AuthDir:                authDir,
		Debug:                  true,
		LoggingToFile:          false,
		UsageStatisticsEnabled: false,
		Routing: proxyconfig.RoutingConfig{
			PathRoutes: []proxyconfig.RoutingPathRoute{
				{Path: "/openai/plus", Group: "pro"},
			},
		},
	}
	cfg.SanitizeRouting()

	authManager := auth.NewManager(nil, nil, nil)
	executor := &staticResponseExecutor{}
	authManager.RegisterExecutor(executor)

	authFile := &auth.Auth{
		ID:       "auth1",
		Provider: executor.Identifier(),
		Status:   auth.StatusActive,
		Prefix:   "pro",
	}
	if _, err := authManager.Register(context.Background(), authFile); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	registry.GetGlobalRegistry().RegisterClient(authFile.ID, authFile.Provider, []*registry.ModelInfo{{ID: "test-model"}})
	t.Cleanup(func() {
		registry.GetGlobalRegistry().UnregisterClient(authFile.ID)
	})

	accessManager := sdkaccess.NewManager()
	configPath := filepath.Join(tmpDir, "config.yaml")
	server := NewServer(cfg, authManager, accessManager, configPath)

	req := httptest.NewRequest(http.MethodPost, "/openai/plus/v1/responses", strings.NewReader(`{"model":"test-model"}`))
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if strings.TrimSpace(rr.Body.String()) != `{"ok":true}` {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestGroupedV1RouteUnknownGroupReturnsNotFound(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/missing/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestNewServerSetsMainWriteTimeout(t *testing.T) {
	server := newTestServer(t)
	if server.server == nil {
		t.Fatal("expected http server to be initialized")
	}
	if got := server.server.WriteTimeout; got != mainAPIServerWriteTimeout {
		t.Fatalf("WriteTimeout = %s, want %s", got, mainAPIServerWriteTimeout)
	}
}

func TestOAuthCallbackRouteStillServesSuccessHTML(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/codex/callback?state=session-1&code=auth-code", nil)
	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if !strings.Contains(rr.Body.String(), "Authentication successful") {
		t.Fatalf("expected success HTML, got %s", rr.Body.String())
	}
}

func TestClearServerWriteDeadlineUsesZeroDeadline(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	tracking := &deadlineTrackingWriter{ResponseWriter: c.Writer}
	c.Writer = tracking

	clearServerWriteDeadline(c)

	if !tracking.sawZeroDeadline() {
		t.Fatal("expected clearServerWriteDeadline to clear the write deadline")
	}
}

func TestGetContextWithCancelUsesRequestContextWhenParentNil(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-model"}`))
	reqCtx, cancelReq := context.WithCancel(req.Context())
	defer cancelReq()
	c.Request = req.WithContext(reqCtx)

	handler := &sdkhandlers.BaseAPIHandler{Cfg: &sdkconfig.SDKConfig{}}
	ctx, cancelHandler := handler.GetContextWithCancel(nil, c, nil)
	defer cancelHandler()

	cancelReq()

	select {
	case <-ctx.Done():
	case <-time.After(250 * time.Millisecond):
		t.Fatal("expected derived context to follow request cancellation when parent context is nil")
	}
}

func TestGetContextWithCancelClearsWriteDeadlineForStreamingRequests(t *testing.T) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"test-model","stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req
	tracking := &deadlineTrackingWriter{ResponseWriter: c.Writer}
	c.Writer = tracking

	handler := &sdkhandlers.BaseAPIHandler{Cfg: &sdkconfig.SDKConfig{}}
	_, cancelHandler := handler.GetContextWithCancel(nil, c, c.Request.Context())
	cancelHandler()

	if !tracking.sawZeroDeadline() {
		t.Fatal("expected streaming request to clear the server write deadline")
	}
}

func TestAttachWebsocketRouteClearsWriteDeadlineBeforeServingHandler(t *testing.T) {
	server := newTestServer(t)

	var sawZeroDeadline bool
	server.engine.Use(func(c *gin.Context) {
		tracker := &deadlineTrackingWriter{ResponseWriter: c.Writer}
		c.Writer = tracker
		c.Next()
		if c.FullPath() == "/v1/ws-test" {
			sawZeroDeadline = tracker.sawZeroDeadline()
		}
	})
	server.AttachWebsocketRoute("/v1/ws-test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/ws-test", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
	if !sawZeroDeadline {
		t.Fatal("expected websocket route to clear the write deadline before serving handler")
	}
}

func TestCORSMiddlewareRejectsUnconfiguredCrossOriginRequest(t *testing.T) {
	server := newTestServer(t)

	origin := "https://evil.example"
	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", origin)

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	var body struct {
		Error  string `json:"error"`
		Origin string `json:"origin"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode response body: %v; body=%s", err, rr.Body.String())
	}
	if body.Error != "origin not allowed" {
		t.Fatalf("error = %q, want %q", body.Error, "origin not allowed")
	}
	if body.Origin != origin {
		t.Fatalf("origin = %q, want %q", body.Origin, origin)
	}
}

func TestCORSMiddlewareAllowsConfiguredOrigin(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.CORSAllowOrigins = []string{"https://admin.example"}
	})

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", "https://admin.example")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestCORSMiddlewareAllowsChromeExtensionOriginByDefault(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", "chrome-extension://abcdefghijklmnop")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "chrome-extension://abcdefghijklmnop" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestCORSMiddlewareUsesUpdatedCORSAllowOrigins(t *testing.T) {
	server := newTestServer(t)

	reqBefore := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	reqBefore.Header.Set("Origin", "https://plugin.example")

	rrBefore := httptest.NewRecorder()
	server.engine.ServeHTTP(rrBefore, reqBefore)

	if rrBefore.Code != http.StatusForbidden {
		t.Fatalf("before status = %d, want %d; body=%s", rrBefore.Code, http.StatusForbidden, rrBefore.Body.String())
	}

	next := *server.cfg
	next.CORSAllowOrigins = []string{"https://plugin.example"}
	server.UpdateClients(&next)

	reqAfter := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	reqAfter.Header.Set("Origin", "https://plugin.example")

	rrAfter := httptest.NewRecorder()
	server.engine.ServeHTTP(rrAfter, reqAfter)

	if rrAfter.Code != http.StatusNoContent {
		t.Fatalf("after status = %d, want %d; body=%s", rrAfter.Code, http.StatusNoContent, rrAfter.Body.String())
	}
	if got := rrAfter.Header().Get("Access-Control-Allow-Origin"); got != "https://plugin.example" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestCORSMiddlewareAllowsConfiguredOriginForManagementAPI(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.CORSAllowOrigins = []string{"http://localhost:5173"}
		cfg.RemoteManagement.SecretKey = "test-secret"
		cfg.RemoteManagement.AllowRemote = true
	})

	req := httptest.NewRequest(http.MethodOptions, "/v0/management/config", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestManagementRemoteRestrictionIgnoresForgedForwardedFor(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.RemoteManagement.SecretKey = "test-secret"
		cfg.RemoteManagement.AllowRemote = false
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	req.RemoteAddr = "203.0.113.10:4321"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusForbidden, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "remote management disabled") {
		t.Fatalf("expected remote management disabled response, got %s", rr.Body.String())
	}
}

func TestServerTrustedProxiesAllowConfiguredReverseProxyClientIP(t *testing.T) {
	server := newTestServerWithConfig(t, func(cfg *proxyconfig.Config) {
		cfg.TrustedProxies = []string{"172.18.0.0/16"}
	})
	server.engine.GET("/test-client-ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/test-client-ip", nil)
	req.RemoteAddr = "172.18.0.1:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.55")
	req.Header.Set("X-Real-IP", "198.51.100.23")

	rr := httptest.NewRecorder()
	server.engine.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "203.0.113.55" {
		t.Fatalf("ClientIP() = %q, want forwarded client IP", got)
	}
}

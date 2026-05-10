package auth

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

type sequenceExecutor struct {
	mu       sync.Mutex
	execAuth []string
}

func (e *sequenceExecutor) Identifier() string { return "codex" }

func (e *sequenceExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts

	authID := ""
	if auth != nil {
		authID = auth.ID
	}

	e.mu.Lock()
	e.execAuth = append(e.execAuth, authID)
	e.mu.Unlock()

	if authID == "auth-a" {
		return cliproxyexecutor.Response{}, &Error{
			Code:       "upstream_failed",
			Message:    "upstream failed",
			HTTPStatus: http.StatusBadGateway,
		}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func (e *sequenceExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, &Error{Code: "not_implemented", Message: "ExecuteStream not implemented"}
}

func (e *sequenceExecutor) Refresh(ctx context.Context, auth *Auth) (*Auth, error) {
	return auth, nil
}

func (e *sequenceExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, &Error{Code: "not_implemented", Message: "CountTokens not implemented"}
}

func (e *sequenceExecutor) HttpRequest(context.Context, *Auth, *http.Request) (*http.Response, error) {
	return nil, &Error{Code: "not_implemented", Message: "HttpRequest not implemented", HTTPStatus: http.StatusNotImplemented}
}

func (e *sequenceExecutor) Calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.execAuth))
	copy(out, e.execAuth)
	return out
}

type successfulSequenceExecutor struct {
	sequenceExecutor
}

func (e *successfulSequenceExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts

	authID := ""
	if auth != nil {
		authID = auth.ID
	}

	e.mu.Lock()
	e.execAuth = append(e.execAuth, authID)
	e.mu.Unlock()

	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

type invalidModelExecutor struct {
	sequenceExecutor
}

func (e *invalidModelExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts

	authID := ""
	if auth != nil {
		authID = auth.ID
	}

	e.mu.Lock()
	e.execAuth = append(e.execAuth, authID)
	e.mu.Unlock()

	if authID == "auth-a" {
		return cliproxyexecutor.Response{}, &Error{
			Code:       "invalid_model",
			Message:    `{"detail":"The 'gpt-5.1-codex' model is not supported when using Codex with a ChatGPT account."}`,
			HTTPStatus: http.StatusBadRequest,
		}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

type kimiAccountAvailabilityExecutor struct {
	sequenceExecutor
}

func (e *kimiAccountAvailabilityExecutor) Identifier() string { return "kimi" }

func (e *kimiAccountAvailabilityExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts

	authID := ""
	if auth != nil {
		authID = auth.ID
	}

	e.mu.Lock()
	e.execAuth = append(e.execAuth, authID)
	e.mu.Unlock()

	if authID == "auth-a" {
		return cliproxyexecutor.Response{}, &Error{
			Message:    `{"error":{"message":"We're unable to verify your membership benefits at this time. Please ensure your membership is active.","type":"invalid_request_error"}}`,
			HTTPStatus: http.StatusBadRequest,
		}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

type firstAuthErrorExecutor struct {
	sequenceExecutor
	id         string
	failAuthID string
	status     int
	message    string
}

func (e *firstAuthErrorExecutor) Identifier() string { return e.id }

func (e *firstAuthErrorExecutor) Execute(ctx context.Context, auth *Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	_ = ctx
	_ = req
	_ = opts

	authID := ""
	if auth != nil {
		authID = auth.ID
	}

	e.mu.Lock()
	e.execAuth = append(e.execAuth, authID)
	e.mu.Unlock()

	if authID == e.failAuthID {
		return cliproxyexecutor.Response{}, &Error{
			Message:    e.message,
			HTTPStatus: e.status,
		}
	}
	return cliproxyexecutor.Response{Payload: []byte("ok")}, nil
}

func registerGroupedRouteTestAuths(t *testing.T, manager *Manager) {
	t.Helper()

	for _, auth := range []*Auth{
		{
			ID:       "auth-a",
			Provider: "codex",
			Status:   StatusActive,
			Prefix:   "pro",
			Metadata: map[string]any{"email": "a@example.com"},
		},
		{
			ID:       "auth-b",
			Provider: "codex",
			Status:   StatusActive,
			Prefix:   "pro",
			Metadata: map[string]any{"email": "b@example.com"},
		},
	} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}
}

func registerProviderRouteTestAuths(t *testing.T, manager *Manager, provider string, models ...string) (string, string) {
	t.Helper()

	firstID := provider + "-auth-a"
	secondID := provider + "-auth-b"
	reg := registry.GetGlobalRegistry()
	now := time.Now().Unix()
	modelInfos := make([]*registry.ModelInfo, 0, len(models))
	for _, model := range models {
		modelInfos = append(modelInfos, &registry.ModelInfo{ID: model, Created: now})
	}
	for _, auth := range []*Auth{
		{ID: firstID, Provider: provider, Status: StatusActive},
		{ID: secondID, Provider: provider, Status: StatusActive},
	} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
		if len(modelInfos) > 0 {
			reg.RegisterClient(auth.ID, provider, modelInfos)
			t.Cleanup(func(id string) func() {
				return func() { reg.UnregisterClient(id) }
			}(auth.ID))
		}
	}
	return firstID, secondID
}

func registerKimiRouteTestAuths(t *testing.T, manager *Manager) {
	t.Helper()

	reg := registry.GetGlobalRegistry()
	for _, auth := range []*Auth{
		{ID: "auth-a", Provider: "kimi", Status: StatusActive},
		{ID: "auth-b", Provider: "kimi", Status: StatusActive},
	} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
		reg.RegisterClient(auth.ID, "kimi", []*registry.ModelInfo{
			{ID: "kimi-for-coding", Created: time.Now().Unix()},
			{ID: "kimi-k2.6", Created: time.Now().Unix()},
		})
		t.Cleanup(func(id string) func() {
			return func() { reg.UnregisterClient(id) }
		}(auth.ID))
	}
}

func TestManagerExecute_GroupedRouteStopsAfterFirstFailure(t *testing.T) {
	t.Parallel()

	executor := &sequenceExecutor{}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)
	registerGroupedRouteTestAuths(t, manager)

	_, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{
		Metadata: map[string]any{cliproxyexecutor.RouteGroupMetadataKey: "pro"},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	calls := executor.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 upstream attempt, got %v", calls)
	}
	if calls[0] != "auth-a" {
		t.Fatalf("expected grouped route to use the first selected auth, got %v", calls)
	}
}

func TestManagerExecute_NonGroupedRouteStillFailsOver(t *testing.T) {
	t.Parallel()

	executor := &sequenceExecutor{}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)
	registerGroupedRouteTestAuths(t, manager)

	resp, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", string(resp.Payload), "ok")
	}

	calls := executor.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 upstream attempts, got %v", calls)
	}
	if calls[0] != "auth-a" || calls[1] != "auth-b" {
		t.Fatalf("expected failover sequence [auth-a auth-b], got %v", calls)
	}
}

func TestManagerExecute_ModelNotSupportedBadRequestDoesNotFailOver(t *testing.T) {
	t.Parallel()

	executor := &invalidModelExecutor{}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)
	registerGroupedRouteTestAuths(t, manager)

	_, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{})
	if err == nil {
		t.Fatal("expected error")
	}

	calls := executor.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 upstream attempt, got %v", calls)
	}
	if calls[0] != "auth-a" {
		t.Fatalf("expected first auth only, got %v", calls)
	}
}

func TestManagerExecute_KimiAccountInvalidRequestFailsOver(t *testing.T) {
	t.Parallel()

	executor := &kimiAccountAvailabilityExecutor{}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)
	registerKimiRouteTestAuths(t, manager)

	resp, err := manager.Execute(context.Background(), []string{"kimi"}, cliproxyexecutor.Request{Model: "kimi-for-coding"}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", string(resp.Payload), "ok")
	}

	calls := executor.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected failover after Kimi account error, got %v", calls)
	}
	if calls[0] != "auth-a" || calls[1] != "auth-b" {
		t.Fatalf("expected failover sequence [auth-a auth-b], got %v", calls)
	}

	updated, ok := manager.GetByID("auth-a")
	if !ok || updated == nil {
		t.Fatalf("expected auth-a to remain registered")
	}
	blocked, reason, _ := isAuthBlockedForModel(updated, "kimi-k2.6", time.Now())
	if !blocked {
		t.Fatalf("expected Kimi account error to block other aliases")
	}
	if reason != blockReasonOther {
		t.Fatalf("block reason = %v, want temporary block", reason)
	}
}

func TestManagerExecute_GenericAccountInvalidRequestFailsOver(t *testing.T) {
	t.Parallel()

	const model = "qwen3.5-plus"
	executor := &firstAuthErrorExecutor{
		id:      "qwen",
		status:  http.StatusBadRequest,
		message: `{"error":{"code":"invalid_api_key","message":"invalid access token or token expired","type":"invalid_request_error"}}`,
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)
	firstID, secondID := registerProviderRouteTestAuths(t, manager, "qwen", model, "qwen3.5-coder")
	executor.failAuthID = firstID

	resp, err := manager.Execute(context.Background(), []string{"qwen"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if string(resp.Payload) != "ok" {
		t.Fatalf("Execute() payload = %q, want %q", string(resp.Payload), "ok")
	}

	calls := executor.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected failover after account error, got %v", calls)
	}
	if calls[0] != firstID || calls[1] != secondID {
		t.Fatalf("expected failover sequence [%s %s], got %v", firstID, secondID, calls)
	}

	updated, ok := manager.GetByID(firstID)
	if !ok || updated == nil {
		t.Fatalf("expected %s to remain registered", firstID)
	}
	blocked, reason, _ := isAuthBlockedForModel(updated, "qwen3.5-coder", time.Now())
	if !blocked {
		t.Fatalf("expected account error to block other aliases")
	}
	if reason != blockReasonOther {
		t.Fatalf("block reason = %v, want temporary block", reason)
	}
}

func TestManagerExecute_RequestShapeInvalidRequestDoesNotFailOver(t *testing.T) {
	t.Parallel()

	const model = "claude-sonnet-4-6"
	executor := &firstAuthErrorExecutor{
		id:      "claude",
		status:  http.StatusBadRequest,
		message: `{"error":{"message":"message 0 content must not be empty","type":"invalid_request_error"}}`,
	}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.RegisterExecutor(executor)
	firstID, _ := registerProviderRouteTestAuths(t, manager, "claude", model)
	executor.failAuthID = firstID

	_, err := manager.Execute(context.Background(), []string{"claude"}, cliproxyexecutor.Request{Model: model}, cliproxyexecutor.Options{})
	if err == nil {
		t.Fatal("expected request-shape error")
	}

	calls := executor.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected request-shape error to stop after first auth, got %v", calls)
	}
	if calls[0] != firstID {
		t.Fatalf("expected first auth only, got %v", calls)
	}
}

func TestManagerExecute_GroupStrategyFillFirstOverridesGlobalRoundRobin(t *testing.T) {
	t.Parallel()

	executor := &successfulSequenceExecutor{}
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	manager.SetConfig(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy: "round-robin",
			ChannelGroups: []internalconfig.RoutingChannelGroup{
				{
					Name:     "pro",
					Strategy: "fill-first",
					Match: internalconfig.ChannelGroupMatch{
						Prefixes: []string{"pro"},
					},
				},
			},
		},
	})
	manager.RegisterExecutor(executor)
	registerGroupedRouteTestAuths(t, manager)

	for i := 0; i < 2; i++ {
		resp, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{
			Metadata: map[string]any{cliproxyexecutor.RouteGroupMetadataKey: "pro"},
		})
		if err != nil {
			t.Fatalf("Execute(%d) error = %v", i, err)
		}
		if string(resp.Payload) != "ok" {
			t.Fatalf("Execute(%d) payload = %q, want ok", i, string(resp.Payload))
		}
	}

	if calls := executor.Calls(); len(calls) != 2 || calls[0] != "auth-a" || calls[1] != "auth-a" {
		t.Fatalf("group fill-first should keep using first available auth, got %v", calls)
	}
}

func TestManagerExecute_GroupStrategyRoundRobinOverridesGlobalFillFirst(t *testing.T) {
	t.Parallel()

	executor := &successfulSequenceExecutor{}
	manager := NewManager(nil, &FillFirstSelector{}, nil)
	manager.SetConfig(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy: "fill-first",
			ChannelGroups: []internalconfig.RoutingChannelGroup{
				{
					Name:     "pro",
					Strategy: "round-robin",
					Match: internalconfig.ChannelGroupMatch{
						Prefixes: []string{"pro"},
					},
				},
			},
		},
	})
	manager.RegisterExecutor(executor)
	registerGroupedRouteTestAuths(t, manager)

	for i := 0; i < 2; i++ {
		resp, err := manager.Execute(context.Background(), []string{"codex"}, cliproxyexecutor.Request{}, cliproxyexecutor.Options{
			Metadata: map[string]any{cliproxyexecutor.RouteGroupMetadataKey: "pro"},
		})
		if err != nil {
			t.Fatalf("Execute(%d) error = %v", i, err)
		}
		if string(resp.Payload) != "ok" {
			t.Fatalf("Execute(%d) payload = %q, want ok", i, string(resp.Payload))
		}
	}

	if calls := executor.Calls(); len(calls) != 2 || calls[0] != "auth-a" || calls[1] != "auth-b" {
		t.Fatalf("group round-robin should rotate scoped route auths, got %v", calls)
	}
}

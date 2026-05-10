package auth

import (
	"context"
	"testing"
	"time"
)

func TestManager_ShouldRetryAfterError_RespectsAuthRequestRetryOverride(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(3, 30*time.Second)

	model := "test-model"
	next := time.Now().Add(5 * time.Second)

	auth := &Auth{
		ID:       "auth-1",
		Provider: "claude",
		Metadata: map[string]any{
			"request_retry": float64(0),
		},
		ModelStates: map[string]*ModelState{
			model: {
				Unavailable:    true,
				Status:         StatusError,
				NextRetryAfter: next,
			},
		},
	}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	_, maxWait := m.retrySettings()
	wait, shouldRetry := m.shouldRetryAfterError(&Error{HTTPStatus: 500, Message: "boom"}, 0, []string{"claude"}, model, maxWait, nil)
	if shouldRetry {
		t.Fatalf("expected shouldRetry=false for request_retry=0, got true (wait=%v)", wait)
	}

	auth.Metadata["request_retry"] = float64(1)
	if _, errUpdate := m.Update(context.Background(), auth); errUpdate != nil {
		t.Fatalf("update auth: %v", errUpdate)
	}

	wait, shouldRetry = m.shouldRetryAfterError(&Error{HTTPStatus: 500, Message: "boom"}, 0, []string{"claude"}, model, maxWait, nil)
	if !shouldRetry {
		t.Fatalf("expected shouldRetry=true for request_retry=1, got false")
	}
	if wait <= 0 {
		t.Fatalf("expected wait > 0, got %v", wait)
	}

	_, shouldRetry = m.shouldRetryAfterError(&Error{HTTPStatus: 500, Message: "boom"}, 1, []string{"claude"}, model, maxWait, nil)
	if shouldRetry {
		t.Fatalf("expected shouldRetry=false on attempt=1 for request_retry=1, got true")
	}
}

func TestManager_ShouldRetryAfterError_DisablesInternalRetryForGroupedRoute(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(3, 30*time.Second)

	model := "test-model"
	next := time.Now().Add(5 * time.Second)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "claude",
		ModelStates: map[string]*ModelState{
			model: {
				Unavailable:    true,
				Status:         StatusError,
				NextRetryAfter: next,
			},
		},
	}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	_, maxWait := m.retrySettings()
	meta := map[string]any{"route_group": "pro"}
	wait, shouldRetry := m.shouldRetryAfterError(&Error{HTTPStatus: 500, Message: "boom"}, 0, []string{"claude"}, model, maxWait, meta)
	if shouldRetry {
		t.Fatalf("expected shouldRetry=false for grouped route, got true (wait=%v)", wait)
	}
	if wait != 0 {
		t.Fatalf("expected wait=0 for grouped route, got %v", wait)
	}
}

func TestManager_ShouldRetryAfterError_DisablesInternalRetryForSinglePick(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.SetRetryConfig(3, 30*time.Second)

	model := "test-model"
	next := time.Now().Add(5 * time.Second)
	auth := &Auth{
		ID:       "auth-1",
		Provider: "codex",
		ModelStates: map[string]*ModelState{
			model: {
				Unavailable:    true,
				Status:         StatusError,
				NextRetryAfter: next,
			},
		},
	}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	_, maxWait := m.retrySettings()
	meta := map[string]any{"single_pick": true}
	wait, shouldRetry := m.shouldRetryAfterError(&Error{HTTPStatus: 500, Message: "boom"}, 0, []string{"codex"}, model, maxWait, meta)
	if shouldRetry {
		t.Fatalf("expected shouldRetry=false for single-pick request, got true (wait=%v)", wait)
	}
	if wait != 0 {
		t.Fatalf("expected wait=0 for single-pick request, got %v", wait)
	}
}

func TestManager_MarkResult_RespectsAuthDisableCoolingOverride(t *testing.T) {
	prev := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(prev) })

	m := NewManager(nil, nil, nil)

	auth := &Auth{
		ID:       "auth-1",
		Provider: "claude",
		Metadata: map[string]any{
			"disable_cooling": true,
		},
	}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	model := "test-model"
	m.MarkResult(context.Background(), Result{
		AuthID:   "auth-1",
		Provider: "claude",
		Model:    model,
		Success:  false,
		Error:    &Error{HTTPStatus: 500, Message: "boom"},
	})

	updated, ok := m.GetByID("auth-1")
	if !ok || updated == nil {
		t.Fatalf("expected auth to be present")
	}
	state := updated.ModelStates[model]
	if state == nil {
		t.Fatalf("expected model state to be present")
	}
	if !state.NextRetryAfter.IsZero() {
		t.Fatalf("expected NextRetryAfter to be zero when disable_cooling=true, got %v", state.NextRetryAfter)
	}
}

func TestManager_MarkResult_CoolsKimiMembershipErrors(t *testing.T) {
	prev := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(prev) })

	m := NewManager(nil, nil, nil)
	auth := &Auth{ID: "kimi-auth-1", Provider: "kimi"}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	model := "kimi-for-coding"
	m.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: "kimi",
		Model:    model,
		Success:  false,
		Error: &Error{
			HTTPStatus: 400,
			Message:    `{"error":{"message":"We're unable to verify your membership benefits at this time. Please ensure your membership is active.","type":"invalid_request_error"}}`,
		},
	})

	updated, ok := m.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth to be present")
	}
	state := updated.ModelStates[model]
	if state == nil {
		t.Fatalf("expected model state to be present")
	}
	if state.NextRetryAfter.IsZero() {
		t.Fatalf("expected Kimi membership error to set NextRetryAfter")
	}
	if wait := time.Until(state.NextRetryAfter); wait < 25*time.Minute || wait > 31*time.Minute {
		t.Fatalf("NextRetryAfter wait = %v, want about 30m", wait)
	}
	blocked, reason, _ := isAuthBlockedForModel(updated, model, time.Now())
	if !blocked {
		t.Fatalf("expected auth to be blocked for model")
	}
	if reason != blockReasonOther {
		t.Fatalf("block reason = %v, want temporary block", reason)
	}
	blockedOtherModel, reasonOtherModel, _ := isAuthBlockedForModel(updated, "kimi-k2.6", time.Now())
	if !blockedOtherModel {
		t.Fatalf("expected Kimi account cooldown to block other model aliases")
	}
	if reasonOtherModel != blockReasonOther {
		t.Fatalf("other model block reason = %v, want temporary block", reasonOtherModel)
	}
	if wait := time.Until(updated.NextRetryAfter); wait < 25*time.Minute || wait > 31*time.Minute {
		t.Fatalf("auth NextRetryAfter wait = %v, want about 30m", wait)
	}
}

func TestManager_MarkResult_CoolsKimiUsageLimitAsQuota(t *testing.T) {
	prev := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(prev) })

	m := NewManager(nil, nil, nil)
	auth := &Auth{ID: "kimi-auth-2", Provider: "kimi"}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	model := "kimi-k2.6"
	m.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: "kimi",
		Model:    model,
		Success:  false,
		Error: &Error{
			HTTPStatus: 400,
			Message:    `{"error":{"message":"You've reached your usage limit for this billing cycle. Your quota will be refreshed in the next cycle.","type":"access_terminated_error"}}`,
		},
	})

	updated, ok := m.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth to be present")
	}
	state := updated.ModelStates[model]
	if state == nil {
		t.Fatalf("expected model state to be present")
	}
	if !state.Quota.Exceeded {
		t.Fatalf("expected quota state to be marked exceeded")
	}
	if state.Quota.Reason != "quota" {
		t.Fatalf("quota reason = %q, want quota", state.Quota.Reason)
	}
	if wait := time.Until(state.NextRetryAfter); wait < 11*time.Hour || wait > 13*time.Hour {
		t.Fatalf("NextRetryAfter wait = %v, want about 12h", wait)
	}
}

func TestManager_MarkResult_CoolsNoStatusExecutionErrors(t *testing.T) {
	prev := quotaCooldownDisabled.Load()
	quotaCooldownDisabled.Store(false)
	t.Cleanup(func() { quotaCooldownDisabled.Store(prev) })

	m := NewManager(nil, nil, nil)
	auth := &Auth{ID: "kimi-auth-3", Provider: "kimi"}
	if _, errRegister := m.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("register auth: %v", errRegister)
	}

	model := "kimi-for-coding"
	m.MarkResult(context.Background(), Result{
		AuthID:   auth.ID,
		Provider: "kimi",
		Model:    model,
		Success:  false,
		Error: &Error{
			Message: `Post "https://api.kimi.com/coding/v1/chat/completions": http2: timeout awaiting response headers`,
		},
	})

	updated, ok := m.GetByID(auth.ID)
	if !ok || updated == nil {
		t.Fatalf("expected auth to be present")
	}
	state := updated.ModelStates[model]
	if state == nil {
		t.Fatalf("expected model state to be present")
	}
	if state.NextRetryAfter.IsZero() {
		t.Fatalf("expected no-status upstream error to set NextRetryAfter")
	}
	if wait := time.Until(state.NextRetryAfter); wait < 45*time.Second || wait > 75*time.Second {
		t.Fatalf("NextRetryAfter wait = %v, want about 1m", wait)
	}
}

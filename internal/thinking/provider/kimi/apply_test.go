package kimi

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	"github.com/tidwall/gjson"
)

func TestApplyClampsKimiXHighEffortToHigh(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"model":"kimi-k2.6"}`)
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2.6",
		Thinking: &registry.ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
	}

	out, err := applier.Apply(body, thinking.ThinkingConfig{
		Mode:  thinking.ModeLevel,
		Level: thinking.LevelXHigh,
	}, modelInfo)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high; body=%s", got, out)
	}
}

func TestApplyClampsLargeKimiBudgetToHigh(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"model":"kimi-for-coding"}`)
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-for-coding",
		Thinking: &registry.ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
	}

	out, err := applier.Apply(body, thinking.ThinkingConfig{
		Mode:   thinking.ModeBudget,
		Budget: 32000,
	}, modelInfo)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high; body=%s", got, out)
	}
}

func TestApplyRemovesKimiReasoningEffortWhenDisabled(t *testing.T) {
	applier := NewApplier()
	body := []byte(`{"model":"kimi-k2.6","reasoning_effort":"high"}`)
	modelInfo := &registry.ModelInfo{
		ID:       "kimi-k2.6",
		Thinking: &registry.ThinkingSupport{Min: 1024, Max: 32000, ZeroAllowed: true, DynamicAllowed: true},
	}

	out, err := applier.Apply(body, thinking.ThinkingConfig{Mode: thinking.ModeNone}, modelInfo)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := gjson.GetBytes(out, "reasoning_effort"); got.Exists() {
		t.Fatalf("reasoning_effort still exists: %s; body=%s", got.Raw, out)
	}
}

func TestApplyCompatibleKimiClampsXHigh(t *testing.T) {
	body := []byte(`{"model":"custom-kimi"}`)
	modelInfo := &registry.ModelInfo{ID: "custom-kimi", UserDefined: true}

	out, err := NewApplier().Apply(body, thinking.ThinkingConfig{
		Mode:  thinking.ModeLevel,
		Level: thinking.LevelXHigh,
	}, modelInfo)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort = %q, want high; body=%s", got, out)
	}
}

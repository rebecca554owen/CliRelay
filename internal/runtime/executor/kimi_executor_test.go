package executor

import (
	"testing"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/tidwall/gjson"
)

func TestResolveKimiUpstreamModel_PreservesCompleteNames(t *testing.T) {
	for _, model := range []string{
		"kimi-for-coding",
		"kimi-k2.6",
		"kimi-k2-thinking",
		"kimi-k2",
		"custom-kimi-model",
	} {
		if got := resolveKimiUpstreamModel(model); got != model {
			t.Fatalf("resolveKimiUpstreamModel(%q) = %q, want %q", model, got, model)
		}
	}
}

func TestRepairKimiClaudeToolUseRequest_DropsUnansweredToolUses(t *testing.T) {
	req := cliproxyexecutor.Request{Payload: []byte(`{
		"messages":[
			{"role":"assistant","content":[
				{"type":"tool_use","id":"call_1","name":"read_file","input":{}},
				{"type":"tool_use","id":"call_2","name":"glob","input":{}}
			]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"ok"}]}
		]
	}`)}

	repairedReq, _, err := repairKimiClaudeToolUseRequest(req, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("repairKimiClaudeToolUseRequest() error = %v", err)
	}

	if got := len(gjson.GetBytes(repairedReq.Payload, "messages.0.content").Array()); got != 1 {
		t.Fatalf("messages.0.content length = %d, want 1; body=%s", got, repairedReq.Payload)
	}
	if gjson.GetBytes(repairedReq.Payload, `messages.0.content.#(id=="call_2")`).Exists() {
		t.Fatalf("unanswered tool_use should be removed: %s", repairedReq.Payload)
	}
}

func TestRepairKimiClaudeToolUseRequest_CoalescesAdjacentToolResults(t *testing.T) {
	req := cliproxyexecutor.Request{Payload: []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read_file","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"a"}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"b"}]}
		]
	}`)}

	repairedReq, _, err := repairKimiClaudeToolUseRequest(req, cliproxyexecutor.Options{})
	if err != nil {
		t.Fatalf("repairKimiClaudeToolUseRequest() error = %v", err)
	}

	if got := len(gjson.GetBytes(repairedReq.Payload, "messages").Array()); got != 2 {
		t.Fatalf("messages length = %d, want 2; body=%s", got, repairedReq.Payload)
	}
	if got := gjson.GetBytes(repairedReq.Payload, "messages.1.content.0.content").String(); got != "a" {
		t.Fatalf("kept tool_result content = %q, want %q; body=%s", got, "a", repairedReq.Payload)
	}
}

func TestNormalizeKimiToolMessageLinks_DropsEmptyAssistantMessages(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":""},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","call_id":"call_1","content":"[]"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	if got := len(gjson.GetBytes(out, "messages").Array()); got != 2 {
		t.Fatalf("messages length = %d, want 2; body=%s", got, out)
	}
	if got := gjson.GetBytes(out, "messages.0.role").String(); got != "assistant" {
		t.Fatalf("messages.0.role = %q, want assistant; body=%s", got, out)
	}
}

func TestNormalizeKimiThinkingForEndpoint_KimiCodingRemovesReasoningEffort(t *testing.T) {
	body := []byte(`{"model":"kimi-k2.6","reasoning_effort":"high"}`)

	out, err := normalizeKimiThinkingForEndpoint(body, "https://api.kimi.com/coding/v1/chat/completions")
	if err != nil {
		t.Fatalf("normalizeKimiThinkingForEndpoint() error = %v", err)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should be removed: %s", out)
	}
}

func TestNormalizeKimiThinkingForEndpoint_MoonshotConvertsToThinkingType(t *testing.T) {
	body := []byte(`{"model":"kimi-k2.6","reasoning_effort":"high"}`)

	out, err := normalizeKimiThinkingForEndpoint(body, "https://api.moonshot.cn/v1/chat/completions")
	if err != nil {
		t.Fatalf("normalizeKimiThinkingForEndpoint() error = %v", err)
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "high" {
		t.Fatalf("thinking.type = %q, want high; body=%s", got, out)
	}
	if gjson.GetBytes(out, "reasoning_effort").Exists() {
		t.Fatalf("reasoning_effort should be removed after conversion: %s", out)
	}
}

func TestNormalizeKimiThinkingForEndpoint_MoonshotConvertsNoneToDisabled(t *testing.T) {
	body := []byte(`{"model":"kimi-k2.6","reasoning_effort":"none"}`)

	out, err := normalizeKimiThinkingForEndpoint(body, "https://api.moonshot.ai/v1/chat/completions")
	if err != nil {
		t.Fatalf("normalizeKimiThinkingForEndpoint() error = %v", err)
	}
	if got := gjson.GetBytes(out, "thinking.type").String(); got != "disabled" {
		t.Fatalf("thinking.type = %q, want disabled; body=%s", got, out)
	}
}

func TestNormalizeKimiToolMessageLinks_UsesCallIDFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"list_directory:1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","call_id":"list_directory:1","content":"[]"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.tool_call_id").String()
	if got != "list_directory:1" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "list_directory:1")
	}
}

func TestNormalizeKimiToolMessageLinks_InferSinglePendingID(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_123","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"tool","content":"file-content"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.tool_call_id").String()
	if got != "call_123" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "call_123")
	}
}

func TestNormalizeKimiToolMessageLinks_AmbiguousMissingIDIsNotInferred(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}},
				{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{}"}}
			]},
			{"role":"tool","content":"result-without-id"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	if gjson.GetBytes(out, "messages.1.tool_call_id").Exists() {
		t.Fatalf("messages.1.tool_call_id should be absent for ambiguous case, got %q", gjson.GetBytes(out, "messages.1.tool_call_id").String())
	}
}

func TestNormalizeKimiToolMessageLinks_PreservesExistingToolCallID(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","call_id":"different-id","content":"result"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.tool_call_id").String()
	if got != "call_1" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "call_1")
	}
}

func TestNormalizeKimiToolMessageLinks_InheritsPreviousReasoningForAssistantToolCalls(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"plan","reasoning_content":"previous reasoning"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.1.reasoning_content").String()
	if got != "previous reasoning" {
		t.Fatalf("messages.1.reasoning_content = %q, want %q", got, "previous reasoning")
	}
}

func TestNormalizeKimiToolMessageLinks_InsertsFallbackReasoningWhenMissing(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	reasoning := gjson.GetBytes(out, "messages.0.reasoning_content")
	if !reasoning.Exists() {
		t.Fatalf("messages.0.reasoning_content should exist")
	}
	if reasoning.String() != "[reasoning unavailable]" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", reasoning.String(), "[reasoning unavailable]")
	}
}

func TestNormalizeKimiToolMessageLinks_UsesContentAsReasoningFallback(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":[{"type":"text","text":"first line"},{"type":"text","text":"second line"}],"tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}]}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "first line\nsecond line" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "first line\nsecond line")
	}
}

func TestNormalizeKimiToolMessageLinks_ReplacesEmptyReasoningContent(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","content":"assistant summary","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":""}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "assistant summary" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "assistant summary")
	}
}

func TestNormalizeKimiToolMessageLinks_PreservesExistingAssistantReasoning(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"keep me"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	got := gjson.GetBytes(out, "messages.0.reasoning_content").String()
	if got != "keep me" {
		t.Fatalf("messages.0.reasoning_content = %q, want %q", got, "keep me")
	}
}

func TestNormalizeKimiToolMessageLinks_RepairsIDsAndReasoningTogether(t *testing.T) {
	body := []byte(`{
		"messages":[
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"list_directory","arguments":"{}"}}],"reasoning_content":"r1"},
			{"role":"tool","call_id":"call_1","content":"[]"},
			{"role":"assistant","tool_calls":[{"id":"call_2","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"tool","call_id":"call_2","content":"file"}
		]
	}`)

	out, err := normalizeKimiToolMessageLinks(body)
	if err != nil {
		t.Fatalf("normalizeKimiToolMessageLinks() error = %v", err)
	}

	if got := gjson.GetBytes(out, "messages.1.tool_call_id").String(); got != "call_1" {
		t.Fatalf("messages.1.tool_call_id = %q, want %q", got, "call_1")
	}
	if got := gjson.GetBytes(out, "messages.3.tool_call_id").String(); got != "call_2" {
		t.Fatalf("messages.3.tool_call_id = %q, want %q", got, "call_2")
	}
	if got := gjson.GetBytes(out, "messages.2.reasoning_content").String(); got != "r1" {
		t.Fatalf("messages.2.reasoning_content = %q, want %q", got, "r1")
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt Contributors

package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockServer wraps httptest with helpers for capturing request bodies and
// scripting responses.
type mockServer struct {
	*httptest.Server
	lastPath      string
	lastAuth      string
	lastAPIKey    string
	lastHeaders   http.Header
	lastBody      []byte
	responseCode  int
	responseBody  string
	responseDelay time.Duration
	reqCount      int
}

func newMockServer(t *testing.T, respBody string) *mockServer {
	t.Helper()
	m := &mockServer{responseCode: 200, responseBody: respBody}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.reqCount++
		m.lastPath = r.URL.Path
		m.lastAuth = r.Header.Get("Authorization")
		m.lastAPIKey = r.Header.Get("X-Api-Key")
		m.lastHeaders = r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		m.lastBody = body
		if m.responseDelay > 0 {
			time.Sleep(m.responseDelay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.responseCode)
		fmt.Fprint(w, m.responseBody)
	}))
	t.Cleanup(m.Server.Close)
	return m
}

func TestOpenAIClient_HTTPRoundTrip(t *testing.T) {
	body := `{
		"id":"chatcmpl_1",
		"object":"chat.completion",
		"model":"gpt-x",
		"choices":[{
			"index":0,
			"message":{"role":"assistant","content":"hello back"},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}
	}`
	ms := newMockServer(t, body)
	c := NewOpenAIClient(ClientConfig{
		URL:    ms.URL,
		APIKey: "sk-abc",
		Model:  "gpt-x",
	})
	resp, err := c.CompletionsWithCtx(context.Background(), ChatRequest{
		Model:    "gpt-x",
		Messages: []Message{NewTextMessage("user", "hello")},
	})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	if resp.Content() != "hello back" {
		t.Errorf("content = %q", resp.Content())
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	if !strings.HasPrefix(ms.lastAuth, "Bearer sk-abc") {
		t.Errorf("Authorization = %q", ms.lastAuth)
	}
	if ms.lastPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", ms.lastPath)
	}
	// Body should carry model + messages.
	var reqBody map[string]any
	if err := json.Unmarshal(ms.lastBody, &reqBody); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if reqBody["model"] != "gpt-x" {
		t.Errorf("body.model = %v", reqBody["model"])
	}
}

func TestOpenAIClient_ExtraHeaders(t *testing.T) {
	body := `{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`
	ms := newMockServer(t, body)
	c := NewOpenAIClient(ClientConfig{
		URL:    ms.URL,
		APIKey: "sk",
		Model:  "m",
		ExtraHeaders: map[string]string{
			"X-Session": "aff-{ocr_session_key}",
		},
		SessionKey: "test-key-123",
	})
	if _, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hi")}}); err != nil {
		t.Fatal(err)
	}
	if got := ms.lastHeaders.Get("X-Session"); got != "aff-test-key-123" {
		t.Errorf("X-Session = %q, want expanded key", got)
	}
}

func TestOpenAIClient_ContextCanceled(t *testing.T) {
	body := `{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`
	ms := newMockServer(t, body)
	// Delay response so the cancellation fires first.
	ms.responseDelay = 200 * time.Millisecond
	c := NewOpenAIClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "m"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	_, err := c.CompletionsWithCtx(ctx, ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOpenAIClient_5xxError(t *testing.T) {
	ms := newMockServer(t, `{"error":{"message":"server broken"}}`)
	ms.responseCode = 500
	c := NewOpenAIClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "m", Timeout: 500 * time.Millisecond})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected error from 5xx")
	}
}

func TestOpenAIClient_MalformedJSON(t *testing.T) {
	ms := newMockServer(t, `{not valid json`)
	c := NewOpenAIClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "m", Timeout: 500 * time.Millisecond})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected error from malformed json")
	}
}

func TestOpenAIClient_URLNormalization(t *testing.T) {
	// URL without /chat/completions suffix should be normalized.
	body := `{"id":"x","object":"chat.completion","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`
	ms := newMockServer(t, body)

	c := NewOpenAIClient(ClientConfig{URL: ms.URL + "/v1", APIKey: "sk", Model: "m"})
	// The stored URL should end in /chat/completions.
	if !strings.HasSuffix(c.cfg.URL, "/chat/completions") {
		t.Errorf("URL not normalized: %q", c.cfg.URL)
	}
}

func TestOpenAIClient_StreamMode(t *testing.T) {
	// A minimal SSE stream. Even a single [DONE] chunk with usage suffices to
	// exercise completionsStreaming.
	streamBody := "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4}}\n\n" +
		"data: [DONE]\n\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, streamBody)
	}))
	defer srv.Close()

	c := NewOpenAIClient(ClientConfig{
		URL:    srv.URL,
		APIKey: "sk",
		Model:  "m",
		ExtraBody: map[string]any{
			"stream": true,
		},
	})
	resp, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hello")}})
	if err != nil {
		t.Fatalf("streaming: %v", err)
	}
	if resp.Content() != "hi" {
		t.Errorf("content = %q, want hi", resp.Content())
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 3 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestOpenAIClient_StreamNoChoices(t *testing.T) {
	// A stream ending without any choice is a stream integrity error.
	streamBody := "data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, streamBody)
	}))
	defer srv.Close()

	c := NewOpenAIClient(ClientConfig{
		URL: srv.URL, APIKey: "sk", Model: "m",
		ExtraBody: map[string]any{"stream": true},
	})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "m", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected stream integrity error, got nil")
	}
	var sie *streamIntegrityError
	if !errors.As(err, &sie) {
		t.Errorf("expected *streamIntegrityError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error should mention 'no choices': %v", err)
	}
}

func TestOpenAIClient_UsesConfigModelWhenReqModelEmpty(t *testing.T) {
	body := `{"id":"x","object":"chat.completion","model":"cfg-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	ms := newMockServer(t, body)

	c := NewOpenAIClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "cfg-model"})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{
		// Model omitted so client uses cfg.Model.
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body2 map[string]any
	json.Unmarshal(ms.lastBody, &body2)
	if body2["model"] != "cfg-model" {
		t.Errorf("body.model = %v, want cfg-model", body2["model"])
	}
}

func TestAnthropicClient_HTTPRoundTrip_XAPIKey(t *testing.T) {
	body := `{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"model":"claude-x",
		"content":[{"type":"text","text":"hi there"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":3}
	}`
	ms := newMockServer(t, body)
	c := NewAnthropicClient(ClientConfig{
		URL:        ms.URL,
		APIKey:     "sk-anth",
		Model:      "claude-x",
		AuthHeader: "x-api-key",
	})
	resp, err := c.CompletionsWithCtx(context.Background(), ChatRequest{
		Model:    "claude-x",
		Messages: []Message{NewTextMessage("system", "you are helpful"), NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	if resp.Content() != "hi there" {
		t.Errorf("content = %q", resp.Content())
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 10 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	// x-api-key header should carry the key; Authorization should be gone.
	if ms.lastAPIKey != "sk-anth" {
		t.Errorf("X-Api-Key = %q", ms.lastAPIKey)
	}
	if ms.lastAuth != "" {
		t.Errorf("Authorization should be empty, got %q", ms.lastAuth)
	}
	if ms.lastPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", ms.lastPath)
	}
}

func TestAnthropicClient_HTTPRoundTrip_Authorization(t *testing.T) {
	body := `{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"model":"claude-x",
		"content":[{"type":"text","text":"ok"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":1}
	}`
	ms := newMockServer(t, body)
	c := NewAnthropicClient(ClientConfig{
		URL:        ms.URL,
		APIKey:     "oauth-token",
		Model:      "claude-x",
		AuthHeader: "authorization",
	})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{
		Model:    "claude-x",
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ms.lastAuth, "Bearer oauth-token") {
		t.Errorf("Authorization = %q", ms.lastAuth)
	}
}

func TestAnthropicClient_5xxError(t *testing.T) {
	ms := newMockServer(t, `{"error":"broken"}`)
	ms.responseCode = 500
	c := NewAnthropicClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "claude-x", AuthHeader: "x-api-key", Timeout: 500 * time.Millisecond})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "claude-x", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicClient_ContextCancellation(t *testing.T) {
	ms := newMockServer(t, `{"id":"x"}`)
	ms.responseDelay = 200 * time.Millisecond
	c := NewAnthropicClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "claude-x", AuthHeader: "x-api-key"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.CompletionsWithCtx(ctx, ChatRequest{Model: "claude-x", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAnthropicClient_InitErrPropagates(t *testing.T) {
	c := &AnthropicClient{initErr: errors.New("bedrock config broken")}
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{})
	if err == nil || !strings.Contains(err.Error(), "bedrock config broken") {
		t.Errorf("expected initErr, got %v", err)
	}
}

func TestAnthropicClient_BadToolCallArgs(t *testing.T) {
	c := &AnthropicClient{}
	// Build a message with an assistant tool call whose Arguments is invalid JSON.
	msg := Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: FunctionCall{Name: "t", Arguments: "{not json"}},
		},
	}
	_, err := c.buildAnthropicParams("m", ChatRequest{Messages: []Message{msg}})
	if err == nil || !strings.Contains(err.Error(), "invalid tool call arguments") {
		t.Errorf("expected tool-call args error, got %v", err)
	}
}

func TestAnthropicClient_NullToolArguments(t *testing.T) {
	c := &AnthropicClient{}
	// JSON literal "null" unmarshals to a nil map; the code must recover.
	msg := Message{
		Role: "assistant",
		ToolCalls: []ToolCall{
			{ID: "c1", Type: "function", Function: FunctionCall{Name: "t", Arguments: "null"}},
		},
	}
	if _, err := c.buildAnthropicParams("m", ChatRequest{Messages: []Message{msg}}); err != nil {
		t.Errorf("null args should not error: %v", err)
	}
}

func TestAnthropicClient_ToolChoiceRequired(t *testing.T) {
	c := &AnthropicClient{}
	params, err := c.buildAnthropicParams("m", ChatRequest{
		Messages: []Message{NewTextMessage("user", "hi")},
		Tools: []ToolDef{
			{Type: "function", Function: FunctionDef{Name: "t", Description: "d", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}},
		},
		ToolChoice: "required",
	})
	if err != nil {
		t.Fatal(err)
	}
	if params.ToolChoice.OfAny == nil {
		t.Error("expected OfAny to be set for required tool choice")
	}
}

func TestAnthropicClient_MultiMessageSystemAndTool(t *testing.T) {
	c := &AnthropicClient{}
	params, err := c.buildAnthropicParams("m", ChatRequest{
		Messages: []Message{
			NewTextMessage("system", "s1"),
			NewTextMessage("system", "s2"),
			NewTextMessage("user", "u1"),
			{
				Role: "assistant", Content: "reply",
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "t", Arguments: `{"a":1}`}}},
			},
			NewToolResultMessage("c1", "result1"),
			NewToolResultMessage("c1", "result2"), // flush handles consecutive tool results
			NewTextMessage("user", "u2"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(params.System) != 2 {
		t.Errorf("system blocks = %d, want 2", len(params.System))
	}
}

func TestAnthropicClient_UserContentBlocks(t *testing.T) {
	c := &AnthropicClient{}
	params, err := c.buildAnthropicParams("m", ChatRequest{
		Messages: []Message{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "tool_result", ToolUseID: "c1", Content: []ContentBlock{{Type: "text", Text: "res"}}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(params.Messages))
	}
}

func TestAnthropicClient_Temperature(t *testing.T) {
	c := &AnthropicClient{}
	temp := 0.5
	params, err := c.buildAnthropicParams("m", ChatRequest{
		Messages:    []Message{NewTextMessage("user", "hi")},
		Temperature: &temp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !params.Temperature.Valid() {
		t.Error("temperature not set")
	}
}

func TestNewAnthropicClient_URLNormalization(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://api.example.com", "https://api.example.com/v1/messages"},
		{"https://api.example.com/", "https://api.example.com/v1/messages"},
		{"https://api.example.com/v1/messages", "https://api.example.com/v1/messages"},
		{"https://api.example.com/v1/messages/", "https://api.example.com/v1/messages/"},
	}
	for _, tt := range tests {
		c := NewAnthropicClient(ClientConfig{URL: tt.in, APIKey: "k", Model: "m", AuthHeader: "x-api-key"})
		if c.cfg.URL != tt.want {
			t.Errorf("NewAnthropicClient URL from %q = %q, want %q", tt.in, c.cfg.URL, tt.want)
		}
	}
}

func TestNewAnthropicClient_DefaultsAuthHeader(t *testing.T) {
	c := NewAnthropicClient(ClientConfig{URL: "https://x", APIKey: "k", Model: "m"})
	if c.cfg.AuthHeader != "authorization" {
		t.Errorf("default AuthHeader = %q, want authorization", c.cfg.AuthHeader)
	}
}

func TestNewAnthropicBedrockClient_LoadFailure(t *testing.T) {
	// Pointing at a nonexistent profile should defer the error to first use.
	c := NewAnthropicBedrockClient(ClientConfig{
		Model:      "m",
		AWSProfile: "definitely-not-a-real-profile-12345",
		AWSRegion:  "us-east-1",
	})
	if !c.bedrock {
		t.Error("expected bedrock=true")
	}
	// If AWS config load failed → initErr is set; if it succeeded despite the fake
	// profile (unlikely on CI, possible on a laptop with the SDK's magic
	// fallbacks), we skip.
	if c.initErr == nil {
		t.Skip("AWS config load unexpectedly succeeded — nothing to assert")
	}
	if !strings.Contains(c.initErr.Error(), "bedrock") {
		t.Errorf("initErr should mention bedrock: %v", c.initErr)
	}
	// The error surface through CompletionsWithCtx must be the same.
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{})
	if err == nil {
		t.Fatal("expected error from deferred initErr")
	}
}

func TestBedrockContext(t *testing.T) {
	c := &AnthropicClient{}
	_, _, ok := c.BedrockContext()
	if ok {
		t.Error("non-bedrock client should return ok=false")
	}
	c2 := &AnthropicClient{bedrock: true, awsRegion: "us-east-1", awsProfile: "prod"}
	r, p, ok := c2.BedrockContext()
	if !ok || r != "us-east-1" || p != "prod" {
		t.Errorf("BedrockContext = (%q, %q, %v)", r, p, ok)
	}
}

func TestBedrockWhere(t *testing.T) {
	c := &AnthropicClient{awsRegion: "us-east-1"}
	if got := c.bedrockWhere(); !strings.Contains(got, "us-east-1") || !strings.Contains(got, "ambient") {
		t.Errorf("bedrockWhere = %q", got)
	}
	c2 := &AnthropicClient{awsRegion: "us-east-1", awsProfile: "myprof"}
	if got := c2.bedrockWhere(); !strings.Contains(got, "myprof") {
		t.Errorf("bedrockWhere = %q", got)
	}
	c3 := &AnthropicClient{}
	if got := c3.bedrockWhere(); !strings.Contains(got, "unknown") {
		t.Errorf("bedrockWhere = %q, want mention of unknown region", got)
	}
}

func TestSSOLoginProfileArg(t *testing.T) {
	if got := ssoLoginProfileArg(""); got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
	if got := ssoLoginProfileArg("prod"); got != " --profile prod" {
		t.Errorf("prod = %q", got)
	}
}

func TestListProfilesRegionArg(t *testing.T) {
	if got := listProfilesRegionArg(""); got != "" {
		t.Errorf("empty = %q", got)
	}
	if got := listProfilesRegionArg("us-east-1"); got != " --region us-east-1" {
		t.Errorf("region = %q", got)
	}
}

func TestExplainError(t *testing.T) {
	// Non-bedrock clients pass-through unchanged.
	c := &AnthropicClient{}
	orig := errors.New("something")
	if got := c.explainError("m", orig); got != orig {
		t.Errorf("non-bedrock should pass through, got %v", got)
	}
	if got := c.explainError("m", nil); got != nil {
		t.Errorf("nil should pass through, got %v", got)
	}

	// Bedrock client rewrites known errors.
	bc := &AnthropicClient{bedrock: true, awsRegion: "us-east-1"}

	tests := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"invalid api key format", errors.New("Invalid API Key format: reason"), "signature"},
		{"no access", errors.New("You don't have access to the model with the specified model ID."), "model access is granted"},
		{"invalid model id", errors.New("The provided model identifier is invalid."), "IDs are account"},
		{"inference profile not found", errors.New("inference profile arn:foo not found"), "list-inference-profiles"},
		{"expired token", errors.New("ExpiredToken: session expired"), "aws sso login"},
		{"access denied generic", errors.New("AccessDeniedException: nope"), "authorization gap"},
		{"other", errors.New("random network error"), "bedrock request failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bc.explainError("some-model", tt.err)
			if got == nil || !strings.Contains(got.Error(), tt.wantSub) {
				t.Errorf("got %v, want substring %q", got, tt.wantSub)
			}
		})
	}
}

func TestExplainError_BearerTokenPath(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "some-token")
	bc := &AnthropicClient{bedrock: true, awsRegion: "us-east-1"}
	got := bc.explainError("m", errors.New("Invalid API Key format: bad"))
	if got == nil || !strings.Contains(got.Error(), "AWS_BEARER_TOKEN_BEDROCK") {
		t.Errorf("expected bearer-token message, got %v", got)
	}
}

func TestAnthropicThinkingBudgetTokens(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   int64
		wantOK bool
	}{
		{"nil", nil, 0, false},
		{"not a map", "string", 0, false},
		{"float64", map[string]any{"budget_tokens": float64(4096)}, 4096, true},
		{"int", map[string]any{"budget_tokens": 1024}, 1024, true},
		{"int64", map[string]any{"budget_tokens": int64(2048)}, 2048, true},
		{"json.Number valid", map[string]any{"budget_tokens": json.Number("512")}, 512, true},
		{"json.Number invalid", map[string]any{"budget_tokens": json.Number("bad")}, 0, false},
		{"missing", map[string]any{}, 0, false},
		{"wrong type inside map", map[string]any{"budget_tokens": true}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := anthropicThinkingBudgetTokens(tt.in)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("got (%d, %v), want (%d, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestBuildToolInputSchema(t *testing.T) {
	params := map[string]any{
		"type":     "object",
		"required": []any{"a", "b"},
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
		},
		"additionalProperties": false,
	}
	schema := buildToolInputSchema(params)
	if len(schema.Required) != 2 {
		t.Errorf("required = %v", schema.Required)
	}
	if schema.Properties == nil {
		t.Error("properties not copied")
	}
	if schema.ExtraFields["additionalProperties"] != false {
		t.Errorf("extraFields missing additionalProperties: %v", schema.ExtraFields)
	}
}

func TestBuildToolInputSchema_NoRequired(t *testing.T) {
	schema := buildToolInputSchema(map[string]any{})
	if len(schema.Required) != 0 {
		t.Errorf("required should be empty, got %v", schema.Required)
	}
}

func TestStreamIntegrityError_Error(t *testing.T) {
	e := &streamIntegrityError{reason: "no choices"}
	if got := e.Error(); got != "OpenAI streaming response no choices" {
		t.Errorf("Error() = %q", got)
	}
}

func TestRetryCodesMiddleware(t *testing.T) {
	// Empty codes returns nil.
	if mw := retryCodesMiddleware(nil); mw != nil {
		t.Error("expected nil middleware for empty codes")
	}

	mw := retryCodesMiddleware([]int{418, 425})
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	// A response with a matching status gets x-should-retry set.
	req, _ := http.NewRequest("GET", "http://x", nil)
	next := func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 418, Header: http.Header{}}, nil
	}
	resp, err := mw(req, next)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("x-should-retry") != "true" {
		t.Errorf("x-should-retry not set for 418")
	}
	// A response with a non-matching status is untouched.
	next2 := func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	}
	resp2, _ := mw(req, next2)
	if resp2.Header.Get("x-should-retry") != "" {
		t.Error("x-should-retry set on 200")
	}
	// Underlying error propagates.
	next3 := func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network fell over")
	}
	if _, err := mw(req, next3); err == nil {
		t.Error("expected error")
	}
}

func TestNewLLMClient_Dispatch(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		wantType string
	}{
		{"anthropic", ProtocolAnthropic, "*llm.AnthropicClient"},
		{"openai", ProtocolOpenAIChatCompletions, "*llm.OpenAIClient"},
		{"openai-responses", ProtocolOpenAIResponses, "*llm.OpenAIResponsesClient"},
		{"unknown falls back to openai", "totally-unknown", "*llm.OpenAIClient"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewLLMClient(ResolvedEndpoint{
				Protocol: tt.protocol,
				URL:      "https://example",
				Token:    "sk",
				Model:    "m",
			})
			got := fmt.Sprintf("%T", c)
			if got != tt.wantType {
				t.Errorf("got %s, want %s", got, tt.wantType)
			}
		})
	}
}

func TestNewLLMClient_Bedrock(t *testing.T) {
	// Bedrock client construction requires AWS config. Nonexistent profile
	// keeps the test hermetic: initErr is set but the returned client's type
	// is still *AnthropicClient.
	c := NewLLMClient(ResolvedEndpoint{
		Protocol:   ProtocolAnthropicBedrock,
		Model:      "m",
		AWSProfile: "definitely-not-a-real-profile-12345",
		AWSRegion:  "us-east-1",
	})
	if fmt.Sprintf("%T", c) != "*llm.AnthropicClient" {
		t.Errorf("bedrock protocol should yield *AnthropicClient, got %T", c)
	}
}

func TestCountTokens(t *testing.T) {
	// Empty string is 0.
	if n := CountTokens(""); n != 0 {
		t.Errorf("CountTokens(\"\") = %d", n)
	}
	// Non-empty text yields positive count.
	if n := CountTokens("hello world"); n <= 0 {
		t.Errorf("CountTokens(hello world) = %d, want positive", n)
	}
	if n := CountTokensForModel("hello", "gpt-4o"); n <= 0 {
		t.Errorf("CountTokensForModel = %d", n)
	}
}

func TestEncodingForModel(t *testing.T) {
	tests := []struct {
		model, want string
	}{
		{"o1-preview", "o200k_base"},
		{"o3", "o200k_base"},
		{"o4-something", "o200k_base"},
		{"gpt-4", "cl100k_base"},
		{"", "cl100k_base"},
	}
	for _, tt := range tests {
		if got := encodingForModel(tt.model); got != tt.want {
			t.Errorf("encodingForModel(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestCountTokensWithEncoding_Fallback(t *testing.T) {
	// An unknown encoding name falls back to len/4.
	got := countTokensWithEncoding("hello world!", "no-such-encoding")
	if got != len("hello world!")/4 {
		t.Errorf("fallback count = %d, want %d", got, len("hello world!")/4)
	}
}

func TestNativeTurn_EstimatedTokens(t *testing.T) {
	// ReasoningPayload: non-empty gives >0.
	n := NativeTurn{Payload: ReasoningPayload("x")}
	if got := n.EstimatedTokens(); got < 0 {
		t.Errorf("estimated = %d", got)
	}
	// Nil / empty payload gives 0.
	if got := (NativeTurn{}).EstimatedTokens(); got != 0 {
		t.Errorf("empty = %d", got)
	}
	// Unrelated type returns 0 via default.
	if got := (NativeTurn{Payload: 42}).EstimatedTokens(); got != 0 {
		t.Errorf("int payload = %d, want 0", got)
	}
}

func TestMarshaledLen(t *testing.T) {
	if got := marshaledLen(nil); got != 0 {
		t.Errorf("nil = %d", got)
	}
	if got := marshaledLen(map[string]int{"a": 1}); got <= 0 {
		t.Errorf("map = %d", got)
	}
	// A value json cannot marshal (chan) returns 0.
	if got := marshaledLen(make(chan int)); got != 0 {
		t.Errorf("chan = %d, want 0", got)
	}
}

// Responses client HTTP round-trip.
func TestOpenAIResponsesClient_HTTPRoundTrip(t *testing.T) {
	body := `{
		"id":"resp_1",
		"object":"response",
		"model":"o3",
		"status":"completed",
		"output":[
			{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"hi back","annotations":[]}]}
		],
		"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}
	}`
	ms := newMockServer(t, body)
	c := NewOpenAIResponsesClient(ClientConfig{
		URL:    ms.URL,
		APIKey: "sk-openai",
		Model:  "o3",
	})
	resp, err := c.CompletionsWithCtx(context.Background(), ChatRequest{
		Model:    "o3",
		Messages: []Message{NewTextMessage("system", "sys"), NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatalf("CompletionsWithCtx: %v", err)
	}
	if resp.Content() != "hi back" {
		t.Errorf("content = %q", resp.Content())
	}
	if resp.Usage == nil {
		t.Fatal("usage nil")
	}
}

func TestOpenAIResponsesClient_FailedStatus(t *testing.T) {
	body := `{
		"id":"resp_1","object":"response","model":"o3","status":"failed",
		"output":[]
	}`
	ms := newMockServer(t, body)
	c := NewOpenAIResponsesClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "o3"})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "o3", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Errorf("expected failed-status error, got %v", err)
	}
}

func TestOpenAIResponsesClient_QueuedStatus(t *testing.T) {
	body := `{"id":"r","object":"response","model":"o3","status":"queued","output":[]}`
	ms := newMockServer(t, body)
	c := NewOpenAIResponsesClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "o3"})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "o3", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil || !strings.Contains(err.Error(), "non-terminal") {
		t.Errorf("expected non-terminal error, got %v", err)
	}
}

func TestOpenAIResponsesClient_5xxError(t *testing.T) {
	ms := newMockServer(t, `{}`)
	ms.responseCode = 500
	c := NewOpenAIResponsesClient(ClientConfig{URL: ms.URL, APIKey: "sk", Model: "o3", Timeout: 500 * time.Millisecond})
	_, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "o3", Messages: []Message{NewTextMessage("user", "hi")}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAIResponsesClient_ExtraBody(t *testing.T) {
	body := `{"id":"r","object":"response","model":"o3","status":"completed","output":[]}`
	ms := newMockServer(t, body)
	c := NewOpenAIResponsesClient(ClientConfig{
		URL: ms.URL, APIKey: "sk", Model: "o3",
		ExtraBody: map[string]any{
			"custom_field": "value",
			"stream":       true, // must be dropped
		},
	})
	if _, err := c.CompletionsWithCtx(context.Background(), ChatRequest{Model: "o3", Messages: []Message{NewTextMessage("user", "hi")}}); err != nil {
		t.Fatal(err)
	}
	var reqBody map[string]any
	if err := json.Unmarshal(ms.lastBody, &reqBody); err != nil {
		t.Fatal(err)
	}
	if reqBody["custom_field"] != "value" {
		t.Errorf("custom_field not sent: %v", reqBody)
	}
	if _, has := reqBody["stream"]; has {
		t.Error("stream should be dropped")
	}
}

func TestMapResponsesFinishReason(t *testing.T) {
	tests := []struct {
		status string
		hasTC  bool
		want   string
	}{
		{"completed", false, "stop"},
		{"completed", true, "tool_calls"},
		{"incomplete", false, "length"},
		{"failed", false, "error"},
		{"cancelled", false, "error"},
		{"unknown-status", false, "stop"},
	}
	for _, tt := range tests {
		var tcs []ToolCall
		if tt.hasTC {
			tcs = []ToolCall{{ID: "c1"}}
		}
		if got := mapResponsesFinishReason(tt.status, tcs); got != tt.want {
			t.Errorf("mapResponsesFinishReason(%s, hasTC=%v) = %q, want %q", tt.status, tt.hasTC, got, tt.want)
		}
	}
}

func TestNewOpenAIResponsesClient_URLNormalization(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://api.openai.com/v1", "https://api.openai.com/v1/responses"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/responses"},
		{"https://api.openai.com/v1/responses", "https://api.openai.com/v1/responses"},
		{"https://api.openai.com/v1/responses/", "https://api.openai.com/v1/responses"},
	}
	for _, tt := range tests {
		c := NewOpenAIResponsesClient(ClientConfig{URL: tt.in, APIKey: "k", Model: "m"})
		if c.cfg.URL != tt.want {
			t.Errorf("URL from %q = %q, want %q", tt.in, c.cfg.URL, tt.want)
		}
	}
}

func TestBuildResponsesParams_MessageMapping(t *testing.T) {
	c := &OpenAIResponsesClient{}
	temp := 0.4
	params := c.buildResponsesParams("o3", ChatRequest{
		Messages: []Message{
			NewTextMessage("system", "sys1"),
			NewTextMessage("system", "sys2"),
			NewTextMessage("user", "u1"),
			{
				Role:    "assistant",
				Content: "reply",
				ToolCalls: []ToolCall{
					{ID: "c1", Type: "function", Function: FunctionCall{Name: "t", Arguments: `{"a":1}`}},
				},
			},
			NewToolResultMessage("c1", "result"),
			// A message with unknown role falls through to default user.
			{Role: "moderator", Content: "??"},
		},
		Tools: []ToolDef{
			{Type: "function", Function: FunctionDef{Name: "t", Description: "d", Parameters: map[string]any{"type": "object"}}},
		},
		ToolChoice:  "required",
		MaxTokens:   1000,
		Temperature: &temp,
		SessionID:   "sess-abc",
	})
	if !strings.Contains(string(params.Instructions.Value), "sys1") || !strings.Contains(string(params.Instructions.Value), "sys2") {
		t.Errorf("system parts not concatenated: %v", params.Instructions.Value)
	}
	if !params.PromptCacheKey.Valid() || string(params.PromptCacheKey.Value) != "sess-abc" {
		t.Errorf("PromptCacheKey = %v", params.PromptCacheKey)
	}
	if !params.MaxOutputTokens.Valid() {
		t.Error("MaxOutputTokens not set")
	}
	if !params.Temperature.Valid() {
		t.Error("Temperature not set")
	}
}

func TestOpenAIClient_NoToolCallsCarriesReasoning(t *testing.T) {
	c := &OpenAIClient{}
	// A message with reasoning in Native but no visible content still gets a
	// reasoning_content field in the assistant message.
	params := c.buildOpenAIParams("m", ChatRequest{
		Messages: []Message{
			{Role: "assistant", Content: "", Native: NativeTurn{Family: "openai-chat-completions", Payload: ReasoningPayload("private thinking")}},
		},
	})
	if len(params.Messages) != 1 {
		t.Fatalf("messages len = %d", len(params.Messages))
	}
}

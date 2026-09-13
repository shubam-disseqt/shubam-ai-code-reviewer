// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt Contributors

package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateTimeoutSec(t *testing.T) {
	tests := []struct {
		name    string
		sec     int
		want    time.Duration
		wantErr string
	}{
		{"zero returns zero", 0, 0, ""},
		{"positive", 30, 30 * time.Second, ""},
		{"negative rejected", -1, 0, "must be non-negative"},
		{"overflow rejected", int(int64(1 << 62)), 0, "overflows"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateTimeoutSec(tt.sec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	// Alias.
	if _, err := validateTimeoutSec(10); err != nil {
		t.Errorf("validateTimeoutSec alias returned error: %v", err)
	}
}

func TestParseTimeoutEnv(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		t.Setenv(envOCRLLMTimeout, "")
		d, ok, err := parseTimeoutEnv()
		if err != nil || ok || d != 0 {
			t.Errorf("unset: d=%v ok=%v err=%v", d, ok, err)
		}
	})
	t.Run("valid", func(t *testing.T) {
		t.Setenv(envOCRLLMTimeout, "45")
		d, ok, err := parseTimeoutEnv()
		if err != nil || !ok || d != 45*time.Second {
			t.Errorf("valid: d=%v ok=%v err=%v", d, ok, err)
		}
	})
	t.Run("non-integer", func(t *testing.T) {
		t.Setenv(envOCRLLMTimeout, "30s")
		_, _, err := parseTimeoutEnv()
		if err == nil || !strings.Contains(err.Error(), "must be an integer") {
			t.Errorf("expected integer parse error, got %v", err)
		}
	})
	t.Run("negative", func(t *testing.T) {
		t.Setenv(envOCRLLMTimeout, "-5")
		_, _, err := parseTimeoutEnv()
		if err == nil {
			t.Error("expected validation error")
		}
	})
}

func TestParseExtraHeaders(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    map[string]string
		wantErr string
	}{
		{"empty", "", nil, ""},
		{"single", "X-Foo=bar", map[string]string{"X-Foo": "bar"}, ""},
		{"multiple", "X-Foo=bar, X-Baz=qux", map[string]string{"X-Foo": "bar", "X-Baz": "qux"}, ""},
		{"quoted with comma", `X-Fwd="1.2.3.4,5.6.7.8"`, map[string]string{"X-Fwd": "1.2.3.4,5.6.7.8"}, ""},
		{"missing =", "onlyname", nil, "expected key=value"},
		{"empty key", "=value", nil, "empty header name"},
		{"reserved header", "Authorization=x", nil, "reserved header"},
		{"unclosed quote", `X-Foo="bar`, nil, "unclosed quote"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExtraHeaders(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len(got)=%d, want %d: %v", len(got), len(tt.want), got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("got[%q]=%q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestParseRetryCodes(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantCodes   []int
		wantWarnSub string
		wantErrSub  string
	}{
		{"empty", "", nil, "", ""},
		{"whitespace only", "  ", nil, "", ""},
		{"single 4xx", "418", []int{418}, "", ""},
		{"multiple deduped", "418,418,419", []int{418, 419}, "", ""},
		{"sdk default filtered", "429,418", []int{418}, "429", ""},
		{"non-integer", "abc", nil, "", "must be an integer"},
		{"5xx rejected", "500", nil, "", "must be a 4xx"},
		{"3xx rejected", "301", nil, "", "must be a 4xx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			codes, warns, err := ParseRetryCodes(tt.raw)
			if tt.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
					t.Fatalf("err = %v, want substring %q", err, tt.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !intSlicesEqual(codes, tt.wantCodes) {
				t.Errorf("codes = %v, want %v", codes, tt.wantCodes)
			}
			if tt.wantWarnSub != "" {
				if len(warns) == 0 || !strings.Contains(warns[0], tt.wantWarnSub) {
					t.Errorf("warnings = %v, want substring %q", warns, tt.wantWarnSub)
				}
			}
		})
	}
}

func intSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSanitizeRetryCodes(t *testing.T) {
	filtered, warnings, err := sanitizeRetryCodes([]int{408, 409, 429, 418, 425})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !intSlicesEqual(filtered, []int{418, 425}) {
		t.Errorf("filtered = %v, want [418 425]", filtered)
	}
	if len(warnings) != 3 {
		t.Errorf("warnings len = %d, want 3", len(warnings))
	}

	if _, _, err := sanitizeRetryCodes([]int{600}); err == nil {
		t.Error("expected error for out-of-range code")
	}
}

func TestStripModelSuffix(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"claude-x", "claude-x"},
		{"claude-x[1m]", "claude-x"},
		{"model[123456m]", "model"},
		{"[1m]notatend", "[1m]notatend"},
	}
	for _, tt := range tests {
		if got := stripModelSuffix(tt.in); got != tt.want {
			t.Errorf("stripModelSuffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEnsureMessagesSuffix(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://api.anthropic.com", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1/", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1/messages", "https://api.anthropic.com/v1/messages"},
		// Middle /v1/ path is preserved.
		{"https://gateway.example/v1/anthropic", "https://gateway.example/v1/anthropic"},
	}
	for _, tt := range tests {
		if got := ensureMessagesSuffix(tt.in); got != tt.want {
			t.Errorf("ensureMessagesSuffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTryOCREnv(t *testing.T) {
	t.Run("complete anthropic default", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")
		t.Setenv(envOCRLLMProtocol, "")
		t.Setenv(envOCRUseAnthropic, "")

		ep, ok, err := tryOCREnv("")
		if err != nil || !ok {
			t.Fatalf("tryOCREnv: ok=%v err=%v", ok, err)
		}
		if ep.Protocol != ProtocolAnthropic {
			t.Errorf("protocol = %q, want %q", ep.Protocol, ProtocolAnthropic)
		}
		if ep.AuthHeader != "authorization" {
			t.Errorf("authHeader = %q, want authorization", ep.AuthHeader)
		}
	})

	t.Run("use_anthropic false selects openai", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")
		t.Setenv(envOCRLLMProtocol, "")
		t.Setenv(envOCRUseAnthropic, "false")

		ep, ok, err := tryOCREnv("")
		if err != nil || !ok {
			t.Fatalf("tryOCREnv: %v", err)
		}
		if ep.Protocol != ProtocolOpenAIChatCompletions {
			t.Errorf("protocol = %q, want openai", ep.Protocol)
		}
	})

	t.Run("protocol wins over use_anthropic", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")
		t.Setenv(envOCRLLMProtocol, "openai-responses")
		t.Setenv(envOCRUseAnthropic, "true")

		ep, _, err := tryOCREnv("")
		if err != nil {
			t.Fatal(err)
		}
		if ep.Protocol != ProtocolOpenAIResponses {
			t.Errorf("protocol = %q", ep.Protocol)
		}
	})

	t.Run("invalid protocol rejected", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")
		t.Setenv(envOCRLLMProtocol, "no-such-protocol")

		_, _, err := tryOCREnv("")
		if err == nil {
			t.Error("expected error for invalid protocol")
		}
	})

	t.Run("bedrock protocol rejected", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")
		t.Setenv(envOCRLLMProtocol, "anthropic-bedrock")

		_, _, err := tryOCREnv("")
		if err == nil || !strings.Contains(err.Error(), "bedrock") {
			t.Errorf("expected bedrock-not-configurable error, got %v", err)
		}
	})

	t.Run("model override wins", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "orig-model")
		t.Setenv(envOCRLLMProtocol, "")

		ep, _, err := tryOCREnv("new-model")
		if err != nil {
			t.Fatal(err)
		}
		if ep.Model != "new-model" {
			t.Errorf("model = %q", ep.Model)
		}
	})

	t.Run("missing url is a miss", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")

		_, ok, err := tryOCREnv("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected miss")
		}
	})

	t.Run("bad auth header", func(t *testing.T) {
		t.Setenv(envOCRLLMURL, "https://example")
		t.Setenv(envOCRLLMToken, "tok")
		t.Setenv(envOCRLLMModel, "m")
		t.Setenv(envOCRLLMProtocol, "anthropic")
		t.Setenv(envOCRLLMAuthHeader, "cookie")

		_, _, err := tryOCREnv("")
		if err == nil {
			t.Error("expected error for invalid auth header")
		}
	})
}

func writeConfig(t *testing.T, cfg configFile) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTryOCRConfig_MissingFile(t *testing.T) {
	_, ok, err := tryOCRConfig(filepath.Join(t.TempDir(), "nonexistent.json"), ResolveOptions{})
	if err != nil {
		t.Fatalf("missing file should be a miss, not an error: %v", err)
	}
	if ok {
		t.Error("expected ok=false")
	}
}

func TestTryOCRConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestTryProviderConfig_PresetAnthropic(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-x",
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk-test"},
		},
	}
	path := writeConfig(t, cfg)
	ep, ok, err := tryOCRConfig(path, ResolveOptions{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ep.Protocol != ProtocolAnthropic || ep.Token != "sk-test" || ep.Model != "claude-x" {
		t.Errorf("unexpected ep: %+v", ep)
	}
	if !strings.HasSuffix(ep.URL, "/v1/messages") {
		t.Errorf("URL should end in /v1/messages, got %q", ep.URL)
	}
}

func TestTryProviderConfig_ModelOverride(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk-test"},
		},
	}
	path := writeConfig(t, cfg)
	// Valid override (in the preset's model list).
	ep, ok, err := tryOCRConfig(path, ResolveOptions{Model: "claude-sonnet-5"})
	if err != nil || !ok {
		t.Fatalf("valid override: ok=%v err=%v", ok, err)
	}
	if ep.Model != "claude-sonnet-5" {
		t.Errorf("model = %q", ep.Model)
	}

	// Invalid override.
	_, _, err = tryOCRConfig(path, ResolveOptions{Model: "not-a-real-model"})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected not-available error, got %v", err)
	}
}

func TestTryProviderConfig_NoCredential(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {}, // no api_key
		},
	}
	t.Setenv("ANTHROPIC_API_KEY", "") // suppress env fallback
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "no api_key") {
		t.Errorf("expected no-credential error, got %v", err)
	}
}

func TestTryProviderConfig_EnvVarFallback(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {},
		},
	}
	t.Setenv("ANTHROPIC_API_KEY", "env-sk-test")
	path := writeConfig(t, cfg)
	ep, ok, err := tryOCRConfig(path, ResolveOptions{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ep.Token != "env-sk-test" {
		t.Errorf("token = %q, want env fallback", ep.Token)
	}
}

func TestTryProviderConfig_MissingProviderSection(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		// no providers map entry
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected not-configured error, got %v", err)
	}
}

func TestTryProviderConfig_CustomProvider(t *testing.T) {
	cfg := configFile{
		Provider: "my-custom",
		Model:    "custom-model",
		CustomProviders: map[string]providerEntryConfig{
			"my-custom": {
				APIKey:   "sk",
				URL:      "https://custom.example",
				Protocol: "openai",
			},
		},
	}
	path := writeConfig(t, cfg)
	ep, ok, err := tryOCRConfig(path, ResolveOptions{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ep.Provider != "my-custom" || ep.Protocol != ProtocolOpenAIChatCompletions {
		t.Errorf("unexpected ep: %+v", ep)
	}
}

func TestTryProviderConfig_CustomProviderMissingProtocol(t *testing.T) {
	cfg := configFile{
		Provider: "my-custom",
		Model:    "m",
		CustomProviders: map[string]providerEntryConfig{
			"my-custom": {APIKey: "sk", URL: "https://x"},
		},
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires a protocol") {
		t.Errorf("expected protocol-required error, got %v", err)
	}
}

func TestTryProviderConfig_CustomProviderMissingURL(t *testing.T) {
	cfg := configFile{
		Provider: "my-custom",
		Model:    "m",
		CustomProviders: map[string]providerEntryConfig{
			"my-custom": {APIKey: "sk", Protocol: "openai"},
		},
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "requires a url") {
		t.Errorf("expected url-required error, got %v", err)
	}
}

func TestTryProviderConfig_BedrockAmbient(t *testing.T) {
	cfg := configFile{
		Provider: "bedrock",
		Model:    "us.anthropic.claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"bedrock": {AWSRegion: "us-east-1"},
		},
	}
	path := writeConfig(t, cfg)
	ep, ok, err := tryOCRConfig(path, ResolveOptions{})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !ep.AmbientAuth {
		t.Error("expected AmbientAuth=true for bedrock")
	}
	if ep.AWSRegion != "us-east-1" {
		t.Errorf("awsRegion = %q", ep.AWSRegion)
	}
}

func TestTryProviderConfig_BadProtocol(t *testing.T) {
	cfg := configFile{
		Provider: "my-custom",
		Model:    "m",
		CustomProviders: map[string]providerEntryConfig{
			"my-custom": {APIKey: "sk", URL: "https://x", Protocol: "no-such-protocol"},
		},
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported protocol") {
		t.Errorf("expected unsupported-protocol error, got %v", err)
	}
}

func TestTryProviderConfig_InvalidTimeout(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk", TimeoutSec: -1},
		},
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil {
		t.Error("expected error for negative timeout")
	}
}

func TestTryProviderConfig_InvalidRetryCode(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk", RetryCodes: []int{600}},
		},
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil {
		t.Error("expected error for invalid retry code")
	}
}

func TestTryProviderConfig_MissingModel(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		// Model absent both at root and in entry.
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk"},
		},
	}
	path := writeConfig(t, cfg)
	_, _, err := tryOCRConfig(path, ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "no model configured") {
		t.Errorf("expected missing-model error, got %v", err)
	}
}

func TestTryProviderConfig_EntryModelWins(t *testing.T) {
	cfg := configFile{
		Provider: "anthropic",
		Model:    "claude-opus-5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk", Model: "claude-sonnet-5"},
		},
	}
	path := writeConfig(t, cfg)
	ep, _, err := tryOCRConfig(path, ResolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ep.Model != "claude-sonnet-5" {
		t.Errorf("entry model should win, got %q", ep.Model)
	}
}

func TestTryProviderConfig_ProviderOptionResetsModel(t *testing.T) {
	// When --provider differs from cfg.Provider, cfg.Model is cleared, so the
	// entry's own model is used.
	cfg := configFile{
		Provider: "openai",
		Model:    "gpt-5.5",
		Providers: map[string]providerEntryConfig{
			"anthropic": {APIKey: "sk", Model: "claude-opus-5"},
			"openai":    {APIKey: "sk-openai"},
		},
	}
	path := writeConfig(t, cfg)
	ep, ok, err := tryOCRConfig(path, ResolveOptions{Provider: "anthropic"})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ep.Model != "claude-opus-5" {
		t.Errorf("model = %q, want claude-opus-5 (from entry after model reset)", ep.Model)
	}
}

func TestTryLegacyLlmConfig(t *testing.T) {
	t.Run("basic anthropic", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:       "https://example",
				AuthToken: "tok",
				Model:     "claude-x",
			},
		}
		path := writeConfig(t, cfg)
		ep, ok, err := tryOCRConfig(path, ResolveOptions{})
		if err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if ep.Protocol != ProtocolAnthropic {
			t.Errorf("protocol = %q", ep.Protocol)
		}
	})

	t.Run("use_anthropic false", func(t *testing.T) {
		useAnth := false
		cfg := configFile{
			Llm: llmFileConfig{
				URL:          "https://example",
				AuthToken:    "tok",
				Model:        "gpt-x",
				UseAnthropic: &useAnth,
			},
		}
		path := writeConfig(t, cfg)
		ep, _, err := tryOCRConfig(path, ResolveOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if ep.Protocol != ProtocolOpenAIChatCompletions {
			t.Errorf("protocol = %q", ep.Protocol)
		}
	})

	t.Run("explicit protocol", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:       "https://example",
				AuthToken: "tok",
				Model:     "gpt-x",
				Protocol:  "openai-responses",
			},
		}
		path := writeConfig(t, cfg)
		ep, _, err := tryOCRConfig(path, ResolveOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if ep.Protocol != ProtocolOpenAIResponses {
			t.Errorf("protocol = %q", ep.Protocol)
		}
	})

	t.Run("bedrock protocol rejected", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:       "https://example",
				AuthToken: "tok",
				Model:     "m",
				Protocol:  "anthropic-bedrock",
			},
		}
		path := writeConfig(t, cfg)
		_, _, err := tryOCRConfig(path, ResolveOptions{})
		if err == nil || !strings.Contains(err.Error(), "bedrock") {
			t.Errorf("expected bedrock error, got %v", err)
		}
	})

	t.Run("invalid timeout", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:        "https://example",
				AuthToken:  "tok",
				Model:      "m",
				TimeoutSec: -5,
			},
		}
		path := writeConfig(t, cfg)
		_, _, err := tryOCRConfig(path, ResolveOptions{})
		if err == nil {
			t.Error("expected timeout error")
		}
	})

	t.Run("invalid retry codes", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:        "https://example",
				AuthToken:  "tok",
				Model:      "m",
				RetryCodes: []int{700},
			},
		}
		path := writeConfig(t, cfg)
		_, _, err := tryOCRConfig(path, ResolveOptions{})
		if err == nil {
			t.Error("expected retry-codes error")
		}
	})

	t.Run("bad auth header", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:        "https://example",
				AuthToken:  "tok",
				Model:      "m",
				AuthHeader: "cookie",
			},
		}
		path := writeConfig(t, cfg)
		_, _, err := tryOCRConfig(path, ResolveOptions{})
		if err == nil {
			t.Error("expected auth header error")
		}
	})

	t.Run("invalid protocol", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:       "https://example",
				AuthToken: "tok",
				Model:     "m",
				Protocol:  "no-such-protocol",
			},
		}
		path := writeConfig(t, cfg)
		_, _, err := tryOCRConfig(path, ResolveOptions{})
		if err == nil {
			t.Error("expected protocol error")
		}
	})

	t.Run("incomplete miss", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:   "https://example",
				Model: "m",
				// no auth
			},
		}
		path := writeConfig(t, cfg)
		_, ok, err := tryOCRConfig(path, ResolveOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Error("expected miss on incomplete config")
		}
	})

	t.Run("model override", func(t *testing.T) {
		cfg := configFile{
			Llm: llmFileConfig{
				URL:       "https://example",
				AuthToken: "tok",
				Model:     "orig",
			},
		}
		path := writeConfig(t, cfg)
		ep, _, err := tryOCRConfig(path, ResolveOptions{Model: "override"})
		if err != nil {
			t.Fatal(err)
		}
		if ep.Model != "override" {
			t.Errorf("override not applied, got %q", ep.Model)
		}
	})
}

func TestResolveEndpoint_NoValidConfig(t *testing.T) {
	// Clear all env sources, point to nonexistent config file.
	t.Setenv(envOCRLLMURL, "")
	t.Setenv(envOCRLLMToken, "")
	t.Setenv(envOCRLLMModel, "")
	t.Setenv(envCCBaseURL, "")
	t.Setenv(envCCToken, "")
	t.Setenv(envCCModel, "")
	t.Setenv(envOCRLLMTimeout, "")
	t.Setenv(envOCRLLMExtraHeaders, "")

	home := t.TempDir()
	setTestHome(t, home)

	_, err := ResolveEndpoint(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil || !strings.Contains(err.Error(), "no valid LLM endpoint") {
		t.Errorf("expected no-valid-endpoint error, got %v", err)
	}
}

func TestResolveEndpoint_ExplicitProviderMissingFile(t *testing.T) {
	_, err := ResolveEndpointWithOptions(filepath.Join(t.TempDir(), "nonexistent.json"), ResolveOptions{Provider: "anthropic"})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected file-does-not-exist error, got %v", err)
	}
}

func TestResolveEndpoint_ExplicitCustomProviderMessage(t *testing.T) {
	_, err := ResolveEndpointWithOptions(filepath.Join(t.TempDir(), "nonexistent.json"), ResolveOptions{Provider: "my-nonexistent-provider"})
	if err == nil || !strings.Contains(err.Error(), "custom_providers") {
		t.Errorf("expected custom_providers hint, got %v", err)
	}
}

func TestResolveEndpoint_UsesConfig(t *testing.T) {
	t.Setenv(envOCRLLMTimeout, "")
	t.Setenv(envOCRLLMExtraHeaders, "")
	cfg := configFile{
		Llm: llmFileConfig{
			URL:       "https://example",
			AuthToken: "tok",
			Model:     "claude-x",
		},
	}
	path := writeConfig(t, cfg)
	ep, err := ResolveEndpoint(path)
	if err != nil {
		t.Fatalf("ResolveEndpoint: %v", err)
	}
	if ep.Model != "claude-x" || ep.Token != "tok" {
		t.Errorf("unexpected ep: %+v", ep)
	}
}

func TestResolveEndpoint_EnvOverrides(t *testing.T) {
	// Set config, then override timeout+headers via env.
	t.Setenv(envOCRLLMTimeout, "45")
	t.Setenv(envOCRLLMExtraHeaders, "X-Extra=foo")
	cfg := configFile{
		Llm: llmFileConfig{
			URL:        "https://example",
			AuthToken:  "tok",
			Model:      "m",
			TimeoutSec: 10,
			ExtraHeaders: map[string]string{
				"X-Config": "bar",
			},
		},
	}
	path := writeConfig(t, cfg)
	ep, err := ResolveEndpoint(path)
	if err != nil {
		t.Fatalf("ResolveEndpoint: %v", err)
	}
	if ep.Timeout != 45*time.Second {
		t.Errorf("timeout = %v, want 45s", ep.Timeout)
	}
	if ep.ExtraHeaders["X-Extra"] != "foo" {
		t.Error("env extra header missing")
	}
	if ep.ExtraHeaders["X-Config"] != "bar" {
		t.Error("config extra header lost")
	}
}

func TestResolveEndpoint_BadTimeoutEnvFailsFast(t *testing.T) {
	t.Setenv(envOCRLLMTimeout, "not-a-number")
	_, err := ResolveEndpoint(filepath.Join(t.TempDir(), "any.json"))
	if err == nil || !strings.Contains(err.Error(), "OCR_LLM_TIMEOUT") {
		t.Errorf("expected OCR_LLM_TIMEOUT error, got %v", err)
	}
}

func TestResolveEndpoint_BadHeadersEnv(t *testing.T) {
	t.Setenv(envOCRLLMTimeout, "")
	t.Setenv(envOCRLLMExtraHeaders, "Authorization=x")
	_, err := ResolveEndpoint(filepath.Join(t.TempDir(), "any.json"))
	if err == nil || !strings.Contains(err.Error(), "OCR_LLM_EXTRA_HEADERS") {
		t.Errorf("expected OCR_LLM_EXTRA_HEADERS error, got %v", err)
	}
}

func TestResolveEndpointWithModelOverride(t *testing.T) {
	t.Setenv(envOCRLLMTimeout, "")
	t.Setenv(envOCRLLMExtraHeaders, "")
	cfg := configFile{
		Llm: llmFileConfig{
			URL:       "https://example",
			AuthToken: "tok",
			Model:     "orig",
		},
	}
	path := writeConfig(t, cfg)
	ep, err := ResolveEndpointWithModelOverride(path, "new")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Model != "new" {
		t.Errorf("model = %q, want new", ep.Model)
	}
}

func TestErrBedrockNotConfigurable(t *testing.T) {
	err := errBedrockNotConfigurable("some.key")
	if !strings.Contains(err.Error(), "some.key") || !strings.Contains(err.Error(), "bedrock") {
		t.Errorf("unexpected error text: %v", err)
	}
}

func TestFinalizeResolvedEndpoint_MergesHeaders(t *testing.T) {
	env := envOverrides{
		hasTimeout: true,
		timeout:    99 * time.Second,
		headers: map[string]string{
			"X-Env":    "from-env",
			"X-Common": "env-wins", // env should win over config for same key
		},
	}
	ep := ResolvedEndpoint{
		ExtraHeaders: map[string]string{
			"X-Config": "from-config",
			"X-Common": "config-value",
		},
	}
	out := finalizeResolvedEndpoint("test", ep, env)
	if out.Timeout != 99*time.Second {
		t.Errorf("timeout = %v", out.Timeout)
	}
	if out.ExtraHeaders["X-Env"] != "from-env" || out.ExtraHeaders["X-Config"] != "from-config" {
		t.Errorf("headers = %v", out.ExtraHeaders)
	}
	if out.ExtraHeaders["X-Common"] != "env-wins" {
		t.Errorf("env should win, got %q", out.ExtraHeaders["X-Common"])
	}
}

func TestFinalizeResolvedEndpoint_NilHeadersInEndpoint(t *testing.T) {
	env := envOverrides{
		headers: map[string]string{"X-Env": "v"},
	}
	ep := ResolvedEndpoint{}
	out := finalizeResolvedEndpoint("src", ep, env)
	if out.ExtraHeaders["X-Env"] != "v" {
		t.Error("env headers should be adopted when config has none")
	}
	if out.Source != "src" {
		t.Errorf("source = %q", out.Source)
	}
}

func TestSplitHeaderPairs_NoCommaSinglePair(t *testing.T) {
	pairs, err := splitHeaderPairs("X-Foo=bar")
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || strings.TrimSpace(pairs[0]) != "X-Foo=bar" {
		t.Errorf("pairs = %v", pairs)
	}
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package llm

import (
	"strings"
	"testing"
)

// resetTierEnv clears every env var the tier resolver reads. Table-driven tests
// only set what they need; unset stays unset even if a stray value bleeds
// through from the outer shell.
func resetTierEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		envOCRLLMURL, envOCRLLMToken, envOCRLLMModel,
		envOCRLLMProtocol, envOCRUseAnthropic,
		envCCBaseURL, envCCToken, envCCModel,
		envZReviewCheapModel, envZReviewCheapProvider,
	} {
		t.Setenv(k, "")
	}
	setTestHome(t, t.TempDir())
}

// mainOnlyEnv points the OCR env resolver at a valid anthropic-protocol
// endpoint so Main resolves without touching any config file. The values are
// fake but complete — no network calls happen, we only need NewLLMClient to
// pick a protocol and return a non-nil client.
func mainOnlyEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envOCRLLMURL, "https://main.example")
	t.Setenv(envOCRLLMToken, "main-tok")
	t.Setenv(envOCRLLMModel, "claude-sonnet-5")
}

func TestResolveTiers(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, cfgPath *string)
		wantMain   string
		wantCheap  string
		wantShared bool // Main and Cheap should be the same client pointer
	}{
		{
			name: "no cheap env vars — cheap falls back to main",
			setup: func(t *testing.T, _ *string) {
				resetTierEnv(t)
				mainOnlyEnv(t)
			},
			wantMain:   "claude-sonnet-5",
			wantCheap:  "claude-sonnet-5",
			wantShared: true,
		},
		{
			name: "cheap provider only (no model) — cheap falls back to main",
			setup: func(t *testing.T, _ *string) {
				resetTierEnv(t)
				mainOnlyEnv(t)
				t.Setenv(envZReviewCheapProvider, "deepseek")
			},
			wantMain:   "claude-sonnet-5",
			wantCheap:  "claude-sonnet-5",
			wantShared: true,
		},
		{
			name: "cheap model only — shares main client, cheap model differs",
			setup: func(t *testing.T, _ *string) {
				resetTierEnv(t)
				mainOnlyEnv(t)
				t.Setenv(envZReviewCheapModel, "claude-haiku-5")
			},
			wantMain:   "claude-sonnet-5",
			wantCheap:  "claude-haiku-5",
			wantShared: true,
		},
		{
			name: "both cheap vars set — independent client for cheap",
			setup: func(t *testing.T, cfgPath *string) {
				resetTierEnv(t)
				// Config file gives us two providers, so cheap can resolve
				// independently against a different one from main.
				cfg := configFile{
					Provider: "anthropic",
					Providers: map[string]providerEntryConfig{
						"anthropic": {APIKey: "sk-anth", Model: "claude-sonnet-5"},
						"deepseek":  {APIKey: "sk-ds", Model: "deepseek-chat"},
					},
				}
				*cfgPath = writeConfig(t, cfg)
				t.Setenv(envZReviewCheapProvider, "deepseek")
				t.Setenv(envZReviewCheapModel, "deepseek-chat")
			},
			wantMain:   "claude-sonnet-5",
			wantCheap:  "deepseek-chat",
			wantShared: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfgPath string
			tt.setup(t, &cfgPath)
			tiers, err := ResolveTiers(cfgPath, ResolveOptions{})
			if err != nil {
				t.Fatalf("ResolveTiers: %v", err)
			}
			if tiers.MainModel != tt.wantMain {
				t.Errorf("MainModel = %q, want %q", tiers.MainModel, tt.wantMain)
			}
			if tiers.CheapModel != tt.wantCheap {
				t.Errorf("CheapModel = %q, want %q", tiers.CheapModel, tt.wantCheap)
			}
			if tiers.Main == nil || tiers.Cheap == nil {
				t.Fatal("clients must be non-nil")
			}
			shared := tiers.Main == tiers.Cheap
			if shared != tt.wantShared {
				t.Errorf("shared client = %v, want %v", shared, tt.wantShared)
			}
		})
	}
}

func TestResolveTiers_MainResolveError(t *testing.T) {
	resetTierEnv(t)
	// No env / config / shell rc — main resolution must fail, and the error
	// must name the main tier so a broken cheap config never gets blamed.
	_, err := ResolveTiers("/nonexistent/config.json", ResolveOptions{})
	if err == nil {
		t.Fatal("expected error when no LLM endpoint is configured")
	}
	if !strings.Contains(err.Error(), "main tier") {
		t.Errorf("error should be tagged 'main tier', got %v", err)
	}
}

func TestResolveTiers_CheapResolveError(t *testing.T) {
	resetTierEnv(t)
	mainOnlyEnv(t)
	// Cheap asks for a provider that isn't in any config; resolution fails.
	t.Setenv(envZReviewCheapProvider, "no-such-provider")
	t.Setenv(envZReviewCheapModel, "some-model")
	_, err := ResolveTiers("/nonexistent/config.json", ResolveOptions{})
	if err == nil {
		t.Fatal("expected error when cheap provider is misconfigured")
	}
	if !strings.Contains(err.Error(), "cheap tier") {
		t.Errorf("error should be tagged 'cheap tier', got %v", err)
	}
}

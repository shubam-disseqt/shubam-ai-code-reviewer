// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 shubam-ai-code-reviewer contributors
// Adapted from alibaba/open-code-review internal/llm/providers_test.go
// (trimmed to the 5 remaining presets).

package llm

import (
	"sort"
	"strings"
	"testing"
)

func TestLookupProvider_KnownProviders(t *testing.T) {
	names := []string{"anthropic", "openai", "openai-responses", "deepseek"}
	for _, name := range names {
		p, ok := LookupProvider(name)
		if !ok {
			t.Errorf("LookupProvider(%q) returned false, want true", name)
			continue
		}
		if p.Name != name {
			t.Errorf("LookupProvider(%q).Name = %q", name, p.Name)
		}
		if p.Protocol == "" {
			t.Errorf("LookupProvider(%q).Protocol is empty", name)
		}
		if p.BaseURL == "" {
			t.Errorf("LookupProvider(%q).BaseURL is empty", name)
		}
		if len(p.Models) == 0 {
			t.Errorf("LookupProvider(%q).Models is empty", name)
		}
	}
}

func TestLookupProvider_Unknown(t *testing.T) {
	_, ok := LookupProvider("nonexistent-provider")
	if ok {
		t.Error("LookupProvider(nonexistent) returned true, want false")
	}
}

func TestListProviders_Order(t *testing.T) {
	providers := ListProviders()
	expected := []string{"anthropic", "bedrock", "deepseek", "openai", "openai-responses"}
	if len(providers) != len(expected) {
		t.Fatalf("expected %d providers, got %d", len(expected), len(providers))
	}
	for i, name := range expected {
		if providers[i].Name != name {
			t.Errorf("providers[%d].Name = %q, want %q", i, providers[i].Name, name)
		}
	}
}

func TestListProviders_ReturnsCopy(t *testing.T) {
	p1 := ListProviders()
	p1[0].Name = "mutated"

	p2 := ListProviders()
	if p2[0].Name == "mutated" {
		t.Error("ListProviders returns a reference to the registry, should return a copy")
	}
}

func TestLookupProvider_ReturnsCopyOfModels(t *testing.T) {
	p1, _ := LookupProvider("anthropic")
	p1.Models[0] = "mutated"

	p2, _ := LookupProvider("anthropic")
	if p2.Models[0] == "mutated" {
		t.Error("LookupProvider returns a reference to Models slice, should return a copy")
	}
}

func TestLookupProvider_PreservesModelOrder(t *testing.T) {
	p, ok := LookupProvider("anthropic")
	if !ok {
		t.Fatal("anthropic not found")
	}
	expected := []string{
		"claude-opus-5",
		"claude-sonnet-5",
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-opus-4-6",
		"claude-sonnet-4-6",
	}
	if len(p.Models) != len(expected) {
		t.Fatalf("expected %d models, got %d", len(expected), len(p.Models))
	}
	for i, model := range expected {
		if p.Models[i] != model {
			t.Errorf("Models[%d] = %q, want %q", i, p.Models[i], model)
		}
	}
}

func TestListProviders_ReturnsSortedProviders(t *testing.T) {
	providers := ListProviders()
	names := make([]string, len(providers))
	for i, p := range providers {
		names[i] = p.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("providers are not sorted: %v", names)
	}
}

func TestLookupProvider_AnthropicDetails(t *testing.T) {
	p, ok := LookupProvider("anthropic")
	if !ok {
		t.Fatal("anthropic not found")
	}
	if p.Protocol != "anthropic" {
		t.Errorf("Protocol = %q, want %q", p.Protocol, "anthropic")
	}
	if p.AuthHeader != "x-api-key" {
		t.Errorf("AuthHeader = %q, want %q", p.AuthHeader, "x-api-key")
	}
	if p.EnvVar != "ANTHROPIC_API_KEY" {
		t.Errorf("EnvVar = %q, want %q", p.EnvVar, "ANTHROPIC_API_KEY")
	}
}

func TestLookupProvider_OpenAIDetails(t *testing.T) {
	p, ok := LookupProvider("openai")
	if !ok {
		t.Fatal("openai not found")
	}
	if p.Protocol != ProtocolOpenAIChatCompletions {
		t.Errorf("Protocol = %q, want %q", p.Protocol, ProtocolOpenAIChatCompletions)
	}
	if p.AuthHeader != "" {
		t.Errorf("AuthHeader = %q, want empty", p.AuthHeader)
	}
	expectedModels := []string{
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
	}
	if len(p.Models) != len(expectedModels) {
		t.Fatalf("Models length = %d, want %d", len(p.Models), len(expectedModels))
	}
	for i, model := range expectedModels {
		if p.Models[i] != model {
			t.Errorf("Models[%d] = %q, want %q", i, p.Models[i], model)
		}
	}
}

func TestLookupProvider_DeepSeekCatalog(t *testing.T) {
	p, ok := LookupProvider("deepseek")
	if !ok {
		t.Fatal("deepseek not found")
	}
	// deepseek-chat must be present and first — it's the default when no
	// SACR_MODEL is set. deepseek-reasoner is the reasoning-tier variant.
	// Previous fixtures used aspirational names (deepseek-v4-*, deepseek-flash)
	// that don't exist on the live platform; keep the test grounded in what
	// api.deepseek.com actually returns.
	if len(p.Models) == 0 || p.Models[0] != "deepseek-chat" {
		t.Errorf("deepseek default model should be deepseek-chat, got %v", p.Models)
	}
	if !ModelListContains(p.Models, "deepseek-reasoner") {
		t.Error(`deepseek models do not contain "deepseek-reasoner"`)
	}
}

func TestLookupProvider_OpenAIResponsesDetails(t *testing.T) {
	const expectedEnvVar = "OPENAI_RESPONSES_API_KEY"

	p, ok := LookupProvider("openai-responses")
	if !ok {
		t.Fatal("openai-responses not found")
	}
	if p.Protocol != ProtocolOpenAIResponses {
		t.Errorf("Protocol = %q, want %q", p.Protocol, ProtocolOpenAIResponses)
	}
	if p.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, "https://api.openai.com/v1")
	}
	if p.EnvVar != expectedEnvVar {
		t.Errorf("EnvVar = %q, want %q", p.EnvVar, expectedEnvVar)
	}
	if p.AuthHeader != "" {
		t.Errorf("AuthHeader = %q, want empty (OpenAI-compatible uses Bearer by default)", p.AuthHeader)
	}
}

// TestGPT56ModelsUseResponsesProtocol guards issue #559: GPT-5.6 models require
// the Responses API. Any built-in provider serving api.openai.com must not offer
// a gpt-5.6 model over Chat Completions.
func TestGPT56ModelsUseResponsesProtocol(t *testing.T) {
	const gpt56Prefix = "gpt-5.6"
	found := false
	for _, p := range ListProviders() {
		if !strings.Contains(p.BaseURL, "api.openai.com") {
			continue
		}
		for _, m := range p.Models {
			if !strings.HasPrefix(m, gpt56Prefix) {
				continue
			}
			found = true
			if p.Protocol != ProtocolOpenAIResponses {
				t.Errorf("provider %q offers %q over %q; GPT-5.6 needs %q", p.Name, m, p.Protocol, ProtocolOpenAIResponses)
			}
		}
	}
	if !found {
		t.Fatalf("no built-in OpenAI provider offers a %q model; expected the openai-responses preset to serve them", gpt56Prefix)
	}
}

// TestProviders_AllProtocolsCanonical verifies every registry entry uses a
// canonical protocol constant.
func TestProviders_AllProtocolsCanonical(t *testing.T) {
	for _, p := range ListProviders() {
		if NormalizeProtocol(p.Protocol) != p.Protocol {
			t.Errorf("provider %q Protocol %q is not in canonical form", p.Name, p.Protocol)
		}
		if err := ValidateProtocol(p.Protocol); err != nil {
			t.Errorf("provider %q has non-canonical Protocol %q: %v", p.Name, p.Protocol, err)
		}
	}
}

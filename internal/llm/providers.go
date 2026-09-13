// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors
// Portions Copyright 2026 disseqt
// Adapted from alibaba/open-code-review internal/llm/providers.go
// (trimmed to 5 presets: Anthropic, OpenAI, OpenAI-Responses, Bedrock, DeepSeek).

package llm

import (
	"sort"
	"strings"
)

// Provider holds the preset configuration for a known LLM provider.
//
// Protocol uses the canonical names defined in protocol.go:
//   - ProtocolAnthropic ("anthropic")
//   - ProtocolOpenAIChatCompletions ("openai")
//   - ProtocolOpenAIResponses ("openai-responses")
//   - ProtocolAnthropicBedrock ("anthropic-bedrock")
//
// To add a built-in provider that speaks a different protocol, set Protocol
// accordingly and ensure NewLLMClient has a matching case.
type Provider struct {
	Name        string
	DisplayName string
	Protocol    string
	BaseURL     string
	AuthHeader  string // Anthropic-only; empty for OpenAI-compatible
	EnvVar      string // environment variable name for API key fallback
	Models      []string

	// AmbientAuth marks a provider whose credentials come from the
	// environment's own chain rather than an api_key — AWS SigV4, for
	// instance. The resolver skips its api_key requirement for these, because
	// there is no key to configure and demanding one would make the provider
	// impossible to use.
	AmbientAuth bool
}

var registry = []Provider{
	{
		Name:        "anthropic",
		DisplayName: "Anthropic Claude API",
		Protocol:    ProtocolAnthropic,
		BaseURL:     "https://api.anthropic.com",
		AuthHeader:  "x-api-key",
		EnvVar:      "ANTHROPIC_API_KEY",
		Models: []string{
			"claude-opus-5",
			"claude-sonnet-5",
			"claude-opus-4-8",
			"claude-opus-4-7",
			"claude-opus-4-6",
			"claude-sonnet-4-6",
		},
	},
	{
		// Bedrock takes no api_key and no base URL: the SDK's bedrock
		// middleware derives the host from the resolved AWS region and signs
		// each request from the ambient credential chain (profile, SSO,
		// instance role, or AWS_* variables). Set AWS_REGION or AWS_PROFILE the
		// way any other AWS tool expects.
		//
		// Model accepts anything Bedrock will route: a foundation model ID, an
		// inference profile ID, or the ARN of an application inference profile
		// when usage needs to be attributed for cost allocation. Run
		// `aws bedrock list-inference-profiles` to see what an account offers —
		// IDs differ per account and per region, so the list below is only a
		// starting point.
		Name:        "bedrock",
		DisplayName: "AWS Bedrock (Anthropic models)",
		Protocol:    ProtocolAnthropicBedrock,
		AmbientAuth: true,
		Models: []string{
			"us.anthropic.claude-opus-5",
			"us.anthropic.claude-sonnet-5",
			"us.anthropic.claude-opus-4-8",
			"us.anthropic.claude-opus-4-7",
			"us.anthropic.claude-sonnet-4-6",
			"global.anthropic.claude-opus-5",
			"global.anthropic.claude-sonnet-5",
			"global.anthropic.claude-opus-4-8",
		},
	},
	{
		Name:        "openai",
		DisplayName: "OpenAI API",
		Protocol:    ProtocolOpenAIChatCompletions,
		BaseURL:     "https://api.openai.com/v1",
		EnvVar:      "OPENAI_API_KEY",
		Models: []string{
			"gpt-5.5",
			"gpt-5.4",
			"gpt-5.4-mini",
		},
	},
	{
		Name:        "openai-responses",
		DisplayName: "OpenAI Responses API",
		Protocol:    ProtocolOpenAIResponses,
		BaseURL:     "https://api.openai.com/v1",
		EnvVar:      "OPENAI_RESPONSES_API_KEY",
		Models: []string{
			"gpt-5.6-sol",
			"gpt-5.6-terra",
			"gpt-5.6-luna",
		},
	},
	{
		Name:        "deepseek",
		DisplayName: "DeepSeek API",
		Protocol:    ProtocolOpenAIChatCompletions,
		BaseURL:     "https://api.deepseek.com",
		EnvVar:      "DEEPSEEK_API_KEY",
		Models: []string{
			"deepseek-v4-pro",
			"deepseek-v4-flash",
			"deepseek-flash",
		},
	},
}

var registryMap map[string]Provider

func init() {
	registryMap = make(map[string]Provider, len(registry))
	for _, p := range registry {
		registryMap[strings.ToLower(p.Name)] = p
	}
}

// LookupProvider returns the preset provider by name.
// The returned Provider has its own copy of the Models slice.
func LookupProvider(name string) (Provider, bool) {
	p, ok := registryMap[strings.ToLower(strings.TrimSpace(name))]
	if ok {
		p = copyProvider(p)
	}
	return p, ok
}

// ListProviders returns all built-in providers sorted by provider name.
// Each returned Provider has its own copy of the Models slice in registry order.
func ListProviders() []Provider {
	out := make([]Provider, len(registry))
	for i, p := range registry {
		out[i] = copyProvider(p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func copyProvider(p Provider) Provider {
	if p.Models != nil {
		models := make([]string, len(p.Models))
		copy(models, p.Models)
		p.Models = models
	}
	return p
}

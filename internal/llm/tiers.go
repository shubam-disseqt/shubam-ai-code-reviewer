// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package llm

import (
	"fmt"
	"os"
	"strings"
)

// Env vars that opt in to cheap-tier routing. Both must be set for a fully
// independent cheap client. Only SACR_CHEAP_MODEL set (with no provider
// override) shares the main client but overrides the model per call.
const (
	envZReviewCheapModel    = "SACR_CHEAP_MODEL"
	envZReviewCheapProvider = "SACR_CHEAP_PROVIDER"
)

// Tiers holds the two LLM clients sacr routes calls through. Main serves the
// reviewer agent loop (Sonnet-class); Cheap serves structured summary and
// labeling calls (Haiku / Flash / DeepSeek). When cheap-tier env vars are
// unset, Cheap is Main — same client, same model — so callers can always dial
// Cheap without a nil check or a fallback path.
type Tiers struct {
	Main       LLMClient
	Cheap      LLMClient
	MainModel  string
	CheapModel string
	// Notes carries non-fatal setup warnings that callers should surface
	// to the operator. Example: a partial cheap-tier config that silently
	// routes Phase 15 traffic through the full-price main tier.
	Notes []string
}

// ResolveTiers resolves both LLM tiers in one pass.
//
// Main is always resolved from opts (typically SACR_PROVIDER + SACR_MODEL).
// Cheap opts in via SACR_CHEAP_PROVIDER + SACR_CHEAP_MODEL:
//
//   - both set: Cheap is resolved independently. New underlying client.
//   - only SACR_CHEAP_MODEL: Cheap reuses the Main client and provider, but
//     CheapModel is the override — cost win with zero extra HTTP setup.
//   - only SACR_CHEAP_PROVIDER (no model), or neither: Cheap == Main.
//
// The one-client-when-possible rule keeps the hot path a single HTTP pool and
// a single set of retry middlewares. It also means a broken cheap-provider
// config never accidentally breaks the reviewer path.
func ResolveTiers(configPath string, opts ResolveOptions) (Tiers, error) {
	mainEp, err := ResolveEndpointWithOptions(configPath, opts)
	if err != nil {
		return Tiers{}, fmt.Errorf("main tier: %w", err)
	}
	main := NewLLMClient(mainEp)

	cheapProvider := strings.TrimSpace(os.Getenv(envZReviewCheapProvider))
	cheapModel := strings.TrimSpace(os.Getenv(envZReviewCheapModel))

	// Fallback shape: no independent config. Cheap is Main.
	if cheapProvider == "" && cheapModel == "" {
		return Tiers{Main: main, Cheap: main, MainModel: mainEp.Model, CheapModel: mainEp.Model}, nil
	}
	if cheapProvider == "" {
		// Model override only — reuse Main's client. CheapModel differs.
		return Tiers{Main: main, Cheap: main, MainModel: mainEp.Model, CheapModel: cheapModel}, nil
	}
	if cheapModel == "" {
		// Provider override alone is meaningless without a model — this is
		// the silent-full-price case. Surface it so the operator knows the
		// summarizer + labeler are still running at main-tier cost.
		return Tiers{
			Main: main, Cheap: main,
			MainModel: mainEp.Model, CheapModel: mainEp.Model,
			Notes: []string{
				"SACR_CHEAP_PROVIDER is set but SACR_CHEAP_MODEL is not — cheap tier is falling back to the main tier at full price. Set SACR_CHEAP_MODEL to a smaller model (e.g. claude-haiku-4-5) to activate cost routing.",
			},
		}, nil
	}

	cheapEp, err := ResolveEndpointWithOptions(configPath, ResolveOptions{
		Provider: cheapProvider,
		Model:    cheapModel,
	})
	if err != nil {
		return Tiers{}, fmt.Errorf("cheap tier: %w", err)
	}
	return Tiers{
		Main:       main,
		Cheap:      NewLLMClient(cheapEp),
		MainModel:  mainEp.Model,
		CheapModel: cheapEp.Model,
	}, nil
}

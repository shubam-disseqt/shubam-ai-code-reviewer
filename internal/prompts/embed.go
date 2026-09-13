// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt Contributors
// Portions Copyright 2026 alibaba/open-code-review Contributors
// Adapted from alibaba/open-code-review internal/config/template/prompts

// Package prompts embeds the prompt templates used by the review agent.
package prompts

import "embed"

//go:embed *.md
var Templates embed.FS

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package rules exposes bundled semgrep rule presets as embedded assets.
// Semgrep expects a filesystem path for --config, so callers materialise
// the raw YAML into a temp file at run time.
package rules

import _ "embed"

//go:embed javascript.yml
var JavaScript []byte

//go:embed python.yml
var Python []byte

//go:embed ruby.yml
var Ruby []byte

//go:embed golang.yml
var Go []byte

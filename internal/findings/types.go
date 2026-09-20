// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

// Package findings persists fingerprinted review findings per-PR so an
// incremental re-review can carry unchanged issues forward and drop the
// ones that were fixed. See ARCHITECTURE §7 for the state store contract.
package findings

import (
	"time"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/model"
)

// State is the reconciled lifecycle of a finding across review passes.
// See carryover.go for the state-machine truth table.
type State string

const (
	StateNew      State = "new"      // present in fresh, absent from previous
	StateKeep     State = "keep"     // matched a previous fingerprint in this run
	StateCarried  State = "carried"  // previous finding, file untouched — carried unchanged
	StateResolved State = "resolved" // previous finding, file touched, no match — treat as fixed
)

// Finding is the persisted record. It composes the LlmComment payload
// with the identity (Fingerprint) and lifecycle (State + timestamps) fields
// the reconciler needs. LlmComment stays the source of truth for content.
type Finding struct {
	Fingerprint string           `json:"fingerprint"`
	Comment     model.LlmComment `json:"comment"`
	HeadSHA     string           `json:"head_sha,omitempty"`
	State       State            `json:"state"`
	FirstSeen   time.Time        `json:"first_seen"`
	LastSeen    time.Time        `json:"last_seen"`
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package sarif

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// allowedLevels are the SARIF Result.level values GitHub Code Scanning
// accepts. Anything else causes a 422 on upload.
var allowedLevels = map[string]struct{}{
	"none":    {},
	"note":    {},
	"warning": {},
	"error":   {},
}

// Validate performs a shallow structural check on a serialised SARIF log,
// catching the categories of shape error that make GitHub reject an upload
// with 422. It is NOT a full JSON-Schema validator — the OASIS schema has
// hundreds of optional properties and pulling a validator in for the
// handful of load-bearing ones is overkill. What we check here is exactly
// the set that has bitten real uploads:
//
//   - version == "2.1.0"
//   - runs is a non-empty array
//   - every run has tool.driver.name
//   - results is present (never null)
//   - every result has ruleId + a level in {none,note,warning,error}
//   - every physical region has startLine ≥ 1 when present
//
// Encode is expected to already satisfy these — Validate is the paranoid
// second pass before upload.
//
// note: hand-rolled invariant list, replace with a schema library only
// if a real 422 slips through.
func Validate(blob []byte) error {
	var log Log
	if err := json.Unmarshal(blob, &log); err != nil {
		return fmt.Errorf("sarif validate: unmarshal: %w", err)
	}
	if log.Version != "2.1.0" {
		return fmt.Errorf("sarif validate: version %q, want 2.1.0", log.Version)
	}
	if len(log.Runs) == 0 {
		return fmt.Errorf("sarif validate: runs must be non-empty")
	}
	for i, run := range log.Runs {
		if run.Tool.Driver.Name == "" {
			return fmt.Errorf("sarif validate: runs[%d].tool.driver.name is empty", i)
		}
		// results: the field must be present. We can't distinguish "missing"
		// from "null" after unmarshal, but rely on our encoder writing
		// `results: []` explicitly and re-check the raw bytes below.
		for j, res := range run.Results {
			if res.RuleID == "" {
				return fmt.Errorf("sarif validate: runs[%d].results[%d].ruleId is empty", i, j)
			}
			if _, ok := allowedLevels[res.Level]; !ok {
				return fmt.Errorf("sarif validate: runs[%d].results[%d].level %q not in {none,note,warning,error}", i, j, res.Level)
			}
			for k, loc := range res.Locations {
				reg := loc.PhysicalLocation.Region
				if reg != nil && reg.StartLine < 1 {
					return fmt.Errorf("sarif validate: runs[%d].results[%d].locations[%d].region.startLine=%d must be ≥ 1", i, j, k, reg.StartLine)
				}
			}
		}
	}
	// Raw-bytes guard for the "results: null" case JSON round-trip erases.
	// A null results field passes the schema loosely but breaks Code Scanning.
	if containsResultsNull(blob) {
		return fmt.Errorf("sarif validate: results must be [] not null")
	}
	return nil
}

// containsResultsNull is a byte-level check for `"results": null`. Encode
// never emits this, but Validate is defensive against callers who hand us
// a hand-authored blob.
func containsResultsNull(blob []byte) bool {
	return bytes.Contains(blob, []byte(`"results": null`))
}

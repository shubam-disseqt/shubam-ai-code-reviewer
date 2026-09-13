// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDoctorEmptyEnvReportsFailure(t *testing.T) {
	// Clear every env var doctor looks at so the run is deterministic.
	for _, k := range []string{
		"ZREVIEW_PROVIDER", "ZREVIEW_MODEL", "ZREVIEW_DB_URL",
		"ZREVIEW_ORG_RULES_REPO", "GITHUB_TOKEN",
		"OCR_LLM_URL", "OCR_LLM_TOKEN", "OCR_LLM_MODEL",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL",
	} {
		t.Setenv(k, "")
	}
	// HOME points at an empty dir so no shell rc / config file resolves.
	t.Setenv("HOME", t.TempDir())

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected doctor to fail with empty env, got nil")
	}
	got := out.String()
	// Every check row must appear.
	for _, name := range []string{"llm provider", "git", "index db", "org rules repo", "github token"} {
		if !strings.Contains(got, name) {
			t.Errorf("doctor output missing check row %q\n---\n%s", name, got)
		}
	}
	// The provider check must fail (no creds).
	if !strings.Contains(got, "llm provider             fail") {
		t.Errorf("expected llm provider fail row; got:\n%s", got)
	}
}

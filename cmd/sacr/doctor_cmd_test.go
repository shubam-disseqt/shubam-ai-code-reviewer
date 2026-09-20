// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCheckDeepSeek(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		status string
	}{
		{"unset", "", "skip"},
		{"whitespace only", "   ", "skip"},
		{"missing sk- prefix", "abcdefghijklmnopqrstuvwxyz12", "fail"},
		{"too short", "sk-abc", "fail"},
		{"too long", "sk-" + strings.Repeat("x", 200), "fail"},
		{"valid shape", "sk-abcdefghijklmnopqrstuvwxyz1234", "ok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEEPSEEK_API_KEY", tc.key)
			got := checkDeepSeek()
			if got.Status != tc.status {
				t.Fatalf("checkDeepSeek() status = %q, want %q (detail: %s)", got.Status, tc.status, got.Detail)
			}
		})
	}
}

func TestCheckBedrockSkipsWhenNoHints(t *testing.T) {
	// No provider selection, no AWS_* env vars → check must skip so a user
	// who has never touched Bedrock does not see spurious failures.
	for _, k := range []string{
		"SACR_PROVIDER",
		"AWS_REGION", "AWS_PROFILE", "AWS_BEARER_TOKEN_BEDROCK",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
	} {
		t.Setenv(k, "")
	}
	got := checkBedrock(context.Background())
	if got.Status != "skip" {
		t.Fatalf("checkBedrock() status = %q, want skip (detail: %s)", got.Status, got.Detail)
	}
}

func TestCheckBedrockAuthMethodClassification(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
		want string
	}{
		{"bearer wins over profile", map[string]string{"AWS_BEARER_TOKEN_BEDROCK": "tok", "AWS_PROFILE": "prod"}, "bearer token (AWS_BEARER_TOKEN_BEDROCK)"},
		{"static keys next", map[string]string{"AWS_ACCESS_KEY_ID": "AKIA", "AWS_SECRET_ACCESS_KEY": "s", "AWS_PROFILE": "prod"}, "static keys (AWS_ACCESS_KEY_ID)"},
		{"profile next", map[string]string{"AWS_PROFILE": "prod"}, "profile (AWS_PROFILE=prod)"},
		{"ambient fallback", map[string]string{}, "ambient chain (SSO cache / instance role / credential_process)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"AWS_BEARER_TOKEN_BEDROCK", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_PROFILE"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.envs {
				t.Setenv(k, v)
			}
			if got := bedrockAuthMethod(); got != tc.want {
				t.Errorf("bedrockAuthMethod() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDoctorEmptyEnvReportsFailure(t *testing.T) {
	// Clear every env var doctor looks at so the run is deterministic.
	for _, k := range []string{
		"SACR_PROVIDER", "SACR_MODEL", "SACR_DB_URL",
		"SACR_ORG_RULES_REPO", "GITHUB_TOKEN",
		"SACR_LLM_URL", "SACR_LLM_TOKEN", "SACR_LLM_MODEL",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL",
		"DEEPSEEK_API_KEY",
		"AWS_REGION", "AWS_PROFILE", "AWS_BEARER_TOKEN_BEDROCK",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
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

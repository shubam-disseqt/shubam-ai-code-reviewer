// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package session

import (
	"strings"
	"testing"

	"github.com/shubam-disseqt/shubam-ai-code-reviewer/internal/llm"
)

func TestRedact(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantHas    []string // substrings that MUST appear
		wantNotHas []string // substrings that MUST NOT appear (leaked secret)
	}{
		// --- env-style assignments ---
		{
			name:       "aws secret access key",
			in:         `AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`,
			wantHas:    []string{"AWS_SECRET_ACCESS_KEY=[REDACTED]"},
			wantNotHas: []string{"wJalrXUtnFEMI"},
		},
		{
			name:       "aws access key id via prefixed pattern",
			in:         `AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE`,
			wantHas:    []string{"[REDACTED]"},
			wantNotHas: []string{"AKIAIOSFODNN7EXAMPLE"},
		},
		{
			name:       "generic api key",
			in:         `OPENAI_API_KEY="sk-realkeymaterialxxxxxxxxxx"`,
			wantHas:    []string{`OPENAI_API_KEY="[REDACTED]"`},
			wantNotHas: []string{"sk-realkeymaterialxxxxxxxxxx"},
		},
		{
			name:       "bare password key",
			in:         "PASSWORD=hunter2hunter2hunter2",
			wantHas:    []string{"PASSWORD=[REDACTED]"},
			wantNotHas: []string{"hunter2hunter2"},
		},
		{
			name:       "generic token suffix",
			in:         "SLACK_TOKEN='xoxb-real-token-material-here'",
			wantHas:    []string{"[REDACTED]"},
			wantNotHas: []string{"real-token-material-here"},
		},
		{
			name:       "export prefix",
			in:         "export MY_API_KEY=abcdefghijklmnop",
			wantHas:    []string{"MY_API_KEY=[REDACTED]"},
			wantNotHas: []string{"abcdefghijklmnop"},
		},
		// --- prefixed tokens on any line ---
		{
			name:       "anthropic sk-ant token",
			in:         "curl -H 'x-api-key: sk-ant-api03-abcdefghijklmnopqrstuv-xyz'",
			wantHas:    []string{"[REDACTED]"},
			wantNotHas: []string{"sk-ant-api03-abcdefghijklmnopqrstuv"},
		},
		{
			name:       "github personal token",
			in:         "token = ghp_abcdefghijklmnopqrstuvwxyz0123",
			wantHas:    []string{"[REDACTED]"},
			wantNotHas: []string{"ghp_abcdefghijklmnop"},
		},
		{
			name:       "github pat",
			in:         "GITHUB_PAT: github_pat_11ABCDEFG0abcdefghijk_lMNOPQRSTUVWXYZ1234567890abcd",
			wantHas:    []string{"[REDACTED]"},
			wantNotHas: []string{"github_pat_11ABCDEFG0abcdefghijk"},
		},
		{
			name:       "aws akia prefix inside prose",
			in:         "The access key AKIAIOSFODNN7EXAMPLE was leaked.",
			wantHas:    []string{"[REDACTED]"},
			wantNotHas: []string{"AKIAIOSFODNN7EXAMPLE"},
		},
		// --- PEM ---
		{
			name: "pem block",
			in: `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAxyz==
MoreBase64PayloadHere==
-----END RSA PRIVATE KEY-----`,
			wantHas:    []string{"[REDACTED-PEM]", "-----BEGIN RSA PRIVATE KEY-----", "-----END RSA PRIVATE KEY-----"},
			wantNotHas: []string{"MIIEowIBAAKCAQEAxyz", "MoreBase64PayloadHere"},
		},
		// --- JWT ---
		{
			name:       "jwt triple",
			in:         "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4ifQ.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			wantHas:    []string{"[REDACTED-JWT]"},
			wantNotHas: []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		// --- basic-auth URL ---
		{
			name:       "basic auth url",
			in:         "clone https://alice:supersecretvalue@github.com/org/repo.git",
			wantHas:    []string{"https://[REDACTED]@github.com/org/repo.git"},
			wantNotHas: []string{"alice:supersecretvalue"},
		},
		// --- must-not-touch cases ---
		{
			name:    "prose containing word token",
			in:      "The auth token flow uses a bearer token exchanged for a session.",
			wantHas: []string{"The auth token flow uses a bearer token exchanged for a session."},
		},
		{
			name:    "code with SecretKey interface",
			in:      "type SecretKey interface {\n\tSign(msg []byte) []byte\n}",
			wantHas: []string{"type SecretKey interface"},
		},
		{
			name:    "empty json api_key sample",
			in:      `{"api_key": ""}`,
			wantHas: []string{`{"api_key": ""}`},
		},
		{
			name:    "placeholder api_key sample",
			in:      `{"api_key": "..."}`,
			wantHas: []string{`"..."`},
		},
		{
			name:    "ssh style git remote is not basic auth",
			in:      "git@github.com:owner/repo.git",
			wantHas: []string{"git@github.com:owner/repo.git"},
		},
		{
			name:    "short prose sk is not a token",
			in:      "The variable sk is short.",
			wantHas: []string{"The variable sk is short."},
		},
		{
			name:    "placeholder angle-bracket value",
			in:      "MY_API_KEY=<your-key-here>",
			wantHas: []string{"MY_API_KEY=<your-key-here>"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.in)
			for _, sub := range tc.wantHas {
				if !strings.Contains(got, sub) {
					t.Errorf("Redact() missing expected substring %q\ninput:  %q\noutput: %q", sub, tc.in, got)
				}
			}
			for _, sub := range tc.wantNotHas {
				if strings.Contains(got, sub) {
					t.Errorf("Redact() leaked forbidden substring %q\ninput:  %q\noutput: %q", sub, tc.in, got)
				}
			}
		})
	}
}

func TestRedact_EmptyReturnsEmpty(t *testing.T) {
	if got := Redact(""); got != "" {
		t.Fatalf("Redact(\"\") = %q, want empty", got)
	}
}

func TestRedact_IdempotentOnCleanInput(t *testing.T) {
	clean := "This is a plain review comment discussing a bug on line 42."
	if got := Redact(clean); got != clean {
		t.Fatalf("Redact mutated clean input:\n in: %q\nout: %q", clean, got)
	}
}

func TestRedactContent_HandlesBlocksAndUnknown(t *testing.T) {
	// []ContentBlock — Claude multi-part shape. Nested block should also
	// have its Text scrubbed.
	blocks := []llm.ContentBlock{
		{Type: "text", Text: "AWS_SECRET_ACCESS_KEY=realkeyvaluehere"},
		{Type: "tool_result", Content: []llm.ContentBlock{
			{Type: "text", Text: "sk-ant-api03-abcdefghijklmnopqrstuv-xyz"},
		}},
	}
	got := redactContent(blocks).([]llm.ContentBlock)
	if !strings.Contains(got[0].Text, "[REDACTED]") || strings.Contains(got[0].Text, "realkeyvaluehere") {
		t.Fatalf("top-level block not redacted: %+v", got[0])
	}
	if !strings.Contains(got[1].Content[0].Text, "[REDACTED]") ||
		strings.Contains(got[1].Content[0].Text, "sk-ant-api03") {
		t.Fatalf("nested block not redacted: %+v", got[1].Content[0])
	}

	// Unknown type passes through unchanged.
	type weird struct{ V int }
	in := weird{V: 42}
	if out := redactContent(in); out != in {
		t.Fatalf("unknown content type mutated: %v -> %v", in, out)
	}
}

func TestRedactToolCalls_ScrubsArguments(t *testing.T) {
	in := []llm.ToolCall{{
		ID:   "call_1",
		Type: "function",
		Function: llm.FunctionCall{
			Name:      "run_shell",
			Arguments: `{"cmd":"aws configure set aws_access_key_id AKIAIOSFODNN7EXAMPLE"}`,
		},
	}}
	out := redactToolCalls(in)
	if strings.Contains(out[0].Function.Arguments, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("tool call arguments leaked secret: %q", out[0].Function.Arguments)
	}
	if !strings.Contains(out[0].Function.Arguments, "[REDACTED]") {
		t.Fatalf("tool call arguments missing marker: %q", out[0].Function.Arguments)
	}
	// nil in -> nil out.
	if got := redactToolCalls(nil); got != nil {
		t.Fatalf("redactToolCalls(nil) = %v, want nil", got)
	}
}

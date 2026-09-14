// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package zconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		yaml    string  // if empty and !write, file is not created
		write   bool    // create the file (even with empty contents)
		want    Config  // expected config
		wantErr bool
	}{
		{
			name:  "missing file returns defaults",
			write: false,
			want:  Default(),
		},
		{
			name:  "empty file returns defaults",
			yaml:  "",
			write: true,
			want:  Default(),
		},
		{
			name:  "suggestions.enabled=false overrides default",
			yaml:  "suggestions:\n  enabled: false\n",
			write: true,
			want: Config{
				Suggestions: SuggestionsConfig{Enabled: false, Blocking: false},
			},
		},
		{
			name:  "suggestions.blocking=true keeps enabled default",
			yaml:  "suggestions:\n  blocking: true\n",
			write: true,
			want: Config{
				Suggestions: SuggestionsConfig{Enabled: true, Blocking: true},
			},
		},
		{
			name:    "malformed yaml returns defaults and error",
			yaml:    "suggestions: [this is not a map\n",
			write:   true,
			want:    Default(),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := t.TempDir()
			if tc.write {
				dir := filepath.Join(repo, ".zreview")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tc.yaml), 0o644); err != nil {
					t.Fatalf("write: %v", err)
				}
			}

			got, err := Load(repo)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Load() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLoad_EmptyRepoSkipsLookup(t *testing.T) {
	t.Parallel()
	got, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != Default() {
		t.Fatalf("Load(\"\") = %+v, want %+v", got, Default())
	}
}

func TestDefault(t *testing.T) {
	t.Parallel()
	d := Default()
	if !d.Suggestions.Enabled {
		t.Errorf("Default().Suggestions.Enabled = false, want true")
	}
	if d.Suggestions.Blocking {
		t.Errorf("Default().Suggestions.Blocking = true, want false")
	}
}
